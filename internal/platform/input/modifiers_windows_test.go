package input

import (
	"slices"
	"testing"
)

// pretendHeld makes keyDown report the given keys as held for one test.
func pretendHeld(t *testing.T, keys ...uint16) {
	t.Helper()
	previous := keyDown
	keyDown = func(vk uint16) bool { return slices.Contains(keys, vk) }
	t.Cleanup(func() { keyDown = previous })
}

func TestShortcutsLetGoOfHeldModifiersFirst(t *testing.T) {
	pretendHeld(t, vkRShift, vkLAlt, vkRControl)

	got := withModifiers([]uint16{vkControl}, repeat(vkC, 1, 0))

	// Right Shift and left Alt are released by their own scan codes before
	// Ctrl goes down; the held right Ctrl is what the shortcut wants anyway.
	want := []keyboardInput{
		{typ: inputKeyboard, vk: vkRShift, scan: 0x36, flags: keyeventfKeyUp},
		{typ: inputKeyboard, vk: vkLAlt, scan: 0x38, flags: keyeventfKeyUp},
		{typ: inputKeyboard, vk: vkControl},
		{typ: inputKeyboard, vk: vkC},
		{typ: inputKeyboard, vk: vkC, flags: keyeventfKeyUp},
		{typ: inputKeyboard, vk: vkControl, flags: keyeventfKeyUp},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Ctrl+C with Shift, Alt and Ctrl held =\n%+v\nwant\n%+v", got, want)
	}
}

func TestReleasesNameTheRightHandSide(t *testing.T) {
	pretendHeld(t, vkRAlt, vkRWin)

	got := releases()

	want := []keyboardInput{
		{typ: inputKeyboard, vk: vkRAlt, scan: 0x38, flags: keyeventfKeyUp | keyeventfExtended},
		{typ: inputKeyboard, vk: vkRWin, scan: 0x5C, flags: keyeventfKeyUp | keyeventfExtended},
	}
	if !slices.Equal(got, want) {
		t.Errorf("releases with right Alt and right Win held =\n%+v\nwant\n%+v", got, want)
	}
}

func TestNothingHeldReleasesNothing(t *testing.T) {
	pretendHeld(t)

	if got := withModifiers([]uint16{vkShift}, repeat(vkLeft, 1, keyeventfExtended)); len(got) != 4 {
		t.Errorf("Shift+Left with nothing held has %d events, want 4", len(got))
	}
}
