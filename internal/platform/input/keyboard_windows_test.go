package input

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// catch records the key events the keyboard would press, one slice a call,
// and presses none.
func catch(t *testing.T) *[][]keyboardInput {
	t.Helper()
	pretendHeld(t)
	var calls [][]keyboardInput
	previous := sendInput
	sendInput = func(events []keyboardInput) (int, error) {
		calls = append(calls, slices.Clone(events))
		return len(events), nil
	}
	t.Cleanup(func() { sendInput = previous })
	return &calls
}

// keys renders events as down and up arrows on key codes, or characters.
func keys(events []keyboardInput) string {
	var b strings.Builder
	for _, e := range events {
		switch {
		case e.flags&keyeventfKeyUp != 0:
			b.WriteString("^")
		default:
			b.WriteString("v")
		}
		if e.flags&keyeventfUnicode != 0 {
			b.WriteRune(rune(e.scan))
		} else {
			b.WriteString(map[uint16]string{vkShift: "Shift", vkControl: "Ctrl", vkHome: "Home", vkLeft: "Left",
				vkRight: "Right", vkBack: "Back", vkInsert: "Ins", vkC: "C", vkX: "X", vkV: "V"}[e.vk])
		}
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

func TestKeyboardPressesWhatEachActionTakes(t *testing.T) {
	calls := catch(t)
	k, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := Permission(true); err != nil {
		t.Errorf("Permission: %v", err)
	}

	tests := []struct {
		name   string
		action func() error
		want   string
	}{
		{"select to the line start", k.SelectToLineStart, "vShift vHome ^Home ^Shift"},
		{"select right", func() error { return k.SelectRight(2) }, "vShift vRight ^Right vRight ^Right ^Shift"},
		{"collapse", k.CollapseSelectionRight, "vRight ^Right"},
		{"nudge", func() error { return k.Nudge(false) }, "vLeft ^Left"},
		{"nudge back", func() error { return k.Nudge(true) }, "vRight ^Right"},
		{"mark", k.Mark, "vShift vLeft ^Left ^Shift"},
		{"retype", func() error { return k.Retype(true, 2, "дa") }, "vRight ^Right vBack ^Back vBack ^Back vд ^д va ^a"},
		{"copy", func() error { return k.Copy(false) }, "vCtrl vC ^C ^Ctrl"},
		{"copy gently", func() error { return k.Copy(true) }, "vCtrl vIns ^Ins ^Ctrl"},
		{"cut", k.Cut, "vCtrl vX ^X ^Ctrl"},
		{"paste", k.Paste, "vCtrl vV ^V ^Ctrl"},
	}
	for _, tt := range tests {
		*calls = nil
		if err := tt.action(); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if len(*calls) != 1 || keys((*calls)[0]) != tt.want {
			t.Errorf("%s pressed %d bursts %v, want one: %s", tt.name, len(*calls), *calls, tt.want)
		}
	}

	*calls = nil
	if err := k.ReleaseModifiers(); err != nil || len(*calls) != 0 {
		t.Errorf("releasing nothing held sent %v, %v", *calls, err)
	}
	if err := k.Retype(false, 0, ""); err != nil || len(*calls) != 0 {
		t.Errorf("an empty retype sent %v, %v", *calls, err)
	}
}

func TestKeyboardReportsKeysWindowsDropped(t *testing.T) {
	pretendHeld(t)
	previous := sendInput
	sendInput = func(events []keyboardInput) (int, error) { return len(events) - 1, errors.New("blocked") }
	t.Cleanup(func() { sendInput = previous })
	if err := (&Keyboard{}).Paste(); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Paste = %v", err)
	}
}

func TestRetypeSendsCharactersBeyondTheBMPAsTwoUnits(t *testing.T) {
	calls := catch(t)
	if err := (&Keyboard{}).Retype(false, 0, "\U0001F44D"); err != nil {
		t.Fatal(err)
	}
	if n := len((*calls)[0]); n != 4 {
		t.Errorf("an emoji takes %d events, want two units pressed and released", n)
	}
}
