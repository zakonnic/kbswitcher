package keymap

import "fmt"

// Press is a physical key together with the shift level it was pressed at.
type Press struct {
	Key   Key
	Level Level
}

// Chars holds the character a layout produces for every tracked key at every
// tracked shift level. Zero means the layout produces nothing there.
type Chars [KeyCount][LevelCount]rune

// Layout is one keyboard layout as reported by the operating system. It is
// immutable once created and therefore safe for concurrent use.
type Layout struct {
	id    string
	name  string
	chars Chars
	index map[rune]Press
}

// NewLayout builds a layout from its character table. When a character sits
// on more than one key, the first position in reading order wins.
func NewLayout(id, name string, chars Chars) *Layout {
	l := &Layout{
		id:    id,
		name:  name,
		chars: chars,
		index: make(map[rune]Press, int(KeyCount)*int(LevelCount)),
	}
	for k := Key(0); k < KeyCount; k++ {
		for lv := Level(0); lv < LevelCount; lv++ {
			r := chars[k][lv]
			if r == 0 {
				continue
			}
			if _, seen := l.index[r]; !seen {
				l.index[r] = Press{Key: k, Level: lv}
			}
		}
	}
	return l
}

// ID returns the operating-system identifier of the layout, such as
// "com.apple.keylayout.US" or "00000409". It is stable across restarts.
func (l *Layout) ID() string { return l.id }

// Name returns the localized name shown to the user.
func (l *Layout) Name() string { return l.name }

// Rune returns the character produced by a key press, or zero.
func (l *Layout) Rune(k Key, lv Level) rune {
	if k >= KeyCount || lv >= LevelCount {
		return 0
	}
	return l.chars[k][lv]
}

// Press returns the key press that types r in this layout.
func (l *Layout) Press(r rune) (Press, bool) {
	p, ok := l.index[r]
	return p, ok
}

// CanType reports whether r can be typed in this layout with at most Shift.
func (l *Layout) CanType(r rune) bool {
	_, ok := l.index[r]
	return ok
}

// Size returns how many key presses produce a character.
func (l *Layout) Size() int { return len(l.index) }

func (l *Layout) String() string { return fmt.Sprintf("%s (%s)", l.name, l.id) }
