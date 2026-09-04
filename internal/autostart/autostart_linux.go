package autostart

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

const (
	entryDirPerm  fs.FileMode = 0o700
	entryFilePerm fs.FileMode = 0o644
)

// entryTemplate is the desktop entry XDG sessions read at login. NoDisplay
// keeps it out of the application menu.
const entryTemplate = `[Desktop Entry]
Type=Application
Name=%s
Comment=Convert text typed in the wrong keyboard layout
Exec=%s
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
`

func entryPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the configuration directory: %w", err)
	}
	return filepath.Join(dir, "autostart", buildinfo.Name+".desktop"), nil
}

// Enabled reports whether the desktop entry is in place.
func Enabled() (bool, error) {
	path, err := entryPath()
	if err != nil {
		return false, err
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("cannot read %s: %w", path, err)
	}
}

// Enable writes the desktop entry for the running binary.
func Enable() error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), entryDirPerm); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	entry := fmt.Sprintf(entryTemplate, buildinfo.Name, quoteExec(executable))
	if err := os.WriteFile(path, []byte(entry), entryFilePerm); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// Disable removes the desktop entry. A missing entry is not an error.
func Disable() error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return nil
}

// quoteExec renders a path for the Exec key, whose specification gives a
// meaning to spaces, quotes and backslashes.
func quoteExec(path string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path)
	return `"` + escaped + `"`
}
