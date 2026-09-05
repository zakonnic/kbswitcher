package ui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

var settingsWindowSize = fyne.NewSize(500, 0)

// showSettings opens the settings window, or brings it back to the front with
// the configuration in force.
func (a *App) showSettings() {
	if a.settingsWindow != nil {
		a.settingsForm.reset(a.config)
		a.settingsWindow.Show()
		a.settingsWindow.RequestFocus()
		return
	}

	window := a.fyne.NewWindow(buildinfo.Name + " — Settings")
	window.SetCloseIntercept(window.Hide)
	form := newSettingsForm(a.config)
	a.settingsWindow, a.settingsForm = window, form

	save := newButton("Save", func() {
		updated := form.config()
		if err := updated.Validate(); err != nil {
			dialog.ShowError(err, window)
			return
		}
		if err := a.options.Apply(updated); err != nil {
			a.log.Error("could not apply the settings", "error", err)
			dialog.ShowError(err, window)
			form.reset(a.config)
			return
		}
		a.config = updated
		window.Hide()
	})
	save.primary = true
	cancel := newButton("Cancel", window.Hide)

	window.SetContent(container.New(windowPadding(), container.NewVBox(
		card(
			row("Start when I log in", form.autostart),
			row("Switch the keyboard to the layout of the converted word", form.switchLayout),
		),
		gap(0, 8),
		card(
			row("Hotkey", form.key),
			row("Modifiers", form.modifiers),
		),
		gap(0, 8),
		form.summary,
		gap(0, 12),
		container.NewHBox(layout.NewSpacer(), cancel, gap(6, 0), save),
	)))
	window.Resize(settingsWindowSize)
	window.CenterOnScreen()
	window.Show()
}

// settingsForm holds the controls of the settings window.
type settingsForm struct {
	autostart    *toggle
	switchLayout *toggle
	key          *popUpButton
	checks       []*checkbox // one per hotkey.Modifiers(), in that order
	modifiers    *fyne.Container
	summary      *widget.RichText
	summaryText  *widget.TextSegment
}

func newSettingsForm(cfg config.Config) *settingsForm {
	f := &settingsForm{
		autostart:    newToggle(nil),
		switchLayout: newToggle(nil),
		key:          newPopUpButton(hotkey.Keys(), nil),
		modifiers:    container.NewHBox(),
		summaryText:  &widget.TextSegment{Style: widget.RichTextStyle{ColorName: colorNameSecondary}},
	}
	f.summary = widget.NewRichText(f.summaryText)
	f.summary.Wrapping = fyne.TextWrapWord
	for i, name := range hotkey.Modifiers() {
		check := newCheckbox(name, func(bool) { f.preview() })
		f.checks = append(f.checks, check)
		if i > 0 {
			f.modifiers.Add(gap(8, 0))
		}
		f.modifiers.Add(check)
	}
	f.key.onChanged = func(string) { f.preview() }
	f.switchLayout.onChanged = func(bool) { f.preview() }
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
	f.key.setSelected(cfg.Hotkey.Key)
	f.preview()
}

func (f *settingsForm) preview() {
	f.summaryText.Text = hotkeySummary(f.config())
	f.summary.Refresh()
}
