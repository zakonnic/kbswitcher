// Package switcher performs the conversion a hotkey asks for: select, copy,
// convert, paste, exactly as a user would by hand. Nothing is read from the
// keyboard or from any window at any other time.
package switcher

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
)

// Keyboard replays the shortcuts that select, copy and paste text in the
// focused application.
type Keyboard interface {
	// ReleaseModifiers lets go of the modifiers the user still holds from the
	// hotkey, so that they cannot alter the shortcuts sent next.
	ReleaseModifiers() error
	// SelectWordLeft extends the selection by whole words towards the start of
	// the line.
	SelectWordLeft(times int) error
	// SelectCharsLeft extends the selection by n characters towards the start
	// of the line.
	SelectCharsLeft(n int) error
	// CollapseSelectionRight drops the selection, leaving the caret at its end.
	CollapseSelectionRight() error
	Copy() error
	Paste() error
}

// Clipboard is the part of the system clipboard this package needs.
type Clipboard interface {
	Snapshot(ctx context.Context) clip.Snapshot
	Restore(ctx context.Context, snapshot clip.Snapshot) error
	WriteText(ctx context.Context, text string) error
	AwaitText(ctx context.Context, exclude string) (string, error)
}

// Terminal borrows the console of the shell behind the focused window. A
// console answers no copy or paste shortcut, so its input line is read and
// rewritten directly instead.
type Terminal interface {
	// Open fails, and the clipboard path is used, whenever the focused window
	// is an ordinary application or its console cannot be identified.
	Open() (Console, error)
}

// Console is one borrowed console, held for the length of a conversion.
type Console interface {
	// Line returns the input row up to the caret, prompt included.
	Line() (string, error)
	// Delete asks the shell to erase n characters before the caret and reports
	// how many it agreed to erase; a shell never erases its own prompt.
	Delete(n int) (int, error)
	// Type writes text into the input line.
	Type(text string) error
	Close() error
}

// Layouts reports and changes the layout of the focused window.
type Layouts interface {
	Active() (string, error)
	Activate(id string) error
}

// Expected outcomes of pressing the hotkey at the wrong moment.
var (
	// ErrBusy means a conversion is already running; a press that arrives
	// meanwhile is dropped rather than queued.
	ErrBusy = errors.New("a conversion is already running")
	// ErrNothingToConvert means there was no text to work on.
	ErrNothingToConvert = errors.New("nothing to convert")
	// ErrNotEnoughLayouts means there is nothing to convert between.
	ErrNotEnoughLayouts = errors.New("at least two keyboard layouts are needed")
)

// Options are the timings and limits of a conversion. They exist so that tests
// run without waiting and a slow remote desktop can be accommodated.
type Options struct {
	// KeyDelay is the pause between two bursts of synthetic key presses.
	KeyDelay time.Duration
	// PasteSettle is how long the converted text stays on the clipboard after
	// the paste, before the previous content is restored.
	PasteSettle time.Duration
	// WordSelections is how many times "select the previous word" is sent.
	// Two, because text fields treat punctuation as a word boundary while it
	// may be part of the word in another layout: ";bpym" is "жизнь". Only the
	// last whitespace-delimited word is converted, so selecting more changes
	// nothing.
	WordSelections int
	// MaxNarrowedChars bounds the "select exactly the word again" step. Beyond
	// it the whole selection is replaced, which costs formatting in rich text
	// editors but never sends hundreds of key presses.
	MaxNarrowedChars int
}

// DefaultOptions returns the timings used in production.
func DefaultOptions() Options {
	return Options{
		KeyDelay:         30 * time.Millisecond,
		PasteSettle:      150 * time.Millisecond,
		WordSelections:   2,
		MaxNarrowedChars: 120,
	}
}

// Deps are the platform pieces a conversion works through. Terminal is
// optional; without it every window takes the clipboard path.
type Deps struct {
	Layouts   Layouts
	Keyboard  Keyboard
	Clipboard Clipboard
	Terminal  Terminal
}

// Switcher converts text between the layouts installed on the machine.
type Switcher struct {
	layouts []*keymap.Layout
	deps    Deps
	options Options
	log     *slog.Logger

	// busy admits one conversion at a time.
	busy sync.Mutex
}

// New builds a switcher over the layouts read at start-up.
func New(layouts []*keymap.Layout, deps Deps, options Options, log *slog.Logger) (*Switcher, error) {
	if len(layouts) < 2 {
		return nil, ErrNotEnoughLayouts
	}
	return &Switcher{layouts: layouts, deps: deps, options: options, log: log}, nil
}

// Layouts returns the layouts the switcher cycles through, in order.
func (s *Switcher) Layouts() []*keymap.Layout { return s.layouts }

// ConvertLastWord rewrites the word before the caret as if it had been typed in
// the next layout, and switches the focused window to that layout.
func (s *Switcher) ConvertLastWord(ctx context.Context) error {
	return s.convert(ctx, true)
}

// ConvertSelection rewrites the selected text as if it had been typed in the
// next layout. The layout of the focused window is left alone.
func (s *Switcher) ConvertSelection(ctx context.Context) error {
	return s.convert(ctx, false)
}

func (s *Switcher) convert(ctx context.Context, lastWordOnly bool) error {
	if !s.busy.TryLock() {
		return ErrBusy
	}
	defer s.busy.Unlock()

	if console := s.console(); console != nil {
		defer func() {
			if err := console.Close(); err != nil {
				s.log.Warn("could not release the console", "error", err)
			}
		}()
		return s.convertInConsole(console, lastWordOnly)
	}
	return s.convertByClipboard(ctx, lastWordOnly)
}

// active is the index of the layout the focused window uses, or the first one.
func (s *Switcher) active() int {
	if id, err := s.deps.Layouts.Active(); err == nil {
		if index := s.indexOf(id); index >= 0 {
			return index
		}
	}
	return 0
}

// console borrows the console behind the focused window, or returns nil when
// the clipboard path is the one to use.
func (s *Switcher) console() Console {
	if s.deps.Terminal == nil {
		return nil
	}
	console, err := s.deps.Terminal.Open()
	if err != nil {
		s.log.Debug("no console behind the focused window", "reason", err)
		return nil
	}
	return console
}

// convertInConsole rewrites the shell's input line in place. How much the
// shell agrees to erase is what bounds the conversion: a prompt refuses, so a
// hotkey pressed on an empty line changes nothing.
func (s *Switcher) convertInConsole(console Console, lastWordOnly bool) error {
	line, err := console.Line()
	if err != nil {
		return err
	}

	// Without a word to isolate, everything the shell lets go of is converted,
	// which is the whole command the user typed.
	typed := line
	if lastWordOnly {
		_, word, tail := keymap.SplitLastWord(line)
		typed = word + tail
	}
	if typed == "" {
		return ErrNothingToConvert
	}

	erased, err := console.Delete(utf8.RuneCountInString(typed))
	if err != nil {
		return err
	}
	if erased == 0 {
		return ErrNothingToConvert
	}

	word := lastRunes(typed, erased)
	source, target := s.pickLayouts(word)
	converted := keymap.Convert(word, s.layouts[source], s.layouts[target])
	s.log.Debug("converting in a console",
		"from", s.layouts[source].ID(), "to", s.layouts[target].ID(),
		"characters", erased)

	if err := console.Type(converted); err != nil {
		if undo := console.Type(word); undo != nil {
			s.log.Error("the erased text could not be put back", "error", undo)
		}
		return err
	}

	if lastWordOnly {
		if err := s.deps.Layouts.Activate(s.layouts[target].ID()); err != nil {
			s.log.Warn("could not switch the keyboard layout",
				"layout", s.layouts[target].ID(), "error", err)
		}
	}
	return nil
}

// lastRunes returns the last n runes of text.
func lastRunes(text string, n int) string {
	runes := []rune(text)
	if n >= len(runes) {
		return text
	}
	return string(runes[len(runes)-n:])
}

// convertByClipboard is the path for ordinary applications: select, copy,
// convert, paste, exactly as a user would by hand.
func (s *Switcher) convertByClipboard(ctx context.Context, lastWordOnly bool) error {
	// The clipboard goes back to the user whatever happens, even after the
	// conversion timed out.
	snapshot := s.deps.Clipboard.Snapshot(ctx)
	defer func() {
		if err := s.deps.Clipboard.Restore(context.WithoutCancel(ctx), snapshot); err != nil {
			s.log.Warn("could not restore the clipboard", "error", err)
		}
	}()

	selected, err := s.readText(ctx, lastWordOnly)
	if err != nil {
		return err
	}

	var head, word, tail string
	if lastWordOnly {
		head, word, tail = keymap.SplitLastWord(selected)
	} else {
		word = selected
	}
	if word == "" {
		return ErrNothingToConvert
	}

	source, target := s.pickLayouts(word)
	converted := keymap.Convert(word, s.layouts[source], s.layouts[target])
	s.log.Debug("converting",
		"from", s.layouts[source].ID(), "to", s.layouts[target].ID(),
		"characters", utf8.RuneCountInString(word))

	if err := s.replace(ctx, head, converted+tail, utf8.RuneCountInString(word+tail)); err != nil {
		return err
	}

	if lastWordOnly {
		if err := s.deps.Layouts.Activate(s.layouts[target].ID()); err != nil {
			// The text is already fixed; this is worth a log line and no more.
			s.log.Warn("could not switch the keyboard layout",
				"layout", s.layouts[target].ID(), "error", err)
		}
	}
	return nil
}

// readText selects the text to convert, when asked to, and copies it out of
// the focused application.
func (s *Switcher) readText(ctx context.Context, selectWord bool) (string, error) {
	if err := s.deps.Keyboard.ReleaseModifiers(); err != nil {
		return "", err
	}

	// The marker tells a fresh copy apart from content that was already there.
	marker := marker()
	if err := s.deps.Clipboard.WriteText(ctx, marker); err != nil {
		return "", err
	}

	if selectWord {
		if err := s.deps.Keyboard.SelectWordLeft(s.options.WordSelections); err != nil {
			return "", err
		}
		s.pause()
	}
	if err := s.deps.Keyboard.Copy(); err != nil {
		return "", err
	}

	text, err := s.deps.Clipboard.AwaitText(ctx, marker)
	if errors.Is(err, clip.ErrNoCopy) {
		return "", fmt.Errorf("%w: the application did not answer the copy shortcut", ErrNothingToConvert)
	}
	if err != nil {
		return "", err
	}
	return text, nil
}

// replace writes the converted text over the selection. When only the tail of
// the selection changed, the selection is first narrowed down to it, so that
// the formatting of the rest survives in rich text editors.
func (s *Switcher) replace(ctx context.Context, unchanged, replacement string, replacementChars int) error {
	narrow := unchanged != "" && replacementChars > 0 && replacementChars <= s.options.MaxNarrowedChars
	if narrow {
		if err := s.deps.Keyboard.CollapseSelectionRight(); err != nil {
			return err
		}
		s.pause()
		if err := s.deps.Keyboard.SelectCharsLeft(replacementChars); err != nil {
			return err
		}
		s.pause()
	} else {
		replacement = unchanged + replacement
	}

	if err := s.deps.Clipboard.WriteText(ctx, replacement); err != nil {
		return err
	}
	s.pause()
	if err := s.deps.Keyboard.Paste(); err != nil {
		return err
	}
	s.settle()
	return nil
}

// pickLayouts guesses which layout the text was typed in and picks the next
// one in the cycle as the target. The active layout wins ties.
func (s *Switcher) pickLayouts(typed string) (source, target int) {
	source = keymap.Detect(typed, s.layouts, s.active())
	return source, (source + 1) % len(s.layouts)
}

func (s *Switcher) indexOf(id string) int {
	for i, layout := range s.layouts {
		if layout.ID() == id {
			return i
		}
	}
	return -1
}

func (s *Switcher) pause() { time.Sleep(s.options.KeyDelay) }

func (s *Switcher) settle() { time.Sleep(s.options.PasteSettle) }

// zeroWidthSpace makes an accidental paste of the marker show nothing.
const zeroWidthSpace = "\u200b"

// marker returns the random string put on the clipboard before a copy.
func marker() string { return zeroWidthSpace + rand.Text() }
