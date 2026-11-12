// Package window gives frameless windows what a title bar normally provides:
// rounded corners, a shadow, a border and a way to move them.
package window

import (
	"fmt"
	"image/color"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Frameless reports whether windows on this platform are drawn without the
// system title bar.
const Frameless = true

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procReleaseCapture = user32.NewProc("ReleaseCapture")
	procSendMessage    = user32.NewProc("SendMessageW")
	procPostMessage    = user32.NewProc("PostMessageW")
)

const (
	wmNCLButtonDown = 0x00A1
	wmLButtonUp     = 0x0202
	htCaption       = 2
	dwmwcpRound     = 2
	// dwmwaBorderThickness is DWMWA_VISIBLE_FRAME_BORDER_THICKNESS.
	dwmwaBorderThickness = 37
)

// Frame has the compositor round the corners of the window and draw a border
// of the given color over its edge; a rounded window gets its shadow too. It
// returns how many pixels the border covers. It needs Windows 11; earlier
// versions keep square corners.
func Frame(hwnd uintptr, border color.Color) (int, error) {
	preference := uint32(dwmwcpRound)
	if err := windows.DwmSetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_WINDOW_CORNER_PREFERENCE,
		unsafe.Pointer(&preference), uint32(unsafe.Sizeof(preference))); err != nil {
		return 0, fmt.Errorf("rounding the corners: %w", err)
	}
	r, g, b, _ := border.RGBA()
	colorRef := r>>8 | g>>8<<8 | b>>8<<16
	if err := windows.DwmSetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_BORDER_COLOR,
		unsafe.Pointer(&colorRef), uint32(unsafe.Sizeof(colorRef))); err != nil {
		return 0, fmt.Errorf("coloring the border: %w", err)
	}
	var thickness uint32
	if err := windows.DwmGetWindowAttribute(windows.HWND(hwnd), dwmwaBorderThickness,
		unsafe.Pointer(&thickness), uint32(unsafe.Sizeof(thickness))); err != nil {
		return 1, nil //nolint:nilerr // the border is still drawn; one pixel is the unscaled width
	}
	return int(thickness), nil
}

// Drag lets the system move the window with the pointer, as if its title bar
// had been pressed, and returns once the button is released. It must run on
// the thread that owns the window, while the primary button is down.
func Drag(hwnd uintptr) {
	_, _, _ = procReleaseCapture.Call()
	_, _, _ = procSendMessage.Call(hwnd, wmNCLButtonDown, htCaption, 0)
	// The move loop swallowed the release; the window still waits for it.
	_, _, _ = procPostMessage.Call(hwnd, wmLButtonUp, 0, 0)
}
