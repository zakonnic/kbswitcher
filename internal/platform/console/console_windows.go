package console

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole           = kernel32.NewProc("AttachConsole")
	procFreeConsole             = kernel32.NewProc("FreeConsole")
	procGetConsoleTitleW        = kernel32.NewProc("GetConsoleTitleW")
	procGetConsoleWindow        = kernel32.NewProc("GetConsoleWindow")
	procReadConsoleOutputCharW  = kernel32.NewProc("ReadConsoleOutputCharacterW")
	procWriteConsoleInputW      = kernel32.NewProc("WriteConsoleInputW")
	procSetConsoleCtrlHandler   = kernel32.NewProc("SetConsoleCtrlHandler")
	procPendingInputEvents      = kernel32.NewProc("GetNumberOfConsoleInputEvents")
	procFlushConsoleInputBuffer = kernel32.NewProc("FlushConsoleInputBuffer")
)

// Reasons a hotkey press finds nothing to work with.
var (
	// ErrNotConsole means the focused window is an ordinary application and
	// the clipboard path should be used instead.
	ErrNotConsole = errors.New("the focused window is not a console")
	// ErrAmbiguous means the host has several consoles and none of them can be
	// told apart, so none is touched.
	ErrAmbiguous = errors.New("the console of the focused pane cannot be identified")
	// ErrShells comes with ErrNotConsole when the window runs shells and
	// nothing showed that the focus is on one of them. It may be all the
	// same: on a shell with an empty input line, or on a program in it.
	ErrShells = errors.New("shells run under the window")

	errNoWindow = errors.New("no focused window")
)

const (
	// maxProcessDepth bounds the walk down the host's process tree.
	maxProcessDepth = 8
	// maxCandidates bounds how many processes are tried, because an editor can
	// have dozens of children and each attempt is a system call.
	maxCandidates = 64
	// matchRowsAbove is how many rows above the caret must agree before a
	// console is taken for the pane that has the focus.
	matchRowsAbove = 3
	// maxInput bounds the input line worked on.
	maxInput = 1024
	// promptContext is how much of the line before the input is checked to be
	// still in place after an edit.
	promptContext = 16
	// recordsPerWrite keeps one WriteConsoleInput call to a sane size.
	recordsPerWrite = 512
	// maxCorrections bounds the attempts to repair an edit the shell did not
	// apply in full, which a console read by several processes, as WSL's is,
	// occasionally makes it do.
	maxCorrections = 2

	// The shell takes the key events at its own pace and redraws its line
	// asynchronously, so every edit is watched rather than assumed. A caret
	// that has not moved quietTimeout after the shell took the key is taken
	// to stay where it is.
	pollDelay    = 2 * time.Millisecond
	stablePolls  = 3
	quietTimeout = 60 * time.Millisecond
	editTimeout  = time.Second
	xtermLag     = 250 * time.Millisecond
	xtermLagPoll = 25 * time.Millisecond

	pseudoConsoleClass = "PseudoConsoleWindow"
)

// attachMu serializes the attachment, which is a property of the process and
// not of a goroutine. A Session holds it from Open until Close.
var attachMu sync.Mutex

// Session is a console borrowed for the length of one conversion.
type Session struct {
	in     windows.Handle
	out    windows.Handle
	closed bool
}

// Open borrows the console of the shell behind the focused window. It fails
// with ErrNotConsole when the window is not a terminal, and with another error
// when it is one that cannot be worked with. probe is used for a window that
// runs shells and does not say whether one of them has the focus; selection
// says that a selection is being converted, which no probe leaves as it was.
func Open(probe Probe, selection bool) (*Session, error) {
	target, err := foregroundWindow()
	if err != nil {
		return nil, err
	}
	if target.kind == Mintty {
		return nil, errors.New("mintty has no Windows console")
	}

	processes, err := processTree()
	if err != nil {
		return nil, err
	}

	attachMu.Lock()
	session, err := open(target, processes.below(target.pid), probe, selection)
	if err != nil {
		attachMu.Unlock()
		return nil, err
	}
	return session, nil
}

// found is one console reachable from the host, named by the first process
// found attached to it.
type found struct {
	pid    uint32
	window uintptr // the console window, which identifies the console itself
	title  string
	// caret is where the caret was in the screen buffer when the console was
	// found.
	caret windows.Coord
	view
}

// view is the visible part of a console: its size in cells, the caret within
// it, and the caret row with the rows above it, right-trimmed.
type view struct {
	cols, lines int16
	caretX      int16
	caretY      int16
	rows        []string
}

func open(target window, candidates []uint32, probe Probe, selection bool) (*Session, error) {
	consoles := discover(target, candidates)
	if len(consoles) == 0 {
		if target.kind == Conhost || target.kind == Cascadia {
			// A terminal still: the clipboard path would cancel or copy the
			// shell's line, so the hotkey does nothing instead.
			return nil, fmt.Errorf("nothing under %s owns a console", target.class)
		}
		return nil, fmt.Errorf("%w: %s runs no terminal", ErrNotConsole, target.class)
	}
	console, err := pick(target, consoles, probe, selection)
	if err != nil {
		return nil, err
	}
	return attach(console)
}

// pick finds the console of the pane that has the focus.
func pick(target window, consoles []found, probe Probe, selection bool) (found, error) {
	if target.kind == JetBrains || target.kind == Other {
		// No pair of keys leaves every selection as it was: the arrows drop
		// it, and with Shift they change one that ends at the start of the
		// text.
		if selection {
			return found{}, fmt.Errorf("%w: %w; a selection in %s is not probed", ErrNotConsole, ErrShells, target.class)
		}
		console, err := byProbe(consoles, probe)
		if errors.Is(err, errNoAnswer) {
			return found{}, fmt.Errorf("%w: %w; %w", ErrNotConsole, ErrShells, err)
		}
		return console, err
	}
	if target.kind == Chromium {
		return byFocus(target, consoles, probe, selection)
	}
	if len(consoles) == 1 {
		return consoles[0], nil
	}

	// Several tabs or panes. The pane that has the focus shows the rows of
	// exactly one console; failing that, the host window carries the title of
	// the active tab.
	f, err := focused(target.pid, true)
	if err == nil {
		var console found
		if console, err = byRows(f.rows, consoles); err == nil {
			return console, nil
		}
	}
	var matching []found
	for _, console := range consoles {
		if console.title == target.title {
			matching = append(matching, console)
		}
	}
	if len(matching) != 1 {
		return found{}, fmt.Errorf("%w: %d consoles; %w; %d of them titled like the window",
			ErrAmbiguous, len(consoles), err, len(matching))
	}
	return matching[0], nil
}

// byFocus picks the console of the terminal pane that has the focus in a
// Chromium window. An xterm.js terminal names its text area, and the caret
// it sits on tells the console. A text area at the caret that names nothing
// (Tabby exposes no class at all) belongs to a terminal or to an editor that
// draws its own text; a shell answers a caret key, and with none answering
// the pane is still left alone, since an editor would name itself.
func byFocus(target window, consoles []found, probe Probe, selection bool) (found, error) {
	// The console is ahead of the terminal that renders it, so a caret that
	// does not match yet is given a moment to catch up.
	deadline := time.Now().Add(xtermLag)
	for {
		f, err := focused(target.pid, false)
		if err != nil {
			return found{}, fmt.Errorf("cannot tell whether a terminal has the focus: %w", err)
		}
		switch {
		case f.hidden && f.class == "":
			// A selection a terminal shows is its own, so probing loses
			// nothing a shell could convert but a PSReadLine selection.
			console, err := byProbe(consoles, probe)
			if errors.Is(err, errNoAnswer) {
				return found{}, fmt.Errorf("the focused pane names nothing about itself: %w", err)
			}
			return console, err
		case !f.xterm():
			return found{}, fmt.Errorf("%w: the focus is on %q", ErrNotConsole, f.class)
		}
		console, err := byCaret(f, consoles)
		if err == nil {
			return console, nil
		}
		if !time.Now().Before(deadline) {
			if selection {
				return found{}, err
			}
			// Terminals of the same size with their carets on the same cell:
			// the one that has the focus answers a key. The focus is on a
			// terminal, so no answer is no reason for the clipboard path.
			if console, probed := byProbe(consoles, probe); probed == nil {
				return console, nil
			}
			return found{}, err
		}
		time.Sleep(xtermLagPoll)
	}
}

// byRows picks the console whose caret row, and the rows above it, are what
// the focused pane shows.
func byRows(rows []string, consoles []found) (found, error) {
	var matching []found
	for _, console := range consoles {
		if sameRows(console.rows, rows) {
			matching = append(matching, console)
		}
	}
	if len(matching) != 1 {
		return found{}, fmt.Errorf("%d of them show the rows of the focused pane", len(matching))
	}
	return matching[0], nil
}

// byCaret picks the console whose grid and caret are those of the focused
// xterm.js terminal: its text area is one cell on the caret, inside a grid of
// as many cells as the console has.
func byCaret(f focus, consoles []found) (found, error) {
	var matching []found
	for _, console := range consoles {
		if onCaret(f, console.view) {
			matching = append(matching, console)
		}
	}
	if len(matching) != 1 {
		var seen []string
		for _, c := range consoles {
			seen = append(seen, fmt.Sprintf("%dx%d at %d,%d", c.cols, c.lines, c.caretX, c.caretY))
		}
		return found{}, fmt.Errorf("%w: %d of %d consoles have their caret where the focused terminal has it "+
			"(text area %v in grid %v; consoles %v)", ErrAmbiguous, len(matching), len(consoles), f.caret, f.screen, seen)
	}
	return matching[0], nil
}

func onCaret(f focus, v view) bool {
	if v.cols <= 0 || v.lines <= 0 {
		return false
	}
	cellW := f.screen.Width() / float64(v.cols)
	cellH := f.screen.Height() / float64(v.lines)
	x := float64(f.caret.Left - f.screen.Left)
	y := float64(f.caret.Top - f.screen.Top)
	return math.Abs(x-float64(v.caretX)*cellW) <= cellW/2 &&
		math.Abs(y-float64(v.caretY)*cellH) <= cellH/2 &&
		math.Abs(f.caret.Height()-cellH) <= cellH/4
}

// sameRows reports whether two row lists end in the same text. Whitespace is
// left out of the comparison: the terminal pads rows differently from the
// screen buffer and joins the rows of a wrapped line, and either list may
// reach further up than the other.
func sameRows(a, b []string) bool {
	x, y := ink(a), ink(b)
	if x == "" || y == "" {
		return false
	}
	return strings.HasSuffix(x, y) || strings.HasSuffix(y, x)
}

func ink(rows []string) string {
	var b strings.Builder
	for _, row := range rows {
		for _, r := range row {
			if !unicode.IsSpace(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// discover attaches to each candidate in turn and notes the console it lands
// in, keeping one entry per console. A console window shows exactly one
// console; Windows Terminal and the editors run pseudoconsoles, and whatever
// else runs under them with a console of its own (a language server, say) is
// left out.
func discover(target window, candidates []uint32) []found {
	var consoles []found
	seen := make(map[uintptr]bool)

	// Being attached to a console makes this process a member of its group,
	// so a Ctrl+C in it must be ignored for as long as that lasts.
	ignoreCtrlC(true)
	defer ignoreCtrlC(false)

	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	for _, pid := range candidates {
		ret, _, _ := procAttachConsole.Call(uintptr(pid))
		if ret == 0 {
			continue
		}
		handle, _, _ := procGetConsoleWindow.Call()
		if handle != 0 && !seen[handle] && hosts(target, handle) {
			seen[handle] = true
			view, caret := currentView(target.kind == Cascadia)
			consoles = append(consoles, found{pid: pid, window: handle, title: consoleTitle(), caret: caret, view: view})
		}
		detach()
	}
	return consoles
}

func hosts(target window, console uintptr) bool {
	if target.kind == Conhost {
		return console == uintptr(target.handle)
	}
	return windowClass(console) == pseudoConsoleClass
}

func attach(console found) (*Session, error) {
	// Whatever happens in the borrowed console, a Ctrl+C in it must not reach
	// this process; the guard goes up before the attachment, not after.
	ignoreCtrlC(true)
	ret, _, err := procAttachConsole.Call(uintptr(console.pid))
	if ret == 0 {
		ignoreCtrlC(false)
		return nil, fmt.Errorf("cannot borrow the console of process %d: %w", console.pid, err)
	}

	session := &Session{}
	if session.in, err = openConsoleFile("CONIN$"); err != nil {
		session.release()
		return nil, err
	}
	if session.out, err = openConsoleFile("CONOUT$"); err != nil {
		session.release()
		return nil, err
	}
	return session, nil
}

func openConsoleFile(name string) (windows.Handle, error) {
	handle, err := windows.CreateFile(windows.StringToUTF16Ptr(name),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("cannot open %s: %w", name, err)
	}
	return handle, nil
}

// Close gives the console back.
func (s *Session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.release()
	attachMu.Unlock()
	return nil
}

func (s *Session) release() {
	if s.in != 0 {
		_ = windows.CloseHandle(s.in)
		s.in = 0
	}
	if s.out != 0 {
		_ = windows.CloseHandle(s.out)
		s.out = 0
	}
	ignoreCtrlC(false)
	detach()
}

// ignoreCtrlC turns the process-wide "a Ctrl+C means nothing here" flag on and
// off. There is nothing to do when Windows refuses.
func ignoreCtrlC(on bool) {
	flag := uintptr(0)
	if on {
		flag = 1
	}
	_, _, _ = procSetConsoleCtrlHandler.Call(0, flag)
}

func detach() { _, _, _ = procFreeConsole.Call() }

func (s *Session) screen() (windows.ConsoleScreenBufferInfo, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(s.out, &info); err != nil {
		return info, fmt.Errorf("cannot read the console screen: %w", err)
	}
	if info.Size.X <= 0 {
		return info, errors.New("the console reports an empty screen")
	}
	return info, nil
}

// pending is how many input events the shell has not taken yet.
func (s *Session) pending() uint32 {
	var n uint32
	if ret, _, _ := procPendingInputEvents.Call(uintptr(s.in), uintptr(unsafe.Pointer(&n))); ret == 0 {
		return 0
	}
	return n
}

// cells reads n cells from a linear offset, across rows.
func (s *Session) cells(from, n, width int) []rune {
	return []rune(readCells(s.out, from, n, width))
}

func readCells(out windows.Handle, from, n, width int) string {
	if n <= 0 || from < 0 || width <= 0 {
		return ""
	}
	buf := make([]uint16, n)
	var got uint32
	at := coord(from, width)
	ret, _, _ := procReadConsoleOutputCharW.Call(uintptr(out),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(n), uintptr(at),
		uintptr(unsafe.Pointer(&got)))
	if ret == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
}

// currentView reads the size and the caret of the attached console and, with
// rows set, the caret row and the rows above it: Windows Terminal is matched
// to its focused pane by them, nothing else needs them.
func currentView(rows bool) (view, windows.Coord) {
	out, err := openConsoleFile("CONOUT$")
	if err != nil {
		return view{}, windows.Coord{}
	}
	defer func() { _ = windows.CloseHandle(out) }()

	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(out, &info); err != nil || info.Size.X <= 0 {
		return view{}, windows.Coord{}
	}
	v := view{
		cols:   info.Window.Right - info.Window.Left + 1,
		lines:  info.Window.Bottom - info.Window.Top + 1,
		caretX: info.CursorPosition.X - info.Window.Left,
		caretY: info.CursorPosition.Y - info.Window.Top,
	}
	if !rows {
		return v, info.CursorPosition
	}
	width := int(info.Size.X)
	first := max(info.CursorPosition.Y-matchRowsAbove, 0)
	for row := first; row <= info.CursorPosition.Y; row++ {
		v.rows = append(v.rows, strings.TrimRight(readCells(out, int(row)*width, width, width), " "))
	}
	return v, info.CursorPosition
}

func consoleTitle() string {
	var buf [512]uint16
	n, _, _ := procGetConsoleTitleW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// Key codes of the keys this package presses; typed characters carry no key
// code at all.
