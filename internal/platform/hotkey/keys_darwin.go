package hotkey

// keys maps the portable key names, US legends for the keys whose legend
// changes with the layout, to virtual key codes from Carbon's Events.h. GLFW
// reports the same code as the scan code. Mac keyboards have no Pause, Scroll
// Lock or Print Screen key.
var keys = map[string]native{
	"0": mac(0x1D), "1": mac(0x12), "2": mac(0x13), "3": mac(0x14), "4": mac(0x15), "5": mac(0x17),
	"6": mac(0x16), "7": mac(0x1A), "8": mac(0x1C), "9": mac(0x19), "A": mac(0x00), "B": mac(0x0B),
	"C": mac(0x08), "D": mac(0x02), "E": mac(0x0E), "F": mac(0x03), "G": mac(0x05), "H": mac(0x04),
	"I": mac(0x22), "J": mac(0x26), "K": mac(0x28), "L": mac(0x25), "M": mac(0x2E), "N": mac(0x2D),
	"O": mac(0x1F), "P": mac(0x23), "Q": mac(0x0C), "R": mac(0x0F), "S": mac(0x01), "T": mac(0x11),
	"U": mac(0x20), "V": mac(0x09), "W": mac(0x0D), "X": mac(0x07), "Y": mac(0x10), "Z": mac(0x06),
	"Backquote": mac(0x32), "Minus": mac(0x1B), "Equal": mac(0x18),
	"BracketLeft": mac(0x21), "BracketRight": mac(0x1E), "Backslash": mac(0x2A),
	"Semicolon": mac(0x29), "Quote": mac(0x27), "Comma": mac(0x2B),
	"Period": mac(0x2F), "Slash": mac(0x2C), "IntlBackslash": mac(0x0A), // kVK_ISO_Section
	"Space": mac(0x31), "Enter": mac(0x24), "Tab": mac(0x30),
	"Backspace": mac(0x33), "Escape": mac(0x35),
	"Insert": mac(0x72), // labelled Help on the keyboards that have it
	"Delete": mac(0x75), "Home": mac(0x73), "End": mac(0x77), "PageUp": mac(0x74), "PageDown": mac(0x79),
	"Left": mac(0x7B), "Right": mac(0x7C), "Down": mac(0x7D), "Up": mac(0x7E),
	"F1": mac(0x7A), "F2": mac(0x78), "F3": mac(0x63), "F4": mac(0x76),
	"F5": mac(0x60), "F6": mac(0x61), "F7": mac(0x62), "F8": mac(0x64),
	"F9": mac(0x65), "F10": mac(0x6D), "F11": mac(0x67), "F12": mac(0x6F),
	"F13": mac(0x69), "F14": mac(0x6B), "F15": mac(0x71), "F16": mac(0x6A),
	"F17": mac(0x40), "F18": mac(0x4F), "F19": mac(0x50), "F20": mac(0x5A),
	"Numpad0": mac(0x52), "Numpad1": mac(0x53), "Numpad2": mac(0x54), "Numpad3": mac(0x55),
	"Numpad4": mac(0x56), "Numpad5": mac(0x57), "Numpad6": mac(0x58), "Numpad7": mac(0x59),
	"Numpad8": mac(0x5B), "Numpad9": mac(0x5C), "NumpadMultiply": mac(0x43),
	"NumpadAdd": mac(0x45), "NumpadSubtract": mac(0x4E), "NumpadDecimal": mac(0x41),
	"NumpadDivide": mac(0x4B),
}

func mac(code uint32) native { return native{code, int(code)} }

// modifierLabels are the macOS names of the modifiers.
var modifierLabels = map[string]string{"Ctrl": "Control", "Alt": "Option", "Super": "Cmd"}

// layoutKey has nothing to add: keys are bound by position here.
func layoutKey(int) (string, bool) { return "", false }

func platformCheck(Combo) error { return nil }
