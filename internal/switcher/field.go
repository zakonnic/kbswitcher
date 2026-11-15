package switcher

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/field"
)

// Field is the text field that has the keyboard focus, where the platform
// can read its text without a key press (on Windows, UI Automation or a
// classic edit control's own messages).
type Field interface {
	// Read returns the selection and up to around characters on either side
	// of it. It fails with ErrNoField (field.ErrNoText) when the focus is not
	// on an editable field that tells its text truthfully, and with
	// ErrPassword on a field that hides it.
	Read(around int) (field.Around, error)
	// Mark presses Shift+Left.
	Mark() error
	// Retype presses Right when right is set, then Backspace erase times,
	// and types text, all in one burst.
	Retype(right bool, erase int, text string) error
}

var (
	// ErrNoField sends a conversion down the clipboard path.
	ErrNoField = field.ErrNoText
	// ErrPassword means the focused field hides its text; it is left alone.
	ErrPassword = field.ErrPassword
	// errFieldLate means the field did not show the effect of a key in time.
	// Nothing more is done: a field that busy would serve the clipboard path
	// worse still.
	errFieldLate = errors.New("the text field did not show a key in time")
)

const (
	// fieldAround is how much is read on either side of the caret: more than
	// a line of a chat box, far less than a document.
	fieldAround = 300
	// maxRetyped bounds a selection that is typed over; a longer one is
	// pasted on the clipboard path rather than sent as thousands of keys.
	maxRetyped = 500
	// fieldPoll is how often a field is read again while waiting for a key to
	// show.
	fieldPoll = 2 * time.Millisecond
)

// convertInField is the path for a field that tells its text: read it, and
// type the conversion over what was read, as a user who noticed the wrong
// layout would: Backspace, then the right characters. Nothing is selected,
// copied or pasted, so no clipboard shortcut reaches the application, the
// clipboard is not touched, and how late an editor learns of a selection
// does not matter.
func (s *Switcher) convertInField(lastWordOnly bool) (*keymap.Layout, error) {
	if s.deps.Field == nil {
		return nil, ErrNoField
	}
	seen, err := s.deps.Field.Read(fieldAround)
	switch {
	case err != nil:
		return nil, err
	case lastWordOnly && seen.Selected == "":
		return s.retypeLastWord(seen)
	case !lastWordOnly && seen.Selected != "":
		return s.retypeSelection(seen)
	default:
		// The last-word hotkey over a selection (an address bar's inline
		// suggestion is one), and the selection hotkey with no selection shown
		// yet (the field may not show one made a moment ago), are left to the
		// clipboard path, whose keys queue behind everything typed.
		return nil, ErrNoField
	}
}

// retypeLastWord replaces the word before the caret.
//
// What the field shows may lag the keys sent to it: typed characters and
// arrows the application has not taken in yet, an accessibility tree that has
// not caught up (measured up to 150 ms in a busy page), a classic edit
// control answering a message ahead of the keys queued for it. A Shift+Left
// goes through the input queue behind all of them, and nothing else the user
// sends selects a single character, so once the field shows one character
// selected, it shows everything before it. The Right that drops that
// selection, at its end, opens the burst that retypes the word.
func (s *Switcher) retypeLastWord(seen field.Around) (*keymap.Layout, error) {
	f := s.deps.Field
	if _, word, _ := keymap.SplitLastWord(lineEnd(seen.Before)); word == "" {
		return nil, ErrNothingToConvert
	}
	if err := f.Mark(); err != nil {
		return nil, err
	}
	now, err := s.awaitField(func(a field.Around) bool { return a.Selected != "" })
	if err != nil {
		// Right follows Shift+Left through the queue, however late both are.
		return nil, errors.Join(err, f.Retype(true, 0, ""))
	}
	head, word, tail := keymap.SplitLastWord(lineEnd(now.Before + now.Selected))
	typed := word + tail
	switch {
	case word == "" || strings.ContainsFunc(now.Selected, lineBreak):
		return nil, errors.Join(ErrNothingToConvert, f.Retype(true, 0, ""))
	case utf8.RuneCountInString(now.Selected) != 1 || !oneKeyEach(typed):
		// Shift+Left took more than one character, or Backspace would: a
		// combining mark, an emoji. The clipboard path cuts what it replaces.
		return nil, errors.Join(ErrNoField, f.Retype(true, 0, ""))
	}
	target, converted := s.converted(typed)
	s.log.Debug("retyping in a text field", "kept", utf8.RuneCountInString(head), "characters", utf8.RuneCountInString(typed))
	if err := f.Retype(true, utf8.RuneCountInString(typed), converted); err != nil {
		return nil, err
	}
	return target, nil
}

// retypeSelection replaces a selection that lies on one line. It is erased
// with Backspace first, and what the field shows gone decides the rest. The
// selection, or more if it grew after it was read, is typed back converted.
// Only its end goes when an editor has not taken the selection in yet and
// erases one character as if there were none; the rest of it is erased
// after. Characters before it go when such an editor's caret is at the
// selection's start; they are put back. Anything else is reported, and
// nothing is typed.
func (s *Switcher) retypeSelection(seen field.Around) (*keymap.Layout, error) {
	if !oneKeyEach(seen.Selected) || strings.ContainsFunc(seen.Selected, lineBreak) ||
		utf8.RuneCountInString(seen.Selected) > maxRetyped {
		// Line breaks, tabs, embedded objects and characters Backspace does
		// not take one at a time cannot be retyped safely. The clipboard path
		// pastes them.
		return nil, ErrNoField
	}
	f := s.deps.Field
	if err := f.Retype(false, 1, ""); err != nil { // Backspace
		return nil, err
	}
	typed, erase := seen.Selected, 0
	// A field that does not show the Backspace in time takes the characters
	// after it all the same.
	if now, err := s.awaitField(func(a field.Around) bool { return a.Selected == "" }); err == nil {
		went, ok := erasure(seen, now)
		selected := utf8.RuneCountInString(seen.Selected)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: the field changed in a way that cannot be told", ErrNotSelected)
		case went.before:
			return nil, errors.Join(ErrNotSelected, f.Retype(false, 0, went.text))
		case went.n < selected:
			erase = selected - went.n
		case !typable(went.text):
			return nil, fmt.Errorf("%w: the selection grew over a tab", ErrNotSelected)
		default:
			typed = went.text
		}
	}
	if strings.TrimSpace(typed) == "" {
		return nil, errors.Join(ErrNothingToConvert, f.Retype(false, erase, typed))
	}
	target, converted := s.converted(typed)
	s.log.Debug("retyping a selection", "characters", utf8.RuneCountInString(typed), "late", erase)
	if err := f.Retype(false, erase, converted); err != nil {
		return nil, err
	}
	return target, nil
}

// gone is what a Backspace took: n characters, text, ending at the end of the
// selection, or with before set ending at its start.
type gone struct {
	n      int
	text   string
	before bool
}

// erasure tells what a Backspace over a selection took, from reads of the
// field before and after it. Either read may be cut short at its length
// limit, so unless both reach the start of the line, only text that begins
// or ends at the same place in both is compared: what follows the erased part
// starts where it did, and what precedes it ends where the old line, less the
// erased part, ends. The likeliest counts are tried first: the selection,
// then less of it, then more; a long run of one repeated character can make
// a wrong count look right.
func erasure(seen, now field.Around) (gone, bool) {
	kept, rest := lineEnd(now.Before), lineStart(now.After)
	after := lineStart(seen.After)
	head := []rune(lineEnd(seen.Before))
	upToEnd := append(head, []rune(seen.Selected)...)
	selected := len(upToEnd) - len(head)
	switch {
	case prefixed(rest, after) && wholeLine(seen.Before) && wholeLine(now.Before):
		// Both reads start at the start of the line: the count is exact.
		if n := len(upToEnd) - utf8.RuneCountInString(kept); n > 0 && strings.HasPrefix(string(upToEnd), kept) {
			return gone{n: n, text: string(upToEnd[len(upToEnd)-n:])}, true
		}
	case prefixed(rest, after):
		if n, ok := endsAt(upToEnd, kept, selected); ok {
			return gone{n: n, text: string(upToEnd[len(upToEnd)-n:])}, true
		}
	case prefixed(rest, seen.Selected+after):
		if n, ok := endsAt(head, kept, 1); ok {
			return gone{n: n, text: string(head[len(head)-n:]), before: true}, true
		}
	}
	return gone{}, false
}

// endsAt finds n, from 1 up to the length of text, such that text less its
// last n characters and kept end alike, trying likely first.
func endsAt(text []rune, kept string, likely int) (int, bool) {
	try := func(n int) bool {
		rest := string(text[:len(text)-n])
		return strings.HasSuffix(rest, kept) || strings.HasSuffix(kept, rest)
	}
	if likely <= len(text) && try(likely) {
		return likely, true
	}
	for n := 1; n <= len(text); n++ {
		if n != likely && try(n) {
			return n, true
		}
	}
	return 0, false
}

// wholeLine reports whether a read of the text before the caret reaches the
// start of its line.
func wholeLine(before string) bool {
	return strings.ContainsFunc(before, lineBreak) || utf8.RuneCountInString(before) < fieldAround
}

// prefixed reports whether one of two texts that start at the same place
// begins with the other, as reads cut at different lengths do.
func prefixed(a, b string) bool { return strings.HasPrefix(a, b) || strings.HasPrefix(b, a) }

// converted rewrites typed for the next layout in the cycle.
func (s *Switcher) converted(typed string) (*keymap.Layout, string) {
	source, target := s.pickLayouts(typed)
	s.log.Debug("converting", "from", s.layouts[source].ID(), "to", s.layouts[target].ID())
	return s.layouts[target], keymap.Convert(typed, s.layouts[source], s.layouts[target])
}

// awaitField reads the field until done holds.
func (s *Switcher) awaitField(done func(field.Around) bool) (field.Around, error) {
	deadline := time.Now().Add(s.options.FieldTimeout)
	for {
		a, err := s.deps.Field.Read(fieldAround)
		if err != nil {
			return field.Around{}, err
		}
		if done(a) {
			return a, nil
		}
		if !time.Now().Before(deadline) {
			return field.Around{}, errFieldLate
		}
		time.Sleep(fieldPoll)
	}
}

// lineEnd is the part of text after its last line break; lineStart, the part
// before its first. An object replacement character stands for something
// embedded, a picture or a widget, and ends a line as well.
func lineEnd(text string) string {
	i := strings.LastIndexFunc(text, lineBreak)
	if i < 0 {
		return text
	}
	_, size := utf8.DecodeRuneInString(text[i:])
	return text[i+size:]
}

func lineStart(text string) string {
	if i := strings.IndexFunc(text, lineBreak); i >= 0 {
		return text[:i]
	}
	return text
}

func lineBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == '\u2028' || r == '\u2029' || r == '\ufffc'
}

// typable reports text that can be typed as it is: no control characters.
func typable(text string) bool {
	return !strings.ContainsFunc(text, unicode.IsControl)
}

// oneKeyEach reports text that Backspace erases one character at a time, as
// it does letters and punctuation. A combining mark, a joiner or a variation
// selector goes together with the character before it, and a character
// beyond the Basic Multilingual Plane (emoji, their skin tones) takes one
// key in some fields and two in others.
func oneKeyEach(text string) bool {
	return typable(text) && !strings.ContainsFunc(text, func(r rune) bool {
		return r > 0xFFFF || unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Variation_Selector)
	})
}
