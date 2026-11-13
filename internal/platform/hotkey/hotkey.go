// Package hotkey registers system-wide hotkeys: RegisterEventHotKey on macOS,
// RegisterHotKey on Windows, XGrabKey on X11. The process is woken up only for
// the registered combinations and never sees any other keystroke.
package hotkey

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Modifier is a set of modifier keys.
type Modifier uint8

// Modifier keys, as a bit set.
const (
	ModCtrl Modifier = 1 << iota
	ModAlt
	ModShift
	ModSuper // Command on macOS, the Windows key elsewhere
)

// modifierNames are the canonical, platform-neutral configuration spellings.
var modifierNames = []struct {
	name string
	mod  Modifier
}{
	{"Ctrl", ModCtrl},
	{"Alt", ModAlt},
	{"Shift", ModShift},
	{"Super", ModSuper},
}

// modifierAliases maps every accepted lower-case spelling to its bit.
var modifierAliases = map[string]Modifier{
	"ctrl": ModCtrl, "control": ModCtrl,
	"alt": ModAlt, "option": ModAlt, "opt": ModAlt,
	"shift": ModShift,
	"super": ModSuper, "cmd": ModSuper, "command": ModSuper, "win": ModSuper, "meta": ModSuper,
}

// ParseModifiers turns configuration strings such as ["Ctrl", "Alt"] into a
// bit set. Blank entries are ignored.
func ParseModifiers(names []string) (Modifier, error) {
	var mods Modifier
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		mod, ok := modifierAliases[name]
		if !ok {
			return 0, fmt.Errorf("unknown modifier %q", raw)
		}
		mods |= mod
	}
	return mods, nil
}

// Names returns the modifiers as canonical configuration strings, in a stable
// order.
func (m Modifier) Names() []string {
	names := make([]string, 0, len(modifierNames))
	for _, def := range modifierNames {
		if m&def.mod != 0 {
			names = append(names, def.name)
		}
	}
	return names
}

// Has reports whether every modifier in other is present in m.
func (m Modifier) Has(other Modifier) bool { return m&other == other }

func (m Modifier) String() string { return strings.Join(m.Names(), "+") }

// Combo is a hotkey: one key held together with zero or more modifiers.
type Combo struct {
	Key  string // canonical key name, see Keys
	Mods Modifier
}

// With returns a copy of the combo with extra modifiers added.
func (c Combo) With(extra Modifier) Combo {
	c.Mods |= extra
	return c
}

func (c Combo) String() string {
	if c.Mods == 0 {
		return c.Key
	}
	return c.Mods.String() + "+" + c.Key
}

// native is how a platform knows a key: the code it binds, and the scan code
// GLFW reports when the key is pressed in a window, zero for none.
type native struct {
	code uint32
	scan int
}

var byScanCode = sync.OnceValue(func() map[int]string {
	names := make(map[int]string, len(keys))
	for name, k := range keys {
		if k.scan != 0 {
			names[k.scan] = name
		}
	}
	return names
})

// KeyForScanCode names the key with the scan code GLFW reports, the way this
// platform will bind it: on Windows a letter or punctuation key is the one the
// layout in use puts there, as for any Windows shortcut; elsewhere every key
// is told by its position.
func KeyForScanCode(code int) (string, bool) {
	if name, ok := layoutKey(code); ok {
		return name, true
	}
	name, ok := byScanCode()[code]
	return name, ok
}

// Supported reports whether this platform can bind the named key.
func Supported(key string) bool {
	_, ok := keys[key]
	return ok
}

// typing are the keys that type text or move about in it. Bound without a
// modifier, one of them would be taken away from the user.
var typing = map[string]bool{
	"Backquote": true, "Minus": true, "Equal": true, "BracketLeft": true, "BracketRight": true,
	"Backslash": true, "Semicolon": true, "Quote": true, "Comma": true, "Period": true, "Slash": true,
	"IntlBackslash": true, "Space": true, "Enter": true, "Tab": true, "Backspace": true, "Delete": true,
	"Left": true, "Right": true, "Up": true, "Down": true, "PageUp": true, "PageDown": true,
	"NumpadMultiply": true, "NumpadAdd": true, "NumpadSubtract": true, "NumpadDecimal": true, "NumpadDivide": true,
}

// Typing reports whether a key types text or moves about in it: a letter, a
// digit, a punctuation key, Space, Enter, Tab, Backspace, Delete, an arrow,
// Page Up or Down, or a keypad key.
func Typing(key string) bool {
	if len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9') {
		return true
	}
	if digit, ok := strings.CutPrefix(key, "Numpad"); ok && len(digit) == 1 {
		return true
	}
	return typing[key]
}

// IsModifier reports whether a name is one of the modifiers.
func IsModifier(name string) bool {
	_, ok := modifierAliases[strings.ToLower(name)]
	return ok
}

// labels are what a key cap shows where it differs from the key's name.
var labels = map[string]string{
	"Backquote": "`", "Minus": "-", "Equal": "=", "BracketLeft": "[", "BracketRight": "]",
	"Backslash": "\\", "Semicolon": ";", "Quote": "'", "Comma": ",", "Period": ".", "Slash": "/",
	"IntlBackslash": "< >", "Escape": "Esc", "Delete": "Del", "Insert": "Ins",
	"PageUp": "PgUp", "PageDown": "PgDn", "PrintScreen": "PrtSc", "ScrollLock": "ScrLk",
	"CapsLock": "Caps Lock", "NumLock": "Num Lock",
	"NumpadMultiply": "Num *", "NumpadAdd": "Num +", "NumpadSubtract": "Num -",
	"NumpadDecimal": "Num .", "NumpadDivide": "Num /",
}

// Label is how a key or a modifier is shown to the user.
func Label(name string) string {
	if label, ok := modifierLabels[name]; ok {
		return label
	}
	if label, ok := labels[name]; ok {
		return label
	}
	if digit, ok := strings.CutPrefix(name, "Numpad"); ok && len(digit) == 1 {
		return "Num " + digit
	}
	return name
}

// Names returns the combination as modifier names followed by the key.
func (c Combo) Names() []string { return append(c.Mods.Names(), c.Key) }

// ErrTypingKey means a key used in typing was chosen without a modifier.
var ErrTypingKey = errors.New("letters, digits and other keys used in typing need a modifier")

// Check reports why a combination cannot be a hotkey, if it cannot.
func (c Combo) Check() error {
	switch {
	case c.Key == "":
		return errors.New("no key is set")
	case !Supported(c.Key):
		return fmt.Errorf("the %s key cannot be bound on this system", Label(c.Key))
	case c.Mods == 0 && Typing(c.Key):
		return ErrTypingKey
	}
	return platformCheck(c)
}
