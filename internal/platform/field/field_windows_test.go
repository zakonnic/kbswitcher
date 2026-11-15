package field

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
)

const (
	wsPopup       = 0x80000000
	esMultiline   = 0x0004
	esAutoHScroll = 0x0080
	wmQuit        = 0x0012
	wmSetText     = 0x000C
)

// newControl opens a window of class with text in it, on a thread of its
// own that runs its messages, as an application's thread would; messages
// sent from the test then cross threads as they cross processes.
func newControl(t *testing.T, class string, style uint32, width int, text string) windows.HWND {
	t.Helper()
	type opened struct {
		hwnd   windows.HWND
		thread uint32
	}
	ready := make(chan opened)
	go func() {
		runtime.LockOSThread()
		if strings.HasPrefix(class, "RICHEDIT") {
			if err := windows.NewLazySystemDLL("msftedit.dll").Load(); err != nil {
				panic(err)
			}
		}
		name, _ := windows.UTF16PtrFromString(class)
		hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(name)), 0,
			uintptr(wsPopup|style), 0, 0, uintptr(width), 200, 0, 0, 0, 0)
		ready <- opened{windows.HWND(hwnd), windows.GetCurrentThreadId()}
		var m [48]byte
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0); int32(r) <= 0 {
				break
			}
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m[0])))
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m[0])))
		}
		_, _, _ = procDestroyWindow.Call(hwnd)
	}()
	o := <-ready
	if o.hwnd == 0 {
		t.Fatalf("no %s window", class)
	}
	t.Cleanup(func() { _, _, _ = procPostThreadMessageW.Call(uintptr(o.thread), wmQuit, 0, 0) })
	setText(t, o.hwnd, text)
	return o.hwnd
}

func setText(t *testing.T, hwnd windows.HWND, text string) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(text)
	if _, ok := send(hwnd, wmSetText, 0, uintptr(unsafe.Pointer(p)), readTimeoutMillis); !ok {
		t.Fatal("WM_SETTEXT")
	}
}

func setSelection(t *testing.T, hwnd windows.HWND, start, end int) {
	t.Helper()
	if _, ok := send(hwnd, emSetSel, uintptr(start), uintptr(end), readTimeoutMillis); !ok {
		t.Fatal("EM_SETSEL")
	}
}

// controls are the system edit controls, each as a multi-line field.
var controls = []string{"Edit", "RICHEDIT50W"}

func TestControlOf(t *testing.T) {
	if kind := controlOf(newControl(t, "Edit", 0, 300, "")); kind != edit {
		t.Errorf("an EDIT control is %v", kind)
	}
	if kind := controlOf(newControl(t, "RICHEDIT50W", esMultiline, 300, "")); kind != richEdit {
		t.Errorf("a RichEdit control is %v", kind)
	}
	if kind := controlOf(newControl(t, "Static", 0, 300, "")); kind != other {
		t.Errorf("a static control is %v", kind)
	}
	if kind := controlOf(0); kind != other {
		t.Errorf("no window is %v", kind)
	}
	// A class named like a rich edit control that answers nothing as one.
	if kind := controlOf(newControl(t, registerClass(t, "NotARichEditControl"), 0, 300, "")); kind != other {
		t.Errorf("a window that only bears the name is %v", kind)
	}
}

// registerClass registers a window class that does what DefWindowProc does.
func registerClass(t *testing.T, name string) string {
	t.Helper()
	type wndClassEx struct {
		size, style                 uint32
		proc                        uintptr
		clsExtra, wndExtra          int32
		instance, icon, cursor, bkg windows.Handle
		menuName, className         *uint16
		iconSm                      windows.Handle
	}
	class, _ := windows.UTF16PtrFromString(name)
	wc := wndClassEx{proc: procDefWindowProcW.Addr(), className: class}
	wc.size = uint32(unsafe.Sizeof(wc))
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	return name
}

func TestReadAnEditControl(t *testing.T) {
	for _, class := range controls {
		t.Run(class, func(t *testing.T) {
			hwnd := newControl(t, class, esMultiline, 400, "first line\r\nhello ckjdj world")
			start := len("first line") + 1 + len("hello ")
			if class == "Edit" {
				start++ // CR LF counts two
			}
			setSelection(t, hwnd, start, start+len("ckjdj"))
			a, err := read(hwnd, 300, 300)
			// UI Automation reads past the line break, which a rich edit control
			// writes as CR; the messages stop at it.
			if err != nil || !strings.HasSuffix(a.Before, "hello ") || a.Selected != "ckjdj" || a.After != " world" || !a.Direct {
				t.Errorf("read = %q %q %q %v, %v", a.Before, a.Selected, a.After, a.Direct, err)
			}
			setSelection(t, hwnd, 0, start)
			if a, err := read(hwnd, 300, 300); err != nil || !strings.HasPrefix(a.Selected, "first line") || !strings.ContainsAny(a.Selected, "\r\n") {
				t.Errorf("a selection over a line break reads %q, %v", a.Selected, err)
			}
		})
	}
}

func TestReadJoinsTheRowsOfAWrappedLine(t *testing.T) {
	line := strings.Repeat("the quick brown fox ", 10) + "ckjdj" // rows of 120 pixels hold a few words
	hwnd := newControl(t, "Edit", esMultiline, 120, line)
	setSelection(t, hwnd, len(line), len(line))
	if a, err := read(hwnd, 300, 300); err != nil || a.Before != line {
		t.Errorf("read = %q, %v; want the whole line", a.Before, err)
	}
	if a, err := read(hwnd, 10, 0); err != nil || a.Before != line[len(line)-10:] {
		t.Errorf("read(10) = %q, %v", a.Before, err)
	}
}

func TestReadPastThe16BitPositions(t *testing.T) {
	text := strings.Repeat("abcdefghi\r\n", 7000) + "hello ckjdj"
	hwnd := newControl(t, "Edit", esMultiline|esAutoHScroll, 400, text)
	setSelection(t, hwnd, len(text), len(text))
	if a, err := read(hwnd, 300, 0); err != nil || !strings.HasSuffix(a.Before, "\nhello ckjdj") && a.Before != "hello ckjdj" {
		t.Errorf("read = %q, %v", a.Before, err)
	}
}

func TestReadRefusesHiddenAndReadOnlyText(t *testing.T) {
	if _, err := read(newControl(t, "Edit", esPassword, 300, "secret"), 300, 300); !errors.Is(err, ErrPassword) {
		t.Errorf("a password field reads with %v", err)
	}
	if _, err := read(newControl(t, "Edit", esReadOnly, 300, "text"), 300, 300); !errors.Is(err, ErrNoText) {
		t.Errorf("a read-only field reads with %v", err)
	}
	if _, err := read(newControl(t, "RICHEDIT50W", esMultiline|esPassword, 300, "secret"), 300, 300); !errors.Is(err, ErrPassword) {
		t.Errorf("a rich password field reads with %v", err)
	}
}

func TestReplace(t *testing.T) {
	for _, class := range controls {
		t.Run(class, func(t *testing.T) {
			hwnd := newControl(t, class, esMultiline, 400, "hello ckjdj world")
			end := len("hello ckjdj")
			setSelection(t, hwnd, end-1, end) // what Shift+Left leaves
			if err := replace(hwnd, true, "ckjdj", "слово"); err != nil {
				t.Fatalf("replace: %v", err)
			}
			if a, err := read(hwnd, 300, 300); err != nil || a.Before != "hello слово" || a.After != " world" {
				t.Errorf("the field reads %+v, %v", a, err)
			}

			if err := replace(hwnd, false, "ckjdj", "x"); !errors.Is(err, ErrChanged) {
				t.Errorf("replacing what is not there: %v", err)
			}
			setSelection(t, hwnd, 0, 5)
			if err := replace(hwnd, false, "", "x"); !errors.Is(err, ErrChanged) {
				t.Errorf("replacing at a selection without dropping it: %v", err)
			}
			setSelection(t, hwnd, len("hello слово"), len("hello слово"))
			if err := replace(hwnd, false, "", "!"); err != nil {
				t.Errorf("inserting: %v", err)
			}
		})
	}
	if err := replace(newControl(t, "Static", 0, 300, ""), false, "", "x"); !errors.Is(err, ErrChanged) {
		t.Errorf("replacing in a static control: %v", err)
	}
}

func TestLresultReadsBothWidenings(t *testing.T) {
	if lresult(^uintptr(0)) != -1 || lresult(0xFFFFFFFF) != -1 || lresult(42) != 42 {
		t.Error("lresult misreads -1 or a position")
	}
}
