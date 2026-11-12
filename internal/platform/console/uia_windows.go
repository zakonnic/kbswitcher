package console

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The pane that has the keyboard focus is found through UI Automation. Windows
// Terminal reports the text around its caret, which the screen buffer of
// exactly one console shows; an xterm.js terminal (VS Code and its kin) keeps
// a hidden text area on the cursor cell of its grid, which gives the cursor
// position and the grid size to match against each console.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

var (
	clsidCUIAutomation          = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation            = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	iidIUIAutomationTextPattern = windows.GUID{Data1: 0x32EBA289, Data2: 0x3583, Data3: 0x42C9, Data4: [8]byte{0x9C, 0x59, 0x3B, 0x6D, 0x9A, 0x1E, 0x9B, 0x6A}}
)

// Slots of the virtual tables used, counted as in UIAutomationClient.h.
const (
	slotQueryInterface            = 0
	slotRelease                   = 2
	slotGetFocusedElement         = 8  // IUIAutomation
	slotRawViewWalker             = 16 // IUIAutomation
	slotGetParentElement          = 3  // IUIAutomationTreeWalker
	slotGetCurrentPattern         = 16 // IUIAutomationElement
	slotCurrentProcessID          = 20 // IUIAutomationElement
	slotCurrentClassName          = 30 // IUIAutomationElement
	slotCurrentBoundingRectangle  = 43 // IUIAutomationElement
	slotGetSelection              = 5  // IUIAutomationTextPattern
	slotGetElement                = 4  // IUIAutomationTextRangeArray
	slotExpandToEnclosingUnit     = 6  // IUIAutomationTextRange
	slotGetText                   = 12 // IUIAutomationTextRange
	slotMoveEndpointByUnit        = 14 // IUIAutomationTextRange
	coinitMultithreaded           = 0x0
	clsctxInprocServer            = 0x1
	sFalse                        = 0x1
	rpcChangedMode                = 0x80010106
	textPatternID                 = 10014
	textUnitLine                  = 3
	endpointStart                 = 0
	wholeRange                    = 0xFFFFFFFF // GetText(-1)
	xtermTextArea                 = "xterm-helper-textarea"
	xtermScreen                   = "xterm-screen"
	chromiumView                  = "View"
	xtermScreenSearchDepth        = 4
	chromiumWarmUp                = 600 * time.Millisecond
	chromiumWarmUpPoll            = 25 * time.Millisecond
	maxTextRows                   = matchRowsAbove
	maxBoundingRectangleDimension = 1 << 20
)

// comObject is the this pointer of a COM interface.
type comObject struct{ vtbl *[64]uintptr }

func (o *comObject) call(slot int, args ...uintptr) uint32 {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uint32(r) //nolint:gosec // an HRESULT is 32 bits wide
}

func (o *comObject) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

// rect is a screen rectangle in physical pixels.
type rect struct{ left, top, right, bottom int32 }

func (r rect) width() float64  { return float64(r.right - r.left) }
func (r rect) height() float64 { return float64(r.bottom - r.top) }

// focus is what UI Automation reports about the element with the keyboard
// focus.
type focus struct {
	class string
	// caret and screen are the hidden text area and the grid of an xterm.js
	// terminal.
	caret, screen rect
	// rows are the caret row and the rows above it, right-trimmed, for an
	// element that exposes its text.
	rows []string
}

func (f focus) xterm() bool { return f.class == xtermTextArea && f.screen.width() > 0 }

var errNoFocusedText = errors.New("the focused element reports no text")

// focused describes the element that has the keyboard focus, which must belong
// to process pid. Chromium builds its accessibility tree only once a client
// asks, so its own views are reported until then and the question is repeated
// for a moment.
func focused(pid uint32, withRows bool) (focus, error) {
	// COM is initialized per thread, so the thread must stay the same until
	// the matching CoUninitialize.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	switch uint32(hr) { //nolint:gosec // an HRESULT is 32 bits wide
	case 0, sFalse:
		defer procCoUninitialize.Call()
	case rpcChangedMode:
		// Initialized as an apartment by someone else; that works too.
	default:
		return focus{}, fmt.Errorf("CoInitializeEx: %#x", hr)
	}

	var automation *comObject
	hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if hr != 0 || automation == nil {
		return focus{}, fmt.Errorf("UI Automation is unavailable: %#x", hr)
	}
	defer automation.release()

	deadline := time.Now().Add(chromiumWarmUp)
	for {
		f, err := describeFocus(automation, pid, withRows)
		if err != nil || f.class != chromiumView || !time.Now().Before(deadline) {
			return f, err
		}
		time.Sleep(chromiumWarmUpPoll)
	}
}

func describeFocus(automation *comObject, pid uint32, withRows bool) (focus, error) {
	var element *comObject
	if hr := automation.call(slotGetFocusedElement, uintptr(unsafe.Pointer(&element))); hr != 0 || element == nil {
		return focus{}, fmt.Errorf("no element has the focus: %#x", hr)
	}
	defer element.release()

	var owner int32
	if hr := element.call(slotCurrentProcessID, uintptr(unsafe.Pointer(&owner))); hr != 0 {
		return focus{}, fmt.Errorf("the focused element names no process: %#x", hr)
	}
	if uint32(owner) != pid { //nolint:gosec // a process id is a DWORD
		return focus{}, fmt.Errorf("the focused element belongs to process %d, not %d", owner, pid)
	}

	f := focus{class: className(element)}
	if f.class == xtermTextArea {
		f.caret = bounds(element)
		f.screen = xtermGrid(automation, element)
		return f, nil
	}
	if withRows {
		rows, err := textRows(element)
		if err != nil {
			return f, err
		}
		f.rows = rows
	}
	return f, nil
}

// xtermGrid finds the grid an xterm.js text area sits in; it is a few levels
// up, past the helpers container.
func xtermGrid(automation, element *comObject) rect {
	var walker *comObject
	if hr := automation.call(slotRawViewWalker, uintptr(unsafe.Pointer(&walker))); hr != 0 || walker == nil {
		return rect{}
	}
	defer walker.release()

	current := element
	for range xtermScreenSearchDepth {
		var parent *comObject
		if hr := walker.call(slotGetParentElement, uintptr(unsafe.Pointer(current)), uintptr(unsafe.Pointer(&parent))); hr != 0 || parent == nil {
			break
		}
		if current != element {
			current.release()
		}
		current = parent
		if className(current) == xtermScreen {
			r := bounds(current)
			current.release()
			return r
		}
	}
	if current != element {
		current.release()
	}
	return rect{}
}

func className(element *comObject) string {
	var bstr *uint16
	if hr := element.call(slotCurrentClassName, uintptr(unsafe.Pointer(&bstr))); hr != 0 || bstr == nil {
		return ""
	}
	defer procSysFreeString.Call(uintptr(unsafe.Pointer(bstr)))
	return windows.UTF16PtrToString(bstr)
}

func bounds(element *comObject) rect {
	var r rect
	if hr := element.call(slotCurrentBoundingRectangle, uintptr(unsafe.Pointer(&r))); hr != 0 {
		return rect{}
	}
	if r.width() <= 0 || r.height() <= 0 || r.width() > maxBoundingRectangleDimension {
		return rect{}
	}
	return r
}

// textRows returns the caret row of an element with a TextPattern, preceded
// by up to maxTextRows rows, right-trimmed.
func textRows(element *comObject) ([]string, error) {
	var pattern *comObject
	if hr := element.call(slotGetCurrentPattern, textPatternID, uintptr(unsafe.Pointer(&pattern))); hr != 0 || pattern == nil {
		return nil, errNoFocusedText
	}
	defer pattern.release()
	var text *comObject
	if hr := pattern.call(slotQueryInterface, uintptr(unsafe.Pointer(&iidIUIAutomationTextPattern)),
		uintptr(unsafe.Pointer(&text))); hr != 0 || text == nil {
		return nil, errNoFocusedText
	}
	defer text.release()

	// Without a selection a terminal answers with an empty range at the caret.
	var ranges *comObject
	if hr := text.call(slotGetSelection, uintptr(unsafe.Pointer(&ranges))); hr != 0 || ranges == nil {
		return nil, errNoFocusedText
	}
	defer ranges.release()
	var caret *comObject
	if hr := ranges.call(slotGetElement, 0, uintptr(unsafe.Pointer(&caret))); hr != 0 || caret == nil {
		return nil, errNoFocusedText
	}
	defer caret.release()

	if hr := caret.call(slotExpandToEnclosingUnit, textUnitLine); hr != 0 {
		return nil, fmt.Errorf("cannot expand the caret to its row: %#x", hr)
	}
	var moved int32
	up := int32(-maxTextRows)
	count := uintptr(uint32(up)) //nolint:gosec // a negative count in the 32-bit form the callee reads
	if hr := caret.call(slotMoveEndpointByUnit, endpointStart, textUnitLine, count,
		uintptr(unsafe.Pointer(&moved))); hr != 0 {
		return nil, fmt.Errorf("cannot reach the rows above the caret: %#x", hr)
	}

	var bstr *uint16
	if hr := caret.call(slotGetText, wholeRange, uintptr(unsafe.Pointer(&bstr))); hr != 0 || bstr == nil {
		return nil, fmt.Errorf("cannot read the focused text: %#x", hr)
	}
	defer procSysFreeString.Call(uintptr(unsafe.Pointer(bstr)))
	return splitRows(windows.UTF16PtrToString(bstr)), nil
}

// splitRows turns the text of a range into right-trimmed rows.
func splitRows(text string) []string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	rows := strings.Split(text, "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	return rows
}
