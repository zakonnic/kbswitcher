package console

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole          = kernel32.NewProc("AttachConsole")
	procFreeConsole            = kernel32.NewProc("FreeConsole")
	procGetConsoleTitleW       = kernel32.NewProc("GetConsoleTitleW")
	procGetConsoleWindow       = kernel32.NewProc("GetConsoleWindow")
	procReadConsoleOutputCharW = kernel32.NewProc("ReadConsoleOutputCharacterW")
	procWriteConsoleInputW     = kernel32.NewProc("WriteConsoleInputW")
	procSetConsoleCtrlHandler  = kernel32.NewProc("SetConsoleCtrlHandler")
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
	// maxDeleted bounds one burst of backspaces.
	maxDeleted = 256
	// recordsPerWrite keeps one WriteConsoleInput call to a sane size.
	recordsPerWrite = 512

	// The shell redraws its line asynchronously, so an edit is watched through
	// the caret rather than assumed.
	firstDelay  = 30 * time.Millisecond
	pollDelay   = 8 * time.Millisecond
	stableReads = 6
	editTimeout = 700 * time.Millisecond
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
}

func open(target window, candidates []uint32) (*Session, error) {
	consoles := discover(candidates)
	switch len(consoles) {
	case 0:
		return nil, fmt.Errorf("%w: nothing under %s owns a console", ErrNotConsole, target.class)
	case 1:
		return attach(consoles[0])
	}

	// Several tabs or panes. The host window shows the title of the active
	// one, which is the only thing Windows offers to tell them apart.
	var matching []found
	for _, console := range consoles {
		if console.title == target.title {
			matching = append(matching, console)
		}
	}
	if len(matching) != 1 {
		return nil, fmt.Errorf("%w: %d consoles, %d of them titled %q",
			ErrAmbiguous, len(consoles), len(matching), target.title)
	}
	return attach(matching[0])
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
			consoles = append(consoles, found{pid: pid, window: handle, title: consoleTitle()})
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
func (s *Session) Delete(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	n = min(n, maxDeleted)
	erasing := int32(n) //nolint:gosec // n is at most maxDeleted

	before, err := s.caret()
	if err != nil {
		return 0, err
	}
	records := make([]inputRecord, 0, n*2)
	for range n {
		records = press(records, vkBack, scanBack, '\b')
	}
	if err := s.write(records); err != nil {
		return 0, err
	}

	after := s.settle(before, before-erasing)
	if after > before {
		return 0, errors.New("the shell moved the caret forward while deleting")
	}
	return int(before - after), nil
}

// Type writes text into the input line as if its characters had been pressed,
// and checks that it arrived. A console read by more than one process, as
// WSL's is, swallows a key event now and then, so one retry is allowed.
func (s *Session) Type(text string) error {
	if text == "" {
		return nil
	}
	landed, err := s.typeOnce(text)
	if err != nil {
		return err
	}
	if s.arrived(text) {
		return nil
	}
	if landed > 0 {
		if _, err := s.Delete(landed); err != nil {
			return err
		}
	}
	_, err = s.typeOnce(text)
	return err
}

// typeOnce writes the characters and reports how many cells the caret moved.
func (s *Session) typeOnce(text string) (int, error) {
	before, err := s.caret()
	if err != nil {
		return 0, err
	}

	records := make([]inputRecord, 0, len(text)*2)
	for _, unit := range windows.StringToUTF16(text) {
		if unit == 0 {
			continue
		}
		records = press(records, 0, 0, unit)
	}
	if err := s.write(records); err != nil {
		return 0, err
	}

	//nolint:gosec // a word is never long enough to overflow
	after := s.settle(before, before+int32(utf8.RuneCountInString(text)))
	return max(int(after-before), 0), nil
}

func (s *Session) arrived(text string) bool {
	line, err := s.Line()
	return err == nil && strings.HasSuffix(line, text)
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

// caret is the position of the caret counted in cells from the top of the
// buffer, so that a line unwrapping onto the row above still counts down.
func (s *Session) caret() (int32, error) {
	info, err := s.screen()
	if err != nil {
		return 0, err
	}
	return int32(info.CursorPosition.Y)*int32(info.Size.X) + int32(info.CursorPosition.X), nil
}

// settle waits for the shell to finish redrawing: either the caret reaches
// where the edit should leave it, or it stops moving.
func (s *Session) settle(from, want int32) int32 {
	time.Sleep(firstDelay)
	last, stable := from, 0
	deadline := time.Now().Add(editTimeout)
	for {
		now, err := s.caret()
		if err != nil {
			return last
		}
		switch now {
		case want:
			return now
		case last:
			stable++
			if stable >= stableReads {
				return now
			}
		default:
			last, stable = now, 0
		}
		if !time.Now().Before(deadline) {
			return last
		}
		time.Sleep(pollDelay)
	}
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

func (s *Session) read(row, width int16) string {
	if width <= 0 || row < 0 {
		return ""
	}
	buf := make([]uint16, width)
	var got uint32
	coord := uint32(uint16(row)) << 16 // COORD{X: 0, Y: row}
	ret, _, _ := procReadConsoleOutputCharW.Call(uintptr(s.out),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(width), uintptr(coord),
		uintptr(unsafe.Pointer(&got)))
	if ret == 0 || got == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:got])
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
