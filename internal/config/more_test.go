package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSaveUseTheUserConfigurationDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if cfg, err := Load(); err != nil || cfg.Hotkey.Key != Default().Hotkey.Key {
		t.Fatalf("Load without a file = %+v, %v", cfg, err)
	}
	want := Default()
	want.Autostart = true
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, err := Load(); err != nil || !got.Autostart {
		t.Errorf("Load after Save = %+v, %v", got, err)
	}
}

func TestReadAndWriteFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFrom(dir); err == nil {
		t.Error("a directory loads as a configuration")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(filepath.Join(file, "config.json"), Default()); err == nil {
		t.Error("a configuration was saved inside a file")
	}
}

func TestCombosRefuseUnknownModifiers(t *testing.T) {
	cfg := Default()
	cfg.Hotkey.Modifiers = []string{"Hyper"}
	if _, err := cfg.Combo(); err == nil {
		t.Error("Combo took an unknown modifier")
	}
	if _, err := cfg.SelectionCombo(); err == nil {
		t.Error("SelectionCombo took an unknown modifier")
	}
	cfg = Default()
	cfg.SelectionModifier = "Ctrl+Alt"
	if _, err := cfg.SelectionCombo(); err == nil {
		t.Error("SelectionCombo took two modifiers")
	}
	cfg.SelectionModifier = "Hyper"
	if err := cfg.Validate(); err == nil {
		t.Error("Validate took an unknown selection modifier")
	}
}

func TestNoConfigurationDirectory(t *testing.T) {
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if cfg, err := Load(); err == nil || cfg.Hotkey.Key != Default().Hotkey.Key {
		t.Errorf("Load = %+v, %v; want the defaults and an error", cfg, err)
	}
	if err := Save(Default()); err == nil {
		t.Error("Save found somewhere to write")
	}
}
