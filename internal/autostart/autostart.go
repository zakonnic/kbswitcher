// Package autostart registers kbswitcher to start with the user's session, in
// the one place each platform accepts for that: a launch agent in the user's
// LaunchAgents folder on macOS, the per-user Run key on Windows, a desktop
// entry in the XDG autostart folder on Linux. Nothing is installed
// system-wide and no privileges are needed.
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// Set enables or disables starting with the session.
func Set(enabled bool) error {
	if enabled {
		return Enable()
	}
	return Disable()
}

// executablePath returns the running binary with symlinks resolved.
func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running program: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return path, nil
}
