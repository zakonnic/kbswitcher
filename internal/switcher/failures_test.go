package switcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
)

var errKeys = errors.New("SendInput delivered nothing")

// brokenKeyboard is an editor whose named action fails: Windows took none
// of its keys.
type brokenKeyboard struct {
	*fakeEditor
	broken string
}

func (k brokenKeyboard) fail(name string) error {
	if k.broken == name {
		return errKeys
	}
	return nil
}

func (k brokenKeyboard) ReleaseModifiers() error { return k.fail("release") }
func (k brokenKeyboard) SelectToLineStart() error {
	return errors.Join(k.fail("home"), k.fakeEditor.SelectToLineStart())
}
func (k brokenKeyboard) SelectRight(n int) error {
	if err := k.fail("right"); err != nil {
		return err
	}
	return k.fakeEditor.SelectRight(n)
}
func (k brokenKeyboard) Copy(gentle bool) error {
	if err := k.fail("copy"); err != nil {
		return err
	}
	return k.fakeEditor.Copy(gentle)
}
func (k brokenKeyboard) Paste() error {
	if err := k.fail("paste"); err != nil {
		return err
	}
	return k.fakeEditor.Paste()
}

func TestAKeyboardThatFailsStopsTheConversion(t *testing.T) {
	for _, broken := range []string{"release", "home", "right", "copy", "paste"} {
		for _, selection := range []bool{false, true} {
			layouts := keymap.Fallback()
			clipboard := &fakeClipboard{}
			editor := newEditor("hello ckjdj", clipboard)
			if selection {
				editor.selectFrom(len("hello "))
			}
			deps := Deps{Layouts: &fakeLayouts{active: layouts[0].ID()},
				Keyboard: brokenKeyboard{editor, broken}, Clipboard: clipboard}
			s, err := New(layouts, deps, DefaultOptions(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			convert := s.ConvertLastWord
			if selection {
				convert = s.ConvertSelection
			}
			err = convert(t.Context())
			s.Idle()
			// A selection takes no Shift+Home or Shift+Right: those cannot fail it.
			if skipped := selection && (broken == "home" || broken == "right"); !skipped && !errors.Is(err, errKeys) {
				t.Errorf("%s broken, selection %v: %v", broken, selection, err)
			}
		}
	}
}

func TestALineOfSpacesHasNothingToConvert(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "one\n   ")
	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertLastWord = %v", err)
	}
	if editor.String() != "one\n   " || editor.shown != editor.caret {
		t.Errorf("the field became %q with a selection from %d", editor.String(), editor.shown)
	}
}

func TestASelectionOfSpacesGoesBackAsItWas(t *testing.T) {
	s, editor, _, _ := newTestSwitcher(t, "a   b")
	editor.place(len("a   "))
	editor.selectFrom(1)
	if err := s.ConvertSelection(t.Context()); !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertSelection = %v", err)
	}
	if editor.String() != "a   b" {
		t.Errorf("the field became %q", editor.String())
	}
}

// slowLayouts is a layout list that answers with what it cannot do.
type slowLayouts struct{ fakeLayouts }

func (l *slowLayouts) Activate(string) error { return errors.New("the window is gone") }

// lateClipboard never sees a paste read.
type lateClipboard struct{ *fakeClipboard }

func (c lateClipboard) Offer(ctx context.Context, text string) (<-chan struct{}, error) {
	if err := c.WriteText(ctx, text); err != nil {
		return nil, err
	}
	return make(chan struct{}), nil
}

func (c lateClipboard) Restore(context.Context, clip.Snapshot) error {
	return errors.New("the clipboard is held")
}

func TestConversionsLiveWithWhatTheSystemRefuses(t *testing.T) {
	layouts := keymap.Fallback()
	clipboard := &fakeClipboard{}
	editor := newEditor("hello ckjdj", clipboard)
	options := DefaultOptions()
	options.KeyDelay = time.Millisecond
	options.PasteTimeout = 10 * time.Millisecond
	deps := Deps{Layouts: &slowLayouts{fakeLayouts{active: "unknown"}}, Keyboard: editor, Clipboard: lateClipboard{clipboard}}
	s, err := New(layouts, deps, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	s.Idle()
	if editor.String() != "hello слово" || len(s.Layouts()) != 2 {
		t.Errorf("the field became %q", editor.String())
	}
}
