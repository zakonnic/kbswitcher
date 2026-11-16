package console

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/zakonnic/kbswitcher/internal/platform/uia"
)

var procUpdateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")

// releaseConsole frees the test's own console, since a process attaches to
// one console at a time, and attaches to it again when the test is done, so
// that go test -v run in a terminal can still print there.
func releaseConsole(t *testing.T) {
	t.Helper()
	detach()
	t.Cleanup(func() {
		const parent = ^uintptr(0) // ATTACH_PARENT_PROCESS
		_, _, _ = procAttachConsole.Call(parent)
	})
}

// newShell runs cmd.exe in a pseudoconsole of the test's own, as a terminal
// application (VS Code, Windows Terminal) runs a shell: no window, a console
// whose window is a hidden PseudoConsoleWindow. The prompt is a bare ">".
func newShell(t *testing.T) uint32 {
	t.Helper()
	releaseConsole(t)
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		t.Fatal(err)
	}
	var pc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 100, Y: 30}, inRead, outWrite, 0, &pc); err != nil {
		t.Fatalf("CreatePseudoConsole: %v", err)
	}
	// The pseudoconsole stops when nobody reads what it renders.
	output := os.NewFile(uintptr(outRead), "pseudoconsole")
	go func() { _, _ = io.Copy(io.Discard, output) }()

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}
	// The attribute's value is the handle itself, which an unsafe.Pointer
	// must not carry.
	if ret, _, err := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(attributes.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(pc), unsafe.Sizeof(pc), 0, 0); ret == 0 {
		t.Fatal(err)
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// Without handles of its own the shell would write to the test's output,
	// which go test redirects, rather than to the pseudoconsole.
	si.Flags = windows.STARTF_USESTDHANDLES
	var pi windows.ProcessInformation
	command, _ := windows.UTF16PtrFromString(`cmd.exe /q /k prompt $G`)
	if err := windows.CreateProcess(nil, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT,
		nil, nil, &si.StartupInfo, &pi); err != nil {
		t.Fatalf("CreateProcess: %v", err)
	}
	t.Cleanup(func() {
		// Termination is asynchronous: a shell still alive would be a second
		// console for the next test's Open.
		_ = windows.TerminateProcess(pi.Process, 0)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		_ = windows.CloseHandle(pi.Process)
		_ = windows.CloseHandle(pi.Thread)
		windows.ClosePseudoConsole(pc)
		attributes.Delete()
		for _, h := range []windows.Handle{inRead, inWrite, outWrite} {
			_ = windows.CloseHandle(h)
		}
		_ = output.Close()
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		consoles := discover(terminalWindow, []uint32{pi.ProcessId})
		if len(consoles) == 1 && len(consoles[0].rows) > 0 && strings.HasSuffix(consoles[0].rows[len(consoles[0].rows)-1], ">") {
			return pi.ProcessId
		}
		if time.Now().After(deadline) {
			t.Fatalf("no prompt: %+v", consoles)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// terminalWindow is a terminal application's window, as foreground finds it.
var terminalWindow = window{class: "CASCADIA_HOSTING_WINDOW_CLASS", kind: Cascadia}

// borrow attaches to the shell's console as Open does.
func borrow(t *testing.T, pids []uint32, probe Probe, selection bool, kind Kind) (*Session, error) {
	t.Helper()
	target := terminalWindow
	target.kind = kind
	attachMu.Lock()
	s, err := open(target, pids, probe, selection)
	if err != nil {
		attachMu.Unlock()
	}
	return s, err
}

// edit runs one Edit in a fresh session and returns the line it was shown.
func edit(t *testing.T, pid uint32, whole bool, change func(Line) (int, int, string)) Line {
	t.Helper()
	s, err := borrow(t, []uint32{pid}, nil, false, Cascadia)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	var seen Line
	if err := s.Edit(whole, func(line Line) (int, int, string) {
		seen = line
		return change(line)
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	return seen
}

func look(line Line) (int, int, string) { return 0, 0, "" }

func TestEditTheInputLineOfAShell(t *testing.T) {
	pid := newShell(t)

	edit(t, pid, true, func(line Line) (int, int, string) {
		return 0, len([]rune(line.Text)), "echo ghbdtn ckjdj"
	})
	if got := edit(t, pid, true, look); got.Text != "echo ghbdtn ckjdj" || got.Caret != len("echo ghbdtn ckjdj") {
		t.Fatalf("the line is %+v", got)
	}

	// The last word, as the hotkey replaces it.
	edit(t, pid, false, func(line Line) (int, int, string) {
		from := strings.LastIndex(line.Text, " ") + 1
		return from, line.Caret, "слово"
	})
	if got := edit(t, pid, true, look); got.Text != "echo ghbdtn слово" {
		t.Errorf("the line is %q", got.Text)
	}

	// A word in the middle, with text after it.
	edit(t, pid, true, func(line Line) (int, int, string) { return 5, 11, "привет" })
	if got := edit(t, pid, true, look); got.Text != "echo привет слово" {
		t.Errorf("the line is %q", got.Text)
	}

	// Up to the caret only: what follows it is not shown.
	edit(t, pid, true, func(line Line) (int, int, string) { return 0, len([]rune(line.Text)), "abc def" })
	s, err := borrow(t, []uint32{pid}, nil, false, Cascadia)
	if err != nil {
		t.Fatal(err)
	}
	err = s.write(navigate(nil, vkLeft, scanLeft, 3))
	_ = s.Close() // before any Fatal: a session left open holds attachMu for every later test
	if err != nil {
		t.Fatal(err)
	}
	// The shell takes the keys at its own pace.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if caret, ok := caretOf(pid); ok && caret.X == int16(len(">abc ")) || time.Now().After(deadline) {
			break
		}
	}
	if got := edit(t, pid, false, look); got.Text != "abc " {
		t.Errorf("the line up to the caret is %q", got.Text)
	}
}

func TestOpenFindsNoConsole(t *testing.T) {
	releaseConsole(t)
	if _, err := borrow(t, nil, nil, false, Cascadia); err == nil || errors.Is(err, ErrNotConsole) {
		t.Errorf("a terminal without consoles: %v", err)
	}
	if _, err := borrow(t, []uint32{uint32(os.Getpid())}, nil, false, Other); !errors.Is(err, ErrNotConsole) {
		t.Errorf("an application without consoles: %v", err)
	}
}

// keysTo presses keys in the console of a shell, as a terminal passes on the
// keys the user presses.
func keysTo(t *testing.T, pid uint32) Probe {
	t.Helper()
	return func(back bool) error {
		vk, scan := uint16(vkLeft), uint16(scanLeft)
		if back {
			vk, scan = vkRight, scanRight
		}
		if ret, _, err := procAttachConsole.Call(uintptr(pid)); ret == 0 {
			return err
		}
		defer detach()
		in, err := openConsoleFile("CONIN$")
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(in) }()
		s := &Session{in: in}
		return s.write(navigate(nil, vk, scan, 1))
	}
}

func TestAProbeFindsTheShellThatAnswers(t *testing.T) {
	pid := newShell(t)
	edit(t, pid, true, func(line Line) (int, int, string) { return 0, len([]rune(line.Text)), "echo ghbdtn" })
	quiet := newShell(t) // its line is empty: Left moves nothing

	// The probe gives a shell 120 ms to move its caret; a machine busy
	// with other tests can take longer once.
	var err error
	for range 3 {
		var s *Session
		if s, err = borrow(t, []uint32{quiet, pid}, keysTo(t, pid), false, JetBrains); err == nil {
			_ = s.Close()
			break
		}
	}
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := borrow(t, []uint32{quiet}, keysTo(t, quiet), false, JetBrains); !errors.Is(err, ErrShells) {
		t.Errorf("no shell answers: %v", err)
	}
	if _, err := borrow(t, []uint32{pid}, keysTo(t, pid), true, JetBrains); !errors.Is(err, ErrShells) {
		t.Errorf("a selection is probed: %v", err)
	}
	if _, err := byProbe(nil, nil); !errors.Is(err, errNoAnswer) {
		t.Errorf("no probe: %v", err)
	}
}

func TestSeveralConsolesAreToldApartByTheirRows(t *testing.T) {
	one, two := newShell(t), newShell(t)
	edit(t, one, true, func(line Line) (int, int, string) { return 0, len([]rune(line.Text)), "echo one" })
	edit(t, two, true, func(line Line) (int, int, string) { return 0, len([]rune(line.Text)), "echo two" })

	consoles := discover(terminalWindow, []uint32{one, two})
	if len(consoles) != 2 {
		t.Fatalf("found %d consoles", len(consoles))
	}
	got, err := byRows([]string{">echo two"}, consoles)
	if err != nil || got.pid != two {
		t.Errorf("byRows = %d, %v; want %d", got.pid, err, two)
	}
	if _, err := byRows([]string{">echo"}, consoles); err == nil {
		t.Error("rows both consoles show picked one")
	}
}

func TestProcessTreeAndWindows(t *testing.T) {
	pid := newShell(t)
	tree, err := processTree()
	if err != nil {
		t.Fatal(err)
	}
	if below := tree.below(uint32(os.Getpid())); below[0] != uint32(os.Getpid()) || !slices.Contains(below, pid) {
		t.Errorf("below the test: %v, want the test and its shell %d", below, pid)
	}
	consoles := discover(terminalWindow, []uint32{pid})
	if len(consoles) != 1 || windowClass(consoles[0].window) != pseudoConsoleClass ||
		processOf(windows.HWND(consoles[0].window)) == 0 {
		t.Errorf("the console window is %+v", consoles)
	}
	for class, want := range map[string]Kind{"ConsoleWindowClass": Conhost, "CASCADIA_HOSTING_WINDOW_CLASS": Cascadia,
		"mintty": Mintty, "Chrome_WidgetWin_1": Chromium, "SunAwtFrame": JetBrains, "Notepad": Other} {
		if got := classify(class); got != want {
			t.Errorf("classify(%q) = %v, want %v", class, got, want)
		}
	}
}

func TestOpenBorrowsTheConsoleUnderTheForegroundWindow(t *testing.T) {
	newShell(t)
	previous := foregroundWindow
	t.Cleanup(func() { foregroundWindow = previous })

	// The terminal is this test: the shell runs under it.
	foregroundWindow = func() (window, error) {
		w := terminalWindow
		w.pid = uint32(os.Getpid())
		return w, nil
	}
	s, err := Open(nil, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil || s.Close() != nil {
		t.Errorf("Close: %v", err)
	}
	foregroundWindow = func() (window, error) { return window{class: "mintty", kind: Mintty}, nil }
	if _, err := Open(nil, false); err == nil {
		t.Error("mintty was opened")
	}
	foregroundWindow = func() (window, error) { return window{}, errNoWindow }
	if _, err := Open(nil, false); !errors.Is(err, errNoWindow) {
		t.Errorf("no window: %v", err)
	}
}

// title has the shell set its console's title, as a tab names itself.
func title(t *testing.T, pid uint32, name string) {
	t.Helper()
	edit(t, pid, true, func(line Line) (int, int, string) { return 0, len([]rune(line.Text)), "title " + name })
	s, err := borrow(t, []uint32{pid}, nil, false, Cascadia)
	if err != nil {
		t.Fatal(err)
	}
	err = s.write(press(nil, 0x0D, 0x1C, '\r', 0)) // Enter
	_ = s.Close()                                  // a process attaches to one console at a time
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if consoles := discover(terminalWindow, []uint32{pid}); len(consoles) == 1 && consoles[0].title == name {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the title did not become %q: %+v", name, discover(terminalWindow, []uint32{pid}))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSeveralConsolesAreToldApartByTheTitle(t *testing.T) {
	one, two := newShell(t), newShell(t)
	title(t, one, "one")
	title(t, two, "two")
	consoles := discover(terminalWindow, []uint32{one, two})

	// The window carries the title of its active tab; UI Automation tells
	// nothing of a window this test made up.
	target := terminalWindow
	target.title = "two"
	got, err := pick(target, consoles, nil, false)
	if err != nil || got.pid != two {
		t.Errorf("pick = %d, %v; want %d", got.pid, err, two)
	}
	target.title = "three"
	if _, err := pick(target, consoles, nil, false); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("a title no console has: %v", err)
	}
	// An xterm.js terminal whose focus cannot be asked about.
	target.kind = Chromium
	target.pid = uint32(os.Getpid())
	if _, err := pick(target, consoles, nil, false); err == nil {
		t.Error("a Chromium window without a terminal in focus was picked")
	}
}

func TestByCaretMatchesTheGrid(t *testing.T) {
	f := focus{class: xtermTextArea, caret: uiaRect(110, 40, 118, 58), screen: uiaRect(100, 4, 900, 724)}
	consoles := []found{
		{pid: 1, view: view{cols: 100, lines: 40, caretX: 1, caretY: 2}},
		{pid: 2, view: view{cols: 100, lines: 40, caretX: 5, caretY: 2}},
	}
	if got, err := byCaret(f, consoles); err != nil || got.pid != 1 {
		t.Errorf("byCaret = %d, %v; want 1", got.pid, err)
	}
	if _, err := byCaret(f, append(consoles, consoles[0])); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("two consoles under the caret: %v", err)
	}
	if !f.xterm() || (focus{class: xtermTextArea}).xterm() {
		t.Error("xterm misreads a focus")
	}
}

func uiaRect(left, top, right, bottom int32) uia.Rect {
	return uia.Rect{Left: left, Top: top, Right: right, Bottom: bottom}
}

func TestAttachToAProcessThatIsGone(t *testing.T) {
	releaseConsole(t)
	attachMu.Lock()
	defer attachMu.Unlock()
	if _, err := attach(found{pid: 0x7FFFFFF0}); err == nil {
		t.Error("attached to a process that does not exist")
	}
}
