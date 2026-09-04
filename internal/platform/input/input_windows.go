package input

import (
	"fmt"
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
	vkShift   = 0x10
	vkControl = 0x11
	vkAlt     = 0x12
	vkLeft    = 0x25
	vkRight   = 0x27
	vkC       = 0x43
	vkV       = 0x56
	vkLWin    = 0x5B
	vkRWin    = 0x5C
)

var modifierKeys = []uint16{vkShift, vkControl, vkAlt, vkLWin, vkRWin}

const (
	inputKeyboard     = 1
	keyeventfExtended = 0x0001
	keyeventfKeyUp    = 0x0002
)

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
func (k *Keyboard) ReleaseModifiers() error {
	var events []keyboardInput
	for _, vk := range modifierKeys {
		if isKeyDown(vk) {
			events = append(events, keyboardInput{typ: inputKeyboard, vk: vk, flags: keyeventfKeyUp})
		}
	}
	return k.send(events)
}

func isKeyDown(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return state&0x8000 != 0
}

// SelectWordLeft presses Ctrl+Shift+Left the given number of times.
func (k *Keyboard) SelectWordLeft(times int) error {
	return k.send(withModifiers([]uint16{vkControl, vkShift}, repeat(vkLeft, times, keyeventfExtended)))
}

// SelectCharsLeft presses Shift+Left n times.
func (k *Keyboard) SelectCharsLeft(n int) error {
	return k.send(withModifiers([]uint16{vkShift}, repeat(vkLeft, n, keyeventfExtended)))
}

// CollapseSelectionRight presses Right, which leaves the caret at the end of
// the selection.
func (k *Keyboard) CollapseSelectionRight() error {
	return k.send(repeat(vkRight, 1, keyeventfExtended))
}

// Copy presses Ctrl+C.
func (k *Keyboard) Copy() error {
	return k.send(withModifiers([]uint16{vkControl}, repeat(vkC, 1, 0)))
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

// withModifiers wraps events in the presses and releases of the modifier keys.
func withModifiers(mods []uint16, events []keyboardInput) []keyboardInput {
	if len(events) == 0 {
		return nil
	}
	wrapped := make([]keyboardInput, 0, len(events)+len(mods)*2)
	for _, vk := range mods {
		wrapped = append(wrapped, keyboardInput{typ: inputKeyboard, vk: vk})
	}
	wrapped = append(wrapped, events...)
	for i := len(mods) - 1; i >= 0; i-- {
		wrapped = append(wrapped, keyboardInput{typ: inputKeyboard, vk: mods[i], flags: keyeventfKeyUp})
	}
	return wrapped
}

// send hands the events to Windows in a single call, so that no key press of
// the user can slip in between them.
func (k *Keyboard) send(events []keyboardInput) error {
	if len(events) == 0 {
		return nil
	}
	sent, _, err := procSendInput.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]))
	if int(sent) != len(events) {
		return fmt.Errorf("SendInput delivered %d of %d events: %w", sent, len(events), err)
	}
	return nil
}
