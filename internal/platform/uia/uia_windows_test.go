package uia

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procSendMessageW       = user32.NewProc("SendMessageW")
)

const (
	wsPopup   = 0x80000000
	wsVisible = 0x10000000
	// wsExNoActivate keeps a window from taking the foreground when shown,
	// and wsExToolWindow keeps it off the taskbar.
	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	esMultiline    = 0x0004
	esPassword     = 0x0020
	emSetSel       = 0x00B1
	wmSetText      = 0x000C
	wmQuit         = 0x0012
	// Off the screen, where showing it disturbs no one.
	offScreen = -32000
)

// TestMain ends the process without running its DLLs' detach code: on the CI
// runner, with windows of this process behind UI Automation, a DLL crashed in
// it after every test had passed, and the process hung until killed. The
// result and the coverage profile are written by m.Run.
func TestMain(m *testing.M) {
	code := m.Run()
	_ = windows.TerminateProcess(windows.CurrentProcess(), uint32(code))
}

// offScreenPos passes offScreen as the int a window position is.
var offScreenPos = func() uintptr { n := int64(offScreen); return uintptr(n) }()

// newWindow opens a control of this process on a thread that runs its
// messages, so that UI Automation reaches it as it reaches an application.
func newWindow(t *testing.T, class string, style uint32, width int, text string) uintptr {
	t.Helper()
	ready := make(chan [2]uintptr)
	go func() {
		runtime.LockOSThread()
		if strings.HasPrefix(class, "RICHEDIT") {
			_ = windows.NewLazySystemDLL("msftedit.dll").Load()
		}
		name, _ := windows.UTF16PtrFromString(class)
		// Shown, so that it has bounds, but never activated: the user's window
		// keeps the focus.
		hwnd, _, _ := procCreateWindowExW.Call(wsExNoActivate|wsExToolWindow, uintptr(unsafe.Pointer(name)), 0,
			uintptr(wsPopup|wsVisible|style), offScreenPos, offScreenPos, uintptr(width), 100, 0, 0, 0, 0)
		ready <- [2]uintptr{hwnd, uintptr(windows.GetCurrentThreadId())}
		var m [48]byte
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0); int32(r) <= 0 {
				break
			}
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m[0])))
		}
		_, _, _ = procDestroyWindow.Call(hwnd)
	}()
	opened := <-ready
	if opened[0] == 0 {
		t.Fatalf("no %s window", class)
	}
	t.Cleanup(func() { _, _, _ = procPostThreadMessageW.Call(opened[1], wmQuit, 0, 0) })
	p, _ := windows.UTF16PtrFromString(text)
	_, _, _ = procSendMessageW.Call(opened[0], wmSetText, 0, uintptr(unsafe.Pointer(p)))
	return opened[0]
}

// element runs fn with the element of a window.
func element(t *testing.T, hwnd uintptr, fn func(c *Client, e *Element)) {
	t.Helper()
	err := With(func(c *Client) error {
		e, err := c.Window(hwnd)
		if err != nil {
			return err
		}
		defer e.Release()
		fn(c, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestARichEditControlTellsItsText(t *testing.T) {
	hwnd := newWindow(t, "RICHEDIT50W", esMultiline, 400, "first line\r\nhello ckjdj world")
	_, _, _ = procSendMessageW.Call(hwnd, emSetSel, uintptr(len("first line\rhello ")), uintptr(len("first line\rhello ckjdj")))

	element(t, hwnd, func(c *Client, e *Element) {
		if e.Class() != "RICHEDIT50W" || e.Framework() != "Win32" || e.ControlType() == 0 {
			t.Errorf("class %q, framework %q, control type %d", e.Class(), e.Framework(), e.ControlType())
		}
		if e.ProcessID() != uint32(os.Getpid()) {
			t.Errorf("process %d, want %d", e.ProcessID(), os.Getpid())
		}
		if e.Password() || e.Hidden() || !e.Editable() || e.Bounds().Width() <= 0 || e.Bounds().Height() <= 0 {
			t.Errorf("password %v, hidden %v, editable %v, bounds %+v", e.Password(), e.Hidden(), e.Editable(), e.Bounds())
		}
		if parent := c.Parent(e); parent == nil {
			t.Error("no parent")
		} else {
			parent.Release()
		}

		text, err := e.Text()
		if err != nil {
			t.Fatalf("Text: %v", err)
		}
		defer text.Release()
		before, selected, after, err := text.Around(6, 300)
		if err != nil || before != "hello " || selected != "ckjdj" || !strings.HasPrefix(after, " world") {
			t.Errorf("Around = %q %q %q, %v", before, selected, after, err)
		}
		if rows, err := text.Rows(1); err != nil || !strings.Contains(rows, "first line") || !strings.Contains(rows, "hello ckjdj") {
			t.Errorf("Rows = %q, %v", rows, err)
		}
	})
}

func TestElementsThatTellNoText(t *testing.T) {
	element(t, newWindow(t, "Static", 0, 400, "a label"), func(_ *Client, e *Element) {
		if _, err := e.Text(); err == nil {
			t.Error("a static control has a Text pattern")
		}
	})
	element(t, newWindow(t, "Edit", esPassword, 400, "secret"), func(_ *Client, e *Element) {
		if !e.Password() {
			t.Error("a password field does not say so")
		}
	})
	element(t, newWindow(t, "Edit", 0, 10, ""), func(_ *Client, e *Element) {
		if !e.Hidden() {
			t.Errorf("a field %v pixels wide shows its text", e.Bounds().Width())
		}
	})
	if err := With(func(c *Client) error {
		_, err := c.Window(0)
		return err
	}); err == nil {
		t.Error("no window has an element")
	}
}
