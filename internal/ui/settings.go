package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
	rebindDelay   = 100 * time.Millisecond
)

// messageInk is the color of what the settings window has to tell the user.
var messageInk = rgba(0xFF, 0xB3, 0x40, 0xFF)

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
	// The window grows and shrinks with the message under the hotkeys.
	form.resized = func() { w.window.Resize(fyne.NewSize(w.width, 0)) }
	// Moving from one field to the other loses one focus before it gains the
	// other; the hotkeys are bound again only once neither field listens.
	listening := func(bool) {
		if form.key.focused || form.modifier.focused {
			a.options.Suspend(true)
			return
		}
		time.AfterFunc(rebindDelay, func() {
			fyne.Do(func() {
				if !form.key.focused && !form.modifier.focused {
					a.options.Suspend(false)
				}
			})
		})
	}
	form.key.onFocus, form.modifier.onFocus = listening, listening
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
			form.show(sentence(err.Error()))
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
			row(settingsRow, caption("Switch the layout after a word", "to the layout the word was converted to"), form.switchWord),
			row(settingsRow, caption("Switch the layout after a selection", "to the layout the text was converted to"), form.switchSelection),
		),
		gap(0, bodyGap),
		card(
			row(settingsRow, caption("Hotkey", "converts the last word"), form.key),
			row(settingsRow, caption("Selection modifier", "held with the hotkey, converts the selection"), form.modifier),
		),
		container.New(layout.NewCustomPaddedLayout(10, 0, cardPadding, cardPadding), form.message),
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
	autostart, switchWord, switchSelection *toggle
	key, modifier                          *keyField
	message                                *fyne.Container
	preview                                *fyne.Container
	// note is what a key field had to say about the last press.
	note    string
	resized func()
}

func newSettingsForm(cfg config.Config) *settingsForm {
	f := &settingsForm{
		autostart:       newToggle(nil),
		switchWord:      newToggle(nil),
		switchSelection: newToggle(nil),
		key:             newKeyField(2, "Press keys", checkHotkey),
		modifier:        newKeyField(1, "Press a modifier", checkModifier),
		message:         container.New(column{}),
		preview:         container.New(column{}),
	}
	changed := func([]string) { f.refresh() }
	f.key.onChanged, f.modifier.onChanged = changed, changed
	noted := func(note string) { f.note = note; f.refresh() }
	f.key.onMessage, f.modifier.onMessage = noted, noted
	f.switchWord.onChanged = func(bool) { f.refresh() }
	f.switchSelection.onChanged = func(bool) { f.refresh() }
	f.reset(cfg)
	return f
}

// checkHotkey says why keys cannot be the hotkey: at most one of them may be
// a key other than a modifier, and one must be.
func checkHotkey(keys []string) error {
	combo, err := comboOf(keys)
	if err != nil {
		return err
	}
	return combo.Check()
}

// checkModifier says why keys cannot be the selection modifier.
func checkModifier(keys []string) error {
	if len(keys) != 1 || !hotkey.IsModifier(keys[0]) {
		return fmt.Errorf("only a modifier can be chosen here: %s, %s, %s or %s",
			hotkey.Label("Shift"), hotkey.Label("Ctrl"), hotkey.Label("Alt"), hotkey.Label("Super"))
	}
	return nil
}

func comboOf(keys []string) (hotkey.Combo, error) {
	var combo hotkey.Combo
	for _, key := range keys {
		if hotkey.IsModifier(key) {
			mod, _ := hotkey.ParseModifiers([]string{key})
			combo.Mods |= mod
			continue
		}
		if combo.Key != "" {
			return combo, errors.New("two keys need one of them to be a modifier")
		}
		combo.Key = key
	}
	if combo.Key == "" {
		return combo, errors.New("a modifier needs a key to go with it")
	}
	return combo, nil
}

// config returns what the controls currently show.
func (f *settingsForm) config() config.Config {
	combo, _ := comboOf(f.key.value) // the field only takes what comboOf accepts
	modifier := ""
	if len(f.modifier.value) == 1 {
		modifier = f.modifier.value[0]
	}
	return config.Config{
		Hotkey:                config.Hotkey{Key: combo.Key, Modifiers: combo.Mods.Names()},
		SelectionModifier:     modifier,
		Autostart:             f.autostart.on,
		SwitchLayout:          f.switchWord.on,
		SwitchLayoutSelection: f.switchSelection.on,
	}
}

// reset makes the controls show a configuration.
func (f *settingsForm) reset(cfg config.Config) {
	combo, err := cfg.Combo()
	if err != nil {
		combo, _ = config.Default().Combo()
	}
	modifier, err := hotkey.ParseModifiers([]string{cfg.SelectionModifier})
	if err != nil || len(modifier.Names()) != 1 {
		modifier = hotkey.ModShift
	}
	f.autostart.setOn(cfg.Autostart)
	f.switchWord.setOn(cfg.SwitchLayout)
	f.switchSelection.setOn(cfg.SwitchLayoutSelection)
	f.key.reset(combo.Names())
	f.modifier.reset(modifier.Names())
	f.note = ""
	f.refresh()
}

// show puts a message under the hotkey card, a line a sentence, or takes it
// away.
func (f *settingsForm) show(message string) {
	before := len(f.message.Objects)
	f.message.Objects = nil
	if message != "" {
		for _, sentence := range strings.Split(message, "\n") {
			f.message.Add(label(sentence, 12, messageInk, false))
		}
	}
	f.message.Hidden = message == ""
	f.message.Refresh()
	if len(f.message.Objects) != before && f.resized != nil {
		f.resized()
	}
}

// refresh shows what the combinations on screen will do, and what is wrong
// with them.
func (f *settingsForm) refresh() {
	cfg := f.config()
	message := f.note
	if err := cfg.Validate(); err != nil {
		message = sentence(err.Error())
	}
	f.show(message)

	word, selection, ok := combos(cfg)
	if !ok {
		f.preview.Objects = nil
		f.preview.Refresh()
		return
	}
	secondary := theme.Color(colorNameSecondary)
	what := func(text string, switches bool) fyne.CanvasObject {
		if switches {
			text += " and switches the layout"
		}
		return besideCaps(label(text, 12, secondary, false))
	}
	f.preview.Objects = []fyne.CanvasObject{
		line(8, keyCaps(word), what("converts the last word", cfg.SwitchLayout)),
		gap(0, 6),
		line(8, keyCaps(selection), what("converts the selected text", cfg.SwitchLayoutSelection)),
	}
	f.preview.Refresh()
}
