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

var settingsWindowSize = fyne.NewSize(460, 0)

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

	save := widget.NewButton("Save", func() {
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
	save.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", window.Hide)

	window.SetContent(container.NewPadded(container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Autostart", form.autostart),
			widget.NewFormItem("Hotkey", container.NewVBox(form.key, form.modifiers)),
			widget.NewFormItem("Layout", form.switchLayout),
		),
		widget.NewSeparator(),
		form.summary,
		container.NewHBox(layout.NewSpacer(), cancel, save),
	)))
	window.Resize(settingsWindowSize)
	window.CenterOnScreen()
	window.Show()
}

// settingsForm holds the widgets of the settings window.
type settingsForm struct {
	autostart    *widget.Check
	key          *widget.Select
	modifiers    *fyne.Container
	checks       []*widget.Check // one per hotkey.Modifiers(), in that order
	switchLayout *widget.Check
	summary      *widget.Label
}

func newSettingsForm(cfg config.Config) *settingsForm {
	f := &settingsForm{
		autostart:    widget.NewCheck("Start when I log in", nil),
		key:          widget.NewSelect(hotkey.Keys(), nil),
		modifiers:    container.NewHBox(),
		switchLayout: widget.NewCheck("Switch the keyboard to the layout of the converted word", nil),
		summary:      widget.NewLabel(""),
	}
	f.summary.Wrapping = fyne.TextWrapWord
	for _, name := range hotkey.Modifiers() {
		check := widget.NewCheck(name, func(bool) { f.preview() })
		f.checks = append(f.checks, check)
		f.modifiers.Add(check)
	}
	f.key.OnChanged = func(string) { f.preview() }
	f.switchLayout.OnChanged = func(bool) { f.preview() }
	f.reset(cfg)
	return f
}

// config returns what the widgets currently show.
func (f *settingsForm) config() config.Config {
	names := []string{}
	for _, check := range f.checks {
		if check.Checked {
			names = append(names, check.Text)
		}
	}
	return config.Config{
		Hotkey:       config.Hotkey{Key: f.key.Selected, Modifiers: names},
		Autostart:    f.autostart.Checked,
		SwitchLayout: f.switchLayout.Checked,
	}
}

// reset makes the widgets show a configuration.
func (f *settingsForm) reset(cfg config.Config) {
	mods, err := hotkey.ParseModifiers(cfg.Hotkey.Modifiers)
	if err != nil {
		mods = 0
	}
	names := mods.Names()
	for _, check := range f.checks {
		check.SetChecked(slices.Contains(names, check.Text))
	}
	f.autostart.SetChecked(cfg.Autostart)
	f.switchLayout.SetChecked(cfg.SwitchLayout)
	f.key.SetSelected(cfg.Hotkey.Key)
	f.preview()
}

func (f *settingsForm) preview() {
	f.summary.SetText(hotkeySummary(f.config()))
}
