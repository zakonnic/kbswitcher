package switcher

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/field"
)

// fakeField is a text field that tells its text the way a busy browser does:
// keys wait in a queue, the user's and the switcher's alike, and each read
// lets the field take in at most pace of them first (all of them when pace
// is 0). What a read shows can therefore lag the keys sent.
type fakeField struct {
	text          []rune
	caret, anchor int
	queue         []func()
	pace          int
	// frozen makes the field take in nothing at all; stale makes its next
	// Backspace erase one character as if nothing were selected, as an
	// editor that has not taken a selection in yet does.
	frozen, stale bool
	// clusters makes Shift+Left take a letter with the marks after it.
	clusters bool
	err      error
	reads    int
}

func newField(text string) *fakeField {
	f := &fakeField{text: []rune(text)}
	f.caret, f.anchor = len(f.text), len(f.text)
	return f
}

func (f *fakeField) String() string { return string(f.text) }

func (f *fakeField) sel() (int, int) { return min(f.caret, f.anchor), max(f.caret, f.anchor) }

func (f *fakeField) Read(around int) (field.Around, error) {
	f.reads++
	if f.err != nil {
		return field.Around{}, f.err
	}
	if !f.frozen {
		n := len(f.queue)
		if f.pace > 0 {
			n = min(n, f.pace)
		}
		for _, key := range f.queue[:n] {
			key()
		}
		f.queue = f.queue[n:]
	}
	lo, hi := f.sel()
	return field.Around{
		Before:   string(f.text[max(lo-around, 0):lo]),
		Selected: string(f.text[lo:hi]),
		After:    string(f.text[hi:min(hi+around, len(f.text))]),
	}, nil
}

// settle lets the field take in every key still queued.
func (f *fakeField) settle() {
	f.frozen, f.pace = false, 0
	_, _ = f.Read(fieldAround)
}

func (f *fakeField) Mark() error {
	f.extendLater()
	return nil
}

func (f *fakeField) Retype(right bool, erase int, text string) error {
	if right {
		f.queue = append(f.queue, f.right)
	}
	for range erase {
		f.queue = append(f.queue, f.backspace)
	}
	f.typeLater(text)
	return nil
}

// typeLater queues keys the user typed that the field has not taken in.
func (f *fakeField) typeLater(text string) {
	for _, r := range text {
		f.queue = append(f.queue, func() { f.put(r) })
	}
}

// leftLater queues a Left the field has not taken in.
func (f *fakeField) leftLater() { f.queue = append(f.queue, f.left) }

// extendLater queues a Shift+Left the field has not taken in.
func (f *fakeField) extendLater() {
	f.queue = append(f.queue, func() {
		f.caret = max(f.caret-1, 0)
		for f.clusters && f.caret > 0 && unicode.Is(unicode.Mn, f.text[f.caret]) {
			f.caret--
		}
	})
}

func (f *fakeField) left() {
	if lo, hi := f.sel(); lo != hi {
		f.caret = lo
	} else {
		f.caret = max(f.caret-1, 0)
	}
	f.anchor = f.caret
}

func (f *fakeField) right() {
	if lo, hi := f.sel(); lo != hi {
		f.caret = hi
	} else {
		f.caret = min(f.caret+1, len(f.text))
	}
	f.anchor = f.caret
}

func (f *fakeField) erase() {
	lo, hi := f.sel()
	f.text = append(f.text[:lo:lo], f.text[hi:]...)
	f.caret, f.anchor = lo, lo
}

func (f *fakeField) backspace() {
	if f.stale {
		f.stale = false
		f.anchor = f.caret
	}
	if lo, hi := f.sel(); lo == hi && lo > 0 {
		f.anchor = lo - 1
	}
	f.erase()
}

func (f *fakeField) put(r rune) {
	f.erase()
	f.text = append(f.text[:f.caret:f.caret], append([]rune{r}, f.text[f.caret:]...)...)
	f.caret++
	f.anchor = f.caret
}

// fieldSwitcher has a field and, for the clipboard path, an editor showing
// clipboardText.
func fieldSwitcher(t *testing.T, f *fakeField, clipboardText string) (*Switcher, *fakeEditor) {
	t.Helper()
	layouts := keymap.Fallback()
	clipboard := &fakeClipboard{}
	editor := newEditor(clipboardText, clipboard)
	options := DefaultOptions()
	options.PasteSettle = 0
	options.FieldTimeout = 50 * time.Millisecond
	deps := Deps{
		Layouts:   &fakeLayouts{active: layouts[0].ID()},
		Keyboard:  editor,
		Clipboard: clipboard,
		Field:     f,
	}
	s, err := New(layouts, deps, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Idle)
	return s, editor
}

func TestConvertLastWordInAField(t *testing.T) {
	tests := []struct {
		name string
		text string
		// tail is text after the caret; late is typed but not yet taken in.
		tail, late string
		want       string
	}{
		{"the last word", "hello ckjdj", "", "", "hello слово"},
		{"the only word", "ckjdj", "", "", "слово"},
		{"punctuation inside", "ytn? ghj,ktvf yt lj,fdbkfcm", "", "", "ytn? ghj,ktvf yt добавилась"},
		{"the caret inside a word", "hello ckj", "dj", "", "hello слоdj"},
		{"text after the caret", "hello ckjdj", " world", "", "hello слово world"},
		{"the line the caret is on", "one\nhello ckjdj", "", "", "one\nhello слово"},
		{"keys the field shows late", "hello ckj", "", "dj", "hello слово"},
		{"a whole word typed late", "hello ", "", "ckjdj", "hello слово"},
		{"late keys and text after the caret", "hello c", " tail", "kjdj", "hello слово tail"},
		// A read from before the Left looks like a step back when what follows
		// the caret is one character, or a run of the same one.
		{"one character after the caret", "(ckj", ")", "dj", "(слово)"},
		{"a full stop after the caret", "hello ckj", ".", "dj", "hello слово."},
		{"a run after the caret", "x ckj", "aa", "dj", "x словоaa"},
		{"one character after the caret, nothing late", "(ckjdj", ")", "", "(слово)"},
	}
	for _, tt := range tests {
		for _, pace := range []int{0, 1, 2} {
			t.Run(tt.name+"/pace "+string(rune('0'+pace)), func(t *testing.T) {
				f := newField(tt.text + tt.tail)
				f.caret, f.anchor = len([]rune(tt.text)), len([]rune(tt.text))
				f.pace = pace
				f.typeLater(tt.late)
				s, editor := fieldSwitcher(t, f, "")

				if err := s.ConvertLastWord(t.Context()); err != nil {
					t.Fatalf("ConvertLastWord: %v", err)
				}
				f.settle()
				if got := f.String(); got != tt.want {
					t.Errorf("text is %q, want %q", got, tt.want)
				}
				if editor.copies != 0 {
					t.Errorf("the clipboard path ran as well: %d copies", editor.copies)
				}
			})
		}
	}
}

func TestConvertLastWordInAFieldTwiceGivesTheTextBack(t *testing.T) {
	f := newField("hello ckjdj")
	f.pace = 1
	s, _ := fieldSwitcher(t, f, "")
	for press := range 2 {
		if err := s.ConvertLastWord(t.Context()); err != nil {
			t.Fatalf("press %d: %v", press+1, err)
		}
	}
	f.settle()
	if got := f.String(); got != "hello ckjdj" {
		t.Errorf("text is %q, want it back as it was", got)
	}
}

func TestConvertSelectionInAField(t *testing.T) {
	f := newField("hello ckjdj ckjdj!")
	f.anchor, f.caret = len("hello "), len("hello ckjdj ckjdj")
	s, _ := fieldSwitcher(t, f, "")

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	f.settle()
	if got := f.String(); got != "hello слово слово!" {
		t.Errorf("text is %q, want %q", got, "hello слово слово!")
	}
}

func TestASelectionTheEditorHasNotTakenInIsConvertedWhole(t *testing.T) {
	tests := []struct {
		name           string
		anchor, caret  int
		want           string
		wantNotChanged bool
	}{
		// Backspace takes the last selected character, and the rest follows.
		{"selected rightwards", len("hello "), len("hello ckjdj"), "hello слово!", false},
		// Backspace takes the character before the selection: put back.
		{"selected leftwards", len("hello ckjdj"), len("hello "), "hello ckjdj!", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newField("hello ckjdj!")
			f.anchor, f.caret, f.stale = tt.anchor, tt.caret, true
			s, _ := fieldSwitcher(t, f, "")

			err := s.ConvertSelection(t.Context())
			f.settle()
			if got := f.String(); got != tt.want {
				t.Errorf("text is %q, want %q", got, tt.want)
			}
			if tt.wantNotChanged != errors.Is(err, ErrNotSelected) {
				t.Errorf("ConvertSelection = %v", err)
			}
		})
	}
}

func TestASelectionAfterALongLineIsTold(t *testing.T) {
	// The line before the selection is longer than a read reaches, so the
	// reads before and after the Backspace start at different places.
	var b strings.Builder
	for i := 0; b.Len() < 4*fieldAround; i++ {
		fmt.Fprintf(&b, "%d ", i)
	}
	long := b.String() + "aq "
	start, end := len([]rune(long)), len([]rune(long))+len("ckjdj")
	tests := []struct {
		name          string
		anchor, caret int
		setup         func(f *fakeField)
		want          string
	}{
		{"an editor that has not taken the selection in", start, end, func(f *fakeField) { f.stale = true }, long + "слово!"},
		// Two more Shift+Left presses are on their way: "q " joins.
		{"a selection that grew", end, start, func(f *fakeField) {
			f.extendLater()
			f.extendLater()
			f.pace = 1 // the first read shows only one of them
		}, strings.TrimSuffix(long, "q ") + "й слово!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newField(long + "ckjdj!")
			f.anchor, f.caret = tt.anchor, tt.caret
			s, _ := fieldSwitcher(t, f, "")
			seen, _ := f.Read(fieldAround)
			if seen.Selected != "ckjdj" {
				t.Fatalf("the field shows %q selected", seen.Selected)
			}
			tt.setup(f)
			if err := s.ConvertSelection(t.Context()); err != nil {
				t.Fatalf("ConvertSelection: %v", err)
			}
			f.settle()
			if got := f.String(); got != tt.want {
				t.Errorf("text ends %q, want %q", tail(got), tail(tt.want))
			}
		})
	}
}

func TestASelectionThatGrewOverARunIsCountedExactly(t *testing.T) {
	// A short line: both reads start at its start, and the run of z does not
	// make 6 erased characters look like 7.
	f := newField("zzzz ckjdj!")
	f.anchor, f.caret = len("zzzz ckjdj"), len("zzzz ")
	f.extendLater()
	f.extendLater()
	f.pace = 1
	s, _ := fieldSwitcher(t, f, "")
	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	f.settle()
	if got := f.String(); got != "zzzя слово!" {
		t.Errorf("text is %q, want %q", got, "zzzя слово!")
	}
}

func tail(s string) string {
	r := []rune(s)
	return string(r[max(len(r)-12, 0):])
}

func TestALetterWithMarksBeforeTheCaretGoesToTheClipboard(t *testing.T) {
	f := newField("hello ckjdj\u0301")
	f.clusters = true
	s, editor := fieldSwitcher(t, f, "ckjdj")
	editor.selectFrom(0)

	start := time.Now()
	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if took := time.Since(start); took >= s.options.FieldTimeout {
		t.Errorf("the hotkey waited %v for a one-character selection", took)
	}
	f.settle()
	if f.String() != "hello ckjdj\u0301" || editor.String() != "слово" {
		t.Errorf("field %q, clipboard path %q", f.String(), editor.String())
	}
}

func TestTheFieldPathLeavesUnclearHotkeysToTheClipboard(t *testing.T) {
	// The last-word hotkey over a selection (an address bar selects its
	// inline suggestion), and the selection hotkey with no selection shown,
	// which may have been made a moment ago.
	tests := []struct {
		name      string
		selection bool
		selectTo  int
	}{
		{"the last-word hotkey over a selection", false, len("ckjdj")},
		{"the selection hotkey with nothing selected", true, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newField("ckjdj tail")
			if tt.selectTo >= 0 {
				f.anchor, f.caret = 0, tt.selectTo
			}
			s, editor := fieldSwitcher(t, f, "ckjdj")
			editor.selectFrom(0)
			convert := s.ConvertLastWord
			if tt.selection {
				convert = s.ConvertSelection
			}
			if err := convert(t.Context()); err != nil {
				t.Fatalf("convert: %v", err)
			}
			f.settle()
			if f.String() != "ckjdj tail" || len(f.queue) != 0 {
				t.Errorf("the field became %q", f.String())
			}
			if editor.String() != "слово" {
				t.Errorf("the clipboard path left %q", editor.String())
			}
		})
	}
}

func TestASelectionThatGrewIsConvertedWhole(t *testing.T) {
	// The read shows "ckjdj" selected; two more Shift+Left presses are still
	// on their way, and the Backspace takes the selection they make.
	f := newField("hello ckjdj")
	f.anchor, f.caret = len("hello ckjdj"), len("hello ")
	f.extendLater()
	f.extendLater()
	f.pace = 1
	// The field takes the keys in only on a later read.
	f.frozen = true
	s, _ := fieldSwitcher(t, f, "")
	seen, _ := f.Read(fieldAround)
	f.frozen = false
	if seen.Selected != "ckjdj" {
		t.Fatalf("the field shows %q selected", seen.Selected)
	}

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	f.settle()
	if got := f.String(); got != "hellщ слово" {
		t.Errorf("text is %q, want %q", got, "hellщ слово")
	}
}

func TestAFieldThatTakesNothingInIsLeftAsItWas(t *testing.T) {
	f := newField("hello ckjdj")
	f.frozen = true
	s, editor := fieldSwitcher(t, f, "ckjdj")

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, errFieldLate) {
		t.Fatalf("ConvertLastWord = %v, want errFieldLate", err)
	}
	f.settle()
	if got := f.String(); got != "hello ckjdj" || f.caret != len(f.text) || f.anchor != f.caret {
		t.Errorf("text is %q with the selection %d..%d; want it untouched", got, f.anchor, f.caret)
	}
	if editor.copies != 0 {
		t.Errorf("the clipboard path ran: %d copies", editor.copies)
	}
}

func TestACaretMovedLateIsFollowed(t *testing.T) {
	// The user typed the end of the word and pressed Left; the field shows
	// neither yet.
	for _, pace := range []int{0, 1, 2} {
		f := newField("(ckj)")
		f.caret, f.anchor = len("(ckj"), len("(ckj")
		f.typeLater("dj")
		f.leftLater()
		f.pace = pace
		s, _ := fieldSwitcher(t, f, "")
		if err := s.ConvertLastWord(t.Context()); err != nil {
			t.Fatalf("pace %d: ConvertLastWord: %v", pace, err)
		}
		f.settle()
		if got := f.String(); got != "(словj)" {
			t.Errorf("pace %d: text is %q, want %q", pace, got, "(словj)")
		}
	}
}

func TestNothingIsSentForAnEmptyLine(t *testing.T) {
	f := newField("one\n")
	s, _ := fieldSwitcher(t, f, "")

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertLastWord = %v, want ErrNothingToConvert", err)
	}
	if len(f.queue) != 0 {
		t.Errorf("%d keys were sent", len(f.queue))
	}
}

func TestFieldsLeaveSomeTextToTheClipboard(t *testing.T) {
	tests := []struct {
		name  string
		field func() *fakeField
	}{
		{"no readable field", func() *fakeField {
			f := newField("")
			f.err = field.ErrNoText
			return f
		}},
		{"a selection over two lines", func() *fakeField {
			f := newField("ckjdj\nckjdj")
			f.anchor = 0
			return f
		}},
		{"a combining mark in the word", func() *fakeField {
			return newField("hello ckjdj\u0301")
		}},
		{"an embedded object in the selection", func() *fakeField {
			f := newField("ckjdj\ufffcckjdj")
			f.anchor = 0
			return f
		}},
		{"an emoji in the selection", func() *fakeField {
			f := newField("\U0001F44D ckjdj")
			f.anchor = 0
			return f
		}},
		{"a selection too long to type", func() *fakeField {
			f := newField(strings.Repeat("ckjdj ", maxRetyped))
			f.anchor = 0
			return f
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.field()
			before, caret := f.String(), f.caret
			s, editor := fieldSwitcher(t, f, "ckjdj")
			editor.selectFrom(0)

			convert := s.ConvertLastWord
			if f.anchor != f.caret {
				convert = s.ConvertSelection
			}
			if err := convert(t.Context()); err != nil {
				t.Fatalf("convert: %v", err)
			}
			f.settle()
			if f.String() != before || f.caret != caret {
				t.Errorf("the field became %q with the caret at %d", f.String(), f.caret)
			}
			if editor.String() != "слово" {
				t.Errorf("the clipboard path left %q", editor.String())
			}
		})
	}
}

func TestAPasswordFieldIsLeftAlone(t *testing.T) {
	f := newField("")
	f.err = field.ErrPassword
	s, editor := fieldSwitcher(t, f, "ckjdj")

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrPassword) {
		t.Fatalf("ConvertLastWord = %v, want ErrPassword", err)
	}
	if editor.copies != 0 || editor.String() != "ckjdj" {
		t.Errorf("the clipboard path ran: %d copies, text %q", editor.copies, editor.String())
	}
}

func TestLines(t *testing.T) {
	for _, tt := range []struct{ text, end, start string }{
		{"one\ntwo", "two", "one"},
		{"one\r\ntwo", "two", "one"},
		{"one\u2028two", "two", "one"},
		{"pic\ufffctext", "text", "pic"},
		{"plain", "plain", "plain"},
	} {
		if got := lineEnd(tt.text); got != tt.end {
			t.Errorf("lineEnd(%q) = %q, want %q", tt.text, got, tt.end)
		}
		if got := lineStart(tt.text); got != tt.start {
			t.Errorf("lineStart(%q) = %q, want %q", tt.text, got, tt.start)
		}
	}
	if !oneKeyEach("ghbdtn, ckjdj!") || oneKeyEach("e\u0301") || oneKeyEach("a\tb") || oneKeyEach(strings.Repeat("👍🏻", 1)) {
		t.Error("oneKeyEach misjudges what Backspace erases one at a time")
	}
}
