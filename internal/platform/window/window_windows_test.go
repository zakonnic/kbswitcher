package window

import (
	"image/color"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateWindowExW = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
)

// popup opens a window of the test's own thread, off the screen.
func popup(t *testing.T) uintptr {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	class, _ := windows.UTF16PtrFromString("Static")
	offScreen := int64(-32000)
	const noActivate = 0x08000000 | 0x00000080 // WS_EX_NOACTIVATE|WS_EX_TOOLWINDOW: the user keeps the focus
	hwnd, _, _ := procCreateWindowExW.Call(noActivate, uintptr(unsafe.Pointer(class)), 0,
		0x80000000|0x10000000, uintptr(offScreen), uintptr(offScreen), 200, 100, 0, 0, 0, 0) // WS_POPUP|WS_VISIBLE
	if hwnd == 0 {
		t.Fatal("no window")
	}
	t.Cleanup(func() { _, _, _ = procDestroyWindow.Call(hwnd) })
	return hwnd
}

func TestFrameRoundsAndBordersAWindow(t *testing.T) {
	border, err := Frame(popup(t), color.Black)
	if err != nil {
		t.Skipf("this Windows does not round windows: %v", err)
	}
	if border < 1 {
		t.Errorf("the border is %d pixels", border)
	}
	if _, err := Frame(0, color.Black); err == nil {
		t.Error("a window that does not exist was framed")
	}
}

func TestIconSizes(t *testing.T) {
	small, large := IconSizes()
	if small <= 0 || large < small {
		t.Errorf("icon sizes %d and %d", small, large)
	}
}
