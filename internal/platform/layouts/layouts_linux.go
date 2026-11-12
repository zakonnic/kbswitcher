package layouts

/*
#cgo pkg-config: x11 xkbcommon
#include <stdlib.h>
#include "layouts_linux.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/zakonnic/kbswitcher/internal/keymap"
)

// keycodes maps every tracked key position to its X11 keycode: the Linux
// input event code plus eight, the same on every evdev-driven session.
var keycodes = [keymap.KeyCount]C.uint{
	keymap.KeyBackquote: 49, keymap.Key1: 10, keymap.Key2: 11, keymap.Key3: 12,
	keymap.Key4: 13, keymap.Key5: 14, keymap.Key6: 15, keymap.Key7: 16,
	keymap.Key8: 17, keymap.Key9: 18, keymap.Key0: 19,
	keymap.KeyMinus: 20, keymap.KeyEqual: 21,

	keymap.KeyQ: 24, keymap.KeyW: 25, keymap.KeyE: 26, keymap.KeyR: 27,
	keymap.KeyT: 28, keymap.KeyY: 29, keymap.KeyU: 30, keymap.KeyI: 31,
	keymap.KeyO: 32, keymap.KeyP: 33,
	keymap.KeyBracketLeft: 34, keymap.KeyBracketRight: 35, keymap.KeyBackslash: 51,

	keymap.KeyA: 38, keymap.KeyS: 39, keymap.KeyD: 40, keymap.KeyF: 41,
	keymap.KeyG: 42, keymap.KeyH: 43, keymap.KeyJ: 44, keymap.KeyK: 45,
	keymap.KeyL: 46, keymap.KeySemicolon: 47, keymap.KeyQuote: 48,

	keymap.KeyZ: 52, keymap.KeyX: 53, keymap.KeyC: 54, keymap.KeyV: 55,
	keymap.KeyB: 56, keymap.KeyN: 57, keymap.KeyM: 58,
	keymap.KeyComma: 59, keymap.KeyPeriod: 60, keymap.KeySlash: 61,

	keymap.KeyIntlBackslash: 94,
	keymap.KeyIntlRo:        97,
	keymap.KeyIntlYen:       132,
}

// Reader reads keyboard layouts from the X server. It keeps a connection of
// its own, because Xlib connections must not be shared between threads.
type Reader struct {
	display *C.Display
}

// Open connects to the X server.
func Open() (*Reader, error) {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return nil, errors.New("cannot open the X display: kbswitcher needs an X11 session " +
			"(a Wayland session must be running XWayland)")
	}
	return &Reader{display: display}, nil
}

// Close drops the connection to the X server.
func (r *Reader) Close() error {
	if r.display != nil {
		C.XCloseDisplay(r.display)
		r.display = nil
	}
	return nil
}

// List returns the configured groups, in the order the session cycles them.
func (r *Reader) List() ([]*keymap.Layout, error) {
	native := make([]C.kbs_layout, C.KBS_MAX_LAYOUTS)
	keys := keycodes // a copy, so the pointer handed to C stays valid for the call

	written := C.kbs_list_layouts(r.display, &native[0], C.int(len(native)),
		(*C.uint)(unsafe.Pointer(&keys[0])), C.int(len(keys)))
	if written <= 0 {
		return nil, fmt.Errorf("the keyboard groups could not be read (code %d)", int(written))
	}

	layouts := make([]*keymap.Layout, 0, int(written))
	for i := range int(written) {
		layout := keymap.NewLayout(groupID(i), C.GoString(&native[i].name[0]), charsFromNative(&native[i]))
		if layout.Size() == 0 {
			continue
		}
		layouts = append(layouts, layout)
	}
	if len(layouts) == 0 {
		return nil, errors.New("no usable keyboard group is configured")
	}
	return layouts, nil
}

// Active returns the identifier of the group in use.
func (r *Reader) Active() (string, error) {
	group := C.kbs_current_group(r.display)
	if group < 0 {
		return "", errors.New("the current keyboard group could not be read")
	}
	return groupID(int(group)), nil
}

// Activate switches the session to the given group.
func (r *Reader) Activate(id string) error {
	group, err := groupIndex(id)
	if err != nil {
		return err
	}
	if code := C.kbs_select_group(r.display, C.int(group)); code != 0 {
		return fmt.Errorf("cannot switch to %s (code %d)", id, int(code))
	}
	return nil
}

// groupID names a group by its slot number: X has no stable identifier for a
// layout.
func groupID(index int) string { return "group-" + strconv.Itoa(index+1) }

func groupIndex(id string) (int, error) {
	const prefix = "group-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return 0, fmt.Errorf("unknown layout %q", id)
	}
	number, err := strconv.Atoi(id[len(prefix):])
	if err != nil || number < 1 || number > C.KBS_MAX_LAYOUTS {
		return 0, fmt.Errorf("unknown layout %q", id)
	}
	return number - 1, nil
}

func charsFromNative(native *C.kbs_layout) keymap.Chars {
	var chars keymap.Chars
	for key := keymap.Key(0); key < keymap.KeyCount; key++ {
		for level := keymap.Level(0); level < keymap.LevelCount; level++ {
			index := int(key)*C.KBS_LEVELS + int(level)
			chars[key][level] = rune(native.chars[index])
		}
	}
	return chars
}

// Locale is only known for Windows layouts; identifiers here already read as
// names.
func Locale(string) (string, bool) { return "", false }
