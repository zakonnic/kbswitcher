package input

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include "input_darwin.h"
*/
import "C"

import "errors"

// Virtual key codes from Carbon's Events.h. They name positions on the
// keyboard and do not change with the layout.
const (
	keyC     = 0x08
	keyV     = 0x09
	keyLeft  = 0x7B
	keyRight = 0x7C
)

// Modifier masks of a CGEvent (the kCGEventFlagMask values).
const (
	flagShift   = 0x00020000
	flagControl = 0x00040000
	flagOption  = 0x00080000
	flagCommand = 0x00100000
)

// Keyboard sends synthetic key presses through the window server.
type Keyboard struct{}

// Open prepares the sender.
func Open() (*Keyboard, error) { return &Keyboard{}, nil }

// Close releases the sender.
func (k *Keyboard) Close() error { return nil }

// Permission reports whether macOS lets kbswitcher post key events, which
// needs the Accessibility permission. With prompt set, macOS shows the panel
// that leads to the settings page.
func Permission(prompt bool) error {
	ask := C.int(0)
	if prompt {
		ask = 1
	}
	if C.kbs_accessibility_trusted(ask) == 1 {
		return nil
	}
	return errors.New("kbswitcher needs the Accessibility permission to replace text: " +
		"open System Settings, Privacy & Security, Accessibility, and allow kbswitcher")
}

// ReleaseModifiers has nothing to do on macOS: every posted event carries the
// modifiers it needs, and the keys the user holds do not reach the focused
// application.
func (k *Keyboard) ReleaseModifiers() error { return nil }

// SelectWordLeft presses Option+Shift+Left the given number of times.
func (k *Keyboard) SelectWordLeft(times int) error {
	return k.send(keyLeft, flagOption|flagShift, times)
}

// SelectCharsLeft presses Shift+Left n times.
func (k *Keyboard) SelectCharsLeft(n int) error {
	return k.send(keyLeft, flagShift, n)
}

// CollapseSelectionRight presses Right.
func (k *Keyboard) CollapseSelectionRight() error {
	return k.send(keyRight, 0, 1)
}

// Copy presses Command+C.
func (k *Keyboard) Copy() error { return k.send(keyC, flagCommand, 1) }

// Paste presses Command+V.
func (k *Keyboard) Paste() error { return k.send(keyV, flagCommand, 1) }

func (k *Keyboard) send(key uint16, flags uint64, times int) error {
	if times < 1 {
		return nil
	}
	C.kbs_send_key(C.ushort(key), C.ulonglong(flags), C.int(times))
	return nil
}
