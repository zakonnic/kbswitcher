package hotkey

// keys maps the portable key names, US legends for the keys whose legend
// changes with the layout, to X11 keysyms and the X keycodes GLFW reports, as
// the evdev driver numbers them. The X server turns a keysym into the keycode
// of the physical key, so binding does not depend on the keyboard. F13 and
// above have no keycode here: the stock keymap gives theirs other symbols.
var keys = map[string]native{
	"A": {0x61, 38}, "B": {0x62, 56}, "C": {0x63, 54}, "D": {0x64, 40}, "E": {0x65, 26}, "F": {0x66, 41},
	"G": {0x67, 42}, "H": {0x68, 43}, "I": {0x69, 31}, "J": {0x6A, 44}, "K": {0x6B, 45}, "L": {0x6C, 46},
	"M": {0x6D, 58}, "N": {0x6E, 57}, "O": {0x6F, 32}, "P": {0x70, 33}, "Q": {0x71, 24}, "R": {0x72, 27},
	"S": {0x73, 39}, "T": {0x74, 28}, "U": {0x75, 30}, "V": {0x76, 55}, "W": {0x77, 25}, "X": {0x78, 53},
	"Y": {0x79, 29}, "Z": {0x7A, 52}, "1": {0x31, 10}, "2": {0x32, 11}, "3": {0x33, 12}, "4": {0x34, 13},
	"5": {0x35, 14}, "6": {0x36, 15}, "7": {0x37, 16}, "8": {0x38, 17}, "9": {0x39, 18}, "0": {0x30, 19},
	"Backquote": {0x60, 49}, "Minus": {0x2D, 20}, "Equal": {0x3D, 21},
	"BracketLeft": {0x5B, 34}, "BracketRight": {0x5D, 35}, "Backslash": {0x5C, 51},
	"Semicolon": {0x3B, 47}, "Quote": {0x27, 48}, "Comma": {0x2C, 59},
	"Period": {0x2E, 60}, "Slash": {0x2F, 61}, "IntlBackslash": {0x3C, 94},
	"Space": {0x20, 65}, "Enter": {0xFF0D, 36}, "Tab": {0xFF09, 23},
	"Backspace": {0xFF08, 22}, "Escape": {0xFF1B, 9},
	"Insert": {0xFF63, 118}, "Delete": {0xFFFF, 119}, "Home": {0xFF50, 110}, "End": {0xFF57, 115},
	"PageUp": {0xFF55, 112}, "PageDown": {0xFF56, 117},
	"Left": {0xFF51, 113}, "Up": {0xFF52, 111}, "Right": {0xFF53, 114}, "Down": {0xFF54, 116},
	"F1": {0xFFBE, 67}, "F2": {0xFFBF, 68}, "F3": {0xFFC0, 69}, "F4": {0xFFC1, 70},
	"F5": {0xFFC2, 71}, "F6": {0xFFC3, 72}, "F7": {0xFFC4, 73}, "F8": {0xFFC5, 74},
	"F9": {0xFFC6, 75}, "F10": {0xFFC7, 76}, "F11": {0xFFC8, 95}, "F12": {0xFFC9, 96},
	"F13": {0xFFCA, 0}, "F14": {0xFFCB, 0}, "F15": {0xFFCC, 0}, "F16": {0xFFCD, 0},
	"F17": {0xFFCE, 0}, "F18": {0xFFCF, 0}, "F19": {0xFFD0, 0},
	"Pause": {0xFF13, 127}, "ScrollLock": {0xFF14, 78}, "PrintScreen": {0xFF61, 107},
	"CapsLock": {0xFFE5, 66}, "NumLock": {0xFF7F, 77}, "Menu": {0xFF67, 135},
	"Numpad0": {0xFFB0, 90}, "Numpad1": {0xFFB1, 87}, "Numpad2": {0xFFB2, 88}, "Numpad3": {0xFFB3, 89},
	"Numpad4": {0xFFB4, 83}, "Numpad5": {0xFFB5, 84}, "Numpad6": {0xFFB6, 85}, "Numpad7": {0xFFB7, 79},
	"Numpad8": {0xFFB8, 80}, "Numpad9": {0xFFB9, 81}, "NumpadMultiply": {0xFFAA, 63},
	"NumpadAdd": {0xFFAB, 86}, "NumpadSubtract": {0xFFAD, 82}, "NumpadDecimal": {0xFFAE, 91},
	"NumpadDivide": {0xFFAF, 106},
}

// modifierLabels are the X11 names of the modifiers; none differ.
var modifierLabels = map[string]string{}

// layoutKey has nothing to add: keys are bound by position here.
func layoutKey(int) (string, bool) { return "", false }

func platformCheck(Combo) error { return nil }
