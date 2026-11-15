package field

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/zakonnic/kbswitcher/internal/platform/uia"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetGUIThreadInfo         = user32.NewProc("GetGUIThreadInfo")
	procRealGetWindowClassW      = user32.NewProc("RealGetWindowClassW")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procIsWindowEnabled          = user32.NewProc("IsWindowEnabled")
	procSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")
)

const (
	gwlStyle        = ^uintptr(15) // GWL_STYLE, -16
	esPassword      = 0x0020
	esReadOnly      = 0x0800
	wmGetTextLength = 0x000E
	emGetSel        = 0x00B0
	emLineIndex     = 0x00BB
	emLineLength    = 0x00C1
	emGetLine       = 0x00C4
	emLineFromChar  = 0x00C9
	smtoAbortIfHung = 0x0002
	// A classic edit control answers at once unless its application hangs.
	messageTimeoutMillis = 200
	// EM_GETSEL packs positions into 16 bits each, and past them wraps
	// around without a sign of it (measured: 70000 reads as 4464).
	maxClassicText = 0xFFFF
)

// Read returns the selection of the focused text field and up to before and
// after characters on either side of it. It fails with ErrNoText unless the
// field is editable, shows its text itself and tells it, and with
// ErrPassword for a field that hides it.
func Read(before, after int) (Around, error) {
	a, err := automated(before, after)
	if !errors.Is(err, ErrNoText) {
		return a, err
	}
	// Classic edit controls, and the controls built on them (Windows Forms,
	// Delphi), tell UI Automation nothing of their text; they tell it to a
	// message.
	if b, classicErr := classic(before, after); !errors.Is(classicErr, ErrNoText) {
		return b, classicErr
	}
	return a, err
}

// automated reads the focused element through UI Automation.
func automated(before, after int) (Around, error) {
	var a Around
	err := uia.With(func(c *uia.Client) error {
		e, err := c.Focused()
		if err != nil {
			return err
		}
		defer e.Release()
		switch {
		case e.Password():
			return ErrPassword
		case !e.Editable(), e.Hidden():
			return ErrNoText
		}
		t, err := e.Text()
		if err != nil {
			return err
		}
		defer t.Release()
		a.Before, a.Selected, a.After, err = t.Around(before, after)
		return err
	})
	if err != nil && !errors.Is(err, ErrPassword) && !errors.Is(err, ErrNoText) {
		err = fmt.Errorf("%w: %w", ErrNoText, err)
	}
	return a, err
}

type guiThreadInfo struct {
	size, flags                                        uint32
	active, focus, capture, menuOwner, moveSize, caret windows.HWND
	caretRect                                          [4]int32
}

// classic reads the line the selection starts on from a classic edit
// control. The control counts characters as UTF-16 units, its messages
// cannot tell positions past 65535, and a selection over several lines is
// left to the clipboard path.
func classic(before, after int) (Around, error) {
	focus := focusedWindow()
	if focus == 0 || windowClass(focus) != "Edit" {
		return Around{}, ErrNoText
	}
	style, _, _ := procGetWindowLongPtrW.Call(uintptr(focus), gwlStyle)
	enabled, _, _ := procIsWindowEnabled.Call(uintptr(focus))
	switch {
	case style&esPassword != 0:
		return Around{}, ErrPassword
	case style&esReadOnly != 0, enabled == 0:
		return Around{}, ErrNoText
	}
	if length, ok := send(focus, wmGetTextLength, 0, 0); !ok || length >= maxClassicText {
		return Around{}, ErrNoText
	}

	sel, ok := send(focus, emGetSel, 0, 0)
	if !ok {
		return Around{}, ErrNoText
	}
	start, end := int(sel&0xFFFF), int(sel>>16&0xFFFF)
	line, _ := send(focus, emLineFromChar, uintptr(start), 0)
	first, _ := send(focus, emLineIndex, line, 0)
	length, _ := send(focus, emLineLength, first, 0)
	from := int(first)
	if start < from || end > from+int(length) {
		return Around{}, ErrNoText
	}
	// EM_GETLINE takes the size of the buffer in its first unit.
	buf := make([]uint16, max(int(length), 1)+1)
	buf[0] = uint16(len(buf)) //nolint:gosec // bounded by the line length
	got, ok := send(focus, emGetLine, line, uintptr(unsafe.Pointer(&buf[0])))
	if !ok || int(got) != int(length) {
		return Around{}, fmt.Errorf("%w: the edit control gave %d of %d characters", ErrNoText, got, length)
	}
	text := buf[:got]
	start, end = start-from, end-from
	return Around{
		Before:   units(text[max(start-before, 0):start]),
		Selected: units(text[start:end]),
		After:    units(text[end:min(end+after, len(text))]),
	}, nil
}

// units decodes UTF-16; a surrogate cut off at the far end of a read turns
// into U+FFFD, never one next to the caret.
func units(text []uint16) string { return string(utf16.Decode(text)) }

func focusedWindow() windows.HWND {
	foreground, _, _ := procGetForegroundWindow.Call()
	thread, _, _ := procGetWindowThreadProcessID.Call(foreground, 0)
	info := guiThreadInfo{}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := procGetGUIThreadInfo.Call(thread, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 0
	}
	return info.focus
}

// windowClass is the class a window was derived from: Edit for the edit
// controls of Windows Forms too.
func windowClass(hwnd windows.HWND) string {
	var buf [64]uint16
	n, _, _ := procRealGetWindowClassW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func send(hwnd windows.HWND, message uint32, wParam, lParam uintptr) (uintptr, bool) {
	var result uintptr
	ok, _, _ := procSendMessageTimeoutW.Call(uintptr(hwnd), uintptr(message), wParam, lParam,
		smtoAbortIfHung, messageTimeoutMillis, uintptr(unsafe.Pointer(&result)))
	return result, ok != 0
}
