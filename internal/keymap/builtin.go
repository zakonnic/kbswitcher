package keymap

import "fmt"

// ansiKeys lists the keys of the alphanumeric block in reading order, which is
// the order mustLayout expects.
var ansiKeys = []Key{
	KeyBackquote, Key1, Key2, Key3, Key4, Key5, Key6, Key7, Key8, Key9, Key0, KeyMinus, KeyEqual,
	KeyQ, KeyW, KeyE, KeyR, KeyT, KeyY, KeyU, KeyI, KeyO, KeyP, KeyBracketLeft, KeyBracketRight, KeyBackslash,
	KeyA, KeyS, KeyD, KeyF, KeyG, KeyH, KeyJ, KeyK, KeyL, KeySemicolon, KeyQuote,
	KeyZ, KeyX, KeyC, KeyV, KeyB, KeyN, KeyM, KeyComma, KeyPeriod, KeySlash,
}

// mustLayout builds a layout from the unshifted and shifted characters of the
// alphanumeric block, both in ansiKeys order. It panics on a malformed table.
func mustLayout(id, name, normal, shifted string) *Layout {
	var chars Chars
	for level, row := range []string{normal, shifted} {
		runes := []rune(row)
		if len(runes) != len(ansiKeys) {
			panic(fmt.Sprintf("keymap: layout %s level %d has %d characters, want %d",
				id, level, len(runes), len(ansiKeys)))
		}
		for i, k := range ansiKeys {
			chars[k][Level(level)] = runes[i]
		}
	}
	return NewLayout(id, name, chars)
}

// Built-in layouts, used only when the operating system's own cannot be read.
var (
	builtinUS = mustLayout("builtin.US", "English (US)",
		"`1234567890-=qwertyuiop[]\\asdfghjkl;'zxcvbnm,./",
		"~!@#$%^&*()_+QWERTYUIOP{}|ASDFGHJKL:\"ZXCVBNM<>?")

	builtinRU = mustLayout("builtin.RU", "Russian",
		"ё1234567890-=йцукенгшщзхъ\\фывапролджэячсмитьбю.",
		"Ё!\"№;%:?*()_+ЙЦУКЕНГШЩЗХЪ/ФЫВАПРОЛДЖЭЯЧСМИТЬБЮ,")
)

// Fallback returns the built-in layouts, in cycling order.
func Fallback() []*Layout {
	return []*Layout{builtinUS, builtinRU}
}
