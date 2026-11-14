// Package switcher performs the conversion a hotkey asks for: select, copy,
// cut, convert, paste, as a user would by hand. Nothing is read from the
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

// Keyboard replays the shortcuts that select, copy, cut and paste text in
// the focused application.
type Keyboard interface {
	// ReleaseModifiers lets go of the modifiers the user still holds from the
	// hotkey, so that they cannot alter the shortcuts sent next.
	ReleaseModifiers() error
	// SelectToLineStart extends the selection to the start of the line, with
	// a single key press.
	SelectToLineStart() error
	// SelectCharsLeft extends the selection by n characters towards the start
	// of the line.
	SelectCharsLeft(n int) error
	// CollapseSelectionRight drops the selection, leaving the caret at its end.
	CollapseSelectionRight() error
	// Copy presses the copy shortcut. With gentle set it is one that no
	// shell takes for an interrupt, where there is such a shortcut.
	Copy(gentle bool) error
	Cut() error
	Paste() error
}

// Clipboard is the part of the system clipboard this package needs.
type Clipboard interface {
	Snapshot(ctx context.Context) clip.Snapshot
	Restore(ctx context.Context, snapshot clip.Snapshot) error
	WriteText(ctx context.Context, text string) error
	// Offer puts text on the clipboard for a paste. The channel is closed once
	// something has read it, or at once where that cannot be told.
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
	// selection says that a selection is being converted and must survive
	// whatever it takes to find the console.
	Open(selection bool) (Console, error)
}

// What a Terminal reports for a window to take the clipboard path in.
var (
	// ErrNoConsole means no console was found behind the focus.
	ErrNoConsole = errors.New("the focused window has no console")
	// ErrShellsNearby comes with ErrNoConsole when the window runs shells
	// all the same, and the focus may be on one that did not show.
	ErrShellsNearby = errors.New("the window runs shells")
)

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
	// ErrNotSelected means the application did not give up the text that was
	// read, so nothing was replaced.
	ErrNotSelected = errors.New("the application did not cut the text to replace; nothing was replaced")
)

// cutPatience is how many times longer than a copy a cut is waited for. A
// copy has shown by then that there is text to cut, and a cut given up on
// too early would still take the text out, with nothing put in its place.
const cutPatience = 4

// Options are the timings and limits of a conversion. They exist so that tests
// run without waiting and a slow remote desktop can be accommodated.
type Options struct {
	// KeyDelay is a pause after each burst of synthetic key presses, for
	// applications that fall behind their input queue.
	KeyDelay time.Duration
	// PasteTimeout bounds the wait for the pasted text to be read off the
	// clipboard, and PasteSettle is how long it stays there after that: the
	// first to read it may be a clipboard monitor, the application's own
	// included, with the paste itself a moment behind.
	PasteTimeout time.Duration
	PasteSettle  time.Duration
	// MaxNarrowedChars bounds the "select exactly the word" step. Beyond it
	// the whole line up to the caret is replaced, which costs formatting in
	// rich text editors but never sends hundreds of key presses.
	MaxNarrowedChars int
}

// DefaultOptions returns the timings used in production. Key presses go
// through the application's input queue in order, so no pause is needed
// between them.
func DefaultOptions() Options {
	return Options{
		KeyDelay:         0,
		PasteTimeout:     time.Second,
		PasteSettle:      250 * time.Millisecond,
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

	// switchAfterWord and switchAfterSelection say whether converting moves
	// the focused window to the layout the text now belongs to. Settings can
	// change them at any time.
	switchAfterWord, switchAfterSelection atomic.Bool

	// busy admits one conversion at a time.
	busy sync.Mutex
	// returned is closed once the clipboard the last conversion borrowed is
	// back with the user. Guarded by busy.
	returned chan struct{}
}

// New builds a switcher over the layouts read at start-up. It switches the
// layout after converting until SetSwitchLayout says otherwise.
func New(layouts []*keymap.Layout, deps Deps, options Options, log *slog.Logger) (*Switcher, error) {
	if len(layouts) < 2 {
		return nil, ErrNotEnoughLayouts
	}
	s := &Switcher{layouts: layouts, deps: deps, options: options, log: log}
	s.SetSwitchLayout(true, true)
	return s, nil
}

// SetSwitchLayout turns the layout switch after ConvertLastWord and after
// ConvertSelection on or off.
func (s *Switcher) SetSwitchLayout(afterWord, afterSelection bool) {
	s.switchAfterWord.Store(afterWord)
	s.switchAfterSelection.Store(afterSelection)
}

// Layouts returns the layouts the switcher cycles through, in order.
func (s *Switcher) Layouts() []*keymap.Layout { return s.layouts }

// ConvertLastWord rewrites the word before the caret as if it had been typed in
// the next layout and, unless that is turned off, switches the focused window
// to that layout.
func (s *Switcher) ConvertLastWord(ctx context.Context) error {
	return s.convert(ctx, true)
}

// ConvertSelection rewrites the selected text as if it had been typed in the
// next layout and, unless that is turned off, switches the focused window to
// that layout.
func (s *Switcher) ConvertSelection(ctx context.Context) error {
	return s.convert(ctx, false)
}

func (s *Switcher) convert(ctx context.Context, lastWordOnly bool) error {
	if !s.busy.TryLock() {
		return ErrBusy
	}
	defer s.busy.Unlock()

	if err := s.deps.Keyboard.ReleaseModifiers(); err != nil {
		return err
	}
	console, gentle, err := s.console(!lastWordOnly)
	if err != nil {
		return err
	}
	var target *keymap.Layout
	if console != nil {
		defer s.release(console)
		target, err = s.convertInConsole(console, lastWordOnly)
	} else {
		// The clipboard goes back to the user whatever happens, but without
		// holding anything up: the layout is switched and the next press is
		// taken while the pasted text is still given time to be read. A press
		// that needs the clipboard again waits for it to be back first.
		s.awaitClipboard()
		snapshot := s.deps.Clipboard.Snapshot(ctx)
		var pasted <-chan struct{}
		returned := make(chan struct{})
		s.returned = returned
		defer func() { go s.restore(context.WithoutCancel(ctx), snapshot, pasted, returned) }()
		target, err = s.convertByClipboard(ctx, lastWordOnly, gentle, &pasted)
		if !lastWordOnly && errors.Is(err, ErrNothingToConvert) {
			// Nothing was selected. A window that is an editor and a terminal
			// at once was not asked about its focus so far; with no selection
			// to spare it can be, and a shell's whole line converted.
			if console, _, _ := s.console(false); console != nil {
				defer s.release(console)
				target, err = s.convertInConsole(console, false)
			}
		}
	}
	if err != nil {
		return err
	}
	if s.switchAfter(lastWordOnly) {
		s.activate(target)
	}
	return nil
}

func (s *Switcher) release(console Console) {
	if err := console.Close(); err != nil {
		s.log.Warn("could not release the console", "error", err)
	}
}

func (s *Switcher) switchAfter(lastWordOnly bool) bool {
	if lastWordOnly {
		return s.switchAfterWord.Load()
	}
	return s.switchAfterSelection.Load()
}

// restore gives the clipboard back once the pasted text has had its time.
func (s *Switcher) restore(ctx context.Context, snapshot clip.Snapshot, pasted <-chan struct{}, returned chan<- struct{}) {
	defer close(returned)
	s.settle(pasted)
	if err := s.deps.Clipboard.Restore(ctx, snapshot); err != nil {
		s.log.Warn("could not restore the clipboard", "error", err)
	}
}

// settle waits until a pasted text has been read and has stayed on the
// clipboard a little longer.
func (s *Switcher) settle(pasted <-chan struct{}) {
	if pasted == nil {
		return
	}
	select {
	case <-pasted:
		time.Sleep(s.options.PasteSettle)
	case <-time.After(s.options.PasteTimeout):
		s.log.Debug("nothing read the pasted text in time")
	}
}

// awaitClipboard waits for the clipboard of the last conversion to be back.
// The caller holds busy.
func (s *Switcher) awaitClipboard() {
	if s.returned != nil {
		<-s.returned
	}
}

// Idle returns once no conversion is running and the clipboard is back with
// the user.
func (s *Switcher) Idle() {
	s.busy.Lock()
	defer s.busy.Unlock()
	s.awaitClipboard()
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
// the window has none and the clipboard path is the one to use, gently when
// the focus may be on a shell after all, and an error when the window is a
// terminal that must be left alone.
func (s *Switcher) console(selection bool) (console Console, gentle bool, err error) {
	if s.deps.Terminal == nil {
		return nil, false, nil
	}
	console, err = s.deps.Terminal.Open(selection)
	switch {
	case err == nil:
		return console, false, nil
	case errors.Is(err, ErrNoConsole):
		s.log.Debug("no console behind the focused window", "reason", err)
		return nil, errors.Is(err, ErrShellsNearby), nil
	default:
		return nil, false, fmt.Errorf("the terminal is left alone: %w", err)
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
// cut, convert, paste.
//
// What an application shows as selected and what it works with are not
// always the same thing. An editor driven by scripts (ProseMirror, under the
// input boxes of Claude and ChatGPT) learns of a selection made with the
// keyboard some time after the key presses, more so when it is busy, and
// until then copies one range and pastes over another. Three rules keep that
// from mattering:
//
//   - The line is read after a single key press. A selection made by one
//     press is either there or not yet, and a copy reads the same text in
//     both cases; one made by several presses can be caught halfway.
//   - The text is cut, never pasted over. What a cut puts on the clipboard
//     is what it removed, whichever range that was, and it leaves the caret
//     where the text was, so the paste that follows cannot go anywhere else.
//   - Nothing is cut before a copy has shown that there is text to cut, so a
//     window that answers no shortcut is sent nothing but the copy.
func (s *Switcher) convertByClipboard(ctx context.Context, lastWordOnly, gentle bool, pasted *<-chan struct{}) (*keymap.Layout, error) {
	keys := s.deps.Keyboard
	copySelection := func() error { return keys.Copy(gentle) }
	if !lastWordOnly {
		if _, err := s.take(ctx, copySelection, 1); err != nil {
			return nil, err
		}
		cut, err := s.cut(ctx)
		if err != nil {
			return nil, err
		}
		return s.putConverted(ctx, "", cut, pasted)
	}

	if err := keys.SelectToLineStart(); err != nil {
		return nil, err
	}
	s.pause()
	// A copy that brings nothing leaves nothing known to be selected, and
	// the caret is not touched again.
	read, err := s.take(ctx, copySelection, 1)
	if err != nil {
		return nil, err
	}
	line := lineBreaks.Replace(read)
	head, word, tail := keymap.SplitLastWord(line)
	if word == "" {
		return nil, errors.Join(ErrNothingToConvert, keys.CollapseSelectionRight())
	}
	// Cutting the word alone keeps the formatting of what is before it in
	// rich text editors.
	steps := utf8.RuneCountInString(word + tail)
	if head != "" && steps <= s.options.MaxNarrowedChars {
		if err := keys.CollapseSelectionRight(); err != nil {
			return nil, err
		}
		if err := keys.SelectCharsLeft(steps); err != nil {
			return nil, err
		}
		s.pause()
	}

	for whole := false; ; whole = true {
		cut, err := s.cut(ctx)
		if err != nil {
			// The selection goes, or the next key typed would replace it.
			return nil, errors.Join(err, keys.CollapseSelectionRight())
		}
		got := lineBreaks.Replace(cut)
		read := strings.HasSuffix(line, got)
		if read && utf8.RuneCountInString(got) >= steps {
			head, word, tail := keymap.SplitLastWord(cut)
			return s.putConverted(ctx, head, word+tail, pasted)
		}
		// With the text back in place the caret is where it was.
		if err := s.putBack(ctx, cut); err != nil {
			return nil, err
		}
		if whole || !read {
			// Not the text that was read: the caret is somewhere else by now.
			return nil, ErrNotSelected
		}
		// The end of the word only: the application was caught halfway
		// through the presses that selected it. The line is cut whole, by
		// one press.
		if err := keys.SelectToLineStart(); err != nil {
			return nil, err
		}
		s.pause()
	}
}

// cut cuts the selection and returns what the application removed.
func (s *Switcher) cut(ctx context.Context) (string, error) {
	text, err := s.take(ctx, s.deps.Keyboard.Cut, cutPatience)
	if errors.Is(err, ErrNothingToConvert) {
		return "", fmt.Errorf("%w (text it still cuts goes to the clipboard)", ErrNotSelected)
	}
	return text, err
}

// putConverted pastes unchanged and the conversion of typed where the caret
// is, and reports the layout typed was converted for. Text with nothing to
// convert goes back as it was.
func (s *Switcher) putConverted(ctx context.Context, unchanged, typed string, pasted *<-chan struct{}) (*keymap.Layout, error) {
	if strings.TrimSpace(typed) == "" {
		if err := s.putBack(ctx, unchanged+typed); err != nil {
			return nil, err
		}
		return nil, ErrNothingToConvert
	}
	source, target := s.pickLayouts(typed)
	s.log.Debug("converting",
		"from", s.layouts[source].ID(), "to", s.layouts[target].ID(),
		"characters", utf8.RuneCountInString(typed))

	read, err := s.paste(ctx, unchanged+keymap.Convert(typed, s.layouts[source], s.layouts[target]))
	if err != nil {
		// The text is cut and its conversion did not go in: give it back.
		return nil, errors.Join(err, s.putBack(ctx, unchanged+typed))
	}
	*pasted = read
	return s.layouts[target], nil
}

// paste pastes text where the caret is. The channel is closed once the text
// has been read off the clipboard.
func (s *Switcher) paste(ctx context.Context, text string) (<-chan struct{}, error) {
	read, err := s.deps.Clipboard.Offer(ctx, text)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Keyboard.Paste(); err != nil {
		return nil, err
	}
	return read, nil
}

// putBack pastes text that was cut back in and waits until it has had its
// time on the clipboard, which is needed again right after. The text is out
// of the application, so this is not given up halfway.
func (s *Switcher) putBack(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	read, err := s.paste(context.WithoutCancel(ctx), text)
	if err != nil {
		return fmt.Errorf("the cut text could not be put back: %w", err)
	}
	s.settle(read)
	return nil
}

// take copies or cuts the selection and returns what landed on the
// clipboard, which it waits for up to patience times. The marker tells a
// fresh copy apart from content that was already there.
func (s *Switcher) take(ctx context.Context, shortcut func() error, patience int) (string, error) {
	marker := marker()
	if err := s.deps.Clipboard.WriteText(ctx, marker); err != nil {
		return "", err
	}
	if err := shortcut(); err != nil {
		return "", err
	}

	for {
		text, err := s.deps.Clipboard.AwaitText(ctx, marker)
		patience--
		switch {
		case err == nil:
			return text, nil
		case !errors.Is(err, clip.ErrNoCopy):
			return "", err
		case patience <= 0:
			return "", fmt.Errorf("%w: the application did not answer the shortcut", ErrNothingToConvert)
		}
	}
}

// lineBreaks counts a CR LF pair as the one caret step it is.
var lineBreaks = strings.NewReplacer("\r\n", "\n")

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
