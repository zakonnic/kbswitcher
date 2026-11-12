package layouts

/*
#cgo LDFLAGS: -framework Carbon -framework CoreFoundation
#include <stdlib.h>
#include "layouts_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/zakonnic/kbswitcher/internal/keymap"
)

// virtualKeys maps every tracked key position to its macOS virtual key code
// from Carbon's Events.h. The codes describe where a key sits, not what it
// types.
var virtualKeys = [keymap.KeyCount]C.ushort{
	keymap.KeyBackquote: 0x32, keymap.Key1: 0x12, keymap.Key2: 0x13, keymap.Key3: 0x14,
	keymap.Key4: 0x15, keymap.Key5: 0x17, keymap.Key6: 0x16, keymap.Key7: 0x1A,
	keymap.Key8: 0x1C, keymap.Key9: 0x19, keymap.Key0: 0x1D,
	keymap.KeyMinus: 0x1B, keymap.KeyEqual: 0x18,

	keymap.KeyQ: 0x0C, keymap.KeyW: 0x0D, keymap.KeyE: 0x0E, keymap.KeyR: 0x0F,
	keymap.KeyT: 0x11, keymap.KeyY: 0x10, keymap.KeyU: 0x20, keymap.KeyI: 0x22,
	keymap.KeyO: 0x1F, keymap.KeyP: 0x23,
	keymap.KeyBracketLeft: 0x21, keymap.KeyBracketRight: 0x1E, keymap.KeyBackslash: 0x2A,

	keymap.KeyA: 0x00, keymap.KeyS: 0x01, keymap.KeyD: 0x02, keymap.KeyF: 0x03,
	keymap.KeyG: 0x05, keymap.KeyH: 0x04, keymap.KeyJ: 0x26, keymap.KeyK: 0x28,
	keymap.KeyL: 0x25, keymap.KeySemicolon: 0x29, keymap.KeyQuote: 0x27,

	keymap.KeyZ: 0x06, keymap.KeyX: 0x07, keymap.KeyC: 0x08, keymap.KeyV: 0x09,
	keymap.KeyB: 0x0B, keymap.KeyN: 0x2D, keymap.KeyM: 0x2E,
	keymap.KeyComma: 0x2B, keymap.KeyPeriod: 0x2F, keymap.KeySlash: 0x2C,

	keymap.KeyIntlBackslash: 0x0A, // kVK_ISO_Section
	keymap.KeyIntlRo:        0x5E, // kVK_JIS_Underscore
	keymap.KeyIntlYen:       0x5D, // kVK_JIS_Yen
}

// Reader reads keyboard layouts from macOS.
type Reader struct{}

// Open prepares a reader. macOS needs no handle of its own.
func Open() (*Reader, error) { return &Reader{}, nil }

// Close releases the reader.
func (r *Reader) Close() error { return nil }

// List returns the enabled layouts in the order the input menu shows them.
func (r *Reader) List() ([]*keymap.Layout, error) {
	native := make([]C.kbs_layout, C.KBS_MAX_LAYOUTS)
	keys := virtualKeys // a copy, so the pointer handed to C stays valid for the call

	written := C.kbs_list_layouts(&native[0], C.int(len(native)),
		(*C.ushort)(unsafe.Pointer(&keys[0])), C.int(len(keys)))
	if written < 0 {
		return nil, fmt.Errorf("the list of input sources could not be read (code %d)", int(written))
	}

	layouts := make([]*keymap.Layout, 0, int(written))
	for i := range int(written) {
		layout := keymap.NewLayout(
			C.GoString(&native[i].id[0]),
			C.GoString(&native[i].name[0]),
			charsFromNative(&native[i]))
		if layout.Size() == 0 {
			continue // an input method rather than a keyboard layout
		}
		layouts = append(layouts, layout)
	}
	if len(layouts) == 0 {
		return nil, errors.New("no usable keyboard layout among those enabled")
	}
	return layouts, nil
}

// Active returns the identifier of the layout in use.
func (r *Reader) Active() (string, error) {
	var buf [C.KBS_TEXT_LENGTH]C.char
	if code := C.kbs_current_layout(&buf[0], C.int(len(buf))); code != 0 {
		return "", fmt.Errorf("the current input source could not be read (code %d)", int(code))
	}
	return C.GoString(&buf[0]), nil
}

// Activate switches the system to the given layout.
func (r *Reader) Activate(id string) error {
	identifier := C.CString(id)
	defer C.free(unsafe.Pointer(identifier))

	if code := C.kbs_select_layout(identifier); code != 0 {
		return fmt.Errorf("cannot switch to %q (code %d)", id, int(code))
	}
	return nil
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
