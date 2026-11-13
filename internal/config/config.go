// Package config stores the settings the user can change: the hotkey and the
// modifier that turns it to the selection, whether kbswitcher starts with the
// session, and whether converting also switches the keyboard layout. The file
// is small JSON in the per-user configuration directory, written with
// owner-only permissions.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
	// maxFileSize bounds what is read, so a damaged file cannot exhaust memory.
	maxFileSize = 64 << 10
)

// Hotkey is the configured key combination, which converts the last word.
type Hotkey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers"`
}

// Config is the whole configuration file.
type Config struct {
	Hotkey Hotkey `json:"hotkey"`
	// SelectionModifier, held with the hotkey, converts the selection instead
	// of the last word.
	SelectionModifier string `json:"selection_modifier"`
	Autostart         bool   `json:"autostart"`
	// SwitchLayout makes converting the last word also switch the keyboard to
	// the layout the word now belongs to; SwitchLayoutSelection does the same
	// for the selection.
	SwitchLayout          bool `json:"switch_layout"`
	SwitchLayoutSelection bool `json:"switch_layout_selection"`
}

// Default returns the configuration of a fresh installation.
func Default() Config {
	return Config{
		Hotkey:                Hotkey{Key: hotkey.DefaultKey(), Modifiers: []string{}},
		SelectionModifier:     "Shift",
		Autostart:             false,
		SwitchLayout:          true,
		SwitchLayoutSelection: true,
	}
}

// Combo turns the configured hotkey into the combination the platform binds.
func (c Config) Combo() (hotkey.Combo, error) {
	mods, err := hotkey.ParseModifiers(c.Hotkey.Modifiers)
	if err != nil {
		return hotkey.Combo{}, err
	}
	return hotkey.Combo{Key: c.Hotkey.Key, Mods: mods}, nil
}

// SelectionCombo is the hotkey with the selection modifier added.
func (c Config) SelectionCombo() (hotkey.Combo, error) {
	combo, err := c.Combo()
	if err != nil {
		return hotkey.Combo{}, err
	}
	mod, err := c.selectionModifier()
	if err != nil {
		return hotkey.Combo{}, err
	}
	return combo.With(mod), nil
}

func (c Config) selectionModifier() (hotkey.Modifier, error) {
	mod, err := hotkey.ParseModifiers([]string{c.SelectionModifier})
	if err != nil {
		return 0, err
	}
	if len(mod.Names()) != 1 {
		return 0, errors.New("the selection modifier must be one of Ctrl, Alt, Shift or the system key")
	}
	return mod, nil
}

// Validate reports what is wrong with the configuration, if anything.
func (c Config) Validate() error {
	combo, err := c.Combo()
	if err != nil {
		return err
	}
	if err := combo.Check(); err != nil {
		return err
	}
	mod, err := c.selectionModifier()
	if err != nil {
		return err
	}
	if combo.Mods.Has(mod) {
		return fmt.Errorf("the hotkey already holds %s, the key that converts the selection", hotkey.Label(mod.Names()[0]))
	}
	return combo.With(mod).Check()
}

// Path returns the location of the configuration file.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the user configuration directory: %w", err)
	}
	return filepath.Join(dir, buildinfo.Name, "config.json"), nil
}

// Load reads the configuration from its usual location. A missing file yields
// the defaults without an error.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return LoadFrom(path)
}

// LoadFrom reads the configuration from an explicit path. A damaged file
// returns the defaults together with the error, so the caller can tell the
// user and still run.
func LoadFrom(path string) (Config, error) {
	file, err := os.Open(path) //nolint:gosec // constant path under the user configuration directory
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxFileSize))
	if err != nil {
		return Default(), fmt.Errorf("cannot read %s: %w", path, err)
	}

	// Starting from the defaults keeps a file written by an older version
	// complete.
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the configuration to its usual location.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveTo(path, cfg)
}

// SaveTo writes the configuration to an explicit path, through a temporary
// file and a rename, so an interrupted write cannot leave a half-written file.
func SaveTo(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the configuration: %w", err)
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file next to %s: %w", path, err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if err := temp.Chmod(filePerm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("cannot restrict access to %s: %w", tempName, err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("cannot write %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", tempName, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	return nil
}
