package console

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

// Kind names the terminal a window belongs to.
type Kind int

// The window classes worth telling apart; everything else is an ordinary
// application.
const (
	Other     Kind = iota // any other window; it may still run shells of its own
	Conhost               // the classic console window
	Cascadia              // Windows Terminal
	Mintty                // Git Bash, which has no Windows console at all
	JetBrains             // an IDE whose window is an editor first and a terminal second
	Chromium              // an Electron or Chromium window, which may host an xterm.js terminal
)

// window is the foreground window and what could be learned about it cheaply.
type window struct {
	handle windows.HWND
	class  string
	title  string
	pid    uint32
	kind   Kind
}

// foregroundWindow is the window Open works for; tests name one of their own.
var foregroundWindow = foreground

func foreground() (window, error) {
	handle, _, _ := procGetForegroundWindow.Call()
	if handle == 0 {
		return window{}, errNoWindow
	}
	w := window{
		handle: windows.HWND(handle),
		class:  windowClass(handle),
		title:  windowText(handle),
	}
	w.pid = processOf(w.handle)
	w.kind = classify(w.class)
	return w, nil
}

func classify(class string) Kind {
	switch class {
	case "ConsoleWindowClass":
		return Conhost
	case "CASCADIA_HOSTING_WINDOW_CLASS":
		return Cascadia
	case "mintty":
		return Mintty
	// VS Code and every other Electron application; a browser too, which
	// is told apart by owning no console.
	case "Chrome_WidgetWin_1":
		return Chromium
	case "SunAwtFrame":
		return JetBrains
	default:
		// A terminal with a renderer of its own, when it runs shells; an
		// ordinary application when it runs none.
		return Other
	}
}

func windowClass(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func windowText(hwnd uintptr) string {
	var buf [512]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func processOf(hwnd windows.HWND) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	return pid
}

// tree is one snapshot of which process started which, taken once per
// conversion.
type tree struct {
	parent map[uint32]uint32
}

func processTree() (tree, error) {
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return tree{}, fmt.Errorf("cannot list the running processes: %w", err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	t := tree{parent: make(map[uint32]uint32)}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		t.parent[entry.ProcessID] = entry.ParentProcessID
		entry.Size = uint32(unsafe.Sizeof(entry))
	}
	return t, nil
}

// below returns root and everything descended from it, parents before
// children. The depth is bounded because a process id can be reused by a
// process whose parent is gone.
func (t tree) below(root uint32) []uint32 {
	found := map[uint32]bool{root: true}
	order := []uint32{root}
	for range maxProcessDepth {
		grew := false
		for pid, parent := range t.parent {
			if found[parent] && !found[pid] {
				found[pid] = true
				order = append(order, pid)
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return order
}
