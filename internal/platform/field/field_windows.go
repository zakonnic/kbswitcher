package field

import (
	"errors"
	"fmt"
	"strings"
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
	emGetSel        = 0x00B0
	emSetSel        = 0x00B1
	emGetLineCount  = 0x00BA
	emLineIndex     = 0x00BB
	emLineLength    = 0x00C1
	emReplaceSel    = 0x00C2
	emGetLine       = 0x00C4
	emLineFromChar  = 0x00C9
	smtoAbortIfHung = 0x0002
	// An edit control answers at once unless its application hangs; one
	// that lays out a long document again after a replacement takes longer.
	readTimeoutMillis    = 200
	replaceTimeoutMillis = 2000
	// readAttempts is how many times a read is tried while keys keep
	// changing the control.
	readAttempts = 3
)

// A system edit control is the EDIT control of Windows and the controls built
// on it (Windows Forms, Delphi, dialogs), or a RichEdit control (Notepad,
// Windows Forms' RichTextBox). Windows names the class of the focused window
// to anyone who asks, and copies the text of the controls' messages across
// processes, so the text is replaced through them and no character goes
// through the application's handling of typed keys: Notepad's editor turns
// Unicode key presses that queue up while it is busy into copies of the last
// one.
type control int

const (
	other control = iota
	edit
	richEdit
)

// controlOf tells a system edit control by its class, and by its answer to
// a message no other window answers so: it has at least one line.
func controlOf(hwnd windows.HWND) control {
	if hwnd == 0 {
		return other
	}
	class := windowClass(hwnd)
	kind := other
	switch {
	case class == "Edit":
		kind = edit
	case strings.Contains(strings.ToLower(class), "richedit"):
		kind = richEdit
	}
	if kind == other {
		return other
	}
	if lines, ok := send(hwnd, emGetLineCount, 0, 0, readTimeoutMillis); !ok || lresult(lines) < 1 {
		return other
	}
	return kind
}

// Read returns the selection of the focused text field and up to before and
// after characters on either side of it. It fails with ErrNoText unless the
// field is editable, shows its text itself and tells it, and with
// ErrPassword for a field that hides it.
func Read(before, after int) (Around, error) { return read(focusedWindow(), before, after) }

func read(focus windows.HWND, before, after int) (Around, error) {
	switch controlOf(focus) {
	case edit:
		return classic(focus, before, after)
	case richEdit:
		// A rich edit control tells UI Automation the text it shows, and its
		// messages the text it holds, hidden parts included.
		if err := writable(focus); err != nil {
			return Around{}, err
		}
		a, err := automated(before, after, focus)
		if errors.Is(err, ErrNoText) {
			return classic(focus, before, after)
		}
		a.Direct = err == nil
		return a, err
	}
	return automated(before, after, 0)
}

// Replace replaces erased, the text that ends at the caret, with text, in the
// system edit control that has the focus. With right set, the selection is
// dropped at its end first, which is then the caret. Nothing is replaced, and
// ErrChanged reported, unless the control holds erased there; the positions
// and the text checked come from the same messages.
//
// The control takes keys in between the messages: keys the user presses
// meanwhile can still land between the check and the replacement.
func Replace(right bool, erased, text string) error {
	return replace(focusedWindow(), right, erased, text)
}

func replace(focus windows.HWND, right bool, erased, text string) error {
	if controlOf(focus) == other {
		return fmt.Errorf("%w: the focus has left the edit control", ErrChanged)
	}
	start, end, ok := selection(focus)
	switch {
	case !ok:
		return errNoAnswer
	case start != end && !right:
		return fmt.Errorf("%w: something is selected", ErrChanged)
	case start != end:
		if _, ok := send(focus, emSetSel, uintptr(end), uintptr(end), readTimeoutMillis); !ok {
			return errNoAnswer
		}
	}
	from := end - len(utf16.Encode([]rune(erased)))
	if !holds(focus, end, erased) {
		return fmt.Errorf("%w: %q is not before the caret", ErrChanged, erased)
	}
	// The text goes across as a pointer the system copies for this message.
	replacement, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return err
	}
	if _, ok := send(focus, emSetSel, uintptr(from), uintptr(end), readTimeoutMillis); !ok {
		return errNoAnswer
	}
	if _, ok := send(focus, emReplaceSel, 1, uintptr(unsafe.Pointer(replacement)), replaceTimeoutMillis); !ok {
		return errors.New("the edit control did not take the replacement in time")
	}
	if !holds(focus, from+len(utf16.Encode([]rune(text))), text) {
		return errors.New("the edit control does not show the replacement where it was put")
	}
	return nil
}

var errNoAnswer = errors.New("the edit control does not answer")

// holds reports whether the control has the caret at position, nothing
// selected, and text right before it.
func holds(hwnd windows.HWND, position int, text string) bool {
	start, end, ok := selection(hwnd)
	if !ok || start != position || end != position || position < 0 {
		return false
	}
	a, ok := readRows(editRows(hwnd), position, position, len(utf16.Encode([]rune(text))), 0)
	return ok && strings.HasSuffix(a.Before, text)
}

// writable fails for a control that hides its text, or takes none.
func writable(hwnd windows.HWND) error {
	style, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), gwlStyle)
	enabled, _, _ := procIsWindowEnabled.Call(uintptr(hwnd))
	switch {
	case style&esPassword != 0:
		return ErrPassword
	case style&esReadOnly != 0, enabled == 0:
		return ErrNoText
	}
	return nil
}

// automated reads a field through UI Automation: the element of window, or
// with none, the element that has the focus.
func automated(before, after int, window windows.HWND) (Around, error) {
	var a Around
	err := uia.With(func(c *uia.Client) error {
		var e *uia.Element
		var err error
		if window != 0 {
			e, err = c.Window(uintptr(window))
		} else {
			e, err = c.Focused()
		}
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

// classic reads an edit control through its messages. The control takes
// keys in between them, so a read counts only when the selection is the same
// at its end as at its start.
func classic(focus windows.HWND, before, after int) (Around, error) {
	if err := writable(focus); err != nil {
		return Around{}, err
	}
	for range readAttempts {
		start, end, ok := selection(focus)
		if !ok {
			return Around{}, ErrNoText
		}
		a, ok := readRows(editRows(focus), start, end, before, after)
		if s, e, same := selection(focus); ok && same && s == start && e == end {
			return a, nil
		}
	}
	return Around{}, fmt.Errorf("%w: the edit control kept changing while it was read", ErrNoText)
}

// editRows reads the rows of an edit control through its messages.
type editRows windows.HWND

func (h editRows) indexOf(position int) (int, bool) {
	index, ok := send(windows.HWND(h), emLineFromChar, uintptr(position), 0, readTimeoutMillis)
	return lresult(index), ok && lresult(index) >= 0
}

func (h editRows) rowAt(index int) (row, bool) {
	hwnd := windows.HWND(h)
	if index < 0 {
		return row{}, false
	}
	first, ok := send(hwnd, emLineIndex, uintptr(index), 0, readTimeoutMillis)
	if !ok || lresult(first) < 0 {
		return row{}, false
	}
	length, _ := send(hwnd, emLineLength, first, 0, readTimeoutMillis)
	next, _ := send(hwnd, emLineIndex, uintptr(index+1), 0, readTimeoutMillis)
	n := lresult(length)
	// EM_GETLINE takes the size of the buffer in its first unit; a rich edit
	// control may copy the line break as well.
	buf := make([]uint16, max(n, 1)+1)
	buf[0] = uint16(min(len(buf), 0xFFFF)) //nolint:gosec // bounded
	got, ok := send(hwnd, emGetLine, uintptr(index), uintptr(unsafe.Pointer(&buf[0])), readTimeoutMillis)
	if !ok || n < 0 || lresult(got) < n {
		return row{}, false
	}
	return row{
		index:  index,
		start:  lresult(first),
		text:   buf[:n],
		broken: lresult(next) > lresult(first)+n,
	}, true
}

// lresult reads a message's result as the 32-bit int edit controls return,
// whether the system widened it with its sign or without.
func lresult(r uintptr) int { return int(int32(uint32(r))) } //nolint:gosec // positions fit in 31 bits

// selection asks for the selection through pointers, which the system fills
// in across processes with the whole positions; the value EM_GETSEL returns
// packs them into 16 bits each and wraps past 65535.
func selection(hwnd windows.HWND) (start, end int, ok bool) {
	var from, to uint32
	var result uintptr
	sent, _, _ := procSendMessageTimeoutW.Call(uintptr(hwnd), emGetSel,
		uintptr(unsafe.Pointer(&from)), uintptr(unsafe.Pointer(&to)),
		smtoAbortIfHung, readTimeoutMillis, uintptr(unsafe.Pointer(&result)))
	return int(from), int(to), sent != 0 && from <= to
}

type guiThreadInfo struct {
	size, flags                                        uint32
	active, focus, capture, menuOwner, moveSize, caret windows.HWND
	caretRect                                          [4]int32
}

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

func send(hwnd windows.HWND, message uint32, wParam, lParam uintptr, timeoutMillis uintptr) (uintptr, bool) {
	var result uintptr
	ok, _, _ := procSendMessageTimeoutW.Call(uintptr(hwnd), uintptr(message), wParam, lParam,
		smtoAbortIfHung, timeoutMillis, uintptr(unsafe.Pointer(&result)))
	return result, ok != 0
}
