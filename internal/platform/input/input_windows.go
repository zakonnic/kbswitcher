package input

import (
	"fmt"
	"slices"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

// Virtual key codes name physical keys and do not change with the layout, so
// Ctrl+C stays Ctrl+C in a Cyrillic layout.
const (
	vkBack     = 0x08
	vkShift    = 0x10
	vkControl  = 0x11
	vkAlt      = 0x12
	vkHome     = 0x24
	vkInsert   = 0x2D
	vkLeft     = 0x25
	vkRight    = 0x27
	vkC        = 0x43
	vkX        = 0x58
	vkV        = 0x56
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLAlt     = 0xA4
	vkRAlt     = 0xA5
)

const (
	inputKeyboard     = 1
	keyeventfExtended = 0x0001
	keyeventfKeyUp    = 0x0002
	keyeventfUnicode  = 0x0004
)

// modifier is one side of a modifier key. Windows tracks the two sides
// separately, and only a release naming the side that is down lets go of it.
type modifier struct {
	vk, scan uint16
	extended bool
	// class is the side-agnostic code shortcuts are built from.
	class uint16
}

var modifiers = []modifier{
	{vk: vkLShift, scan: 0x2A, class: vkShift},
	{vk: vkRShift, scan: 0x36, class: vkShift},
	{vk: vkLControl, scan: 0x1D, class: vkControl},
	{vk: vkRControl, scan: 0x1D, extended: true, class: vkControl},
	{vk: vkLAlt, scan: 0x38, class: vkAlt},
	{vk: vkRAlt, scan: 0x38, extended: true, class: vkAlt},
	{vk: vkLWin, scan: 0x5B, extended: true, class: vkLWin},
	{vk: vkRWin, scan: 0x5C, extended: true, class: vkRWin},
}

// keyDown reports whether a key is physically held. A variable, so that tests
// can pretend.
var keyDown = func(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}

// keyboardInput mirrors the Win32 INPUT structure holding a KEYBDINPUT,
// including the padding of the union on 64-bit Windows. SendInput rejects a
// structure whose size it does not recognize, so a mismatch fails loudly.
type keyboardInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uint64
	_     [8]byte
}

// Keyboard sends synthetic key presses through the Win32 input queue.
type Keyboard struct{}

// Open prepares the sender. Windows needs no setup or permission for it.
func Open() (*Keyboard, error) { return &Keyboard{}, nil }

// Close releases the sender.
func (k *Keyboard) Close() error { return nil }

// Permission reports whether the system lets kbswitcher send key presses.
// Windows asks for none.
func Permission(bool) error { return nil }

// ReleaseModifiers lets go of every modifier that is currently held, so that
// the hotkey still under the user's fingers cannot alter the shortcuts sent
// next.
func (k *Keyboard) ReleaseModifiers() error { return k.send(releases()) }

// SelectToLineStart presses Shift+Home.
func (k *Keyboard) SelectToLineStart() error {
	return k.send(withModifiers([]uint16{vkShift}, repeat(vkHome, 1, keyeventfExtended)))
}

// SelectRight presses Shift+Right n times.
func (k *Keyboard) SelectRight(n int) error {
	return k.send(withModifiers([]uint16{vkShift}, repeat(vkRight, n, keyeventfExtended)))
}

// CollapseSelectionRight presses Right.
func (k *Keyboard) CollapseSelectionRight() error {
	return k.send(withModifiers(nil, repeat(vkRight, 1, keyeventfExtended)))
}

// Nudge presses Left, or Right with back set.
func (k *Keyboard) Nudge(back bool) error {
	key := uint16(vkLeft)
	if back {
		key = vkRight
	}
	return k.send(withModifiers(nil, repeat(key, 1, keyeventfExtended)))
}

// Mark presses Shift+Left.
func (k *Keyboard) Mark() error {
	return k.send(withModifiers([]uint16{vkShift}, repeat(vkLeft, 1, keyeventfExtended)))
}

// Retype presses Right when right is set, then Backspace erase times, and
// types text, all in one burst. The characters go as Unicode, whatever the
// layout, so text needs no keys of its own; it holds no control characters.
func (k *Keyboard) Retype(right bool, erase int, text string) error {
	var events []keyboardInput
	if right {
		events = append(events, repeat(vkRight, 1, keyeventfExtended)...)
	}
	events = append(events, repeat(vkBack, erase, 0)...)
	for _, unit := range utf16.Encode([]rune(text)) {
		events = append(events,
			keyboardInput{typ: inputKeyboard, scan: unit, flags: keyeventfUnicode},
			keyboardInput{typ: inputKeyboard, scan: unit, flags: keyeventfUnicode | keyeventfKeyUp})
	}
	return k.send(withModifiers(nil, events))
}

// Copy presses Ctrl+C, or with gentle set Ctrl+Insert, which copies as well
// and which no shell takes for an interrupt.
func (k *Keyboard) Copy(gentle bool) error {
	if gentle {
		return k.send(withModifiers([]uint16{vkControl}, repeat(vkInsert, 1, keyeventfExtended)))
	}
	return k.send(withModifiers([]uint16{vkControl}, repeat(vkC, 1, 0)))
}

// Cut presses Ctrl+X.
func (k *Keyboard) Cut() error {
	return k.send(withModifiers([]uint16{vkControl}, repeat(vkX, 1, 0)))
}

// Paste presses Ctrl+V.
func (k *Keyboard) Paste() error {
	return k.send(withModifiers([]uint16{vkControl}, repeat(vkV, 1, 0)))
}

// repeat builds n press-and-release pairs for one key.
func repeat(vk uint16, n int, flags uint32) []keyboardInput {
	if n < 1 {
		return nil
	}
	events := make([]keyboardInput, 0, n*2)
	for range n {
		events = append(events,
			keyboardInput{typ: inputKeyboard, vk: vk, flags: flags},
			keyboardInput{typ: inputKeyboard, vk: vk, flags: flags | keyeventfKeyUp})
	}
	return events
}

// withModifiers wraps events in the presses and releases of the modifier keys
// a shortcut needs, after letting go of every other modifier the user holds.
// Everything goes to Windows in one call, because the keyboard repeats a held
// modifier and would press it again within milliseconds of a separate release.
func withModifiers(mods []uint16, events []keyboardInput) []keyboardInput {
	if len(events) == 0 {
		return nil
	}
	wrapped := releases(mods...)
	for _, vk := range mods {
		wrapped = append(wrapped, keyboardInput{typ: inputKeyboard, vk: vk})
	}
	wrapped = append(wrapped, events...)
	for i := len(mods) - 1; i >= 0; i-- {
		wrapped = append(wrapped, keyboardInput{typ: inputKeyboard, vk: mods[i], flags: keyeventfKeyUp})
	}
	return wrapped
}

// releases builds a key-up for every held modifier whose class is not in
// keep, each by its own scan code so that the right-hand keys are released
// too.
func releases(keep ...uint16) []keyboardInput {
	var events []keyboardInput
	for _, m := range modifiers {
		if slices.Contains(keep, m.class) || !keyDown(m.vk) {
			continue
		}
		flags := uint32(keyeventfKeyUp)
		if m.extended {
			flags |= keyeventfExtended
		}
		events = append(events, keyboardInput{typ: inputKeyboard, vk: m.vk, scan: m.scan, flags: flags})
	}
	return events
}

// send hands the events to Windows in a single call, so that no key press of
// the user can slip in between them.
func (k *Keyboard) send(events []keyboardInput) error {
	if len(events) == 0 {
		return nil
	}
	if sent, err := sendInput(events); sent != len(events) {
		return fmt.Errorf("SendInput delivered %d of %d events: %w", sent, len(events), err)
	}
	return nil
}

// sendInput presses keys in whatever window has the focus; tests catch them
// instead.
var sendInput = func(events []keyboardInput) (int, error) {
	sent, _, err := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]))
	return int(sent), err
}
