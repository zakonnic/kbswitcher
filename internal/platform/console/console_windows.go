package console

import (
	"errors"
	"fmt"
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
	ErrAmbiguous = errors.New("the host has more than one console and none of them can be identified")

	errNoWindow = errors.New("no focused window")
)

const (
	// maxProcessDepth bounds the walk down the host's process tree.
	maxProcessDepth = 8
	// maxCandidates bounds how many processes are tried, because an IDE can
	// have dozens of children and each attempt is a system call.
	maxCandidates = 64
	// maxWrappedRows is how far back a wrapped input line is followed.
	maxWrappedRows = 4
	// matchRowsAbove is how many rows above the caret must agree before a
	// console is taken for the pane that has the focus.
	matchRowsAbove = 3
	// maxDeleted bounds one burst of backspaces.
	maxDeleted = 256
	// recordsPerWrite keeps one WriteConsoleInput call to a sane size.
	recordsPerWrite = 512

	// The shell takes the key events at its own pace and redraws its line
	// asynchronously, so an edit is watched rather than assumed: first until
	// the input queue is empty, then until the rendered line comes to rest.
	// A press that changes nothing (a backspace at the prompt) is given up on
	// after quietTimeout; a change is then confirmed over stableReads polls,
	// with editTimeout as the overall ceiling.
	pollDelay    = 6 * time.Millisecond
	stableReads  = 2
	quietTimeout = 150 * time.Millisecond
	drainTimeout = 2 * time.Second
	editTimeout  = 1500 * time.Millisecond
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

// Open borrows the console of the shell behind the focused window. Every
// failure is a reason to fall back to the clipboard path.
func Open() (*Session, error) {
	target, err := foreground()
	if err != nil {
		return nil, err
	}
	if err := usable(target); err != nil {
		return nil, err
	}

	processes, err := processTree()
	if err != nil {
		return nil, err
	}

	attachMu.Lock()
	session, err := open(target, processes.below(target.pid))
	if err != nil {
		attachMu.Unlock()
		return nil, err
	}
	return session, nil
}

// usable rejects the windows that are not worth attaching to at all.
func usable(target window) error {
	switch target.kind {
	case Conhost, Cascadia:
		return nil
	case Embedded:
		return fmt.Errorf("%w: %s cannot say whether a terminal pane has the focus", ErrNotConsole, target.class)
	case Mintty:
		return fmt.Errorf("%w: mintty has no Windows console", ErrNotConsole)
	default:
		return fmt.Errorf("%w: window class %s", ErrNotConsole, target.class)
	}
}

// found is one console reachable from the host, named by the first process
// found attached to it.
type found struct {
	pid    uint32
	window uintptr // the console window, which identifies the console itself
	title  string
	// rows are the caret row and the rows above it, right-trimmed.
	rows []string
}

func open(target window, candidates []uint32) (*Session, error) {
	consoles := discover(candidates)
	switch len(consoles) {
	case 0:
		// The window is a terminal, so the clipboard path is not an option even
		// with no console to attach to: it would cancel or copy the shell's
		// line. The hotkey does nothing instead.
		return nil, fmt.Errorf("nothing under %s owns a console", target.class)
	case 1:
		return attach(consoles[0])
	}

	// Several tabs or panes. The pane that has the focus shows the rows of
	// exactly one console; failing that, the host window carries the title of
	// the active tab.
	console, err := byFocusedRows(target, consoles)
	if err == nil {
		return attach(console)
	}
	var matching []found
	for _, console := range consoles {
		if console.title == target.title {
			matching = append(matching, console)
		}
	}
	if len(matching) != 1 {
		return nil, fmt.Errorf("%w: %d consoles; %w; %d of them titled %q",
			ErrAmbiguous, len(consoles), err, len(matching), target.title)
	}
	return attach(matching[0])
}

// byFocusedRows picks the console whose caret row, and the rows above it,
// are what the focused pane shows.
func byFocusedRows(target window, consoles []found) (found, error) {
	rows, err := focusedRows(target.pid, matchRowsAbove)
	if err != nil {
		return found{}, fmt.Errorf("the focused pane cannot be read: %w", err)
	}
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
// in, keeping one entry per console.
func discover(candidates []uint32) []found {
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
		if handle != 0 && !seen[handle] {
			seen[handle] = true
			consoles = append(consoles, found{pid: pid, window: handle, title: consoleTitle(), rows: caretRows()})
		}
		detach()
	}
	return consoles
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

// Line returns what the shell shows on the input row up to the caret, prompt
// included. A line that wrapped is followed back over the full rows above it.
func (s *Session) Line() (string, error) {
	info, err := s.screen()
	if err != nil {
		return "", err
	}

	top := info.CursorPosition.Y
	for top > 0 && info.CursorPosition.Y-top < maxWrappedRows && s.full(top-1, info.Size.X) {
		top--
	}

	var b strings.Builder
	for row := top; row < info.CursorPosition.Y; row++ {
		b.WriteString(s.read(row, info.Size.X))
	}
	b.WriteString(s.read(info.CursorPosition.Y, info.CursorPosition.X))
	return b.String(), nil
}

// Delete asks the shell to erase n characters before the caret and reports how
// many it agreed to erase. A shell stops at its prompt, so the answer is the
// length of what the user actually typed.
//
// The backspaces go one at a time, each waited on through the rendered line
// rather than the caret, which the shell moves in a batch that lags the redraw.
// A burst of backspaces that runs past the prompt stalls PSReadLine after the
// first, so erasing stops as soon as the line no longer shrinks: nothing is
// ever sent into the prompt but the one press that proves the boundary.
func (s *Session) Delete(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	n = min(n, maxDeleted)

	before, err := s.Line()
	if err != nil {
		return 0, err
	}
	prev := before
	for range n {
		if err := s.write(press(nil, vkBack, scanBack, '\b')); err != nil {
			return 0, err
		}
		next := s.awaitStable(prev)
		removed, added := tailDiff(prev, next)
		if added > 0 {
			return 0, errors.New("the shell added text while deleting")
		}
		if removed == 0 {
			break // the shell erases no further; the prompt is reached
		}
		prev = next
	}

	removed, _ := tailDiff(before, prev)
	return removed, nil
}

// Type writes text into the input line as if its characters had been pressed,
// and checks that it arrived. A console read by more than one process, as
// WSL's is, swallows a key event now and then, so one retry is allowed.
func (s *Session) Type(text string) error {
	if text == "" {
		return nil
	}
	before, err := s.Line()
	if err != nil {
		return err
	}
	if err := s.typeOnce(text); err != nil {
		return err
	}
	after := s.awaitStable(before)
	if strings.HasSuffix(after, text) {
		return nil
	}

	// Erase whatever landed and type once more.
	if _, added := tailDiff(before, after); added > 0 {
		if _, err := s.Delete(added); err != nil {
			return err
		}
	}
	if before, err = s.Line(); err != nil {
		return err
	}
	if err := s.typeOnce(text); err != nil {
		return err
	}
	if after := s.awaitStable(before); !strings.HasSuffix(after, text) {
		return fmt.Errorf("the console did not accept the text; the line ends %q", after)
	}
	return nil
}

// typeOnce writes the characters of text into the input buffer.
func (s *Session) typeOnce(text string) error {
	records := make([]inputRecord, 0, len(text)*2)
	for _, unit := range windows.StringToUTF16(text) {
		if unit == 0 {
			continue
		}
		records = press(records, 0, 0, unit)
	}
	return s.write(records)
}

// tailDiff compares two lines by their common prefix: removed is how many
// trailing runes of before are gone in after, added how many runes after holds
// beyond the shared prefix. The prompt is the shared prefix, so it drops out.
func tailDiff(before, after string) (removed, added int) {
	b, a := []rune(before), []rune(after)
	i := 0
	for i < len(b) && i < len(a) && b[i] == a[i] {
		i++
	}
	return len(b) - i, len(a) - i
}

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

// drainInput waits for the shell to take every key event written, discarding
// any it never took so a leftover cannot reach whatever the shell reads next.
func (s *Session) drainInput() {
	deadline := time.Now().Add(drainTimeout)
	for s.pending() > 0 {
		if !time.Now().Before(deadline) {
			_, _, _ = procFlushConsoleInputBuffer.Call(uintptr(s.in))
			return
		}
		time.Sleep(pollDelay)
	}
}

// awaitStable returns the input line once the shell has finished the edit that
// changed it away from orig and the redraw has come to rest. It reads the
// rendered text rather than the caret, which the shell moves in a batch that
// lags the redraw. A press that changes nothing is given up on after
// quietTimeout, so a backspace at the prompt leaves the line untouched.
func (s *Session) awaitStable(orig string) string {
	s.drainInput()

	start := time.Now()
	last, stable, changed := orig, 0, false
	deadline := time.Now().Add(editTimeout)
	for time.Now().Before(deadline) {
		now, err := s.Line()
		if err == nil {
			switch {
			case now != last:
				last, stable = now, 0
				changed = changed || now != orig
			case changed:
				if stable++; stable >= stableReads {
					return now
				}
			}
		}
		if !changed && time.Since(start) >= quietTimeout {
			return orig
		}
		time.Sleep(pollDelay)
	}
	return last
}

// full reports whether a row has been written to its last cell, which is what
// a wrapped line leaves behind.
func (s *Session) full(row, width int16) bool {
	text := s.read(row, width)
	if text == "" {
		return false
	}
	return !strings.HasSuffix(text, " ")
}

func (s *Session) read(row, width int16) string { return readRow(s.out, row, width) }

func readRow(out windows.Handle, row, width int16) string {
	if width <= 0 || row < 0 {
		return ""
	}
	buf := make([]uint16, width)
	var got uint32
	coord := uint32(uint16(row)) << 16 // COORD{X: 0, Y: row}
	ret, _, _ := procReadConsoleOutputCharW.Call(uintptr(out),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(width), uintptr(coord),
		uintptr(unsafe.Pointer(&got)))
	if ret == 0 || got == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
}

// caretRows reads the caret row of the attached console and up to
// matchRowsAbove rows above it, right-trimmed.
func caretRows() []string {
	out, err := openConsoleFile("CONOUT$")
	if err != nil {
		return nil
	}
	defer func() { _ = windows.CloseHandle(out) }()

	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(out, &info); err != nil || info.Size.X <= 0 {
		return nil
	}
	first := max(info.CursorPosition.Y-matchRowsAbove, 0)
	rows := make([]string, 0, info.CursorPosition.Y-first+1)
	for row := first; row <= info.CursorPosition.Y; row++ {
		rows = append(rows, strings.TrimRight(readRow(out, row, info.Size.X), " "))
	}
	return rows
}

func consoleTitle() string {
	var buf [512]uint16
	n, _, _ := procGetConsoleTitleW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// Virtual key and scan code of Backspace; every other key this package sends
// carries a character and no key code at all.
const (
	vkBack   = 0x08
	scanBack = 0x0E
)

// keyEventRecord mirrors KEY_EVENT_RECORD.
type keyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

// inputRecord mirrors INPUT_RECORD holding a key event.
type inputRecord struct {
	eventType uint16
	_         uint16
	event     keyEventRecord
}

const keyEvent = 0x0001

func press(records []inputRecord, vk, scan, char uint16) []inputRecord {
	down := inputRecord{eventType: keyEvent, event: keyEventRecord{
		keyDown: 1, repeatCount: 1, virtualKeyCode: vk,
		virtualScanCode: scan, unicodeChar: char}}
	up := down
	up.event.keyDown = 0
	return append(records, down, up)
}

func (s *Session) write(records []inputRecord) error {
	for len(records) > 0 {
		batch := records[:min(len(records), recordsPerWrite)]
		var written uint32
		ret, _, err := procWriteConsoleInputW.Call(uintptr(s.in),
			uintptr(unsafe.Pointer(&batch[0])), uintptr(len(batch)),
			uintptr(unsafe.Pointer(&written)))
		if ret == 0 || int(written) != len(batch) {
			return fmt.Errorf("the console accepted %d of %d key events: %w", written, len(batch), err)
		}
		records = records[len(batch):]
	}
	return nil
}
