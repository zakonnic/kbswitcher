package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

const (
	agentDirPerm  fs.FileMode = 0o755
	agentFilePerm fs.FileMode = 0o644
)

// agentTemplate asks launchd for one thing: start the program once the user
// logs in. No schedule, no restart on exit, no privileged domain.
const agentTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", buildinfo.ID+".plist"), nil
}

// Enabled reports whether the launch agent is in place.
func Enabled() (bool, error) {
	path, err := agentPath()
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

// Enable writes the launch agent for the running binary. It takes effect at
// the next login; the program is already running when the box is ticked.
func Enable() error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), agentDirPerm); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	plist := fmt.Sprintf(agentTemplate, escapeXML(buildinfo.ID), escapeXML(executable))
	if err := os.WriteFile(path, []byte(plist), agentFilePerm); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// Disable removes the launch agent. A missing agent is not an error.
func Disable() error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return nil
}

func escapeXML(value string) string {
	var out bytes.Buffer
	if err := xml.EscapeText(&out, []byte(value)); err != nil {
		return ""
	}
	return out.String()
}
