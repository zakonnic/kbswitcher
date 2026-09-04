// Package keymap is the layout-conversion core of kbswitcher. It knows about
// physical keys, the characters a layout produces for them, and how to rewrite
// text from one layout to another; nothing in it depends on the operating
// system.
package keymap

// Key identifies a physical key by its position, independently of the active
// layout. The names follow the US QWERTY legends, like the W3C "code" values.
//
// Only the alphanumeric block is tracked: it is the part whose meaning changes
// between layouts.
type Key uint8

// Keys of the alphanumeric block in reading order, then the extra keys of ISO
// and JIS keyboards.
const (
	KeyBackquote Key = iota
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	Key0
	KeyMinus
	KeyEqual

	KeyQ
	KeyW
	KeyE
	KeyR
	KeyT
	KeyY
	KeyU
	KeyI
	KeyO
	KeyP
	KeyBracketLeft
	KeyBracketRight
	KeyBackslash

	KeyA
	KeyS
	KeyD
	KeyF
	KeyG
	KeyH
	KeyJ
	KeyK
	KeyL
	KeySemicolon
	KeyQuote

	KeyZ
	KeyX
	KeyC
	KeyV
	KeyB
	KeyN
	KeyM
	KeyComma
	KeyPeriod
	KeySlash

	KeyIntlBackslash // next to left Shift on ISO keyboards
	KeyIntlRo        // next to right Shift on JIS keyboards
	KeyIntlYen       // next to Backspace on JIS keyboards

	// KeyCount is the number of tracked keys, not a key itself.
	KeyCount
)

var keyNames = [KeyCount]string{
	KeyBackquote: "Backquote", Key1: "1", Key2: "2", Key3: "3", Key4: "4",
	Key5: "5", Key6: "6", Key7: "7", Key8: "8", Key9: "9", Key0: "0",
	KeyMinus: "Minus", KeyEqual: "Equal",

	KeyQ: "Q", KeyW: "W", KeyE: "E", KeyR: "R", KeyT: "T", KeyY: "Y",
	KeyU: "U", KeyI: "I", KeyO: "O", KeyP: "P",
	KeyBracketLeft: "BracketLeft", KeyBracketRight: "BracketRight", KeyBackslash: "Backslash",

	KeyA: "A", KeyS: "S", KeyD: "D", KeyF: "F", KeyG: "G", KeyH: "H",
	KeyJ: "J", KeyK: "K", KeyL: "L", KeySemicolon: "Semicolon", KeyQuote: "Quote",

	KeyZ: "Z", KeyX: "X", KeyC: "C", KeyV: "V", KeyB: "B", KeyN: "N", KeyM: "M",
	KeyComma: "Comma", KeyPeriod: "Period", KeySlash: "Slash",

	KeyIntlBackslash: "IntlBackslash", KeyIntlRo: "IntlRo", KeyIntlYen: "IntlYen",
}

// String returns the US QWERTY name of the key position.
func (k Key) String() string {
	if k >= KeyCount {
		return "Key(invalid)"
	}
	return keyNames[k]
}

// Level is the shift level of a key press. Shift is the only modifier
// kbswitcher reproduces; characters that need AltGr are left untouched.
type Level uint8

// Shift levels of a key press.
const (
	LevelNormal Level = iota
	LevelShift

	// LevelCount is the number of tracked shift levels.
	LevelCount
)
