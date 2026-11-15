package switcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
)

// The fakes stand in for a text field, the clipboard and the layout list, so
// that the whole select-copy-convert-paste sequence runs without touching the
// machine.

type fakeClipboard struct {
	text  string
	saved string
	// broken makes every write fail; failOffers, the next so many offers.
	broken     bool
	failOffers int
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

func (c *fakeClipboard) Offer(ctx context.Context, text string) (<-chan struct{}, error) {
	if c.failOffers > 0 {
		c.failOffers--
		return nil, errors.New("clipboard is locked by another application")
	}
	if err := c.WriteText(ctx, text); err != nil {
		return nil, err
	}
	read := make(chan struct{})
	close(read)
	return read, nil
}

func (c *fakeClipboard) AwaitText(_ context.Context, exclude string) (string, error) {
	if c.text == "" || c.text == exclude {
		return "", clip.ErrNoCopy
	}
	return c.text, nil
}

// fakeEditor is a text field the way an editor driven by scripts implements
// one: the selection the keyboard makes on screen and the selection the
// editor works with, its model, are two things. The model follows the screen
// only once lag more shortcuts have been answered, or never when lag is
// negative. Copy and cut answer from the model, or from the screen when the
// model holds no selection; a paste goes over the model's. After a cut or a
// paste the two agree, with the caret where the text changed. With no lag
// this is an ordinary text field.
type fakeEditor struct {
	text  []rune
	caret int // where both selections end
	shown int // where the selection on screen starts
	model int // where the selection the editor works with starts
	lag   int
	// behind counts the shortcuts left before the model follows the screen.
	behind    int
	clipboard *fakeClipboard
	// halfway makes a lagging editor take in half of a selection change made
	// by several key presses at once, and the rest when it catches up.
	halfway bool
	// deaf makes the editor ignore copy and cut; uncuttable, cut alone.
	deaf, uncuttable bool
	// copies counts the copy shortcuts answered.
	copies int
}

func newEditor(text string, clipboard *fakeClipboard) *fakeEditor {
	e := &fakeEditor{text: []rune(text), clipboard: clipboard}
	e.place(len(e.text))
	return e
}

// place puts the caret, with nothing selected.
func (e *fakeEditor) place(caret int) { e.caret, e.shown, e.model = caret, caret, caret }

// selectFrom selects from start to the caret, as a user did some time ago.
func (e *fakeEditor) selectFrom(start int) { e.shown, e.model = start, start }

func (e *fakeEditor) String() string { return string(e.text) }

func (e *fakeEditor) ReleaseModifiers() error { return nil }

// moved notes a selection made with the keyboard.
func (e *fakeEditor) moved(shown int) {
	e.shown = shown
	if e.lag == 0 {
		e.model = shown
	}
	e.behind = e.lag
}

// answered notes a shortcut answered: the model may catch up after it.
func (e *fakeEditor) answered() {
	if e.behind > 0 {
		if e.behind--; e.behind == 0 {
			e.model = e.shown
		}
	}
}

// start is where the text a copy or a cut takes begins.
func (e *fakeEditor) start() int {
	if e.model == e.caret {
		return e.shown
	}
	return e.model
}

func (e *fakeEditor) SelectToLineStart() error {
	pos := e.shown
	for pos > 0 && e.text[pos-1] != '\n' {
		pos--
	}
	e.moved(pos)
	return nil
}

// SelectRight takes several key presses, so a lagging editor can be caught
// with only some of them taken in: with less of the line stepped over, more
// of it selected.
func (e *fakeEditor) SelectRight(n int) error {
	from := e.shown
	e.moved(min(from+n, e.caret))
	if e.halfway && e.lag != 0 && n > 1 {
		e.model = min(from+n/2, e.caret)
	}
	return nil
}

func (e *fakeEditor) CollapseSelectionRight() error {
	e.moved(e.caret)
	return nil
}

func (e *fakeEditor) Copy(bool) error {
	if e.deaf {
		return nil
	}
	e.copies++
	e.clipboard.text = string(e.text[e.start():e.caret])
	e.answered()
	return nil
}

func (e *fakeEditor) Cut() error {
	start := e.start()
	if e.deaf || e.uncuttable || start == e.caret {
		return nil
	}
	e.clipboard.text = string(e.text[start:e.caret])
	e.text = append(e.text[:start:start], e.text[e.caret:]...)
	e.place(start)
	return nil
}

func (e *fakeEditor) Paste() error {
	pasted := []rune(e.clipboard.text)
	rest := append(pasted, e.text[e.caret:]...)
	e.text = append(e.text[:e.model:e.model], rest...)
	e.place(e.model + len(pasted))
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
	t.Cleanup(s.Idle)
	return s, editor, clipboard, provider
}

// lags are how far behind the screen an editor's own idea of the selection
// is tried: not at all, by a few shortcuts, and for good.
var lags = []int{0, 1, 2, 3, 4, -1}

func TestConvertLastWord(t *testing.T) {
	tests := []struct {
		name string
		text string
		// tail is typed text after the caret, which must stay as it is.
		tail string
		want string
	}{
		{"single word", "ckjdj", "", "слово"},
		{"last word only", "hello ckjdj", "", "hello слово"},
		{"word starting with punctuation", "hello ;bpym", "", "hello жизнь"},
		{"punctuation at the start of the line", ";bpym", "", "жизнь"},
		{"trailing space is kept", "hello ckjdj ", "", "hello слово "},
		{"capitals are kept", "Ghbdtn", "", "Привет"},
		{"punctuation inside the word", "hello j,]`v", "", "hello объём"},
		{"punctuation inside the only word", "j,]`v", "", "объём"},
		{"word ending in punctuation", "hello ckjdj/", "", "hello слово."},
		{"a long line with punctuation inside the word", "ytn? ghj,ktvf yt bcghfdktyf b lj,fdbkfcm", "", "ytn? ghj,ktvf yt bcghfdktyf b добавилась"},
		{"an apostrophe inside the word", "ghbdtn? xnj yt nfr C'nbv", "", "ghbdtn? xnj yt nfr Сэтим"},
		{"the caret in the middle of the line", "hello ckjdj", " world", "hello слово world"},
		{"the caret inside the word", "hello ckj", "dj", "hello слоdj"},
		{"the caret inside the only word", "ckj", "dj", "слоdj"},
		{"only the line the caret is on", "one\nhello ckjdj", "", "one\nhello слово"},
		{"the first word of a later line", "one\nckjdj", "", "one\nслово"},
	}
	for _, tt := range tests {
		for i, lag := range append(lags, lags...) {
			halfway := i >= len(lags)
			t.Run(fmt.Sprintf("%s/lag %d/halfway %v", tt.name, lag, halfway), func(t *testing.T) {
				s, editor, _, _ := newTestSwitcher(t, tt.text+tt.tail)
				editor.lag, editor.halfway = lag, halfway
				editor.place(len([]rune(tt.text)))
				if err := s.ConvertLastWord(t.Context()); err != nil {
					t.Fatalf("ConvertLastWord: %v", err)
				}
				if got := editor.String(); got != tt.want {
					t.Errorf("text is %q, want %q", got, tt.want)
				}
			})
		}
	}
}

func TestConvertLastWordTwiceReturnsToTheOriginal(t *testing.T) {
	for _, lag := range lags {
		t.Run(fmt.Sprintf("lag %d", lag), func(t *testing.T) {
			s, editor, _, _ := newTestSwitcher(t, "ytn? ghj,ktvf yt bcghfdktyf b lj,fdbkfcm")
			editor.lag = lag
			for i := range 2 {
				if err := s.ConvertLastWord(t.Context()); err != nil {
					t.Fatalf("ConvertLastWord (press %d): %v", i+1, err)
				}
			}
			if got := editor.String(); got != "ytn? ghj,ktvf yt bcghfdktyf b lj,fdbkfcm" {
				t.Errorf("after two presses text is %q, want the original", got)
			}
		})
	}
}

func TestAnApplicationThatDoesNotCutIsLeftAlone(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "hello ckjdj")
	editor.uncuttable = true

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrNotSelected) {
		t.Fatalf("ConvertLastWord = %v, want ErrNotSelected", err)
	}
	if got := editor.String(); got != "hello ckjdj" {
		t.Errorf("text is %q, want it untouched", got)
	}
}

func TestTheWordGoesBackWhenItsConversionCannotBePasted(t *testing.T) {
	s, editor, clipboard, _ := newTestSwitcher(t, "hello ckjdj")
	clipboard.failOffers = 1

	if err := s.ConvertLastWord(t.Context()); err == nil {
		t.Fatal("ConvertLastWord succeeded although the paste failed")
	}
	if got := editor.String(); got != "hello ckjdj" {
		t.Errorf("text is %q, want the cut word back", got)
	}
}

func TestASelectionOfSpacesIsPutBack(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "hello   ")
	editor.selectFrom(len("hello"))

	if err := s.ConvertSelection(t.Context()); !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertSelection = %v, want ErrNothingToConvert", err)
	}
	if got := editor.String(); got != "hello   " {
		t.Errorf("text is %q, want it untouched", got)
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
	s.SetSwitchLayout(false, true)

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

func TestConvertSelection(t *testing.T) {
	s, editor, _, provider := newTestSwitcher(t, "hello ckjdj ckjdj")
	editor.selectFrom(len("hello "))

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	if got := editor.String(); got != "hello слово слово" {
		t.Errorf("text is %q, want %q", got, "hello слово слово")
	}
	if russian := keymap.Fallback()[1].ID(); len(provider.activated) != 1 || provider.activated[0] != russian {
		t.Errorf("activated layouts %v, want a single switch to %s", provider.activated, russian)
	}
}

func TestConvertSelectionCanLeaveTheLayoutAlone(t *testing.T) {
	s, editor, _, provider := newTestSwitcher(t, "ckjdj")
	editor.selectFrom(0)
	s.SetSwitchLayout(true, false)

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	if len(provider.activated) != 0 {
		t.Errorf("the layout was switched to %v although switching after a selection is off", provider.activated)
	}
}

func TestConvertRestoresTheClipboard(t *testing.T) {
	s, _, clipboard, _ := newTestSwitcher(t, "ckjdj")
	clipboard.text = "something the user copied earlier"

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	s.Idle()
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
	s.Idle()
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
