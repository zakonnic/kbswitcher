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
	keyX     = 0x07
	keyC     = 0x08
	keyV     = 0x09
	keyLeft  = 0x7B
	keyRight = 0x7C
)

// Modifier masks of a CGEvent (the kCGEventFlagMask values).
const (
	flagShift   = 0x00020000
	flagControl = 0x00040000
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

// SelectToLineStart presses Command+Shift+Left: Home scrolls on a Mac.
func (k *Keyboard) SelectToLineStart() error {
	return k.send(keyLeft, flagCommand|flagShift, 1)
}

// SelectRight presses Shift+Right n times.
func (k *Keyboard) SelectRight(n int) error {
	return k.send(keyRight, flagShift, n)
}

// CollapseSelectionRight presses Right.
func (k *Keyboard) CollapseSelectionRight() error {
	return k.send(keyRight, 0, 1)
}

// Nudge presses Left, or Right with back set.
func (k *Keyboard) Nudge(back bool) error {
	if back {
		return k.send(keyRight, 0, 1)
	}
	return k.send(keyLeft, 0, 1)
}

// Copy presses Command+C, which no shell takes for an interrupt.
func (k *Keyboard) Copy(bool) error { return k.send(keyC, flagCommand, 1) }

// Cut presses Command+X.
func (k *Keyboard) Cut() error { return k.send(keyX, flagCommand, 1) }

// Paste presses Command+V.
func (k *Keyboard) Paste() error { return k.send(keyV, flagCommand, 1) }

func (k *Keyboard) send(key uint16, flags uint64, times int) error {
	if times < 1 {
		return nil
	}
	C.kbs_send_key(C.ushort(key), C.ulonglong(flags), C.int(times))
	return nil
}
