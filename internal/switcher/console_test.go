package switcher

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/keymap"
)

// fakeConsole is a shell showing a prompt and an input line. Like a real one it
// refuses to erase the prompt, which is what bounds a conversion.
type fakeConsole struct {
	prompt string
	input  string
	closed bool
}

func (c *fakeConsole) Line() (string, error) { return c.prompt + c.input, nil }

func (c *fakeConsole) Delete(n int) (int, error) {
	runes := []rune(c.input)
	n = min(n, len(runes))
	c.input = string(runes[:len(runes)-n])
	return n, nil
}

func (c *fakeConsole) Type(text string) error { c.input += text; return nil }

func (c *fakeConsole) Close() error { c.closed = true; return nil }

type fakeTerminal struct {
	console *fakeConsole
	err     error
}

func (t *fakeTerminal) Open() (Console, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.console, nil
}

func consoleSwitcher(t *testing.T, terminal Terminal, text string) (*Switcher, *fakeClipboard, *fakeEditor) {
	t.Helper()

	layouts := keymap.Fallback()
	clipboard := &fakeClipboard{}
	editor := newEditor(text, clipboard)
	options := DefaultOptions()
	options.KeyDelay = 0
	options.PasteSettle = 0

	deps := Deps{
		Layouts:   &fakeLayouts{active: layouts[0].ID()},
		Keyboard:  editor,
		Clipboard: clipboard,
		Terminal:  terminal,
	}
	s, err := New(layouts, deps, options, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, clipboard, editor
}

func TestConvertLastWordInAConsole(t *testing.T) {
	console := &fakeConsole{prompt: "PS C:\\> ", input: "hello ckjdj"}
	s, clipboard, _ := consoleSwitcher(t, &fakeTerminal{console: console}, "")

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if console.input != "hello слово" {
		t.Errorf("the input line is %q, want %q", console.input, "hello слово")
	}
	if !console.closed {
		t.Error("the console was not given back")
	}
	if clipboard.text != "" {
		t.Errorf("the clipboard was used: %q", clipboard.text)
	}
}

func TestAConsoleKeepsItsPrompt(t *testing.T) {
	// A prompt with no space before the word, as cmd.exe writes it: the whole
	// line is one word, and only what the shell erases may be converted.
	console := &fakeConsole{prompt: "C:\\Users\\Zak>", input: "ckjdj"}
	s, _, _ := consoleSwitcher(t, &fakeTerminal{console: console}, "")

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if console.input != "слово" {
		t.Errorf("the input line is %q, want %q", console.input, "слово")
	}
	if console.prompt != "C:\\Users\\Zak>" {
		t.Errorf("the prompt became %q", console.prompt)
	}
}

func TestAnEmptyConsoleLineIsLeftAlone(t *testing.T) {
	console := &fakeConsole{prompt: "PS C:\\Users\\Zak> "}
	s, _, _ := consoleSwitcher(t, &fakeTerminal{console: console}, "")

	if err := s.ConvertLastWord(t.Context()); !errors.Is(err, ErrNothingToConvert) {
		t.Fatalf("ConvertLastWord: %v, want %v", err, ErrNothingToConvert)
	}
	if console.input != "" {
		t.Errorf("something was typed on an empty line: %q", console.input)
	}
}

func TestConvertSelectionInAConsoleTakesTheWholeLine(t *testing.T) {
	console := &fakeConsole{prompt: "PS C:\\> ", input: "ghbdtn ckjdj"}
	s, _, _ := consoleSwitcher(t, &fakeTerminal{console: console}, "")

	if err := s.ConvertSelection(t.Context()); err != nil {
		t.Fatalf("ConvertSelection: %v", err)
	}
	if console.input != "привет слово" {
		t.Errorf("the input line is %q, want %q", console.input, "привет слово")
	}
}

func TestAWindowWithoutAConsoleFallsBackToTheClipboard(t *testing.T) {
	terminal := &fakeTerminal{err: errors.New("not a console")}
	s, _, editor := consoleSwitcher(t, terminal, "hello ckjdj")

	if err := s.ConvertLastWord(t.Context()); err != nil {
		t.Fatalf("ConvertLastWord: %v", err)
	}
	if got := editor.String(); got != "hello слово" {
		t.Errorf("the text is %q, want %q", got, "hello слово")
	}
}
