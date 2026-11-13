package hotkey

import (
	"errors"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// keys maps the portable key names, US legends for the keys whose legend
// changes with the layout, to virtual key codes and the scan codes GLFW
// reports, with 0x100 set for the extended ones.
var keys = map[string]native{
	"A": {0x41, 0x1E}, "B": {0x42, 0x30}, "C": {0x43, 0x2E}, "D": {0x44, 0x20}, "E": {0x45, 0x12}, "F": {0x46, 0x21},
	"G": {0x47, 0x22}, "H": {0x48, 0x23}, "I": {0x49, 0x17}, "J": {0x4A, 0x24}, "K": {0x4B, 0x25}, "L": {0x4C, 0x26},
	"M": {0x4D, 0x32}, "N": {0x4E, 0x31}, "O": {0x4F, 0x18}, "P": {0x50, 0x19}, "Q": {0x51, 0x10}, "R": {0x52, 0x13},
	"S": {0x53, 0x1F}, "T": {0x54, 0x14}, "U": {0x55, 0x16}, "V": {0x56, 0x2F}, "W": {0x57, 0x11}, "X": {0x58, 0x2D},
	"Y": {0x59, 0x15}, "Z": {0x5A, 0x2C}, "1": {0x31, 0x02}, "2": {0x32, 0x03}, "3": {0x33, 0x04}, "4": {0x34, 0x05},
	"5": {0x35, 0x06}, "6": {0x36, 0x07}, "7": {0x37, 0x08}, "8": {0x38, 0x09}, "9": {0x39, 0x0A}, "0": {0x30, 0x0B},
	"Backquote": {0xC0, 0x29}, "Minus": {0xBD, 0x0C}, "Equal": {0xBB, 0x0D},
	"BracketLeft": {0xDB, 0x1A}, "BracketRight": {0xDD, 0x1B}, "Backslash": {0xDC, 0x2B},
	"Semicolon": {0xBA, 0x27}, "Quote": {0xDE, 0x28}, "Comma": {0xBC, 0x33},
	"Period": {0xBE, 0x34}, "Slash": {0xBF, 0x35}, "IntlBackslash": {0xE2, 0x56},
	"Space": {0x20, 0x39}, "Enter": {0x0D, 0x1C}, "Tab": {0x09, 0x0F},
	"Backspace": {0x08, 0x0E}, "Escape": {0x1B, 0x01},
	"Insert": {0x2D, 0x152}, "Delete": {0x2E, 0x153}, "Home": {0x24, 0x147}, "End": {0x23, 0x14F},
	"PageUp": {0x21, 0x149}, "PageDown": {0x22, 0x151},
	"Left": {0x25, 0x14B}, "Up": {0x26, 0x148}, "Right": {0x27, 0x14D}, "Down": {0x28, 0x150},
	"F1": {0x70, 0x3B}, "F2": {0x71, 0x3C}, "F3": {0x72, 0x3D}, "F4": {0x73, 0x3E},
	"F5": {0x74, 0x3F}, "F6": {0x75, 0x40}, "F7": {0x76, 0x41}, "F8": {0x77, 0x42},
	"F9": {0x78, 0x43}, "F10": {0x79, 0x44}, "F11": {0x7A, 0x57}, "F12": {0x7B, 0x58},
	"F13": {0x7C, 0x64}, "F14": {0x7D, 0x65}, "F15": {0x7E, 0x66}, "F16": {0x7F, 0x67},
	"F17": {0x80, 0x68}, "F18": {0x81, 0x69}, "F19": {0x82, 0x6A}, "F20": {0x83, 0x6B},
	"F21": {0x84, 0x6C}, "F22": {0x85, 0x6D}, "F23": {0x86, 0x6E}, "F24": {0x87, 0x76},
	"Pause": {0x13, 0x45}, "ScrollLock": {0x91, 0x46}, "PrintScreen": {0x2C, 0x137},
	"CapsLock": {0x14, 0x3A}, "NumLock": {0x90, 0x145}, "Menu": {0x5D, 0x15D},
	"Numpad0": {0x60, 0x52}, "Numpad1": {0x61, 0x4F}, "Numpad2": {0x62, 0x50}, "Numpad3": {0x63, 0x51},
	"Numpad4": {0x64, 0x4B}, "Numpad5": {0x65, 0x4C}, "Numpad6": {0x66, 0x4D}, "Numpad7": {0x67, 0x47},
	"Numpad8": {0x68, 0x48}, "Numpad9": {0x69, 0x49}, "NumpadMultiply": {0x6A, 0x37},
	"NumpadAdd": {0x6B, 0x4E}, "NumpadSubtract": {0x6D, 0x4A}, "NumpadDecimal": {0x6E, 0x53},
	"NumpadDivide": {0x6F, 0x135},
}

// modifierLabels are the Windows names of the modifiers.
var modifierLabels = map[string]string{"Super": "Win"}

var procMapVirtualKeyExW = windows.NewLazySystemDLL("user32.dll").NewProc("MapVirtualKeyExW")

const (
	mapvkVSCToVK = 1
	// vkCancel is what Windows sends for Pause while Ctrl is held.
	vkCancel = 0x03
)

// byLegendVK names the keys whose legend the layout decides by their virtual
// key code.
var byLegendVK = sync.OnceValue(func() map[uint32]string {
	names := map[uint32]string{}
	for name, k := range keys {
		if k.scan < 0x100 && Typing(name) && !strings.HasPrefix(name, "Numpad") {
			names[k.code] = name
		}
	}
	return names
})

// layoutKey names a letter, digit or punctuation key after the virtual key
// the calling thread's layout gives it, which is what RegisterHotKey will
// wait for: on an AZERTY keyboard the key US calls Q is A.
func layoutKey(code int) (string, bool) {
	if code <= 0 || code >= 0x100 {
		return "", false
	}
	if name, ok := byScanCode()[code]; !ok || byLegendVK()[keys[name].code] != name {
		return "", false
	}
	vk, _, _ := procMapVirtualKeyExW.Call(uintptr(code), mapvkVSCToVK, uintptr(windows.GetKeyboardLayout(0)))
	name, ok := byLegendVK()[uint32(vk)] //nolint:gosec // a virtual key code is a byte
	return name, ok
}

// boundCode is the virtual key Windows reports for a key pressed with the
// combination's modifiers.
func boundCode(c Combo) uint32 {
	if c.Key == "Pause" && c.Mods.Has(ModCtrl) {
		return vkCancel
	}
	return keys[c.Key].code
}

// platformCheck refuses what Windows cannot report: with Num Lock on, Shift
// turns a keypad digit into Home, End, the arrows and so on.
func platformCheck(c Combo) error {
	if c.Mods.Has(ModShift) && (strings.HasPrefix(c.Key, "Numpad") && len(c.Key) == len("Numpad0") || c.Key == "NumpadDecimal") {
		return errors.New("a keypad digit cannot go with Shift, which turns it into an arrow")
	}
	return nil
}
