package ui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
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

func TestCheckboxFlipsOnTap(t *testing.T) {
	newTestApp(t)
	var got bool
	box := newCheckbox("Alt", func(on bool) { got = on })

	test.Tap(box)
	if !box.checked || !got {
		t.Errorf("after a tap checked=%v reported=%v, want both true", box.checked, got)
	}
}

func TestKeyFieldTakesOnlyBindableKeys(t *testing.T) {
	newTestApp(t)
	keys := hotkey.Keys()
	var reported []string
	field := newKeyField()
	field.reset(keys[0])
	field.onChanged = func(key string) { reported = append(reported, key) }

	field.KeyDown(&fyne.KeyEvent{Name: fyne.KeyA})
	field.KeyDown(&fyne.KeyEvent{Name: fyne.KeyName(keys[0])}) // already shown
	field.KeyDown(&fyne.KeyEvent{Name: fyne.KeyName(keys[1])})

	if !slices.Equal(reported, []string{keys[1]}) || field.selected != keys[1] || field.current != keys[0] {
		t.Errorf("reported %v, selected %q, current %q; want one report of %q over %q",
			reported, field.selected, field.current, keys[1], keys[0])
	}
}

func TestSettingsFormShowsAndReturnsAConfiguration(t *testing.T) {
	newTestApp(t)
	mods := hotkey.Modifiers()[:2]
	want := config.Config{
		Hotkey:       config.Hotkey{Key: hotkey.DefaultKey(), Modifiers: mods},
		Autostart:    true,
		SwitchLayout: false,
	}

	got := newSettingsForm(want).config()

	if got.Hotkey.Key != want.Hotkey.Key || !slices.Equal(got.Hotkey.Modifiers, mods) ||
		got.Autostart != want.Autostart || got.SwitchLayout != want.SwitchLayout {
		t.Errorf("form returned %+v, want %+v", got, want)
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
