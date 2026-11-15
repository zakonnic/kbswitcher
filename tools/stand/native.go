package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A window of the stand's own with one classic Win32 text field: an EDIT
// control, or a RichEdit from msftedit.dll. They stand for the dialogs and
// older applications built on the system controls.

var (
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetFocus         = user32.NewProc("SetFocus")
	procMoveWindow       = user32.NewProc("MoveWindow")
	procSendMessageW     = user32.NewProc("SendMessageW")
)

const (
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmActivate       = 0x0006
	wmSetFocus       = 0x0007
	wmSetText        = 0x000C
	wmClose          = 0x0010
	wsOverlappedWnd  = 0x00CF0000
	wsVisible        = 0x10000000
	wsChild          = 0x40000000
	wsVScroll        = 0x00200000
	wsBorder         = 0x00800000
	esMultiline      = 0x0004
	esAutoVScroll    = 0x0040
	esWantReturn     = 0x1000
	wsExTopmost      = 0x00000008
	cwUseDefault     = 0x80000000
	nativeClassName  = "kbswitcherStandNative"
	nativeWindowName = "native stand"
)

type wndClassEx struct {
	size, style                 uint32
	proc                        uintptr
	clsExtra, wndExtra          int32
	instance, icon, cursor, bkg windows.Handle
	menuName, className         *uint16
	iconSm                      windows.Handle
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
	private uint32
}

// launchNative opens the window in a process of its own, as an application
// would have it, and returns once its field has the focus.
func launchNative(kind string) *editorHost {
	class := "Edit"
	if kind == "rich" {
		class = "RICHEDIT50W"
	}
	// CREATE_NO_WINDOW: a GUI application has no console, and kbswitcher
	// would look for shells in one that has.
	pid := createProcess(fmt.Sprintf(`"%s" -host native-serve -editor %s`, os.Args[0], kind), 0x08000000)
	var top, field uintptr
	deadline := time.Now().Add(10 * time.Second)
	for field == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		for h := range topLevel(nativeClassName, pid) {
			top, field = h, childOfClass(h, class)
		}
	}
	if field == 0 {
		fatal("the native window did not open")
	}
	guardFocus = field
	return &editorHost{hwnd: top,
		read:    func() (string, error) { return controlText(field), nil },
		cleanup: func() { _, _, _ = procPostMessage.Call(top, wmClose, 0, 0) },
		// A classic EDIT has no Ctrl+A.
		wipe: func() {
			empty := windows.StringToUTF16Ptr("")
			_, _, _ = procSendMessageTimeoutW.Call(field, wmSetText, 0, uintptr(unsafe.Pointer(empty)), 2, 1000, 0)
		},
	}
}

// serveNative runs the window until it is closed.
func serveNative(kind string) {
	runtime.LockOSThread()
	class := "EDIT"
	if kind == "rich" {
		check(windows.NewLazySystemDLL("msftedit.dll").Load(), "msftedit.dll")
		class = "RICHEDIT50W"
	}
	var field uintptr
	proc := windows.NewCallback(func(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
		switch message {
		case wmSize:
			_, _, _ = procMoveWindow.Call(field, 0, 0, lParam&0xFFFF, lParam>>16&0xFFFF, 1)
		case wmActivate, wmSetFocus:
			_, _, _ = procSetFocus.Call(field)
		case wmClose:
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			_, _, _ = procPostQuitMessage.Call(0)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	})
	name := windows.StringToUTF16Ptr(nativeClassName)
	wc := wndClassEx{proc: proc, className: name}
	wc.size = uint32(unsafe.Sizeof(wc))
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	top, _, _ := procCreateWindowExW.Call(wsExTopmost, uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(nativeWindowName))), wsOverlappedWnd|wsVisible,
		200, 200, 700, 300, 0, 0, 0, 0)
	field, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(class))), 0,
		wsChild|wsVisible|wsVScroll|wsBorder|esMultiline|esAutoVScroll|esWantReturn, 0, 0, 680, 260, top, 0, 0, 0)
	// Lift the default limits, 30000 and 32767 characters, for documents
	// past 65535.
	if kind == "rich" {
		_, _, _ = procSendMessageW.Call(field, 0x0435, 0, 0x7FFFFFFF) // EM_EXLIMITTEXT
	} else {
		_, _, _ = procSendMessageW.Call(field, 0x00C5, 0, 0) // EM_SETLIMITTEXT
	}
	_, _, _ = procSetFocus.Call(field)
	// STAND_SLOW=ms makes every key press take that long, as in a busy
	// application, so that the keys sent after it pile up in the queue.
	slow, _ := time.ParseDuration(os.Getenv("STAND_SLOW"))
	var m msg
	for {
		if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
			return
		}
		if m.message == 0x0100 && slow > 0 { // WM_KEYDOWN
			time.Sleep(slow)
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// launchTitled runs a command that opens a window titled "field:" plus the
// text of its one field, as the scripts beside the stand do for Windows Forms
// and WPF, and reads the field through the title.
func launchTitled(cmdline string) *editorHost {
	before := processes()
	createProcess(cmdline, 0x08000000) // CREATE_NO_WINDOW: no console for the script host
	var hwnd uintptr
	deadline := time.Now().Add(20 * time.Second)
	for hwnd == 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		cb := windows.NewCallback(func(h, _ uintptr) uintptr {
			if _, known := before[windowPID(h)]; !known && strings.HasPrefix(windowText(h), "field:") {
				hwnd = h
				return 0
			}
			return 1
		})
		_, _, _ = procEnumWindows.Call(cb, 0)
	}
	if hwnd == 0 {
		fatal("no window titled field: appeared")
	}
	return &editorHost{hwnd: hwnd,
		read: func() (string, error) {
			text, ok := strings.CutPrefix(windowText(hwnd), "field:")
			if !ok {
				return "", fmt.Errorf("the title is %q", windowText(hwnd))
			}
			return text, nil
		},
		cleanup: func() { _, _, _ = procPostMessage.Call(hwnd, wmClose, 0, 0) },
	}
}
