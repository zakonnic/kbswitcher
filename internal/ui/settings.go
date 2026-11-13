package ui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

const (
	settingsWidth = 520
	settingsRow   = 42
)

// settingsWindow is the settings window and the form it edits.
type settingsWindow struct {
	*chrome
	form *settingsForm
}

// showSettings opens the settings window, or brings it back to the front with
// the configuration in force.
func (a *App) showSettings() {
	if a.settings == nil {
		a.settings = a.newSettings()
	}
	a.settings.form.reset(a.config)
	a.settings.show()
}

func (a *App) newSettings() *settingsWindow {
	w := &settingsWindow{chrome: a.newWindow(buildinfo.Name + " Settings"), form: newSettingsForm(a.config)}
	form := w.form
	form.key.onFocus = a.options.Suspend
	hide := func() {
		w.window.Canvas().Unfocus()
		w.window.Hide()
	}
	w.window.SetCloseIntercept(hide)
	w.window.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if e.Name == fyne.KeyEscape {
			hide()
		}
	})

	save := newButton("Save", func() {
		updated := form.config()
		if err := updated.Validate(); err != nil {
			dialog.ShowError(err, w.window)
			return
		}
		if err := a.options.Apply(updated); err != nil {
			a.log.Error("could not apply the settings", "error", err)
			dialog.ShowError(err, w.window)
			form.reset(a.config)
			return
		}
		a.config = updated
		hide()
	})
	save.primary = true

	fg := theme.Color(theme.ColorNameForeground)
	text := theme.Size(theme.SizeNameText)
	hero := header(0, 12, 16, container.NewBorder(nil, nil,
		line(10, logo(28), label(buildinfo.Name+" Settings", 14, fg, true)),
		newCloseButton(hide)))

	body := container.New(column{},
		card(
			row(settingsRow, label("Start when I log in", text, fg, false), form.autostart),
			row(settingsRow, caption("Switch the keyboard layout", "to the layout of the converted word"), form.switchLayout),
		),
		gap(0, bodyGap),
		card(
			row(settingsRow, label("Hotkey", text, fg, false), form.key),
			row(settingsRow, label("Modifiers", text, fg, false), form.modifiers),
		),
		gap(0, 14),
		container.New(layout.NewCustomPaddedLayout(0, 0, cardPadding, cardPadding), form.preview),
		gap(0, bodyGap),
		container.NewBorder(nil, nil, nil, line(8, newButton("Cancel", hide), save)),
	)
	w.setContent(headed(hero,
		container.New(layout.NewCustomPaddedLayout(18, bodyGap, bodyPadding, bodyPadding), body),
	), settingsWidth)
	return w
}

// settingsForm holds the controls of the settings window.
type settingsForm struct {
	autostart    *toggle
	switchLayout *toggle
	key          *keyField
	checks       []*checkbox // one per hotkey.Modifiers(), in that order
	modifiers    fyne.CanvasObject
	preview      *fyne.Container
}

func newSettingsForm(cfg config.Config) *settingsForm {
	f := &settingsForm{
		autostart:    newToggle(nil),
		switchLayout: newToggle(nil),
		key:          newKeyField(),
		preview:      container.New(column{}),
	}
	boxes := make([]fyne.CanvasObject, 0, len(hotkey.Modifiers()))
	for _, name := range hotkey.Modifiers() {
		check := newCheckbox(name, func(bool) { f.refreshPreview() })
		f.checks = append(f.checks, check)
		boxes = append(boxes, check)
	}
	f.modifiers = line(12, boxes...)
	f.key.onChanged = func(string) { f.refreshPreview() }
	f.switchLayout.onChanged = func(bool) { f.refreshPreview() }
	f.reset(cfg)
	return f
}

// config returns what the controls currently show.
func (f *settingsForm) config() config.Config {
	names := []string{}
	for _, check := range f.checks {
		if check.checked {
			names = append(names, check.text)
		}
	}
	return config.Config{
		Hotkey:       config.Hotkey{Key: f.key.selected, Modifiers: names},
		Autostart:    f.autostart.on,
		SwitchLayout: f.switchLayout.on,
	}
}

// reset makes the controls show a configuration.
func (f *settingsForm) reset(cfg config.Config) {
	mods, err := hotkey.ParseModifiers(cfg.Hotkey.Modifiers)
	if err != nil {
		mods = 0
	}
	names := mods.Names()
	for _, check := range f.checks {
		check.setChecked(slices.Contains(names, check.text))
	}
	f.autostart.setOn(cfg.Autostart)
	f.switchLayout.setOn(cfg.SwitchLayout)
	f.key.reset(cfg.Hotkey.Key)
	f.refreshPreview()
}

// refreshPreview shows what the combinations on screen will do.
func (f *settingsForm) refreshPreview() {
	cfg := f.config()
	secondary := theme.Color(colorNameSecondary)
	// The form only offers valid modifiers, so the combination always parses.
	word, selection, _ := combos(cfg)
	what := "converts the last word"
	if cfg.SwitchLayout {
		what = "converts the last word and switches the layout"
	}
	f.preview.Objects = []fyne.CanvasObject{
		line(8, keyCaps(word), label(what, 12, secondary, false)),
		gap(0, 6),
		line(8, keyCaps(selection), label("converts the selected text", 12, secondary, false)),
	}
	f.preview.Refresh()
}
