package switcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"unicode"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
)

// The fakes stand in for a text field, the clipboard and the layout list, so
// that the whole select-copy-convert-paste sequence runs without touching the
// machine.

type fakeClipboard struct {
	text  string
	saved string
	// broken makes every write fail.
	broken bool
}

func (c *fakeClipboard) Snapshot(context.Context) clip.Snapshot {
	c.saved = c.text
	return clip.Snapshot{}
}

func (c *fakeClipboard) Restore(context.Context, clip.Snapshot) error {
	c.text = c.saved
	return nil
}

func (c *fakeClipboard) WriteText(_ context.Context, text string) error {
	if c.broken {
		return errors.New("clipboard is locked by another application")
	}
	c.text = text
	return nil
}

func (c *fakeClipboard) AwaitText(_ context.Context, exclude string) (string, error) {
	if c.text == "" || c.text == exclude {
		return "", clip.ErrNoCopy
	}
	return c.text, nil
}

type fakeEditor struct {
	text      []rune
	selStart  int
	selEnd    int
	clipboard *fakeClipboard
	// deaf makes the editor ignore the copy shortcut.
	deaf bool
	// copies counts the copy shortcuts answered.
	copies int
}

func newEditor(text string, clipboard *fakeClipboard) *fakeEditor {
	runes := []rune(text)
	return &fakeEditor{
		text:      runes,
		selStart:  len(runes),
		selEnd:    len(runes),
		clipboard: clipboard,
	}
}

func (e *fakeEditor) String() string { return string(e.text) }

func (e *fakeEditor) ReleaseModifiers() error { return nil }

// isWordBoundary includes punctuation, as text fields do, which is why one
// "select the previous word" is not enough for ";bpym".
func isWordBoundary(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func (e *fakeEditor) SelectWordLeft(times int) error {
	pos := e.selStart
	for range times {
		for pos > 0 && isWordBoundary(e.text[pos-1]) {
			pos--
		}
		for pos > 0 && !isWordBoundary(e.text[pos-1]) {
			pos--
		}
	}
	e.selStart = pos
	return nil
}

func (e *fakeEditor) SelectCharsLeft(n int) error {
	e.selStart = max(0, e.selEnd-n)
	return nil
}

func (e *fakeEditor) CollapseSelectionRight() error {
	e.selStart = e.selEnd
	return nil
}

func (e *fakeEditor) Copy() error {
	if e.deaf {
		return nil
	}
	e.copies++
	e.clipboard.text = string(e.text[e.selStart:e.selEnd])
	return nil
}

func (e *fakeEditor) Paste() error {
	pasted := []rune(e.clipboard.text)
	rest := append(pasted, e.text[e.selEnd:]...)
	e.text = append(e.text[:e.selStart], rest...)
	e.selStart += len(pasted)
	e.selEnd = e.selStart
	return nil
}

type fakeLayouts struct {
	active     string
	activated  []string
	readFailed bool
}

func (l *fakeLayouts) Active() (string, error) {
	if l.readFailed {
		return "", errors.New("no focused window")
	}
	return l.active, nil
}

func (l *fakeLayouts) Activate(id string) error {
	l.active = id
	l.activated = append(l.activated, id)
	return nil
}

// newTestSwitcher uses the two built-in layouts, US first and Russian second.
func newTestSwitcher(t *testing.T, text string) (*Switcher, *fakeEditor, *fakeClipboard, *fakeLayouts) {
	t.Helper()

	layouts := keymap.Fallback()
	clipboard := &fakeClipboard{}
	editor := newEditor(text, clipboard)
	provider := &fakeLayouts{active: layouts[0].ID()}

	options := DefaultOptions()
	options.KeyDelay = 0
	options.PasteSettle = 0

	deps := Deps{Layouts: provider, Keyboard: editor, Clipboard: clipboard}
	s, err := New(layouts, deps, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, editor, clipboard, provider
}

func TestConvertLastWord(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"single word", "ckjdj", "слово"},
		{"last word only", "hello ckjdj", "hello слово"},
		{"word starting with punctuation", "hello ;bpym", "hello жизнь"},
		{"punctuation at the start of the line", ";bpym", "жизнь"},
		{"trailing space is kept", "hello ckjdj ", "hello слово "},
		{"capitals are kept", "Ghbdtn", "Привет"},
		{"punctuation inside the word", "hello j,]`v", "hello объём"},
		{"punctuation inside the only word", "j,]`v", "объём"},
		{"word ending in punctuation", "hello ckjdj/", "hello слово."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, editor, _, _ := newTestSwitcher(t, tt.text)
			if err := s.ConvertLastWord(t.Context()); err != nil {
				t.Fatalf("ConvertLastWord: %v", err)
			}
			if got := editor.String(); got != tt.want {
				t.Errorf("text is %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAPlainWordIsCopiedOnce(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "hello ckjdj")

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if editor.copies != 1 {
		t.Errorf("the word was copied %d times, want once", editor.copies)
	}
}

func TestConvertLastWordSwitchesLayout(t *testing.T) {
	s, _, _, provider := newTestSwitcher(t, "ckjdj")
	russian := keymap.Fallback()[1].ID()

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if len(provider.activated) != 1 || provider.activated[0] != russian {
		t.Errorf("activated layouts %v, want a single switch to %s", provider.activated, russian)
	}
}

func TestConvertLastWordCanLeaveTheLayoutAlone(t *testing.T) {
	s, editor, _, provider := newTestSwitcher(t, "ckjdj")
	s.SetSwitchLayout(false)

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if got := editor.String(); got != "слово" {
		t.Errorf("text is %q, want %q", got, "слово")
	}
	if len(provider.activated) != 0 {
		t.Errorf("the layout was switched to %v although switching is off", provider.activated)
	}
}

func TestConvertLastWordTwiceReturnsToTheOriginal(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "hello ckjdj")

	for i := range 2 {
		if err := s.ConvertLastWord(t.Context()); err != nil {
			t.Fatalf("ConvertLastWord (press %d): %v", i+1, err)
		}
	}
	if got := editor.String(); got != "hello ckjdj" {
		t.Errorf("after two presses text is %q, want the original", got)
	}
}

func TestConvertSelection(t *testing.T) {
	s, editor, _, provider := newTestSwitcher(t, "hello ckjdj ckjdj")
	editor.selStart = len("hello ")

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	if got := editor.String(); got != "hello слово слово" {
		t.Errorf("text is %q, want %q", got, "hello слово слово")
	}
	if len(provider.activated) != 0 {
		t.Errorf("the selection hotkey switched the layout to %v, it must leave it alone", provider.activated)
	}
}

func TestConvertRestoresTheClipboard(t *testing.T) {
	s, _, clipboard, _ := newTestSwitcher(t, "ckjdj")
	clipboard.text = "something the user copied earlier"

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if clipboard.text != "something the user copied earlier" {
		t.Errorf("clipboard holds %q, want what the user had put there", clipboard.text)
	}
}

func TestConvertWhenNothingIsCopied(t *testing.T) {
	s, editor, clipboard, _ := newTestSwitcher(t, "ckjdj")
	clipboard.text = "user content"
	editor.deaf = true

	err := s.ConvertLastWord(t.Context())
	if !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertLastWord = %v, want ErrNothingToConvert", err)
	}
	if editor.String() != "ckjdj" {
		t.Errorf("the text was changed to %q even though nothing could be copied", editor)
	}
	if clipboard.text != "user content" {
		t.Errorf("clipboard holds %q, want it restored", clipboard.text)
	}
}

func TestConvertWithAnUnreadableActiveLayout(t *testing.T) {
	s, editor, _, provider := newTestSwitcher(t, "ckjdj")
	provider.readFailed = true

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if got := editor.String(); got != "слово" {
		t.Errorf("text is %q, want %q", got, "слово")
	}
}

func TestConvertReportsClipboardFailure(t *testing.T) {
	s, editor, clipboard, _ := newTestSwitcher(t, "ckjdj")
	clipboard.broken = true

	if err := s.ConvertLastWord(t.Context()); err == nil {
		t.Fatal("ConvertLastWord succeeded with a clipboard that cannot be written")
	}
	if editor.String() != "ckjdj" {
		t.Errorf("the text was changed to %q despite the failure", editor)
	}
}

func TestConvertIsNotReentrant(t *testing.T) {
	s, _, _, _ := newTestSwitcher(t, "ckjdj")

	s.busy.Lock()
	defer s.busy.Unlock()

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrBusy) {
		t.Errorf("ConvertLastWord = %v, want ErrBusy", err)
	}
}

func TestNewNeedsTwoLayouts(t *testing.T) {
	_, err := New(keymap.Fallback()[:1], Deps{Layouts: &fakeLayouts{}},
		DefaultOptions(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, ErrNotEnoughLayouts) {
		t.Errorf("New with one layout = %v, want ErrNotEnoughLayouts", err)
	}
}

func TestMarkerIsUnique(t *testing.T) {
	first, second := marker(), marker()
	if first == second {
		t.Error("two markers came out identical")
	}
	if len(first) < 10 {
		t.Errorf("marker %q is too short to be unmistakable", first)
	}
}
