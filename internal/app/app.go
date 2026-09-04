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
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
	"github.com/zakonnic/kbswitcher/internal/switcher"
	"github.com/zakonnic/kbswitcher/internal/ui"
	"github.com/zakonnic/kbswitcher/internal/updater"
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

	release, err := updater.New()
	if err != nil {
		a.warn("Updates are unavailable: %v", err)
	}

	a.ui = ui.New(ui.Options{
		Log:      log,
		Updater:  release,
		Layouts:  a.switcher.Layouts(),
		Config:   a.config,
		Apply:    a.Apply,
		Warnings: a.warnings,
	})
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

// Apply puts a new configuration in force: hotkeys, autostart entry, file. When
// a step fails the earlier ones are undone, so that what is running always
// matches what the settings window shows.
func (a *App) Apply(cfg config.Config) error {
	if err := a.bindHotkeys(cfg); err != nil {
		a.restoreHotkeys()
		return err
	}
	if cfg.Autostart != a.config.Autostart {
		if err := autostart.Set(cfg.Autostart); err != nil {
			a.restoreHotkeys()
			return err
		}
	}
	if err := config.Save(cfg); err != nil {
		a.restoreHotkeys()
		if undoErr := autostart.Set(a.config.Autostart); undoErr != nil {
			a.log.Error("could not restore the autostart entry", "error", undoErr)
		}
		return err
	}

	a.config = cfg
	a.log.Info("settings applied", "hotkey", cfg.Hotkey.Key, "autostart", cfg.Autostart)
	return nil
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
	if enabled, err := autostart.Enabled(); err == nil && enabled {
		return
	}
	if err := autostart.Enable(); err != nil {
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

	a.switcher, err = switcher.New(installed, a.layouts, a.keyboard, a.clipboard,
		switcher.DefaultOptions(), a.log)
	return err
}

// bindHotkeys replaces the current bindings with the configured combination and
// the same combination plus Shift.
func (a *App) bindHotkeys(cfg config.Config) error {
	combo, err := cfg.Combo()
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
	selection := combo.With(hotkey.ModShift)
	if err := manager.Register(combo, a.convertLastWord); err != nil {
		_ = manager.Close()
		return err
	}
	if err := manager.Register(selection, a.convertSelection); err != nil {
		_ = manager.Close()
		return err
	}

	a.hotkeys = manager
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
