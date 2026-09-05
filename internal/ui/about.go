package ui

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

var aboutWindowSize = fyne.NewSize(560, 480)

// showAbout opens the About window, or brings it back to the front.
func (a *App) showAbout() {
	if a.aboutWindow != nil {
		a.aboutWindow.Show()
		a.aboutWindow.RequestFocus()
		return
	}

	window := a.fyne.NewWindow("About " + buildinfo.Name)
	a.aboutWindow = window
	window.SetCloseIntercept(window.Hide)

	description := widget.NewRichTextFromMarkdown(a.aboutText())
	description.Wrapping = fyne.TextWrapWord

	panel := newUpdatePanel(a, window)

	window.SetContent(container.New(windowPadding(), container.NewBorder(
		nil,
		container.NewVBox(
			panel.status,
			container.NewHBox(layout.NewSpacer(), panel.button, gap(6, 0), newButton("Close", window.Hide)),
		),
		nil, nil,
		container.NewVScroll(card(container.New(
			layout.NewCustomPaddedLayout(cardPadding, cardPadding, cardPadding, cardPadding),
			container.NewVBox(description, repositoryLink()),
		))),
	)))
	window.Resize(aboutWindowSize)
	window.CenterOnScreen()
	window.Show()
}

// aboutText describes the running application in Markdown.
func (a *App) aboutText() string {
	var text strings.Builder

	fmt.Fprintf(&text, "## %s %s\n\n", buildinfo.Name, buildinfo.Version)
	fmt.Fprintf(&text, "Build %s, %s/%s.\n\n", buildinfo.Commit, runtime.GOOS, runtime.GOARCH)
	text.WriteString("Rewrites text that was typed with the wrong keyboard layout, " +
		"as if the same keys had been pressed with the next layout selected.\n\n")

	text.WriteString("### Hotkeys\n\n")
	for line := range strings.Lines(hotkeySummary(a.config)) {
		fmt.Fprintf(&text, "- %s\n", strings.TrimSpace(line))
	}

	text.WriteString("\n### Layouts, in the order they are cycled\n\n")
	for i, layout := range a.options.Layouts {
		fmt.Fprintf(&text, "%d. %s — `%s`\n", i+1, layout.Name(), layout.ID())
	}

	if len(a.options.Warnings) > 0 {
		text.WriteString("\n### Warnings\n\n")
		for _, warning := range a.options.Warnings {
			fmt.Fprintf(&text, "%s\n\n", warning)
		}
	}
	return text.String()
}

func repositoryLink() fyne.CanvasObject {
	address, err := url.Parse("https://github.com/" + buildinfo.Repository)
	if err != nil {
		return widget.NewLabel(buildinfo.Repository)
	}
	return widget.NewHyperlink(buildinfo.Repository, address)
}

// updatePanel is the status line and the button that check for and install a
// release.
type updatePanel struct {
	app    *App
	window fyne.Window
	status *widget.Label
	button *button
}

func newUpdatePanel(app *App, window fyne.Window) *updatePanel {
	p := &updatePanel{
		app:    app,
		window: window,
		status: widget.NewLabel(""),
		button: newButton("Check for updates", nil),
	}
	p.status.Wrapping = fyne.TextWrapWord
	p.button.primary = true
	if app.options.Updater == nil {
		p.button.disable()
		p.status.SetText("Updates are unavailable; see the warnings above.")
		return p
	}
	p.button.onTapped = p.check
	return p
}

// check asks for the latest release and offers to install it when it is newer.
func (p *updatePanel) check() {
	p.button.disable()
	p.status.SetText("Looking for a newer release…")
	u := p.app.options.Updater

	go func() {
		release, err := u.Latest(context.Background())

		fyne.Do(func() {
			p.button.enable()
			switch {
			case err != nil:
				p.app.log.Warn("update check failed", "error", err)
				p.status.SetText("Could not check for updates: " + err.Error())
			case !u.IsNewer(release):
				p.status.SetText(fmt.Sprintf("%s is the latest release; this build is %s.",
					release.Version, u.Current()))
			default:
				p.status.SetText(release.Version + " is available.")
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
		if !confirmed {
			p.status.SetText(release.Version + " is available; not installed.")
			return
		}
		p.install(release)
	}, p.window)
}

// install downloads and installs a release, then offers to restart.
func (p *updatePanel) install(release updater.Release) {
	p.button.disable()
	p.status.SetText("Downloading " + release.Version + "…")
	u := p.app.options.Updater

	go func() {
		path, err := u.Update(context.Background(), release)

		fyne.Do(func() {
			p.button.enable()
			if err != nil {
				p.app.log.Error("update failed", "version", release.Version, "error", err)
				p.status.SetText("Update failed: " + err.Error())
				dialog.ShowError(err, p.window)
				return
			}
			p.app.log.Info("update installed", "version", release.Version, "path", path)
			p.status.SetText(release.Version + " installed.")

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
