package autostart

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

// runKeyPath is the per-user Run key; nothing is written under
// HKEY_LOCAL_MACHINE.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// Enabled reports whether kbswitcher is registered to start with the session.
func Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the autostart key: %w", err)
	}
	defer func() { _ = key.Close() }()

	value, _, err := key.GetStringValue(buildinfo.Name)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot read the autostart entry: %w", err)
	}
	return strings.TrimSpace(value) != "", nil
}

// Enable registers the running binary to start with the session.
func Enable() error {
	path, err := executablePath()
	if err != nil {
		return err
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("cannot open the autostart key: %w", err)
	}
	defer func() { _ = key.Close() }()

	// Quoted, so that a directory containing spaces is not split into a
	// command and its arguments.
	if err := key.SetStringValue(buildinfo.Name, `"`+path+`"`); err != nil {
		return fmt.Errorf("cannot write the autostart entry: %w", err)
	}
	return nil
}

// Disable removes the entry. A missing entry is not an error.
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot open the autostart key: %w", err)
	}
	defer func() { _ = key.Close() }()

	if err := key.DeleteValue(buildinfo.Name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("cannot remove the autostart entry: %w", err)
	}
	return nil
}
