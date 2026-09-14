//go:build windows

// Command stand drives a real console to test kbswitcher's terminal path. It
// opens a shell in conhost or Windows Terminal, brings it to the foreground,
// types a word, converts it and dumps what the console shows afterwards.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/zakonnic/kbswitcher/internal/platform/clip"
	"github.com/zakonnic/kbswitcher/internal/platform/console"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/platform/layouts"
	"github.com/zakonnic/kbswitcher/internal/switcher"
)

var (
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole          = kernel32.NewProc("AttachConsole")
	procFreeConsole            = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow       = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleTitleW       = kernel32.NewProc("GetConsoleTitleW")
	procReadConsoleOutputCharW = kernel32.NewProc("ReadConsoleOutputCharacterW")
	procWriteConsoleInputW     = kernel32.NewProc("WriteConsoleInputW")

	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procShowWindow               = user32.NewProc("ShowWindow")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procSendInput                = user32.NewProc("SendInput")
	procPostMessage              = user32.NewProc("PostMessageW")
)

type keyboardInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uint64
	_     [8]byte
}

type keyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

type inputRecord struct {
	eventType uint16
	_         uint16
	event     keyEventRecord
}

func main() {
	host := flag.String("host", "conhost", "conhost or wt")
	shell := flag.String("shell", "powershell.exe", "shell command line")
	word := flag.String("word", "ghbdtn", "text to type before converting")
	tabs := flag.Int("tabs", 1, "how many tabs to open in Windows Terminal")
	mode := flag.String("mode", "inproc", "inproc, hotkey or none")
	kb := flag.String("kb", "", "kbswitcher.exe to run in hotkey mode")
	layout := flag.String("layout", "00000409", "layout to activate before typing")
	keep := flag.Bool("keep", false, "leave the shell open")
	selection := flag.Bool("selection", false, "convert the whole line instead of the last word")
	closeList := flag.String("close", "", "comma-separated shell pids to type exit into, then quit")
	typeAfter := flag.Bool("typeafter", false, "type the word again after converting, to see the layout")
	windowsCount := flag.Int("windows", 1, "how many Windows Terminal windows to open; the first one is used")
	pre := flag.String("pre", "", "a command to run before typing the word")
	closeWindows := flag.Bool("closewindows", false, "close every Windows Terminal window and quit")
	flag.Parse()

	_, _, _ = procFreeConsole.Call()
	if *closeWindows {
		for h := range cascadiaWindows() {
			fmt.Printf("closing window %#x %q\n", h, windowText(h))
			_, _, _ = procPostMessage.Call(h, 0x0010, 0, 0) // WM_CLOSE
		}
		return
	}
	if *closeList != "" {
		var pids []uint32
		for _, field := range strings.Split(*closeList, ",") {
			var pid uint32
			fmt.Sscan(field, &pid)
			pids = append(pids, pid)
		}
		closeShells(pids)
		return
	}

	var pids []uint32
	var hwnd uintptr
	switch *host {
	case "conhost":
		pid, h := launchConhost(*shell)
		pids, hwnd = []uint32{pid}, h
	case "wt":
		pids, hwnd = launchWT(*shell, *tabs)
	default:
		fatal("unknown host %q", *host)
	}
	fmt.Printf("shell pids=%v hwnd=%#x class=%q\n", pids, hwnd, className(hwnd))
	target := pids[len(pids)-1] // the last tab opened is the active one
	for i := 1; i < *windowsCount; i++ {
		extra, extraHwnd := launchWT(*shell, 1)
		fmt.Printf("extra window pids=%v hwnd=%#x\n", extra, extraHwnd)
		pids = append(pids, extra...)
	}
	if !*keep {
		defer closeShells(pids)
	}
	if !forceForeground(hwnd) {
		fatal("could not bring the shell to the foreground; foreground is %#x %q", fg(), className(fg()))
	}
	fmt.Printf("foreground=%#x title=%q\n", fg(), windowText(fg()))

	reader, err := layouts.Open()
	check(err, "layouts.Open")
	_, err = reader.List()
	check(err, "layouts.List")
	check(reader.Activate(*layout), "Activate")
	time.Sleep(300 * time.Millisecond)
	active, _ := reader.Active()
	fmt.Printf("layout before=%s\n", active)

	waitPrompt(target)
	if *pre != "" {
		typeText(*pre)
		sendKey(0x0D, 0x1C, false, false)
		sendKey(0x0D, 0x1C, false, true)
		time.Sleep(500 * time.Millisecond)
		waitPrompt(target)
	}
	typeText(*word)
	if !waitSuffix(target, *word) {
		fmt.Println("the word did not arrive")
	}
	fmt.Println("--- before ---")
	dump(target)

	var proc *os.Process
	switch *mode {
	case "inproc":
		convertInProcess(*selection)
	case "hotkey":
		proc = startKbswitcher(*kb)
		if *selection {
			sendKey(0x10, 0x2A, false, false)
		}
		sendKey(0x13, 0x45, false, false)
		sendKey(0x13, 0x45, false, true)
		if *selection {
			sendKey(0x10, 0x2A, false, true)
		}
		time.Sleep(2500 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)
	fmt.Println("--- after ---")
	dump(target)
	active, _ = reader.Active()
	fmt.Printf("layout after=%s foreground=%#x\n", active, fg())
	if *typeAfter {
		typeText(" " + *word)
		time.Sleep(400 * time.Millisecond)
		fmt.Println("--- typed again ---")
		dump(target)
	}

	if proc != nil {
		_ = proc.Kill()
		time.Sleep(300 * time.Millisecond)
		log, _ := os.ReadFile(os.Getenv("LOCALAPPDATA") + `\kbswitcher\kbswitcher.log`)
		fmt.Println("--- kbswitcher log ---")
		fmt.Print(string(log))
	}
}

func convertInProcess(selection bool) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reader, err := layouts.Open()
	check(err, "layouts.Open")
	installed, err := reader.List()
	check(err, "layouts.List")
	keyboard, err := input.Open()
	check(err, "input.Open")
	clipboard, err := clip.Open()
	check(err, "clip.Open")
	deps := switcher.Deps{Layouts: reader, Keyboard: keyboard, Clipboard: clipboard, Terminal: terminal{}}
	s, err := switcher.New(installed, deps, switcher.DefaultOptions(), log)
	check(err, "switcher.New")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if selection {
		err = s.ConvertSelection(ctx)
	} else {
		err = s.ConvertLastWord(ctx)
	}
	fmt.Printf("convert: err=%v took=%s\n", err, time.Since(start))
}

type terminal struct{}

func (terminal) Open() (switcher.Console, error) {
	session, err := console.Open()
	if err != nil {
		return nil, err
	}
	return session, nil
}

func startKbswitcher(path string) *os.Process {
	if path == "" {
		fatal("-kb is required in hotkey mode")
	}
	cmd := exec.Command(path, "-verbose")
	check(cmd.Start(), "start kbswitcher")
	time.Sleep(2 * time.Second)
	return cmd.Process
}

// launchConhost starts the shell in a console window of its own.
func launchConhost(shell string) (uint32, uintptr) {
	host := createProcess("conhost.exe "+shell, windows.CREATE_NEW_CONSOLE)
	exe := strings.Fields(shell)[0]
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		parents := parentsOf()
		for pid := range processesNamed(exe) {
			if parents[pid] != host {
				continue
			}
			if ret, _, _ := procAttachConsole.Call(uintptr(pid)); ret != 0 {
				hwnd, _, _ := procGetConsoleWindow.Call()
				_, _, _ = procFreeConsole.Call()
				if hwnd != 0 {
					return pid, hwnd
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fatal("the console of %d never appeared", host)
	return 0, 0
}

func parentsOf() map[uint32]uint32 {
	parents := make(map[uint32]uint32)
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	check(err, "snapshot")
	defer windows.CloseHandle(handle)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
		entry.Size = uint32(unsafe.Sizeof(entry))
	}
	return parents
}

// launchWT opens a new Windows Terminal window running the shell and, if asked,
// more tabs of it. The shell processes are found by comparing snapshots.
func launchWT(shell string, tabs int) ([]uint32, uintptr) {
	exe := strings.Fields(shell)[0]
	wt, err := exec.LookPath("wt.exe")
	check(err, "wt.exe")

	windowsBefore := cascadiaWindows()
	pidsBefore := processesNamed(exe)
	createProcess(fmt.Sprintf(`"%s" -w -1 %s`, wt, shell), 0)

	var hwnd uintptr
	var pids []uint32
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pids = topmost(newPIDs(pidsBefore, processesNamed(exe)))
		for h := range cascadiaWindows() {
			if !windowsBefore[h] {
				hwnd = h
			}
		}
		if hwnd != 0 && len(pids) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hwnd == 0 || len(pids) != 1 {
		fatal("Windows Terminal did not open as expected: hwnd=%#x pids=%v", hwnd, pids)
	}

	for i := 1; i < tabs; i++ {
		before := processesNamed(exe)
		createProcess(fmt.Sprintf(`"%s" -w last nt %s`, wt, shell), 0)
		deadline := time.Now().Add(10 * time.Second)
		var fresh []uint32
		for time.Now().Before(deadline) {
			if fresh = topmost(newPIDs(before, processesNamed(exe))); len(fresh) == 1 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if len(fresh) != 1 {
			fatal("tab %d did not open: %v", i+1, fresh)
		}
		pids = append(pids, fresh[0])
	}
	time.Sleep(500 * time.Millisecond)
	return pids, hwnd
}

// topmost drops the processes started by another one of the list, as WSL's
// relay is.
func topmost(pids []uint32) []uint32 {
	parents := parentsOf()
	fresh := make(map[uint32]bool, len(pids))
	for _, pid := range pids {
		fresh[pid] = true
	}
	var top []uint32
	for _, pid := range pids {
		if !fresh[parents[pid]] {
			top = append(top, pid)
		}
	}
	return top
}

func createProcess(cmdline string, flags uint32) uint32 {
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	err := windows.CreateProcess(nil, windows.StringToUTF16Ptr(cmdline), nil, nil, false, flags, nil, nil, &si, &pi)
	check(err, "CreateProcess "+cmdline)
	_ = windows.CloseHandle(pi.Thread)
	_ = windows.CloseHandle(pi.Process)
	return pi.ProcessId
}

func processesNamed(exe string) map[uint32]bool {
	found := make(map[uint32]bool)
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	check(err, "snapshot")
	defer windows.CloseHandle(handle)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exe) {
			found[entry.ProcessID] = true
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
	}
	return found
}

func newPIDs(before, after map[uint32]bool) []uint32 {
	var fresh []uint32
	for pid := range after {
		if !before[pid] {
			fresh = append(fresh, pid)
		}
	}
	return fresh
}

func cascadiaWindows() map[uintptr]bool {
	found := make(map[uintptr]bool)
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if className(hwnd) == "CASCADIA_HOSTING_WINDOW_CLASS" {
			found[hwnd] = true
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return found
}

func forceForeground(hwnd uintptr) bool {
	for i := 0; i < 20; i++ {
		if fg() == hwnd {
			return true
		}
		_, _, _ = procShowWindow.Call(hwnd, 9) // SW_RESTORE
		sendKey(0x12, 0x38, false, false)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		sendKey(0x12, 0x38, false, true)
		time.Sleep(150 * time.Millisecond)
		if fg() == hwnd {
			return true
		}
		var pid uint32
		tid, _, _ := procGetWindowThreadProcessID.Call(fg(), uintptr(unsafe.Pointer(&pid)))
		me := windows.GetCurrentThreadId()
		_, _, _ = procAttachThreadInput.Call(uintptr(me), tid, 1)
		_, _, _ = procBringWindowToTop.Call(hwnd)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		_, _, _ = procAttachThreadInput.Call(uintptr(me), tid, 0)
		time.Sleep(150 * time.Millisecond)
	}
	return fg() == hwnd
}

func fg() uintptr {
	h, _, _ := procGetForegroundWindow.Call()
	return h
}

func className(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func windowText(hwnd uintptr) string {
	var buf [512]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// Virtual keys of the US layout for the characters a test word may hold.
var oemKeys = map[rune]uint16{
	' ': 0x20, ';': 0xBA, '=': 0xBB, ',': 0xBC, '-': 0xBD, '.': 0xBE, '/': 0xBF,
	'`': 0xC0, '[': 0xDB, '\\': 0xDC, ']': 0xDD, '\'': 0xDE,
}

// shifted maps the characters of the US layout that need Shift to the
// unshifted character of the same key.
var shifted = map[rune]rune{
	'!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6', '&': '7', '*': '8', '(': '9', ')': '0',
	'"': '\'', ':': ';', '<': ',', '>': '.', '?': '/', '_': '-', '+': '=', '{': '[', '}': ']', '|': '\\', '~': '`',
}

func typeText(text string) {
	for _, r := range text {
		var vk uint16
		shift := false
		if base, ok := shifted[r]; ok {
			r, shift = base, true
		}
		switch {
		case r >= 'a' && r <= 'z':
			vk = uint16(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			vk, shift = uint16(r), true
		case r >= '0' && r <= '9':
			vk = uint16(r)
		default:
			var ok bool
			if vk, ok = oemKeys[r]; !ok {
				fatal("cannot type %q", r)
			}
		}
		if shift {
			sendKey(0x10, 0x2A, false, false)
		}
		sendKey(vk, 0, false, false)
		sendKey(vk, 0, false, true)
		if shift {
			sendKey(0x10, 0x2A, false, true)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func sendKey(vk, scan uint16, extended, up bool) {
	var flags uint32
	if extended {
		flags |= 1
	}
	if up {
		flags |= 2
	}
	ev := keyboardInput{typ: 1, vk: vk, scan: scan, flags: flags}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&ev)), unsafe.Sizeof(ev))
	if n != 1 {
		fatal("SendInput: %v", err)
	}
}

// screen is what one console shows around its caret.
type screen struct {
	rows      []string
	firstRow  int16
	caretX    int16
	caretY    int16
	width     int16
	title     string
	windowTop int16
	windowBot int16
}

func (s screen) line() string {
	i := int(s.caretY - s.firstRow)
	if i < 0 || i >= len(s.rows) {
		return ""
	}
	row := []rune(s.rows[i])
	if int(s.caretX) < len(row) {
		return string(row[:s.caretX])
	}
	return string(row)
}

func readScreen(pid uint32) (screen, error) {
	if ret, _, err := procAttachConsole.Call(uintptr(pid)); ret == 0 {
		return screen{}, fmt.Errorf("AttachConsole(%d): %w", pid, err)
	}
	defer procFreeConsole.Call()
	out, err := windows.CreateFile(windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return screen{}, err
	}
	defer windows.CloseHandle(out)
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(out, &info); err != nil {
		return screen{}, err
	}
	s := screen{caretX: info.CursorPosition.X, caretY: info.CursorPosition.Y, width: info.Size.X,
		windowTop: info.Window.Top, windowBot: info.Window.Bottom}
	s.firstRow = max(info.CursorPosition.Y-5, 0)
	last := min(info.CursorPosition.Y+2, info.Size.Y-1)
	for row := s.firstRow; row <= last; row++ {
		buf := make([]uint16, info.Size.X)
		var got uint32
		coord := uint32(uint16(row)) << 16
		procReadConsoleOutputCharW.Call(uintptr(out), uintptr(unsafe.Pointer(&buf[0])), uintptr(info.Size.X), uintptr(coord), uintptr(unsafe.Pointer(&got)))
		s.rows = append(s.rows, windows.UTF16ToString(buf[:got]))
	}
	var tbuf [512]uint16
	n, _, _ := procGetConsoleTitleW.Call(uintptr(unsafe.Pointer(&tbuf[0])), uintptr(len(tbuf)))
	s.title = windows.UTF16ToString(tbuf[:n])
	return s, nil
}

func dump(pid uint32) {
	s, err := readScreen(pid)
	if err != nil {
		fmt.Println("read:", err)
		return
	}
	for i, row := range s.rows {
		mark := " "
		if int16(i)+s.firstRow == s.caretY {
			mark = ">"
		}
		fmt.Printf("%s %3d |%s|\n", mark, int(s.firstRow)+i, strings.TrimRight(row, " "))
	}
	fmt.Printf("caret=(%d,%d) width=%d window=%d..%d title=%q line=%q\n",
		s.caretX, s.caretY, s.width, s.windowTop, s.windowBot, s.title, s.line())
}

func waitPrompt(pid uint32) {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := readScreen(pid); err == nil && (strings.HasSuffix(s.line(), "> ") || strings.HasSuffix(s.line(), "$ ")) {
			time.Sleep(300 * time.Millisecond)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("no prompt appeared")
}

func waitSuffix(pid uint32, suffix string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := readScreen(pid); err == nil && strings.HasSuffix(s.line(), suffix) {
			time.Sleep(200 * time.Millisecond)
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// closeShells types exit into every shell and kills whatever survives.
func closeShells(pids []uint32) {
	for _, pid := range pids {
		if ret, _, _ := procAttachConsole.Call(uintptr(pid)); ret != 0 {
			if in, err := windows.CreateFile(windows.StringToUTF16Ptr("CONIN$"),
				windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
				nil, windows.OPEN_EXISTING, 0, 0); err == nil {
				var records []inputRecord
				for _, c := range "exit\r" {
					down := inputRecord{eventType: 1, event: keyEventRecord{keyDown: 1, repeatCount: 1, unicodeChar: uint16(c)}}
					if c == '\r' {
						down.event.virtualKeyCode, down.event.virtualScanCode = 0x0D, 0x1C
					}
					up := down
					up.event.keyDown = 0
					records = append(records, down, up)
				}
				var written uint32
				procWriteConsoleInputW.Call(uintptr(in), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&written)))
				windows.CloseHandle(in)
			}
			procFreeConsole.Call()
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			continue
		}
		wait := max(time.Until(deadline), 0)
		if ev, _ := windows.WaitForSingleObject(h, uint32(wait/time.Millisecond)); ev != windows.WAIT_OBJECT_0 {
			// Exit code 0 lets Windows Terminal close the tab as after a clean exit.
			fmt.Printf("killing %d\n", pid)
			_ = windows.TerminateProcess(h, 0)
		}
		windows.CloseHandle(h)
	}
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
