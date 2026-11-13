package ui

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/zakonnic/kbswitcher/internal/config"
	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

// newTestApp runs a test under this package's theme, which the controls'
// colors are looked up in.
func newTestApp(t *testing.T) {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(newTheme())
}

func TestToggleFlipsOnTapAndReportsChanges(t *testing.T) {
	newTestApp(t)
	var reported []bool
	sw := newToggle(func(on bool) { reported = append(reported, on) })

	test.Tap(sw)
	sw.setOn(true) // already on: nothing to report
	sw.setOn(false)

	if !slices.Equal(reported, []bool{true, false}) {
		t.Errorf("reported %v, want [true false]", reported)
	}
}

// scanCode finds the scan code this platform reports for a key.
func scanCode(t *testing.T, name string) int {
	t.Helper()
	for code := range 0x400 {
		if got, ok := hotkey.KeyForScanCode(code); ok && got == name {
			return code
		}
	}
	t.Fatalf("no scan code names %q", name)
	return 0
}

// press presses keys in order and releases them in reverse.
func press(field *keyField, keys ...fyne.KeyEvent) {
	for i := range keys {
		field.KeyDown(&keys[i])
	}
	for i := len(keys) - 1; i >= 0; i-- {
		field.KeyUp(&keys[i])
	}
}

func key(t *testing.T, name string) fyne.KeyEvent {
	return fyne.KeyEvent{Physical: fyne.HardwareKey{ScanCode: scanCode(t, name)}}
}

var ctrl = fyne.KeyEvent{Name: desktop.KeyControlLeft}

func TestKeyFieldTakesACombination(t *testing.T) {
	newTestApp(t)
	var changed [][]string
	var message string
	field := newKeyField(2, "", checkHotkey)
	field.reset([]string{hotkey.DefaultKey()})
	field.onChanged = func(keys []string) { changed = append(changed, keys) }
	field.onMessage = func(m string) { message = m }

	press(field, ctrl, key(t, "Q"))

	if !slices.Equal(field.value, []string{"Ctrl", "Q"}) || len(changed) != 1 || message != "" {
		t.Errorf("value %v, %d changes, message %q; want Ctrl+Q once and no message", field.value, len(changed), message)
	}
}

func TestKeyFieldRefusesALetterAlone(t *testing.T) {
	newTestApp(t)
	var message string
	field := newKeyField(2, "", checkHotkey)
	field.reset([]string{hotkey.DefaultKey()})
	field.onMessage = func(m string) { message = m }

	press(field, key(t, "Q"))

	if !slices.Equal(field.value, []string{hotkey.DefaultKey()}) {
		t.Errorf("value %v, want the old key kept", field.value)
	}
	if message != "Letters, digits and other keys used in typing need a modifier." {
		t.Errorf("message %q", message)
	}
}

func TestKeyFieldKeepsTheFirstTwoKeys(t *testing.T) {
	newTestApp(t)
	var message string
	field := newKeyField(2, "", checkHotkey)
	field.onMessage = func(m string) { message = m }

	press(field, ctrl, key(t, "Q"), fyne.KeyEvent{Name: desktop.KeyShiftLeft})

	if !slices.Equal(field.value, []string{"Ctrl", "Q"}) || message != "At most two keys can be combined; the first two are kept." {
		t.Errorf("value %v, message %q; want Ctrl+Q and a note on the limit", field.value, message)
	}
}

func TestModifierFieldTakesOnlyAModifier(t *testing.T) {
	newTestApp(t)
	var message string
	field := newKeyField(1, "", checkModifier)
	field.reset([]string{"Shift"})
	field.onMessage = func(m string) { message = m }

	press(field, key(t, "Q"))
	if !slices.Equal(field.value, []string{"Shift"}) || !strings.HasPrefix(message, "Only a modifier") {
		t.Errorf("after Q: value %v, message %q", field.value, message)
	}
	press(field, fyne.KeyEvent{Name: desktop.KeyAltRight})
	if !slices.Equal(field.value, []string{"Alt"}) || message != "" {
		t.Errorf("after Alt: value %v, message %q", field.value, message)
	}
}

func TestSettingsFormShowsAndReturnsAConfiguration(t *testing.T) {
	newTestApp(t)
	want := config.Config{
		Hotkey:                config.Hotkey{Key: "Q", Modifiers: []string{"Ctrl"}},
		SelectionModifier:     "Alt",
		Autostart:             true,
		SwitchLayout:          false,
		SwitchLayoutSelection: true,
	}

	form := newSettingsForm(want)
	got := form.config()

	if got.Hotkey.Key != want.Hotkey.Key || !slices.Equal(got.Hotkey.Modifiers, want.Hotkey.Modifiers) ||
		got.SelectionModifier != want.SelectionModifier || got.Autostart != want.Autostart ||
		got.SwitchLayout != want.SwitchLayout || got.SwitchLayoutSelection != want.SwitchLayoutSelection {
		t.Errorf("form returned %+v, want %+v", got, want)
	}
	if !form.message.Hidden {
		t.Errorf("a valid configuration shows a message")
	}
}

func TestSettingsFormReportsAModifierUsedTwice(t *testing.T) {
	newTestApp(t)
	form := newSettingsForm(config.Default())

	press(form.key, ctrl, key(t, "Q"))
	press(form.modifier, ctrl)

	if form.message.Hidden || len(form.message.Objects) != 1 ||
		!strings.Contains(form.message.Objects[0].(*canvas.Text).Text, "already holds") {
		t.Errorf("message %v (hidden %v), want the conflict explained", form.message.Objects, form.message.Hidden)
	}
}

func TestLayoutBadge(t *testing.T) {
	tests := []struct {
		name, locale string
		known        bool
		want         string
	}{
		{"US", "en-US", true, "EN"},
		{"Serbian (Latin)", "sr-Latn-RS", true, "SR"},
		{"English (US)", "builtin.US", false, "EN"},
		{"Я", "", false, "Я"},
	}
	for _, tt := range tests {
		if got := layoutBadge(tt.name, tt.locale, tt.known); got != tt.want {
			t.Errorf("layoutBadge(%q, %q, %v) = %q, want %q", tt.name, tt.locale, tt.known, got, tt.want)
		}
	}
}
