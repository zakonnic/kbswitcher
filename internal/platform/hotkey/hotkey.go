// Package hotkey registers system-wide hotkeys: RegisterEventHotKey on macOS,
// RegisterHotKey on Windows, XGrabKey on X11. The process is woken up only for
// the registered combinations and never sees any other keystroke.
package hotkey

import (
	"fmt"
	"sort"
	"strings"
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

// keyNames lists every key kbswitcher is willing to bind, in the order the
// settings window shows them. Only keys that no layout turns into a character
// are eligible: binding a letter would swallow it while typing.
var keyNames = []string{
	"Pause", "ScrollLock", "Insert", "Home", "End",
	"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10",
	"F11", "F12", "F13", "F14", "F15", "F16", "F17", "F18", "F19",
}

// Keys returns the key names this platform can bind: Mac keyboards have no
// Pause key, PC keyboards have no F13 and above.
func Keys() []string {
	supported := make([]string, 0, len(keyNames))
	for _, name := range keyNames {
		if _, ok := keyCode(name); ok {
			supported = append(supported, name)
		}
	}
	return supported
}

// Supported reports whether this platform can bind the named key.
func Supported(key string) bool {
	_, ok := keyCode(key)
	return ok
}

// Modifiers returns the modifier names offered in the settings window. Shift
// is reserved for the "convert the selection" variant of the hotkey.
func Modifiers() []string {
	names := make([]string, 0, len(modifierNames))
	for _, m := range modifierNames {
		if m.mod != ModShift {
			names = append(names, m.name)
		}
	}
	sort.Strings(names)
	return names
}
