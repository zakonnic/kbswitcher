// Package ui is the part of kbswitcher the user sees: a tray icon with a
// short menu and the two windows it opens.
//
// Everything here runs on the goroutine that called Run. Slow work, such as
// looking for a release, runs elsewhere and comes back through fyne.Do.
package ui

import (
	"context"
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

// Updater finds and installs releases; *updater.Updater is one.
type Updater interface {
	Latest(ctx context.Context) (updater.Release, error)
	IsNewer(release updater.Release) bool
	Update(ctx context.Context, release updater.Release) (string, error)
}

// Options are what the application hands to the interface.
type Options struct {
	Log *slog.Logger
	// Updater is nil when updates are unavailable.
	Updater Updater
	// Layouts is the cycle read at start-up, shown in About.
	Layouts []*keymap.Layout
	Config  config.Config
	// Apply puts a configuration in force and returns an error the user can
	// act on.
	Apply func(config.Config) error
	// Suspend releases the hotkeys while the settings window waits for a key,
	// and binds them again when called with false.
	Suspend func(bool)
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

	settings *settingsWindow
	about    *aboutWindow

	// restart is set when the user asked to relaunch after an update.
	restart bool
}

// New builds the tray application.
func New(options Options) *App {
	fyneApp := fyneapp.NewWithID(buildinfo.ID)
	fyneApp.SetIcon(icon())
	fyneApp.Settings().SetTheme(newTheme())

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
	// The icon sizes are only right once the toolkit has made the process
	// aware of the display scale, which it does on start.
	a.fyne.Lifecycle().SetOnStarted(func() { desk.SetSystemTrayIcon(trayIcon()) })

	a.fyne.Run()
	return a.restart
}

func icon() fyne.Resource {
	return fyne.NewStaticResource("icon.png", assets.IconPNG)
}

// combos are the configured combination and the same plus the selection
// modifier.
func combos(cfg config.Config) (word, selection hotkey.Combo, ok bool) {
	word, err := cfg.Combo()
	if err != nil {
		return hotkey.Combo{}, hotkey.Combo{}, false
	}
	selection, err = cfg.SelectionCombo()
	if err != nil {
		return hotkey.Combo{}, hotkey.Combo{}, false
	}
	return word, selection, true
}
