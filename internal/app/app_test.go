package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
	"github.com/zakonnic/kbswitcher/internal/switcher"
)

// testHotkey is a combination no keyboard of the usual kind can press, so
// that nothing converts the user's text while it is bound, and that no
// driver holds; the hotkey package's tests bind others at the same time.
var testHotkey = config.Hotkey{Key: "F13", Modifiers: []string{"Ctrl", "Alt"}}

// autostartEntry stands for the user's autostart entry.
type autostartEntry struct {
	on      bool
	setErr  error
	enabled int
}

// sandbox points the configuration at a fresh directory and the autostart
// entry at a, and writes cfg there.
func sandbox(t *testing.T, cfg config.Config, a *autostartEntry) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	path, err := config.Path()
	if err != nil || !strings.HasPrefix(path, dir) {
		t.Fatalf("the configuration is at %s, %v; want it under %s", path, err, dir)
	}
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	savedEnabled, savedSet := autostartEnabled, setAutostart
	t.Cleanup(func() { autostartEnabled, setAutostart = savedEnabled, savedSet })
	autostartEnabled = func() (bool, error) { return a.on, nil }
	setAutostart = func(on bool) error {
		if a.setErr != nil {
			return a.setErr
		}
		if on && !a.on {
			a.enabled++
		}
		a.on = on
		return nil
	}
}

func testConfig() config.Config {
	cfg := config.Default()
	cfg.Hotkey = testHotkey
	return cfg
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestNewStartsAndCloses(t *testing.T) {
	cfg := testConfig()
	cfg.Autostart = true
	entry := &autostartEntry{} // the entry has disappeared
	sandbox(t, cfg, entry)

	a, err := New(discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if entry.enabled != 1 {
		t.Errorf("the missing autostart entry was registered %d times", entry.enabled)
	}
	if a.hotkeys == nil || a.switcher == nil || a.ui == nil {
		t.Errorf("not everything is open: %+v", a)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := (&App{}).Close(); err != nil {
		t.Errorf("closing an application that never opened: %v", err)
	}
}

// opened builds an application whose hotkeys are bound, without a window.
func opened(t *testing.T, entry *autostartEntry) *App {
	t.Helper()
	sandbox(t, testConfig(), entry)
	a := &App{log: discard(), config: testConfig()}
	if err := a.openPlatform(); err != nil {
		t.Fatal(err)
	}
	if err := a.buildSwitcher(); err != nil {
		t.Fatal(err)
	}
	if err := a.bindHotkeys(a.config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestApply(t *testing.T) {
	entry := &autostartEntry{}
	a := opened(t, entry)
	cfg := testConfig()
	cfg.Autostart, cfg.SwitchLayout = true, false
	cfg.Hotkey.Key = "F14"

	if err := a.Apply(cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	saved, err := config.Load()
	if err != nil || saved.Hotkey.Key != "F14" || !saved.Autostart || !entry.on || a.config.Hotkey.Key != "F14" {
		t.Errorf("saved %+v (%v), autostart %v, in force %+v", saved, err, entry.on, a.config)
	}
}

func TestApplyUndoesWhatFailed(t *testing.T) {
	t.Run("a hotkey that cannot be bound", func(t *testing.T) {
		a := opened(t, &autostartEntry{})
		bad := testConfig()
		bad.Hotkey.Modifiers = []string{"Hyper"}
		if err := a.Apply(bad); err == nil {
			t.Fatal("Apply took a hotkey with an unknown modifier")
		}
		if a.hotkeys == nil || a.config.Hotkey.Key != testHotkey.Key {
			t.Error("the hotkeys in force were lost")
		}
	})
	t.Run("an autostart entry that cannot be written", func(t *testing.T) {
		a := opened(t, &autostartEntry{setErr: errors.New("access denied")})
		cfg := testConfig()
		cfg.Autostart = true
		if err := a.Apply(cfg); err == nil || a.config.Autostart {
			t.Errorf("Apply = %v, autostart in force %v", err, a.config.Autostart)
		}
	})
	t.Run("a configuration that cannot be saved", func(t *testing.T) {
		entry := &autostartEntry{}
		a := opened(t, entry)
		path, _ := config.Path()
		// A file where the directory should be.
		if err := os.RemoveAll(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Dir(path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := testConfig()
		cfg.Autostart = true
		if err := a.Apply(cfg); err == nil {
			t.Fatal("Apply could not save and said nothing")
		}
		if entry.on || a.config.Autostart {
			t.Error("the autostart entry was left as the failed settings asked")
		}
	})
}

func TestSuspendHotkeys(t *testing.T) {
	a := opened(t, &autostartEntry{})
	a.suspendHotkeys(true)
	a.suspendHotkeys(true)
	if a.hotkeys != nil || !a.suspended {
		t.Fatal("the hotkeys are still bound")
	}
	a.suspendHotkeys(false)
	if a.hotkeys == nil || a.suspended {
		t.Fatal("the hotkeys were not bound again")
	}
}

func TestRunLogsTheOutcome(t *testing.T) {
	var log strings.Builder
	a := &App{log: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	for _, err := range []error{nil, switcher.ErrBusy, switcher.ErrNothingToConvert, errors.New("broken")} {
		a.run("word", func(context.Context) error { return err })
	}
	for _, want := range []string{"converted", "nothing done", "conversion failed"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the log does not say %q:\n%s", want, log.String())
		}
	}
}

func TestEnsureAutostart(t *testing.T) {
	for _, entry := range []*autostartEntry{{on: true}, {setErr: errors.New("access denied")}} {
		sandbox(t, testConfig(), entry)
		a := &App{log: discard(), config: testConfig()}
		a.config.Autostart = true
		a.ensureAutostart()
		if entry.setErr == nil && (entry.enabled != 0 || len(a.warnings) != 0) {
			t.Errorf("an entry in place was written again: %d times, warnings %q", entry.enabled, a.warnings)
		}
		if entry.setErr != nil && len(a.warnings) != 1 {
			t.Errorf("a failed registration was not reported: %q", a.warnings)
		}
	}
}

func TestBindHotkeysReportsACombinationTaken(t *testing.T) {
	cfg := testConfig()
	selection, _ := cfg.SelectionCombo()
	word, _ := cfg.Combo()
	for _, taken := range []hotkey.Combo{word, selection} {
		other, err := hotkey.Open()
		if err != nil {
			t.Fatal(err)
		}
		if err := other.Register(taken, func() {}); err != nil {
			t.Fatal(err)
		}
		a := &App{log: discard()}
		if err := a.bindHotkeys(cfg); err == nil {
			t.Errorf("%s was bound twice", taken)
		}
		a.config = cfg
		a.config.Hotkey.Modifiers = []string{"Hyper"}
		a.restoreHotkeys() // logs that the old hotkeys cannot come back
		_ = a.Close()
		_ = other.Close()
	}
}
