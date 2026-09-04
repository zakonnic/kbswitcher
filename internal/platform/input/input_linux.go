package input

/*
#cgo pkg-config: x11 xtst
#include <X11/Xlib.h>
#include <X11/XKBlib.h>
#include <X11/extensions/XTest.h>
*/
import "C"

import "errors"

// X11 keysyms of the keys kbswitcher presses.
const (
	keysymShift   = 0xFFE1
	keysymControl = 0xFFE3
	keysymAlt     = 0xFFE9
	keysymSuper   = 0xFFEB
	keysymLeft    = 0xFF51
	keysymRight   = 0xFF53
	keysymC       = 0x0063
	keysymV       = 0x0076
)

// Evdev keycodes of the same positions, used when a keysym cannot be looked
// up, which happens for the letters when no Latin group is configured.
const (
	fallbackShift   = 50
	fallbackControl = 37
	fallbackAlt     = 64
	fallbackSuper   = 133
	fallbackLeft    = 113
	fallbackRight   = 114
	fallbackC       = 54
	fallbackV       = 55
)

// X11 modifier masks, as they appear in the keyboard state.
const (
	maskShift   = 1 << 0
	maskControl = 1 << 2
	maskAlt     = 1 << 3 // Mod1
	maskSuper   = 1 << 6 // Mod4
)

// Keyboard sends synthetic key presses through the XTEST extension. It keeps
// an X connection of its own, because Xlib connections must not be shared
// between threads.
type Keyboard struct {
	display *C.Display

	shift, control, alt, super C.KeyCode
	left, right, c, v          C.KeyCode
}

// Open connects to the X server and checks that it can fake key presses.
func Open() (*Keyboard, error) {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return nil, errors.New("cannot open the X display: kbswitcher needs an X11 session " +
			"(a Wayland session must be running XWayland)")
	}

	var event, errorBase, major, minor C.int
	if C.XTestQueryExtension(display, &event, &errorBase, &major, &minor) == C.False {
		C.XCloseDisplay(display)
		return nil, errors.New("the X server does not offer the XTEST extension, " +
			"which kbswitcher needs to replace text")
	}

	k := &Keyboard{display: display}
	k.shift = k.keycode(keysymShift, fallbackShift)
	k.control = k.keycode(keysymControl, fallbackControl)
	k.alt = k.keycode(keysymAlt, fallbackAlt)
	k.super = k.keycode(keysymSuper, fallbackSuper)
	k.left = k.keycode(keysymLeft, fallbackLeft)
	k.right = k.keycode(keysymRight, fallbackRight)
	k.c = k.keycode(keysymC, fallbackC)
	k.v = k.keycode(keysymV, fallbackV)
	return k, nil
}

// Close drops the connection to the X server.
func (k *Keyboard) Close() error {
	if k.display != nil {
		C.XCloseDisplay(k.display)
		k.display = nil
	}
	return nil
}

// Permission reports whether the system lets kbswitcher send key presses. An
// X11 session asks for nothing beyond the connection Open has made.
func Permission(bool) error { return nil }

// ReleaseModifiers lets go of every modifier that is currently held: X applies
// the real modifier state to faked key presses, so the hotkey still under the
// user's fingers would otherwise alter the shortcuts sent next.
func (k *Keyboard) ReleaseModifiers() error {
	var state C.XkbStateRec
	if C.XkbGetState(k.display, C.XkbUseCoreKbd, &state) != C.Success {
		return nil
	}

	held := uint(state.mods)
	for _, modifier := range []struct {
		mask uint
		key  C.KeyCode
	}{
		{maskShift, k.shift},
		{maskControl, k.control},
		{maskAlt, k.alt},
		{maskSuper, k.super},
	} {
		if held&modifier.mask != 0 {
			C.XTestFakeKeyEvent(k.display, C.uint(modifier.key), C.False, C.CurrentTime)
		}
	}
	return k.flush()
}

// SelectWordLeft presses Ctrl+Shift+Left the given number of times.
func (k *Keyboard) SelectWordLeft(times int) error {
	return k.send([]C.KeyCode{k.control, k.shift}, k.left, times)
}

// SelectCharsLeft presses Shift+Left n times.
func (k *Keyboard) SelectCharsLeft(n int) error {
	return k.send([]C.KeyCode{k.shift}, k.left, n)
}

// CollapseSelectionRight presses Right.
func (k *Keyboard) CollapseSelectionRight() error {
	return k.send(nil, k.right, 1)
}

// Copy presses Ctrl+C.
func (k *Keyboard) Copy() error { return k.send([]C.KeyCode{k.control}, k.c, 1) }

// Paste presses Ctrl+V.
func (k *Keyboard) Paste() error { return k.send([]C.KeyCode{k.control}, k.v, 1) }

// send holds the modifiers down, presses a key a number of times, and lets the
// modifiers go.
func (k *Keyboard) send(modifiers []C.KeyCode, key C.KeyCode, times int) error {
	if times < 1 || key == 0 {
		return nil
	}
	for _, modifier := range modifiers {
		C.XTestFakeKeyEvent(k.display, C.uint(modifier), C.True, C.CurrentTime)
	}
	for range times {
		C.XTestFakeKeyEvent(k.display, C.uint(key), C.True, C.CurrentTime)
		C.XTestFakeKeyEvent(k.display, C.uint(key), C.False, C.CurrentTime)
	}
	for i := len(modifiers) - 1; i >= 0; i-- {
		C.XTestFakeKeyEvent(k.display, C.uint(modifiers[i]), C.False, C.CurrentTime)
	}
	return k.flush()
}

func (k *Keyboard) flush() error {
	if C.XFlush(k.display) == 0 {
		return errors.New("the connection to the X server was lost")
	}
	return nil
}

// keycode looks up the key that carries a keysym, falling back to a fixed
// position when the session has no group that types it.
func (k *Keyboard) keycode(keysym C.KeySym, fallback C.KeyCode) C.KeyCode {
	if code := C.XKeysymToKeycode(k.display, keysym); code != 0 {
		return code
	}
	return fallback
}
