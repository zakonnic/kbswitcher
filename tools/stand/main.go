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
	tabs, n, left, presses, load, pid                            int
	click, ide, file, ctrl                                       string
	gap                                                          time.Duration
	selection, keep, verbose, paste                              bool
}

func main() {
	var o options
	flag.StringVar(&o.host, "host", "conhost", "conhost, wt, vscode, notepad, browser, attach or ide")
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
	flag.IntVar(&o.left, "left", 0, "press Left this many times before converting, to put the caret inside the text")
	flag.IntVar(&o.presses, "presses", 1, "how many times to convert in a row; twice gives the text back")
	flag.DurationVar(&o.gap, "gap", 400*time.Millisecond, "pause between the presses of -presses")
	flag.IntVar(&o.load, "load", 0, "browser: keep the page's main thread busy in bursts of about this many milliseconds")
	flag.BoolVar(&o.paste, "paste", false, "paste the text from the clipboard instead of typing it")
	flag.IntVar(&o.pid, "pid", 0, "attach: the shell already running in some terminal; ide: a terminal that must not change")
	flag.StringVar(&o.ide, "ide", "", "ide: the launcher of the running JetBrains IDE")
	flag.StringVar(&o.file, "file", "", "ide: a scratch text file to open and type into")
	flag.StringVar(&o.click, "click", "", "attach: x,y inside the window to click first, to give the terminal the focus")
	flag.StringVar(&o.ctrl, "ctrl", "", "press Ctrl with this letter before converting, to see what the host makes of it")
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
	case "attach":
		h = attachShell(uint32(o.pid), o.shell, o.click)
	case "ide":
		// The editor of a running JetBrains IDE, with its terminal watched.
		if o.pid != 0 {
			bystander = &shellHost{shell: uint32(o.pid)}
		}
		h = openInIDE(o.ide, o.file, o.click)
	case "browser":
		h = launchBrowser(filepath.Join(filepath.Dir(o.vsdir), "edge"), o.load)
	default:
		fatal("unknown host %q", o.host)
	}
	if !o.keep {
		defer h.close()
		if bystander != nil && o.host != "ide" {
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
			switch sh, isShell := h.(*shellHost); {
			case o.paste:
				pasteText(typed)
			case isShell:
				// One key first: if it does not show in the shell, the focus
				// is elsewhere and nothing more is typed there.
				first := []rune(o.word)[:1]
				empty, _ := sh.line()
				typeKeys(string(first))
				if !awaitChange(sh, empty) {
					fatal("a key typed into %q did not reach the shell; stopping", windowText(h.window()))
				}
				typeKeys(string([]rune(o.word)[1:]))
			default:
				typeKeys(o.word)
			}
			if awaitSuffix(h, typed) {
				break
			}
			fmt.Printf("#%d the word did not arrive\n", i+1)
		}
		for range o.left {
			chord(vkLeft)
		}
		if o.selection {
			h.selectLine()
		}
		if o.ctrl != "" {
			chord(uint16(o.ctrl[0]), vkControl) // see what the host makes of a shortcut
		}
		time.Sleep(150 * time.Millisecond)
		before, _ := h.line()
		start := time.Now()
		after, settled := before, time.Duration(0)
		for press := range o.presses {
			if press > 0 {
				time.Sleep(o.gap)
			}
			convert()
			// Every press but the last is expected to change the line to
			// something other than the final text.
			goal := want
			if press < o.presses-1 {
				goal = "\x00"
			}
			after, settled = awaitSettled(h, after, goal, start)
		}
		shown := strings.ReplaceAll(after, caretMark, "")
		ok := strings.HasSuffix(shown, want) && (after != before || o.presses%2 == 0)
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
	if o.presses%len(installed) == 0 {
		return typed // a full cycle
	}
	i := slices.Index(installed, from)
	to := installed[(i+1)%len(installed)]
	if o.selection {
		return keymap.Convert(typed, from, to)
	}
	// Only what lies before the caret is converted.
	runes := []rune(typed)
	caret := max(len(runes)-o.left, 0)
	head, word, rest := keymap.SplitLastWord(string(runes[:caret]))
	return head + keymap.Convert(word, from, to) + rest + string(runes[caret:])
}

// pasteText puts text into the host through the clipboard, as a user who
// copied a phrase and pasted it.
func pasteText(text string) {
	clipboard, err := clip.Open()
	check(err, "clip.Open")
	defer clipboard.Close()
	check(clipboard.WriteText(context.Background(), text), "clipboard")
	chord('V', vkControl)
	time.Sleep(200 * time.Millisecond)
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
		deps := switcher.Deps{Layouts: reader, Keyboard: keyboard, Clipboard: clipboard, Terminal: terminal{keyboard}}
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

type terminal struct{ keyboard *input.Keyboard }

func (t terminal) Open(selection bool) (switcher.Console, error) {
	session, err := console.Open(t.keyboard.Nudge, selection)
	switch {
	case errors.Is(err, console.ErrShells):
		return nil, fmt.Errorf("%w: %w: %w", switcher.ErrNoConsole, switcher.ErrShellsNearby, err)
	case errors.Is(err, console.ErrNotConsole):
		return nil, fmt.Errorf("%w: %w", switcher.ErrNoConsole, err)
	case err != nil:
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

func awaitChange(h host, from string) bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if line, err := h.line(); err == nil && line != from {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
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
