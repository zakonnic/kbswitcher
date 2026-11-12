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
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
	"github.com/zakonnic/kbswitcher/internal/platform/console"
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
	// Offer puts text on the clipboard for a paste. The channel is closed once
	// the application has read it.
	Offer(ctx context.Context, text string) (<-chan struct{}, error)
	AwaitText(ctx context.Context, exclude string) (string, error)
}

// Terminal borrows the console of the shell behind the focused window. A
// console answers no copy or paste shortcut, so its input line is read and
// rewritten directly instead.
type Terminal interface {
	// Open fails with ErrNoConsole, and the clipboard path is used, when the
	// focused window is an ordinary application. Any other failure means the
	// window is a terminal that cannot be worked with, and nothing is done: a
	// shell answers the clipboard shortcuts by canceling or mangling the line.
	Open() (Console, error)
}

// ErrNoConsole is what a Terminal reports for a window without a console.
var ErrNoConsole = errors.New("the focused window has no console")

// Console is one borrowed console, held for the length of a conversion.
type Console interface {
	// Edit shows change the shell's input line, whole or up to the caret,
	// and has the shell replace the characters from..to with text.
	Edit(whole bool, change func(line console.Line) (from, to int, text string)) error
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
	// KeyDelay is a pause after each burst of synthetic key presses, for
	// applications that fall behind their input queue.
	KeyDelay time.Duration
	// PasteTimeout bounds the wait for the application to read the pasted
	// text before the previous clipboard content is restored anyway.
	PasteTimeout time.Duration
	// WordSelections are the "select the previous word" presses sent in
	// successive rounds. Applications stop at punctuation, which may be a
	// letter in another layout (";bpym" is "жизнь", "j,]`v" is "объём"), so
	// after each round the selection is copied and, unless whitespace precedes
	// the last word by now, the next round adds more presses. Selecting too
	// much is harmless: only the last whitespace-delimited word is replaced.
	WordSelections []int
	// MaxNarrowedChars bounds the "select exactly the word again" step. Beyond
	// it the whole selection is replaced, which costs formatting in rich text
	// editors but never sends hundreds of key presses.
	MaxNarrowedChars int
}

// DefaultOptions returns the timings used in production. Key presses go
// through the application's input queue in order, so no pause is needed
// between them.
func DefaultOptions() Options {
	return Options{
		KeyDelay:         0,
		PasteTimeout:     time.Second,
		WordSelections:   []int{2, 6, 8},
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

	// switchLayout says whether ConvertLastWord also moves the focused window
	// to the layout the word now belongs to. Settings can change it at any time.
	switchLayout atomic.Bool

	// busy admits one conversion at a time.
	busy sync.Mutex
}

// New builds a switcher over the layouts read at start-up. It switches the
// layout after a word until SetSwitchLayout says otherwise.
func New(layouts []*keymap.Layout, deps Deps, options Options, log *slog.Logger) (*Switcher, error) {
	if len(layouts) < 2 {
		return nil, ErrNotEnoughLayouts
	}
	s := &Switcher{layouts: layouts, deps: deps, options: options, log: log}
	s.switchLayout.Store(true)
	return s, nil
}

// SetSwitchLayout turns the layout switch after ConvertLastWord on or off.
func (s *Switcher) SetSwitchLayout(on bool) { s.switchLayout.Store(on) }

// Layouts returns the layouts the switcher cycles through, in order.
func (s *Switcher) Layouts() []*keymap.Layout { return s.layouts }

// ConvertLastWord rewrites the word before the caret as if it had been typed in
// the next layout and, unless that is turned off, switches the focused window
// to that layout.
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

	console, err := s.console()
	if err != nil {
		return err
	}
	var target *keymap.Layout
	if console != nil {
		defer func() {
			if err := console.Close(); err != nil {
				s.log.Warn("could not release the console", "error", err)
			}
		}()
		target, err = s.convertInConsole(console, lastWordOnly)
	} else {
		// The clipboard goes back to the user whatever happens, even after the
		// conversion timed out, but only once the layout has been switched:
		// the user is typing again by then.
		snapshot := s.deps.Clipboard.Snapshot(ctx)
		var pasted <-chan struct{}
		defer s.restore(ctx, snapshot, &pasted)
		target, err = s.convertByClipboard(ctx, lastWordOnly, &pasted)
	}
	if err != nil {
		return err
	}
	if lastWordOnly && s.switchLayout.Load() {
		s.activate(target)
	}
	return nil
}

// restore gives the clipboard back once the application has read the paste.
func (s *Switcher) restore(ctx context.Context, snapshot clip.Snapshot, pasted *<-chan struct{}) {
	if *pasted != nil {
		select {
		case <-*pasted:
		case <-time.After(s.options.PasteTimeout):
			s.log.Debug("the application did not read the paste in time")
		}
	}
	if err := s.deps.Clipboard.Restore(context.WithoutCancel(ctx), snapshot); err != nil {
		s.log.Warn("could not restore the clipboard", "error", err)
	}
}

// activate switches the focused window to a layout. The text is already fixed,
// so a failure is worth a log line and no more.
func (s *Switcher) activate(layout *keymap.Layout) {
	if err := s.deps.Layouts.Activate(layout.ID()); err != nil {
		s.log.Warn("could not switch the keyboard layout", "layout", layout.ID(), "error", err)
	}
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

// console borrows the console behind the focused window. It returns nil when
// the window has none and the clipboard path is the one to use, and an error
// when the window is a terminal that must be left alone.
func (s *Switcher) console() (Console, error) {
	if s.deps.Terminal == nil {
		return nil, nil
	}
	console, err := s.deps.Terminal.Open()
	switch {
	case err == nil:
		return console, nil
	case errors.Is(err, ErrNoConsole):
		s.log.Debug("no console behind the focused window", "reason", err)
		return nil, nil
	default:
		return nil, fmt.Errorf("the terminal is left alone: %w", err)
	}
}

// convertInConsole rewrites the shell's input line in place: the last word
// before the caret, or else the text the shell shows as selected, or the
// whole line when it shows none (a selection made with the mouse belongs to
// the terminal, and no shell knows of it).
func (s *Switcher) convertInConsole(terminal Console, lastWordOnly bool) (*keymap.Layout, error) {
	var target *keymap.Layout
	err := terminal.Edit(!lastWordOnly, func(line console.Line) (int, int, string) {
		text := []rune(line.Text)
		from, to := 0, len(text)
		switch {
		case lastWordOnly:
			to = min(max(line.Caret, 0), len(text))
			head, _, _ := keymap.SplitLastWord(string(text[:to]))
			from = utf8.RuneCountInString(head)
		case line.SelStart < line.SelEnd && line.SelEnd <= len(text):
			from, to = max(line.SelStart, 0), line.SelEnd
		}
		typed := string(text[from:to])
		if strings.TrimSpace(typed) == "" {
			return 0, 0, ""
		}
		source, next := s.pickLayouts(typed)
		target = s.layouts[next]
		s.log.Debug("converting in a console",
			"from", s.layouts[source].ID(), "to", target.ID(), "characters", to-from)
		return from, to, keymap.Convert(typed, s.layouts[source], target)
	})
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrNothingToConvert
	}
	return target, nil
}

// convertByClipboard is the path for ordinary applications: select, copy,
// convert, paste, exactly as a user would by hand.
func (s *Switcher) convertByClipboard(ctx context.Context, lastWordOnly bool, pasted *<-chan struct{}) (*keymap.Layout, error) {
	selected, err := s.readText(ctx, lastWordOnly)
	if err != nil {
		return nil, err
	}

	var head, word, tail string
	if lastWordOnly {
		head, word, tail = keymap.SplitLastWord(selected)
	} else {
		word = selected
	}
	if word == "" {
		return nil, ErrNothingToConvert
	}

	source, target := s.pickLayouts(word)
	converted := keymap.Convert(word, s.layouts[source], s.layouts[target])
	s.log.Debug("converting",
		"from", s.layouts[source].ID(), "to", s.layouts[target].ID(),
		"characters", utf8.RuneCountInString(word))

	if err := s.replace(ctx, head, converted+tail, utf8.RuneCountInString(word+tail), pasted); err != nil {
		return nil, err
	}
	return s.layouts[target], nil
}

// readText copies the text to convert out of the focused application: the
// selection as it is, or the last word after selecting it.
func (s *Switcher) readText(ctx context.Context, selectWord bool) (string, error) {
	if err := s.deps.Keyboard.ReleaseModifiers(); err != nil {
		return "", err
	}
	if !selectWord {
		return s.copySelection(ctx)
	}
	return s.selectLastWord(ctx)
}

// selectLastWord extends the selection backwards word by word until
// whitespace precedes the last word or the selection stops growing, and
// returns what is selected.
func (s *Switcher) selectLastWord(ctx context.Context) (string, error) {
	var selected string
	for _, presses := range s.options.WordSelections {
		if err := s.deps.Keyboard.SelectWordLeft(presses); err != nil {
			return "", err
		}
		s.pause()
		text, err := s.copySelection(ctx)
		if err != nil {
			return "", err
		}
		if head, _, _ := keymap.SplitLastWord(text); head != "" || text == selected {
			return text, nil
		}
		selected = text
	}
	return selected, nil
}

// copySelection copies the selection through the clipboard. The marker tells
// a fresh copy apart from content that was already there.
func (s *Switcher) copySelection(ctx context.Context) (string, error) {
	marker := marker()
	if err := s.deps.Clipboard.WriteText(ctx, marker); err != nil {
		return "", err
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
func (s *Switcher) replace(ctx context.Context, unchanged, replacement string, replacementChars int, pasted *<-chan struct{}) error {
	narrow := unchanged != "" && replacementChars > 0 && replacementChars <= s.options.MaxNarrowedChars
	if narrow {
		if err := s.deps.Keyboard.CollapseSelectionRight(); err != nil {
			return err
		}
		if err := s.deps.Keyboard.SelectCharsLeft(replacementChars); err != nil {
			return err
		}
		s.pause()
	} else {
		replacement = unchanged + replacement
	}

	read, err := s.deps.Clipboard.Offer(ctx, replacement)
	if err != nil {
		return err
	}
	if err := s.deps.Keyboard.Paste(); err != nil {
		return err
	}
	*pasted = read
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

func (s *Switcher) pause() {
	if s.options.KeyDelay > 0 {
		time.Sleep(s.options.KeyDelay)
	}
}

// zeroWidthSpace makes an accidental paste of the marker show nothing.
const zeroWidthSpace = "\u200b"

// marker returns the random string put on the clipboard before a copy.
func marker() string { return zeroWidthSpace + rand.Text() }
