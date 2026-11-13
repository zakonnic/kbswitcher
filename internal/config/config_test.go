package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("the default configuration does not validate: %v", err)
	}
	combo, err := Default().Combo()
	if err != nil {
		t.Fatalf("Combo: %v", err)
	}
	if combo.Key != hotkey.DefaultKey() || combo.Mods != 0 {
		t.Errorf("default combo = %v, want the bare %s", combo, hotkey.DefaultKey())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	want := Config{
		Hotkey:            Hotkey{Key: "Q", Modifiers: []string{"Ctrl"}},
		SelectionModifier: "Alt",
		Autostart:         true,
		// Off, unlike the default, so that the file is proven to carry them.
		SwitchLayout:          false,
		SwitchLayoutSelection: false,
	}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got.Hotkey.Key != want.Hotkey.Key || got.Autostart != want.Autostart ||
		got.SwitchLayout != want.SwitchLayout || got.SwitchLayoutSelection != want.SwitchLayoutSelection ||
		got.SelectionModifier != want.SelectionModifier ||
		len(got.Hotkey.Modifiers) != 1 || got.Hotkey.Modifiers[0] != "Ctrl" {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

func TestLoadFromMissingFileGivesDefaults(t *testing.T) {
	got, err := LoadFrom(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a missing configuration file must not be an error, got %v", err)
	}
	if got.Hotkey.Key != Default().Hotkey.Key || got.Autostart || !got.SwitchLayout || !got.SwitchLayoutSelection {
		t.Errorf("LoadFrom(missing) = %+v, want the defaults", got)
	}
}

func TestLoadFromDamagedFile(t *testing.T) {
	tests := map[string]string{
		"not json":                   "{ this is not json",
		"unknown key":                `{"hotkey": {"key": "NoSuchKey"}}`,
		"a letter alone":             `{"hotkey": {"key": "Q"}}`,
		"selection modifier in use":  `{"hotkey": {"key": "F9", "modifiers": ["Shift"]}}`,
		"selection modifier unknown": `{"selection_modifier": "Fn"}`,
		"selection modifier missing": `{"selection_modifier": ""}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			got, err := LoadFrom(path)
			if err == nil {
				t.Fatal("LoadFrom accepted a damaged configuration")
			}
			if got.Hotkey.Key != Default().Hotkey.Key {
				t.Errorf("LoadFrom returned %+v, want the defaults alongside the error", got)
			}
		})
	}
}

func TestAFileFromAnOlderVersionGetsTheNewDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"hotkey": {"key": "F9", "modifiers": ["Ctrl"]}, "autostart": true, "switch_layout": false}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got.SelectionModifier != "Shift" || !got.SwitchLayoutSelection || got.SwitchLayout {
		t.Errorf("LoadFrom = %+v, want Shift, switching after a selection, and the file's own switch_layout", got)
	}
	selection, err := got.SelectionCombo()
	if err != nil || selection.String() != "Ctrl+Shift+F9" {
		t.Errorf("SelectionCombo = %v, %v; want Ctrl+Shift+F9", selection, err)
	}
}

func TestSaveToRejectsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := SaveTo(path, Config{Hotkey: Hotkey{Key: "NoSuchKey"}})
	if err == nil {
		t.Fatal("SaveTo wrote a configuration that cannot be bound")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("SaveTo left a file behind after refusing to write")
	}
}

func TestPath(t *testing.T) {
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if filepath.Base(path) != "config.json" || !filepath.IsAbs(path) {
		t.Errorf("Path() = %q, want an absolute path ending in config.json", path)
	}
}
