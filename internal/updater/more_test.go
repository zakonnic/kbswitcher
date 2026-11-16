package updater

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

func TestNewTargetsTheRunningProgram(t *testing.T) {
	u, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	exe, _ := executablePath()
	if u.target != exe || u.Current() != buildinfo.Version {
		t.Errorf("target %q, version %q", u.target, u.Current())
	}
	plain := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.invalid"}}
	secure := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.invalid"}}
	if err := u.client.CheckRedirect(plain, nil); err == nil {
		t.Error("a redirect to plain HTTP is followed")
	}
	if err := u.client.CheckRedirect(secure, make([]*http.Request, 10)); err == nil {
		t.Error("an endless chain of redirects is followed")
	}
	if err := u.client.CheckRedirect(secure, nil); err != nil {
		t.Errorf("an HTTPS redirect is refused: %v", err)
	}
}

func TestCleanUpRemovesTheProgramAnUpdateMovedAside(t *testing.T) {
	exe, err := executablePath()
	if err != nil {
		t.Fatal(err)
	}
	old := exe + ".old"
	if err := os.WriteFile(old, []byte("the old program"), 0o600); err != nil {
		t.Skipf("cannot write next to the test binary: %v", err)
	}
	CleanUp()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		_ = os.Remove(old)
		t.Errorf("%s is still there: %v", old, err)
	}
}

func TestLatestRefusesBadAnswers(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"not JSON", "{", http.StatusOK, "cannot read the release description"},
		{"no tag", `{"tag_name": ""}`, http.StatusOK, "no version tag"},
		{"an error status", "", http.StatusNotFound, "404"},
		{"too long", `{"tag_name": "v1", "body": "` + strings.Repeat("x", maxMetadataBytes) + `"}`, http.StatusOK, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			u := testUpdater(t, server.URL, "")
			u.client = server.Client()
			if _, err := u.Latest(t.Context()); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Latest = %v, want an error about %q", err, tt.want)
			}
		})
	}
	u := testUpdater(t, "https://exa mple.invalid", "")
	if _, err := u.Latest(t.Context()); err == nil {
		t.Error("a malformed address is fetched")
	}
	u = testUpdater(t, "https://127.0.0.1:1", "")
	if _, err := u.Latest(t.Context()); err == nil || !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("an unreachable server: %v", err)
	}
}

func TestUpdateFailsWithoutReplacingTheProgram(t *testing.T) {
	archive := archiveForThisMachine(t, "the new program")
	server, _ := releaseServer(t, "v9.9.9", archive, sha256Hex(archive))
	defer server.Close()

	// The program's directory is a file: nothing can be written next to it.
	dir := filepath.Join(t.TempDir(), "not a directory")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	u := testUpdater(t, server.URL, filepath.Join(dir, binaryName()))
	u.client = server.Client()
	release, err := u.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Update(t.Context(), release); err == nil {
		t.Error("Update wrote a program where it cannot")
	}
}

func TestExtractRefusesDamagedArchives(t *testing.T) {
	if _, err := extractFromZip([]byte("not a zip"), binaryName()); err == nil {
		t.Error("a damaged zip is read")
	}
	if _, err := extractFromZip(zipArchive(t, "README.md", "text"), binaryName()); err == nil {
		t.Error("a zip without the program is read")
	}
	if _, err := extractFromTarGz(tarGzArchive(t, binaryName(), ""), binaryName()); err == nil {
		t.Error("an empty program is accepted")
	}
	if _, err := extractFromZip(zipArchive(t, binaryName(), ""), binaryName()); err == nil {
		t.Error("an empty program is accepted from a zip")
	}
}

func TestInstallLeavesNoProgramWhereThereWasNone(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows moves the running program aside before installing")
	}
	u := testUpdater(t, "", filepath.Join(t.TempDir(), binaryName())) // nothing to move aside
	if err := u.install([]byte("the new program")); err == nil {
		t.Error("install put a program in place of one that is not there")
	}
}
