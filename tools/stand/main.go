//go:build windows

// Command stand drives real terminals and editors to test kbswitcher end to
// end on this machine. It opens a host (a console window, Windows Terminal, an
// isolated VS Code, Notepad), types a word in a chosen layout, converts it
// in-process or through a running kbswitcher.exe and its hotkey, and reports
// what the host shows afterwards and how long that took.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zakonnic/kbswitcher/internal/keymap"
	"github.com/zakonnic/kbswitcher/internal/platform/clip"
	"github.com/zakonnic/kbswitcher/internal/platform/console"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
	"github.com/zakonnic/kbswitcher/internal/switcher"
)

type options struct {
	host, shell, word, layout, want, mode, kb, pre, vsdir, focus string
	tabs, n                                                      int
	selection, keep, verbose                                     bool
}

func main() {
	var o options
	flag.StringVar(&o.host, "host", "conhost", "conhost, wt, vscode or notepad")
	flag.StringVar(&o.shell, "shell", "powershell.exe", "shell command line")
	flag.StringVar(&o.word, "word", "ghbdtn", "keys to press, named as on a US keyboard")
	flag.StringVar(&o.layout, "layout", "00000409", "layout the word is typed in")
	flag.StringVar(&o.want, "want", "", "expected end of the line after converting; computed when empty")
	flag.StringVar(&o.mode, "mode", "inproc", "inproc, hotkey or none")
	flag.StringVar(&o.kb, "kb", "", "kbswitcher.exe to run in hotkey mode")
	flag.StringVar(&o.pre, "pre", "", "a command to run in the shell before the first word")
	flag.StringVar(&o.vsdir, "vsdir", filepath.Join(os.TempDir(), "kbswitcher-stand", "vscode"), "VS Code user data for the stand")
	flag.StringVar(&o.focus, "focus", "terminal", "vscode: terminal or editor")
	flag.IntVar(&o.tabs, "tabs", 1, "wt: how many tabs to open; the last one is used")
	flag.IntVar(&o.n, "n", 1, "how many conversions to run")
	flag.BoolVar(&o.selection, "selection", false, "convert the selection (Shift+hotkey) instead of the last word")
	flag.IntVar(&selectWords, "selwords", 0, "with -selection in a shell: select this many words back with Ctrl+Shift+Left")
	flag.BoolVar(&o.keep, "keep", false, "leave the host open")
	flag.BoolVar(&o.verbose, "v", false, "log the switcher in detail (inproc)")
	flag.Parse()

	_, _, _ = procFreeConsole.Call()

	reader, err := layouts.Open()
	check(err, "layouts.Open")
	installed, err := reader.List()
	check(err, "layouts.List")
	want := o.want
	if want == "" {
		want = expected(installed, o)
	}

	var h host
	var bystander *shellHost // a terminal that must stay untouched while an editor is tested
	switch o.host {
	case "conhost":
		h = launchConhost(o.shell)
	case "wt":
		h = launchWT(o.shell, o.tabs)
	case "vscode":
		v := openVSCode(o.vsdir, o.shell)
		if o.focus == "editor" {
			bystander = v.newTerminal(o.shell)
			awaitPrompt(bystander)
			typeKeys("echo keep")
			h = v.editor()
		} else {
			h = v.newTerminal(o.shell)
		}
	case "notepad":
		h = launchNotepad()
	default:
		fatal("unknown host %q", o.host)
	}
	if !o.keep {
		defer h.close()
		if bystander != nil {
			defer bystander.close()
		}
	}
	if !forceForeground(h.window()) {
		fatal("could not bring the host to the foreground; it is %#x %q", fg(), className(fg()))
	}
	guard = h.window()
	fmt.Printf("host=%s window=%#x class=%q title=%q want=%q\n", o.host, h.window(), className(h.window()), windowText(h.window()), want)

	if sh, ok := h.(*shellHost); ok {
		awaitPrompt(sh)
		if o.pre != "" {
			typeKeys(o.pre)
			chord(vkReturn)
			time.Sleep(500 * time.Millisecond)
			awaitPrompt(sh)
		}
	}

	convert := converter(o, installed, reader)
	defer stopKB()
	var bystanderLine string
	if bystander != nil {
		bystanderLine, _ = bystander.line()
	}

	var passed int
	var took []time.Duration
	for i := range o.n {
		// A layout switch reaches a console window a moment after it is
		// reported, so a word typed too early is typed once more.
		typed := keymap.Convert(o.word, find(installed, "00000409"), find(installed, o.layout))
		for attempt := range 2 {
			time.Sleep(200 * time.Millisecond) // the layout switch of the last conversion lands late in conhost
			h.clear()
			activate(reader, o.layout)
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
			typeKeys(o.word)
			if awaitSuffix(h, typed) {
				break
			}
			fmt.Printf("#%d the word did not arrive\n", i+1)
		}
		if o.selection {
			h.selectLine()
		}
		before, _ := h.line()
		start := time.Now()
		convert()
		after, settled := awaitSettled(h, before, want, start)
		ok := strings.HasSuffix(after, want) && after != before
		if bystander != nil {
			if g, _ := bystander.line(); g != bystanderLine {
				ok = false
				fmt.Printf("#%d the terminal behind the editor changed: %q -> %q\n", i+1, bystanderLine, g)
			}
		}
		verdict := "FAIL"
		if ok {
			verdict = "ok"
			passed++
			took = append(took, settled)
		}
		active, _ := reader.Active()
		fmt.Printf("#%d %s %6s layout=%s before=%q after=%q\n", i+1, verdict, settled.Round(time.Millisecond), active, tail(before), tail(after))
	}
	slices.Sort(took)
	if len(took) > 0 {
		fmt.Printf("passed %d/%d; settled in median %s, max %s\n", passed, o.n,
			took[len(took)/2].Round(time.Millisecond), took[len(took)-1].Round(time.Millisecond))
	} else {
		fmt.Printf("passed 0/%d\n", o.n)
	}
}

// expected is what the switcher should make of the word: typed in the chosen
// layout, rewritten for the next one in the cycle.
func expected(installed []*keymap.Layout, o options) string {
	from := find(installed, o.layout)
	typed := keymap.Convert(o.word, find(installed, "00000409"), from)
	i := slices.Index(installed, from)
	to := installed[(i+1)%len(installed)]
	if o.selection {
		return keymap.Convert(typed, from, to)
	}
	head, word, rest := keymap.SplitLastWord(typed)
	return head + keymap.Convert(word, from, to) + rest
}

func find(installed []*keymap.Layout, id string) *keymap.Layout {
	for _, l := range installed {
		if l.ID() == id {
			return l
		}
	}
	fatal("layout %s is not installed", id)
	return nil
}

func activate(reader *layouts.Reader, id string) {
	for range 20 {
		if active, _ := reader.Active(); active == id {
			return
		}
		check(reader.Activate(id), "Activate")
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("layout %s could not be activated\n", id)
}

// converter returns the action that converts once: in this process, through a
// kbswitcher.exe pressed by its hotkey, or nothing.
func converter(o options, installed []*keymap.Layout, reader *layouts.Reader) func() {
	switch o.mode {
	case "inproc":
		level := slog.LevelInfo
		if o.verbose {
			level = slog.LevelDebug
		}
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
		keyboard, err := input.Open()
		check(err, "input.Open")
		clipboard, err := clip.Open()
		check(err, "clip.Open")
		deps := switcher.Deps{Layouts: reader, Keyboard: keyboard, Clipboard: clipboard, Terminal: terminal{}}
		s, err := switcher.New(installed, deps, switcher.DefaultOptions(), log)
		check(err, "switcher.New")
		return func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			if o.selection {
				err = s.ConvertSelection(ctx)
			} else {
				err = s.ConvertLastWord(ctx)
			}
			if err != nil || o.verbose {
				fmt.Printf("   convert: err=%v took=%s\n", err, time.Since(start).Round(time.Millisecond))
			}
		}
	case "hotkey":
		if o.kb == "" {
			fatal("-kb is required in hotkey mode")
		}
		cmd := exec.Command(o.kb, "-verbose")
		check(cmd.Start(), "start kbswitcher")
		time.Sleep(2 * time.Second)
		stopKB = func() { _ = cmd.Process.Kill() }
		return func() {
			ensureForeground()
			if o.selection {
				sendKey(vkShift, 0x2A, false)
			}
			sendKey(vkPause, 0x45, false)
			sendKey(vkPause, 0x45, true)
			if o.selection {
				sendKey(vkShift, 0x2A, true)
			}
		}
	default:
		return func() {}
	}
}

// stopKB ends the kbswitcher.exe the hotkey mode started.
var stopKB = func() {}

type terminal struct{}

func (terminal) Open() (switcher.Console, error) {
	session, err := console.Open()
	if errors.Is(err, console.ErrNotConsole) {
		return nil, fmt.Errorf("%w: %w", switcher.ErrNoConsole, err)
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

func awaitPrompt(h *shellHost) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if line, err := h.line(); err == nil {
			trimmed := strings.TrimRight(line, " ")
			if strings.HasSuffix(trimmed, ">") || strings.HasSuffix(trimmed, "$") || strings.HasSuffix(trimmed, "#") {
				time.Sleep(300 * time.Millisecond)
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("no prompt appeared")
}

func awaitSuffix(h host, suffix string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if line, err := h.line(); err == nil && strings.HasSuffix(line, suffix) {
			time.Sleep(150 * time.Millisecond)
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// awaitSettled polls the line until it shows want, or has stopped changing
// for a while after changing away from before, and reports when it reached
// its final value.
func awaitSettled(h host, before, want string, start time.Time) (string, time.Duration) {
	const quiet = 700 * time.Millisecond
	last, lastChange := before, time.Duration(0)
	deadline := start.Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := h.line()
		if err == nil && line != last {
			last, lastChange = line, time.Since(start)
		}
		if last != before && strings.HasSuffix(last, want) {
			// Give a late keystroke the chance to spoil it.
			time.Sleep(quiet)
			if line, err := h.line(); err == nil && line != last {
				return line, time.Since(start)
			}
			return last, lastChange
		}
		if last != before && time.Since(start)-lastChange > quiet {
			return last, lastChange
		}
		time.Sleep(5 * time.Millisecond)
	}
	return last, lastChange
}

func tail(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return "…" + string(r[len(r)-40:])
	}
	return s
}

func check(err error, what string) {
	if err != nil {
		fatal("%s: %v", what, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
