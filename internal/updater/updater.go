// Package updater checks the project's GitHub releases for a newer version and
// installs it. It is deliberately narrow: HTTPS only, one bounded archive for
// the current OS and architecture, a SHA-256 check against the checksum file
// of the same release, exactly one file extracted by base name, and a rename
// into place. No shell, installer or downloaded code is ever run.
//
// Integrity rests on TLS to the release host plus the published checksum. A
// deployment that needs more should distribute kbswitcher through its own
// device management and leave the update button unused.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

const (
	requestTimeout  = 15 * time.Second
	downloadTimeout = 5 * time.Minute
	// Size limits of what is read from the network.
	maxMetadataBytes = 1 << 20
	maxChecksumBytes = 64 << 10
	maxArchiveBytes  = 128 << 20

	// checksumFileName is the file a release must publish next to its archives.
	checksumFileName = "checksums.txt"
)

const newBinaryPerm fs.FileMode = 0o755

// ErrNoAsset means the release has nothing built for this machine.
var ErrNoAsset = errors.New("this release has no build for this operating system and architecture")

// ErrChecksumMismatch means the download does not match the published checksum
// and was discarded.
var ErrChecksumMismatch = errors.New("the download does not match the published checksum")

// Release is a published version of kbswitcher.
type Release struct {
	// Version is the release tag, for example "v1.2.0".
	Version string
	// URL is the release page.
	URL string
	// Notes is the release description.
	Notes string

	assets []asset
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Updater looks for and installs new releases.
type Updater struct {
	client  *http.Client
	apiBase string
	repo    string
	current string
	target  string
}

// New builds an updater for the repository this binary was built from, ready
// to replace this binary.
func New() (*Updater, error) {
	executable, err := executablePath()
	if err != nil {
		return nil, err
	}

	return &Updater{
		client: &http.Client{
			Timeout: downloadTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("refusing a redirect to %s", req.URL.Scheme)
				}
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		apiBase: buildinfo.APIBaseURL,
		repo:    buildinfo.Repository,
		current: buildinfo.Version,
		target:  executable,
	}, nil
}

// Current returns the version of the running binary.
func (u *Updater) Current() string { return u.current }

// Latest fetches the most recent published release.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimSuffix(u.apiBase, "/"), u.repo)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := u.get(ctx, endpoint, maxMetadataBytes)
	if err != nil {
		return Release{}, err
	}

	var payload struct {
		TagName string  `json:"tag_name"`
		HTMLURL string  `json:"html_url"`
		Body    string  `json:"body"`
		Assets  []asset `json:"assets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("cannot read the release description: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, errors.New("the release has no version tag")
	}

	return Release{
		Version: payload.TagName,
		URL:     payload.HTMLURL,
		Notes:   payload.Body,
		assets:  payload.Assets,
	}, nil
}

// IsNewer reports whether the release is more recent than the running binary.
// A "dev" build is always considered older.
func (u *Updater) IsNewer(release Release) bool {
	return compareVersions(u.current, release.Version) < 0
}

// Update downloads the release, verifies it and replaces the running binary.
// It returns the path of the installed program.
func (u *Updater) Update(ctx context.Context, release Release) (string, error) {
	archive, err := release.assetForThisMachine()
	if err != nil {
		return "", err
	}
	checksums, err := release.assetNamed(checksumFileName)
	if err != nil {
		return "", fmt.Errorf("%w: kbswitcher only installs releases that publish %s",
			err, checksumFileName)
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	checksumFile, err := u.get(ctx, checksums.URL, maxChecksumBytes)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(string(checksumFile), archive.Name)
	if err != nil {
		return "", err
	}

	payload, err := u.get(ctx, archive.URL, maxArchiveBytes)
	if err != nil {
		return "", err
	}
	if got := sha256.Sum256(payload); hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%w: %s", ErrChecksumMismatch, archive.Name)
	}

	binary, err := extractBinary(payload, archive.Name)
	if err != nil {
		return "", err
	}
	if err := u.install(binary); err != nil {
		return "", err
	}
	return u.target, nil
}

// Relaunch starts a fresh copy of the program and returns without waiting for
// it. The caller is expected to have shut down already, so that the new copy
// finds the hotkeys and the instance lock free.
func Relaunch() error {
	executable, err := executablePath()
	if err != nil {
		return err
	}
	command := exec.Command(executable) //nolint:gosec // the path comes from os.Executable, never from input
	if err := command.Start(); err != nil {
		return fmt.Errorf("cannot start the updated program: %w", err)
	}
	return command.Process.Release()
}

// get fetches a URL over HTTPS and returns at most limit bytes.
func (u *Updater) get(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("malformed address %q: %w", endpoint, err)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("refusing to fetch %q: only HTTPS is allowed", endpoint)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", buildinfo.Name+"/"+buildinfo.Version)

	response, err := u.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", parsed.Host, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", parsed.Host, response.Status)
	}

	// One byte over the limit tells an oversized body from one that fits.
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the answer from %s: %w", parsed.Host, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("the answer from %s is larger than the %d byte limit", parsed.Host, limit)
	}
	return body, nil
}

// install writes the new binary next to the current one and renames it into
// place.
func (u *Updater) install(binary []byte) error {
	directory := filepath.Dir(u.target)

	temp, err := os.CreateTemp(directory, ".kbswitcher-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", directory, err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(binary); err != nil {
		_ = temp.Close()
		return fmt.Errorf("cannot write %s: %w", tempName, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("cannot flush %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", tempName, err)
	}
	if err := os.Chmod(tempName, newBinaryPerm); err != nil {
		return fmt.Errorf("cannot make %s executable: %w", tempName, err)
	}

	// Windows refuses to overwrite a running program but allows renaming it;
	// the displaced file is removed at the next start.
	previous := u.target + ".old"
	_ = os.Remove(previous)
	if runtime.GOOS == "windows" {
		if err := os.Rename(u.target, previous); err != nil {
			return fmt.Errorf("cannot move the running program aside: %w", err)
		}
	}
	if err := os.Rename(tempName, u.target); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Rename(previous, u.target)
		}
		return fmt.Errorf("cannot install the new program: %w", err)
	}
	return nil
}

// CleanUp removes the file a previous update on Windows left behind.
func CleanUp() {
	if executable, err := executablePath(); err == nil {
		_ = os.Remove(executable + ".old")
	}
}

// executablePath returns the running binary with symlinks resolved.
func executablePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running program: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		return resolved, nil
	}
	return executable, nil
}

// assetForThisMachine picks the archive built for the current OS and
// architecture.
func (r Release) assetForThisMachine() (asset, error) {
	for _, candidate := range r.assets {
		name := strings.ToLower(candidate.Name)
		if !isArchive(name) {
			continue
		}
		if matchesToken(name, osAliases[runtime.GOOS]) && matchesToken(name, archAliases[runtime.GOARCH]) {
			return candidate, nil
		}
	}
	return asset{}, fmt.Errorf("%w: %s/%s", ErrNoAsset, runtime.GOOS, runtime.GOARCH)
}

func (r Release) assetNamed(name string) (asset, error) {
	for _, candidate := range r.assets {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return asset{}, fmt.Errorf("the release does not publish %s", name)
}

// Spellings release builders commonly use in archive names.
var (
	osAliases = map[string][]string{
		"darwin":  {"darwin", "macos", "mac"},
		"windows": {"windows", "win"},
		"linux":   {"linux"},
	}
	archAliases = map[string][]string{
		"amd64": {"amd64", "x86_64", "x64"},
		"arm64": {"arm64", "aarch64"},
	}
)

// matchesToken reports whether one of the aliases occurs in name with no
// letter on either side: "win" does not match "darwin" but matches "win64".
func matchesToken(name string, aliases []string) bool {
	for _, alias := range aliases {
		for from := 0; ; {
			i := strings.Index(name[from:], alias)
			if i < 0 {
				break
			}
			start, end := from+i, from+i+len(alias)
			if (start == 0 || !isLetter(name[start-1])) && (end == len(name) || !isLetter(name[end])) {
				return true
			}
			from = start + 1
		}
	}
	return false
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' }

func isArchive(name string) bool {
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")
}

// checksumFor returns the SHA-256 recorded for a file in a checksum file in
// sha256sum format: "<hex>  <name>", one per line.
func checksumFor(checksums, name string) (string, error) {
	for line := range strings.Lines(checksums) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum, file := fields[0], strings.TrimPrefix(fields[1], "*")
		if file != name {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil || len(sum) != sha256.Size*2 {
			return "", fmt.Errorf("the checksum published for %s is not a SHA-256 digest", name)
		}
		return strings.ToLower(sum), nil
	}
	return "", fmt.Errorf("no checksum is published for %s", name)
}

// extractBinary pulls the program out of a downloaded archive. Only one file
// is taken, matched by base name, so nothing in the archive can decide where
// anything lands.
func extractBinary(payload []byte, archiveName string) ([]byte, error) {
	wanted := buildinfo.Name
	if runtime.GOOS == "windows" {
		wanted += ".exe"
	}

	if strings.HasSuffix(strings.ToLower(archiveName), ".zip") {
		return extractFromZip(payload, wanted)
	}
	return extractFromTarGz(payload, wanted)
}

func extractFromZip(payload []byte, wanted string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("the downloaded archive is damaged: %w", err)
	}
	for _, file := range reader.File {
		if path.Base(filepath.ToSlash(file.Name)) != wanted {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("cannot read %s from the archive: %w", file.Name, err)
		}
		defer func() { _ = opened.Close() }()
		return readCapped(opened, file.Name)
	}
	return nil, fmt.Errorf("the archive does not contain %s", wanted)
}

func extractFromTarGz(payload []byte, wanted string) ([]byte, error) {
	uncompressed, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("the downloaded archive is damaged: %w", err)
	}
	defer func() { _ = uncompressed.Close() }()

	archive := tar.NewReader(uncompressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the downloaded archive is damaged: %w", err)
		}
		if header.Typeflag != tar.TypeReg || path.Base(filepath.ToSlash(header.Name)) != wanted {
			continue
		}
		return readCapped(archive, header.Name)
	}
	return nil, fmt.Errorf("the archive does not contain %s", wanted)
}

// readCapped reads a file out of an archive, refusing anything oversized.
func readCapped(reader io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s from the archive: %w", name, err)
	}
	if int64(len(data)) > maxArchiveBytes {
		return nil, fmt.Errorf("%s is larger than the %d byte limit", name, maxArchiveBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	return data, nil
}

// compareVersions orders two tags such as "v1.2.0": negative when a is older
// than b, zero when equal, positive when newer. Fields are compared as
// numbers; a pre-release suffix ("-rc1") sorts before the bare tag. Anything
// unparsable, "dev" included, is the oldest possible version.
func compareVersions(a, b string) int {
	aFields, aSuffix := splitVersion(a)
	bFields, bSuffix := splitVersion(b)

	for i := range max(len(aFields), len(bFields)) {
		var left, right int
		if i < len(aFields) {
			left = aFields[i]
		}
		if i < len(bFields) {
			right = bFields[i]
		}
		if left != right {
			return left - right
		}
	}

	switch {
	case aSuffix == bSuffix:
		return 0
	case aSuffix == "":
		return 1
	case bSuffix == "":
		return -1
	default:
		return strings.Compare(aSuffix, bSuffix)
	}
}

// splitVersion breaks "v1.2.3-rc1" into its numbers and its suffix. An
// unparsable version yields no numbers.
func splitVersion(version string) ([]int, string) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")

	suffix := ""
	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version, suffix = version[:cut], version[cut:]
	}

	var numbers []int
	for _, field := range strings.Split(version, ".") {
		number, err := strconv.Atoi(field)
		if err != nil {
			return nil, suffix
		}
		numbers = append(numbers, number)
	}
	return numbers, suffix
}
