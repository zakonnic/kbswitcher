package ui

import (
	"context"
	"fmt"
	"image/color"
	"net/url"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

const (
	aboutWidth = 560
	aboutRow   = 38
)

// aboutWindow is the About window. Its hotkey card follows the settings.
type aboutWindow struct {
	*chrome
	hotkeys *fyne.Container
}

// showAbout opens the About window, or brings it back to the front.
func (a *App) showAbout() {
	if a.about == nil {
		a.about = a.newAbout()
	}
	a.about.hotkeys.Objects = []fyne.CanvasObject{titledCard("Hotkeys", hotkeyRows(a.config)...)}
	a.about.hotkeys.Refresh()
	a.about.show()
}

func (a *App) newAbout() *aboutWindow {
	w := &aboutWindow{chrome: a.newWindow("About " + buildinfo.Name), hotkeys: container.NewStack()}
	hide := w.window.Hide
	w.window.SetCloseIntercept(hide)
	w.window.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if e.Name == fyne.KeyEscape {
			hide()
		}
	})

	title := container.New(column{},
		label(buildinfo.Name, 26, theme.Color(theme.ColorNameForeground), true),
		gap(0, 4),
		label("Rewrites text that was typed with the wrong keyboard layout.", 12.5, theme.Color(colorNameSecondary), false),
		gap(0, 8),
		container.NewHBox(newChip(versionText(), versionStyle)),
	)
	hero := header(150, 24, 26, line(18, logo(72), title))

	body := []fyne.CanvasObject{
		container.New(columns{12}, w.hotkeys, titledCard("Layouts", layoutRows(a.options.Layouts)...)),
	}
	if len(a.options.Warnings) > 0 {
		body = append(body, gap(0, 12), titledCard("Warnings", warningRows(a.options.Warnings)...))
	}
	panel := newUpdatePanel(a, w.window)
	done := newButton("Close", hide)
	done.primary = true
	footer := container.NewBorder(nil, nil,
		line(8, panel.status, repositoryLink()),
		line(8, panel.button, done))
	body = append(body, gap(0, bodyGap), footer)

	w.setContent(headed(hero,
		container.New(layout.NewCustomPaddedLayout(18, bodyGap, bodyPadding, bodyPadding), container.New(column{}, body...)),
	), aboutWidth)
	return w
}

// versionText names the commit once even when the version already holds it,
// as a git describe of an untagged build does.
func versionText() string {
	parts := []string{buildinfo.Version}
	if !strings.Contains(buildinfo.Version, buildinfo.Commit) && buildinfo.Commit != "unknown" {
		parts = append(parts, buildinfo.Commit)
	}
	return strings.Join(append(parts, runtime.GOOS+"/"+runtime.GOARCH), " · ")
}

// hotkeyRows shows the configured combinations as key caps.
func hotkeyRows(cfg config.Config) []fyne.CanvasObject {
	word, selection, ok := combos(cfg)
	secondary := theme.Color(colorNameSecondary)
	if !ok {
		return []fyne.CanvasObject{row(aboutRow, label("The hotkey is not configured.", 12, secondary, false), gap(0, 0))}
	}
	what := func(text string, switches bool) fyne.CanvasObject {
		if switches {
			text += " + switch"
		}
		return besideCaps(label(text, 11.5, secondary, false))
	}
	return []fyne.CanvasObject{
		row(aboutRow, keyCaps(word), what("last word", cfg.SwitchLayout)),
		row(aboutRow, keyCaps(selection), what("selection", cfg.SwitchLayoutSelection)),
	}
}

// keyCaps draws a combination as key caps joined by plus signs.
func keyCaps(combo hotkey.Combo) fyne.CanvasObject {
	var caps []fyne.CanvasObject
	for i, name := range combo.Names() {
		if i > 0 {
			caps = append(caps, besideCaps(label("+", 11, withAlpha(theme.Color(theme.ColorNameForeground), 0.55), false)))
		}
		caps = append(caps, newChip(hotkey.Label(name), keycapStyle))
	}
	return line(5, caps...)
}

// besideCaps makes a text as tall as a key cap and centers its glyphs on the
// cap's face, as the cap centers its own legend, so that the two read on one
// line. Centered on the whole cap, lip included, the text would sit low.
func besideCaps(text *canvas.Text) fyne.CanvasObject { return container.New(capBand{}, text) }

type capBand struct{}

func (capBand) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(objects[0].MinSize().Width, keycapStyle.height+lipHeight)
}

func (capBand) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	placeText(objects[0].(*canvas.Text), 0, size.Width, keycapStyle.height)
}

var badgeColors = []color.Color{
	rgba(0x34, 0x78, 0xF6, 0xFF), rgba(0xD9, 0x47, 0x3F, 0xFF), rgba(0x2F, 0xA8, 0x5A, 0xFF),
	rgba(0xE8, 0x8B, 0x1A, 0xFF), rgba(0x8E, 0x5B, 0xD9, 0xFF),
}

func layoutRows(installed []*keymap.Layout) []fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(installed))
	for i, l := range installed {
		code, known := layouts.Locale(l.ID())
		if !known {
			code = l.ID()
		}
		badge := newChip(layoutBadge(l.Name(), code, known), badgeStyle(badgeColors[i%len(badgeColors)]))
		rows = append(rows, row(aboutRow,
			line(8, badge, label(l.Name(), theme.Size(theme.SizeNameText), theme.Color(theme.ColorNameForeground), false)),
			label(code, 11.5, theme.Color(colorNameSecondary), false)))
	}
	return rows
}

// layoutBadge is the language of a layout in two or three letters: from its
// locale when known, or else from the start of its name.
func layoutBadge(name, locale string, known bool) string {
	if known {
		language, _, _ := strings.Cut(locale, "-")
		return strings.ToUpper(language)
	}
	letters := []rune(strings.ToUpper(name))
	return string(letters[:min(2, len(letters))])
}

func warningRows(warnings []string) []fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(warnings))
	for _, warning := range warnings {
		text := widget.NewLabel(warning)
		text.Wrapping = fyne.TextWrapWord
		rows = append(rows, container.New(layout.NewCustomPaddedLayout(0, 0, cardPadding-6, cardPadding-6), text))
	}
	return rows
}

func repositoryLink() fyne.CanvasObject {
	address, _ := url.Parse(buildinfo.RepositoryURL) // a constant, checked by TestRepositoryURL
	return newLink("GitHub", address)
}

// link is a text that opens an address, no taller than the text itself.
type link struct {
	widget.BaseWidget
	text    string
	address *url.URL
}

func newLink(text string, address *url.URL) *link {
	l := &link{text: text, address: address}
	l.ExtendBaseWidget(l)
	return l
}

// Tapped opens the address.
func (l *link) Tapped(*fyne.PointEvent) {
	if err := fyne.CurrentApp().OpenURL(l.address); err != nil {
		fyne.LogError("could not open "+l.address.String(), err)
	}
}

// Cursor shows that the text is a link.
func (l *link) Cursor() desktop.Cursor { return desktop.PointerCursor }

// CreateRenderer draws the text.
func (l *link) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(label(l.text, 12, theme.Color(theme.ColorNameHyperlink), false))
}

// updatePanel is the status pill and the button that check for and install a
// release.
type updatePanel struct {
	app    *App
	window fyne.Window
	status *chip
	button *button
}

// lastUpdateKey is the preference holding the local date of the last
// installed update.
const lastUpdateKey = "lastUpdate"

// idleStatus is what the pill says before a check: when the last update was
// installed, if one ever was.
func idleStatus(prefs fyne.Preferences) string {
	updated, err := time.Parse(time.DateOnly, prefs.String(lastUpdateKey))
	if err != nil {
		return "Not checked"
	}
	return "Updated " + updated.Format("2 Jan 2006")
}

func newUpdatePanel(app *App, window fyne.Window) *updatePanel {
	p := &updatePanel{
		app:    app,
		window: window,
		status: newChip(idleStatus(app.fyne.Preferences()), pillNeutral),
		button: newButton("Check for updates", nil),
	}
	if app.options.Updater == nil {
		p.button.disable()
		p.show("Updates unavailable", pillWarn)
		return p
	}
	p.button.onTapped = p.check
	return p
}

func (p *updatePanel) show(text string, style chipStyle) { p.status.set(text, style) }

// check asks for the latest release and offers to install it when it is newer.
func (p *updatePanel) check() {
	p.button.disable()
	p.show("Checking…", pillNeutral)
	u := p.app.options.Updater

	go func() {
		release, err := u.Latest(context.Background())

		fyne.Do(func() {
			p.button.enable()
			switch {
			case err != nil:
				p.app.log.Warn("update check failed", "error", err)
				p.show("Check failed", pillWarn)
				dialog.ShowError(fmt.Errorf("could not check for updates: %w", err), p.window)
			case !u.IsNewer(release):
				p.show("Up to date", pillOK)
			default:
				p.show(release.Version+" available", pillNeutral)
				p.confirm(release)
			}
		})
	}()
}

func (p *updatePanel) confirm(release updater.Release) {
	message := fmt.Sprintf("Install %s and restart %s?\n\n"+
		"The archive published for this platform is downloaded, checked against "+
		"the release checksum and put in place of the running program.",
		release.Version, buildinfo.Name)
	if runtime.GOOS == "darwin" {
		message += "\n\nNote: replacing the program inside a signed application " +
			"bundle invalidates its signature. On a managed Mac, install the " +
			"update through your device management instead."
	}

	dialog.ShowConfirm("Update available", message, func(confirmed bool) {
		if confirmed {
			p.install(release)
		}
	}, p.window)
}

// install downloads and installs a release, then offers to restart.
func (p *updatePanel) install(release updater.Release) {
	p.button.disable()
	p.show("Downloading…", pillNeutral)
	u := p.app.options.Updater

	go func() {
		path, err := u.Update(context.Background(), release)

		fyne.Do(func() {
			p.button.enable()
			if err != nil {
				p.app.log.Error("update failed", "version", release.Version, "error", err)
				p.show("Update failed", pillWarn)
				dialog.ShowError(err, p.window)
				return
			}
			p.app.log.Info("update installed", "version", release.Version, "path", path)
			p.app.fyne.Preferences().SetString(lastUpdateKey, time.Now().Format(time.DateOnly))
			p.show(release.Version+" installed", pillOK)

			dialog.ShowConfirm("Restart now?",
				buildinfo.Name+" has been updated. Restart it now to use "+release.Version+"?",
				func(restart bool) {
					if !restart {
						return
					}
					p.app.restart = true
					p.app.fyne.Quit()
				}, p.window)
		})
	}()
}
