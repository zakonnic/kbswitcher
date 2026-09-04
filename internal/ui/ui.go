// Package ui is the part of kbswitcher the user sees: a tray icon with a
// short menu and the two windows it opens.
//
// Everything here runs on the goroutine that called Run. Slow work, such as
// looking for a release, runs elsewhere and comes back through fyne.Do.
package ui

import (
	"log/slog"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/zakonnic/kbswitcher/internal/assets"
	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

// Options are what the application hands to the interface.
type Options struct {
	Log *slog.Logger
	// Updater is nil when updates are unavailable.
	Updater *updater.Updater
	// Layouts is the cycle read at start-up, shown in About.
	Layouts []*keymap.Layout
	Config  config.Config
	// Apply puts a configuration in force and returns an error the user can
	// act on.
	Apply func(config.Config) error
	// Warnings are the start-up problems shown in About.
	Warnings []string
}

// App is the tray application.
type App struct {
	fyne    fyne.App
	log     *slog.Logger
	options Options

	// config is the configuration in force. It is only touched on the main
	// goroutine.
	config config.Config

	settingsWindow fyne.Window
	settingsForm   *settingsForm
	aboutWindow    fyne.Window

	// restart is set when the user asked to relaunch after an update.
	restart bool
}

// New builds the tray application.
func New(options Options) *App {
	fyneApp := fyneapp.NewWithID(buildinfo.ID)
	fyneApp.SetIcon(icon())

	return &App{
		fyne:    fyneApp,
		log:     options.Log,
		options: options,
		config:  options.Config,
	}
}

// Run shows the tray icon and blocks until the user quits. It must be called
// from the main goroutine. It reports whether the user asked for a restart.
func (a *App) Run() (restart bool) {
	desk, ok := a.fyne.(desktop.App)
	if !ok {
		a.log.Error("this system has no system tray")
		return false
	}

	// The toolkit appends a Quit entry of its own unless the last item is
	// marked as one.
	quit := fyne.NewMenuItem("Quit", a.fyne.Quit)
	quit.IsQuit = true

	desk.SetSystemTrayMenu(fyne.NewMenu(buildinfo.Name,
		fyne.NewMenuItem("About", a.showAbout),
		fyne.NewMenuItem("Settings", a.showSettings),
		fyne.NewMenuItemSeparator(),
		quit,
	))
	desk.SetSystemTrayIcon(icon())

	a.fyne.Run()
	return a.restart
}

func icon() fyne.Resource {
	return fyne.NewStaticResource("icon.png", assets.IconPNG)
}

// hotkeySummary describes what the configured hotkey does.
func hotkeySummary(cfg config.Config) string {
	combo, err := cfg.Combo()
	if err != nil {
		return "The hotkey is not configured."
	}
	return combo.String() + " converts the last word.\n" +
		combo.With(hotkey.ModShift).String() + " converts the selected text."
}
