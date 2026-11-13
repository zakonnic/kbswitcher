package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // negative: a is older, zero: same, positive: a is newer
	}{
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.9.0", "v1.10.0", -1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.0", "v1.0.0", 0},
		{"v1.0.0-rc1", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc1", 1},
		{"v1.0.0-rc1", "v1.0.0-rc2", -1},
		{"dev", "v0.0.1", -1},
		{"dev", "dev", 0},
	}
	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if (got < 0) != (tt.want < 0) || (got > 0) != (tt.want > 0) {
				t.Errorf("compareVersions(%q, %q) = %d, want the sign of %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestChecksumFor(t *testing.T) {
	const digest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	checksums := "  \n" +
		digest + "  kbswitcher_1.0.0_linux_amd64.tar.gz\n" +
		digest + " *kbswitcher_1.0.0_windows_amd64.zip\n" +
		"not-a-digest  broken.zip\n"

	t.Run("plain entry", func(t *testing.T) {
		got, err := checksumFor(checksums, "kbswitcher_1.0.0_linux_amd64.tar.gz")
		if err != nil || got != digest {
			t.Errorf("checksumFor = %q, %v; want %q", got, err, digest)
		}
	})
	t.Run("binary marker is ignored", func(t *testing.T) {
		got, err := checksumFor(checksums, "kbswitcher_1.0.0_windows_amd64.zip")
		if err != nil || got != digest {
			t.Errorf("checksumFor = %q, %v; want %q", got, err, digest)
		}
	})
	t.Run("malformed digest", func(t *testing.T) {
		if _, err := checksumFor(checksums, "broken.zip"); err == nil {
			t.Error("checksumFor accepted a digest that is not SHA-256")
		}
	})
	t.Run("missing entry", func(t *testing.T) {
		if _, err := checksumFor(checksums, "absent.zip"); err == nil {
			t.Error("checksumFor invented a checksum for a file that is not listed")
		}
	})
}

func TestAssetForThisMachine(t *testing.T) {
	release := Release{assets: []asset{
		{Name: "checksums.txt"},
		{Name: "kbswitcher_1.0.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"},
		{Name: "kbswitcher_1.0.0_plan9_mips.tar.gz"},
	}}
	got, err := release.assetForThisMachine()
	if err != nil {
		t.Fatalf("assetForThisMachine: %v", err)
	}
	if !strings.Contains(got.Name, runtime.GOOS) {
		t.Errorf("picked %q, want the build for %s", got.Name, runtime.GOOS)
	}

	empty := Release{assets: []asset{{Name: "checksums.txt"}}}
	if _, err := empty.assetForThisMachine(); !errors.Is(err, ErrNoAsset) {
		t.Errorf("assetForThisMachine on a release without builds = %v, want ErrNoAsset", err)
	}
}

func TestMatchesToken(t *testing.T) {
	tests := []struct {
		name    string
		aliases []string
		want    bool
	}{
		{"kbswitcher_v1.0.0_windows_amd64.zip", osAliases["windows"], true},
		{"kbswitcher_v1.0.0_darwin_amd64.tar.gz", osAliases["windows"], false},
		{"kbswitcher-win-x64.zip", osAliases["windows"], true},
		{"kbswitcher-win64.zip", osAliases["windows"], true},
		{"kbswitcher_windows64.zip", osAliases["windows"], true},
		{"win.zip", osAliases["windows"], true},
		{"kbswitcher-win", osAliases["windows"], true},
		{"swin_win.zip", osAliases["windows"], true},
		{"swin_twins.zip", osAliases["windows"], false},
		{"kbswitcher_v1.0.0_darwin_arm64.tar.gz", osAliases["darwin"], true},
		{"kbswitcher-macos.tar.gz", osAliases["darwin"], true},
		{"kbswitcher_linux_x86_64.tar.gz", archAliases["amd64"], true},
		{"kbswitcher_linux_arm64.tar.gz", archAliases["amd64"], false},
	}
	for _, tt := range tests {
		if got := matchesToken(tt.name, tt.aliases); got != tt.want {
			t.Errorf("matchesToken(%q, %v) = %v, want %v", tt.name, tt.aliases, got, tt.want)
		}
	}
}

func TestExtractBinary(t *testing.T) {
	const content = "the new program"

	t.Run("tar.gz", func(t *testing.T) {
		got, err := extractBinary(tarGzArchive(t, binaryName(), content), "release.tar.gz")
		if err != nil || string(got) != content {
			t.Errorf("extractBinary = %q, %v; want %q", got, err, content)
		}
	})
	t.Run("zip", func(t *testing.T) {
		got, err := extractBinary(zipArchive(t, binaryName(), content), "release.zip")
		if err != nil || string(got) != content {
			t.Errorf("extractBinary = %q, %v; want %q", got, err, content)
		}
	})
	t.Run("the path inside the archive is ignored", func(t *testing.T) {
		// An entry that tries to escape is matched on its base name only, and
		// what is extracted is written to a destination this package chooses.
		got, err := extractBinary(tarGzArchive(t, "../../"+binaryName(), content), "release.tar.gz")
		if err != nil || string(got) != content {
			t.Errorf("extractBinary = %q, %v; want %q", got, err, content)
		}
	})
	t.Run("archive without the program", func(t *testing.T) {
		if _, err := extractBinary(tarGzArchive(t, "README.md", content), "release.tar.gz"); err == nil {
			t.Error("extractBinary accepted an archive that does not contain the program")
		}
	})
	t.Run("damaged archive", func(t *testing.T) {
		if _, err := extractBinary([]byte("not an archive"), "release.tar.gz"); err == nil {
			t.Error("extractBinary accepted a damaged archive")
		}
	})
}

func TestLatestRejectsPlainHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v9.9.9"}`)
	}))
	defer server.Close()

	u := testUpdater(t, server.URL, "")
	u.client = server.Client()

	if _, err := u.Latest(t.Context()); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("Latest over plain HTTP = %v, want a refusal mentioning HTTPS", err)
	}
}

// TestUpdateInstallsAVerifiedRelease walks the whole update: read the release,
// fetch the checksums, download the archive, verify it and replace the binary.
func TestUpdateInstallsAVerifiedRelease(t *testing.T) {
	const newContent = "the new program"
	archive := archiveForThisMachine(t, newContent)
	server, assetName := releaseServer(t, "v9.9.9", archive, sha256Hex(archive))
	defer server.Close()

	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("the old program"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	u := testUpdater(t, server.URL, target)
	u.client = server.Client()

	release, err := u.Latest(t.Context())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if release.Version != "v9.9.9" || !u.IsNewer(release) {
		t.Fatalf("Latest returned %+v, want a newer release", release)
	}
	if _, err := release.assetNamed(assetName); err != nil {
		t.Fatalf("the release does not list %q: %v", assetName, err)
	}

	installed, err := u.Update(t.Context(), release)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if installed != target {
		t.Errorf("Update installed to %q, want %q", installed, target)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != newContent {
		t.Errorf("the installed program is %q, want %q", got, newContent)
	}
}

func TestUpdateRefusesAWrongChecksum(t *testing.T) {
	archive := archiveForThisMachine(t, "the new program")
	server, _ := releaseServer(t, "v9.9.9", archive, strings.Repeat("00", sha256.Size))
	defer server.Close()

	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("the old program"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	u := testUpdater(t, server.URL, target)
	u.client = server.Client()

	release, err := u.Latest(t.Context())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if _, err := u.Update(t.Context(), release); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Update = %v, want ErrChecksumMismatch", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "the old program" {
		t.Errorf("the program was replaced despite the mismatch: %q", got)
	}
}

func TestUpdateNeedsAChecksumFile(t *testing.T) {
	archive := archiveForThisMachine(t, "the new program")
	server, assetName := releaseServer(t, "v9.9.9", archive, "")
	defer server.Close()

	u := testUpdater(t, server.URL, filepath.Join(t.TempDir(), binaryName()))
	u.client = server.Client()

	release, err := u.Latest(t.Context())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	// Drop the checksum file the way a release published without one would.
	release.assets = []asset{{Name: assetName, URL: server.URL + "/" + assetName}}

	if _, err := u.Update(t.Context(), release); err == nil ||
		!strings.Contains(err.Error(), checksumFileName) {
		t.Errorf("Update = %v, want a refusal naming %s", err, checksumFileName)
	}
}

// Helpers.

func testUpdater(t *testing.T, apiBase, target string) *Updater {
	t.Helper()
	return &Updater{
		client:  http.DefaultClient,
		apiBase: apiBase,
		repo:    "zakonnic/kbswitcher",
		current: "v1.0.0",
		target:  target,
	}
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "kbswitcher.exe"
	}
	return "kbswitcher"
}

func assetNameForThisMachine() string {
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("kbswitcher_9.9.9_%s_%s%s", runtime.GOOS, runtime.GOARCH, extension)
}

func archiveForThisMachine(t *testing.T, content string) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		return zipArchive(t, binaryName(), content)
	}
	return tarGzArchive(t, binaryName(), content)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseServer serves one release: its description, its archive and its
// checksum file. An empty digest leaves the checksum file out.
func releaseServer(t *testing.T, version string, archive []byte, digest string) (*httptest.Server, string) {
	t.Helper()

	assetName := assetNameForThisMachine()
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)

	assets := fmt.Sprintf(`{"name": %q, "browser_download_url": "%s/%s", "size": %d}`,
		assetName, server.URL, assetName, len(archive))
	if digest != "" {
		assets += fmt.Sprintf(`, {"name": %q, "browser_download_url": "%s/%s", "size": 0}`,
			checksumFileName, server.URL, checksumFileName)
	}

	mux.HandleFunc("/repos/zakonnic/kbswitcher/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": %q, "html_url": "https://example.invalid/release", "body": "notes", "assets": [%s]}`,
			version, assets)
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/"+checksumFileName, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", digest, assetName)
	})
	return server, assetName
}

func tarGzArchive(t *testing.T, name, content string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	compressor := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressor)

	header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := archive.Write([]byte(content)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buffer.Bytes()
}

func zipArchive(t *testing.T, name, content string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)

	entry, err := archive.Create(name)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buffer.Bytes()
}
