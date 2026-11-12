//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole          = kernel32.NewProc("AttachConsole")
	procFreeConsole            = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow       = kernel32.NewProc("GetConsoleWindow")
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
	procEnumChildWindows         = user32.NewProc("EnumChildWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")
	procSendInput                = user32.NewProc("SendInput")
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

type proc struct {
	parent uint32
	exe    string
}

func processes() map[uint32]proc {
	all := make(map[uint32]proc)
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	check(err, "snapshot")
	defer windows.CloseHandle(handle)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		all[entry.ProcessID] = proc{parent: entry.ParentProcessID, exe: windows.UTF16ToString(entry.ExeFile[:])}
		entry.Size = uint32(unsafe.Sizeof(entry))
	}
	return all
}

func processesNamed(exe string) map[uint32]bool {
	found := make(map[uint32]bool)
	for pid, p := range processes() {
		if strings.EqualFold(p.exe, exe) {
			found[pid] = true
		}
	}
	return found
}

// descendants returns the processes below root whose executable is exe.
func descendants(root uint32, exe string) map[uint32]bool {
	all := processes()
	found := make(map[uint32]bool)
	for pid, p := range all {
		for hop, at := 0, p.parent; hop < 8 && at != 0; hop, at = hop+1, all[at].parent {
			if at == root {
				if strings.EqualFold(p.exe, exe) {
					found[pid] = true
				}
				break
			}
		}
	}
	return found
}

func alive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
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

// topLevel returns the visible top-level windows of a class, optionally owned
// by pid.
func topLevel(class string, pid uint32) map[uintptr]bool {
	found := make(map[uintptr]bool)
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if className(hwnd) == class && (pid == 0 || windowPID(hwnd) == pid) {
			found[hwnd] = true
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return found
}

// childOfClass finds a visible child window; Notepad keeps one hidden editor
// per inactive tab.
func childOfClass(parent uintptr, prefix string) uintptr {
	var found uintptr
	cb := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible != 0 && strings.HasPrefix(className(hwnd), prefix) {
			found = hwnd
			return 0
		}
		return 1
	})
	_, _, _ = procEnumChildWindows.Call(parent, cb, 0)
	return found
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// controlText reads a window's text through WM_GETTEXT, which Windows
// marshals across processes for edit controls.
func controlText(hwnd uintptr) string {
	const wmGetText, wmGetTextLength = 0x000D, 0x000E
	var n uintptr
	_, _, _ = procSendMessageTimeoutW.Call(hwnd, wmGetTextLength, 0, 0, 2, 1000, uintptr(unsafe.Pointer(&n)))
	buf := make([]uint16, n+1)
	var got uintptr
	_, _, _ = procSendMessageTimeoutW.Call(hwnd, wmGetText, n+1, uintptr(unsafe.Pointer(&buf[0])), 2, 1000, uintptr(unsafe.Pointer(&got)))
	return windows.UTF16ToString(buf)
}

func forceForeground(hwnd uintptr) bool {
	for range 20 {
		if fg() == hwnd {
			return true
		}
		_, _, _ = procShowWindow.Call(hwnd, 9) // SW_RESTORE
		sendKey(0x12, 0x38, false)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
		sendKey(0x12, 0x38, true)
		time.Sleep(150 * time.Millisecond)
		if fg() == hwnd {
			return true
		}
		tid, _, _ := procGetWindowThreadProcessID.Call(fg(), 0)
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

// guard is the window every synthetic key press must go to; the stand stops
// rather than type into anything else.
var guard uintptr

func ensureForeground() {
	if guard == 0 || fg() == guard {
		return
	}
	if !forceForeground(guard) {
		fatal("the foreground moved to %#x %q %q; stopping before typing into it", fg(), className(fg()), windowText(fg()))
	}
}

// typeKeys presses the keys that type text on a US keyboard, whatever layout
// is active.
func typeKeys(text string) {
	ensureForeground()
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
			sendKey(0x10, 0x2A, false)
		}
		sendKey(vk, 0, false)
		sendKey(vk, 0, true)
		if shift {
			sendKey(0x10, 0x2A, true)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// chord presses mods, then key, and lets go in reverse order.
func chord(key uint16, mods ...uint16) {
	ensureForeground()
	for _, m := range mods {
		sendKey(m, 0, false)
	}
	sendKey(key, 0, false)
	sendKey(key, 0, true)
	for i := len(mods) - 1; i >= 0; i-- {
		sendKey(mods[i], 0, true)
	}
	time.Sleep(30 * time.Millisecond)
}

const (
	vkBack    = 0x08
	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkEscape  = 0x1B
	vkEnd     = 0x23
	vkDelete  = 0x2E
	vkPause   = 0x13
)

func sendKey(vk, scan uint16, up bool) {
	var flags uint32
	if up {
		flags |= 2
	}
	if vk == vkEnd || vk == vkDelete || vk == 0x24 || vk == 0x25 {
		flags |= 1 // extended
	}
	ev := keyboardInput{typ: 1, vk: vk, scan: scan, flags: flags}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&ev)), unsafe.Sizeof(ev))
	if n != 1 {
		fatal("SendInput: %v", err)
	}
}

// consoleLine reads what a console shows on the caret row up to the caret,
// following a wrapped line back over full rows.
func consoleLine(pid uint32) (string, error) {
	if ret, _, err := procAttachConsole.Call(uintptr(pid)); ret == 0 {
		return "", fmt.Errorf("AttachConsole(%d): %w", pid, err)
	}
	defer procFreeConsole.Call()
	out, err := windows.CreateFile(windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(out)
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(out, &info); err != nil {
		return "", err
	}
	read := func(row, width int16) string {
		buf := make([]uint16, width)
		var got uint32
		coord := uint32(uint16(row)) << 16
		procReadConsoleOutputCharW.Call(uintptr(out), uintptr(unsafe.Pointer(&buf[0])), uintptr(width), uintptr(coord), uintptr(unsafe.Pointer(&got)))
		return windows.UTF16ToString(buf[:got])
	}
	y, top := info.CursorPosition.Y, info.CursorPosition.Y
	for top > 0 && y-top < 4 {
		if row := read(top-1, info.Size.X); row == "" || strings.HasSuffix(row, " ") {
			break
		}
		top--
	}
	var b strings.Builder
	for row := top; row < y; row++ {
		b.WriteString(read(row, info.Size.X))
	}
	if info.CursorPosition.X > 0 {
		b.WriteString(read(y, info.CursorPosition.X))
	}
	// Text after the caret is shown behind a marker, so that a caret that
	// merely moved never passes for an edit.
	row := []rune(read(y, info.Size.X))
	if rest := strings.TrimRight(string(row[min(int(info.CursorPosition.X), len(row)):]), " "); rest != "" {
		b.WriteString("‸" + rest)
	}
	return b.String(), nil
}

// writeConsole puts characters into a console's input buffer; '\r' is Enter.
func writeConsole(pid uint32, text string) {
	if ret, _, _ := procAttachConsole.Call(uintptr(pid)); ret == 0 {
		return
	}
	defer procFreeConsole.Call()
	in, err := windows.CreateFile(windows.StringToUTF16Ptr("CONIN$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(in)
	var records []inputRecord
	for _, c := range text {
		down := inputRecord{eventType: 1, event: keyEventRecord{keyDown: 1, repeatCount: 1, unicodeChar: uint16(c)}}
		switch {
		case c == '\r':
			down.event.virtualKeyCode, down.event.virtualScanCode = vkReturn, 0x1C
		case c == '\x1b':
			down.event.virtualKeyCode, down.event.virtualScanCode = vkEscape, 0x01
		case c < ' ':
			down.event.virtualKeyCode = uint16('A' + c - 1)
			down.event.controlKeyState = 0x0008 // LEFT_CTRL_PRESSED
		}
		up := down
		up.event.keyDown = 0
		records = append(records, down, up)
	}
	var written uint32
	procWriteConsoleInputW.Call(uintptr(in), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&written)))
}

// closeShells types exit into every shell and kills whatever survives.
func closeShells(pids []uint32) {
	for _, pid := range pids {
		writeConsole(pid, "exit\r")
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			continue
		}
		wait := max(time.Until(deadline), 0)
		if ev, _ := windows.WaitForSingleObject(h, uint32(wait/time.Millisecond)); ev != windows.WAIT_OBJECT_0 {
			// Exit code 0 lets a terminal close the tab as after a clean exit.
			_ = windows.TerminateProcess(h, 0)
		}
		windows.CloseHandle(h)
	}
}
