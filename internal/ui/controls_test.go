package ui

import (
	"slices"
	"testing"

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

func TestPopUpButtonReportsANewChoiceOnce(t *testing.T) {
	newTestApp(t)
	var reported []string
	pick := newPopUpButton([]string{"F1", "F2"}, func(s string) { reported = append(reported, s) })

	pick.setSelected("F2")
	pick.setSelected("F2")

	if !slices.Equal(reported, []string{"F2"}) || pick.selected != "F2" {
		t.Errorf("reported %v, selected %q; want one report of F2", reported, pick.selected)
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
