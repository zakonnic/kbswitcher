// Package app wires kbswitcher together: it opens the platform pieces, reads
// the keyboard layouts once, binds the hotkeys and hands everything to the
// tray interface.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zakonnic/kbswitcher/internal/autostart"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
	"github.com/zakonnic/kbswitcher/internal/platform/console"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
	"github.com/zakonnic/kbswitcher/internal/switcher"
	"github.com/zakonnic/kbswitcher/internal/ui"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

// The autostart entry, through variables that tests point elsewhere: the
// real one lives in the user's registry or home.
var (
	autostartEnabled = autostart.Enabled
	setAutostart     = autostart.Set
)

// conversionTimeout bounds one conversion: a few key presses and a clipboard
// round trip. Anything slower means the focused application stopped answering.
const conversionTimeout = 5 * time.Second

// App is the running application.
type App struct {
	log       *slog.Logger
	config    config.Config
	layouts   *layouts.Reader
	keyboard  *input.Keyboard
	clipboard *clip.Clipboard
	hotkeys   *hotkey.Manager
	switcher  *switcher.Switcher
	ui        *ui.App

	// suspended is set while the settings window listens for a new hotkey.
	suspended bool

	// warnings are the start-up problems shown to the user in About.
	warnings []string
}

// New opens everything the application needs. The caller must Close it.
func New(log *slog.Logger) (*App, error) {
	a := &App{log: log}

	cfg, err := config.Load()
	if err != nil {
		a.warn("The configuration could not be read and the defaults are in use: %v", err)
	}
	a.config = cfg
	a.ensureAutostart()

	if err := a.openPlatform(); err != nil {
		_ = a.Close()
		return nil, err
	}
	if err := a.buildSwitcher(); err != nil {
		_ = a.Close()
		return nil, err
	}
	if err := a.bindHotkeys(a.config); err != nil {
		// Useless without hotkeys, but About can still tell the user why.
		a.warn("The hotkey could not be bound: %v", err)
	}

	options := ui.Options{
		Log:     log,
		Layouts: a.switcher.Layouts(),
		Config:  a.config,
		Apply:   a.Apply,
		Suspend: a.suspendHotkeys,
	}
	// A nil *Updater in the interface would not read as nil.
	if release, err := updater.New(); err != nil {
		a.warn("Updates are unavailable: %v", err)
	} else {
		options.Updater = release
	}
	options.Warnings = a.warnings
	a.ui = ui.New(options)
	return a, nil
}

// Run shows the tray icon and blocks until the user quits. It must be called
// from the main goroutine. It reports whether the user asked for a restart
// after an update.
func (a *App) Run() (restart bool) { return a.ui.Run() }

// Close releases the platform resources. It is safe to call on a partly built
// application.
func (a *App) Close() error {
	var errs []error
	if a.hotkeys != nil {
		errs = append(errs, a.hotkeys.Close())
	}
	if a.switcher != nil {
		a.switcher.Idle() // the last conversion ends, and the clipboard goes back to the user
	}
	if a.keyboard != nil {
		errs = append(errs, a.keyboard.Close())
	}
	if a.clipboard != nil {
		errs = append(errs, a.clipboard.Close())
	}
	if a.layouts != nil {
		errs = append(errs, a.layouts.Close())
	}
	return errors.Join(errs...)
}

// Apply puts a new configuration in force: hotkeys, autostart entry, file,
// layout switching. When a step fails the earlier ones are undone, so that what
// is running always matches what the settings window shows.
func (a *App) Apply(cfg config.Config) error {
	if err := a.bindHotkeys(cfg); err != nil {
		a.restoreHotkeys()
		return err
	}
	if cfg.Autostart != a.config.Autostart {
		if err := setAutostart(cfg.Autostart); err != nil {
			a.restoreHotkeys()
			return err
		}
	}
	if err := config.Save(cfg); err != nil {
		a.restoreHotkeys()
		if undoErr := setAutostart(a.config.Autostart); undoErr != nil {
			a.log.Error("could not restore the autostart entry", "error", undoErr)
		}
		return err
	}

	a.switcher.SetSwitchLayout(cfg.SwitchLayout, cfg.SwitchLayoutSelection)
	a.config = cfg
	a.log.Info("settings applied", "hotkey", cfg.Hotkey.Key, "selection_modifier", cfg.SelectionModifier,
		"autostart", cfg.Autostart, "switch_layout", cfg.SwitchLayout, "switch_layout_selection", cfg.SwitchLayoutSelection)
	return nil
}

// suspendHotkeys releases the bound combinations so that their keys reach the
// settings window, and binds them again when it is done.
func (a *App) suspendHotkeys(suspend bool) {
	if suspend == a.suspended {
		return
	}
	if !suspend {
		a.restoreHotkeys()
		return
	}
	if a.hotkeys != nil {
		if err := a.hotkeys.Close(); err != nil {
			a.log.Warn("could not release the hotkeys", "error", err)
		}
		a.hotkeys = nil
	}
	a.suspended = true
}

func (a *App) restoreHotkeys() {
	if err := a.bindHotkeys(a.config); err != nil {
		a.log.Error("could not restore the previous hotkeys", "error", err)
	}
}

// ensureAutostart re-creates the autostart entry when the configuration asks
// for one that has disappeared, for example after the profile was restored
// from a backup.
func (a *App) ensureAutostart() {
	if !a.config.Autostart {
		return
	}
	if enabled, err := autostartEnabled(); err == nil && enabled {
		return
	}
	if err := setAutostart(true); err != nil {
		a.warn("Autostart is enabled in the settings but could not be registered: %v", err)
	}
}

func (a *App) openPlatform() error {
	var err error
	if a.layouts, err = layouts.Open(); err != nil {
		return fmt.Errorf("cannot read the keyboard layouts: %w", err)
	}
	if a.keyboard, err = input.Open(); err != nil {
		return fmt.Errorf("cannot send key presses: %w", err)
	}
	if a.clipboard, err = clip.Open(); err != nil {
		return fmt.Errorf("cannot use the clipboard: %w", err)
	}
	if err := input.Permission(true); err != nil {
		a.warn("%v", err)
	}
	return nil
}

// buildSwitcher reads the installed layouts once; they do not change while a
// session runs.
func (a *App) buildSwitcher() error {
	installed, err := a.layouts.List()
	if err != nil {
		a.warn("The installed layouts could not be read (%v); the built-in English and Russian tables are used instead.", err)
		installed = keymap.Fallback()
	}
	if len(installed) < 2 {
		a.warn("Only one keyboard layout is installed; the built-in English and Russian tables are used instead.")
		installed = keymap.Fallback()
	}
	for i, layout := range installed {
		a.log.Info("layout", "position", i+1, "id", layout.ID(), "name", layout.Name(),
			"characters", layout.Size())
	}

	deps := switcher.Deps{
		Layouts:   a.layouts,
		Keyboard:  a.keyboard,
		Clipboard: a.clipboard,
		Terminal:  terminal{a.keyboard},
		Field:     newField(a.keyboard),
	}
	a.switcher, err = switcher.New(installed, deps, switcher.DefaultOptions(), a.log)
	if err != nil {
		return err
	}
	a.switcher.SetSwitchLayout(a.config.SwitchLayout, a.config.SwitchLayoutSelection)
	return nil
}

// terminal hands the switcher the console behind the focused window. It is a
// separate type only so that the switcher stays free of platform code.
type terminal struct{ keyboard *input.Keyboard }

func (t terminal) Open(selection bool) (switcher.Console, error) {
	session, err := console.Open(t.keyboard.Nudge, selection)
	switch {
	case errors.Is(err, console.ErrShells):
		return nil, fmt.Errorf("%w: %w: %w", switcher.ErrNoConsole, switcher.ErrShellsNearby, err)
	case errors.Is(err, console.ErrNotConsole):
		return nil, fmt.Errorf("%w: %w", switcher.ErrNoConsole, err)
	case err != nil:
		return nil, err
	}
	return session, nil
}

// bindHotkeys replaces the current bindings with the configured combination and
// the same combination plus the selection modifier.
func (a *App) bindHotkeys(cfg config.Config) error {
	combo, err := cfg.Combo()
	if err != nil {
		return err
	}
	selection, err := cfg.SelectionCombo()
	if err != nil {
		return err
	}

	if a.hotkeys != nil {
		if err := a.hotkeys.Close(); err != nil {
			a.log.Warn("could not release the previous hotkeys", "error", err)
		}
		a.hotkeys = nil
	}

	manager, err := hotkey.Open()
	if err != nil {
		return err
	}
	if err := manager.Register(combo, a.convertLastWord); err != nil {
		_ = manager.Close()
		return err
	}
	if err := manager.Register(selection, a.convertSelection); err != nil {
		_ = manager.Close()
		return err
	}

	a.hotkeys, a.suspended = manager, false
	a.log.Info("hotkeys bound", "word", combo.String(), "selection", selection.String())
	return nil
}

// The hotkey manager calls these on a goroutine of their own.

func (a *App) convertLastWord() { a.run("last word", a.switcher.ConvertLastWord) }

func (a *App) convertSelection() { a.run("selection", a.switcher.ConvertSelection) }

func (a *App) run(what string, convert func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), conversionTimeout)
	defer cancel()

	start := time.Now()
	err := convert(ctx)
	switch {
	case err == nil:
		a.log.Debug("converted", "what", what, "took", time.Since(start))
	case errors.Is(err, switcher.ErrBusy), errors.Is(err, switcher.ErrNothingToConvert):
		a.log.Debug("nothing done", "what", what, "reason", err)
	default:
		a.log.Warn("conversion failed", "what", what, "error", err)
	}
}

// warn records a start-up problem for the log and for the About window.
func (a *App) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	a.log.Warn(message)
	a.warnings = append(a.warnings, message)
}
