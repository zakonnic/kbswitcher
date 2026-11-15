package uia

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                   = windows.NewLazySystemDLL("ole32.dll")
	oleaut32                = windows.NewLazySystemDLL("oleaut32.dll")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoCreateInstance    = ole32.NewProc("CoCreateInstance")
	procSysFreeString       = oleaut32.NewProc("SysFreeString")
	clsidCUIAutomation      = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation        = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	iidTextPattern          = windows.GUID{Data1: 0x32EBA289, Data2: 0x3583, Data3: 0x42C9, Data4: [8]byte{0x9C, 0x59, 0x3B, 0x6D, 0x9A, 0x1E, 0x9B, 0x6A}}
	iidLegacyIAccessible    = windows.GUID{Data1: 0x828055AD, Data2: 0x355B, Data3: 0x4435, Data4: [8]byte{0x86, 0xD5, 0x3B, 0x51, 0xC1, 0x4A, 0x9B, 0x1B}}
	iidValuePattern         = windows.GUID{Data1: 0xA94CD8B1, Data2: 0x0844, Data3: 0x4CD6, Data4: [8]byte{0x9D, 0x2D, 0x64, 0x05, 0x37, 0xAB, 0x39, 0xE9}}
	coinitMultithreaded     = uintptr(0)
	clsctxInprocServer      = uintptr(1)
	sFalse                  = uintptr(1)
	rpcChangedMode          = uintptr(0x80010106)
	maxBoundingRectangle    = int32(1 << 20)
	stateReadOnly           = uint32(0x40)
	stateProtected          = uint32(0x20000000)
	stateUnavailable        = uint32(0x1)
	patternText             = uintptr(10014)
	patternValue            = uintptr(10002)
	patternLegacyAccessible = uintptr(10018)
)

// Slots of the virtual tables used, counted as in UIAutomationClient.h.
const (
	slotQueryInterface      = 0
	slotRelease             = 2
	slotGetFocusedElement   = 8  // IUIAutomation
	slotRawViewWalker       = 16 // IUIAutomation
	slotGetParentElement    = 3  // IUIAutomationTreeWalker
	slotGetCurrentPattern   = 16 // IUIAutomationElement
	slotCurrentProcessID    = 20 // IUIAutomationElement
	slotCurrentControlType  = 21 // IUIAutomationElement
	slotCurrentClassName    = 30 // IUIAutomationElement
	slotCurrentIsPassword   = 35 // IUIAutomationElement
	slotCurrentFrameworkID  = 40 // IUIAutomationElement
	slotCurrentBounds       = 43 // IUIAutomationElement
	slotGetSelection        = 5  // IUIAutomationTextPattern
	slotDocumentRange       = 7  // IUIAutomationTextPattern
	slotArrayLength         = 3  // IUIAutomationTextRangeArray
	slotArrayElement        = 4  // IUIAutomationTextRangeArray
	slotClone               = 3  // IUIAutomationTextRange
	slotCompareEndpoints    = 5  // IUIAutomationTextRange
	slotExpandToEnclosing   = 6  // IUIAutomationTextRange
	slotGetText             = 12 // IUIAutomationTextRange
	slotMoveEndpointByUnit  = 14 // IUIAutomationTextRange
	slotMoveEndpointByRange = 15 // IUIAutomationTextRange
	slotValueIsReadOnly     = 5  // IUIAutomationValuePattern: get_CurrentIsReadOnly
	slotLegacyCurrentState  = 11 // IUIAutomationLegacyIAccessiblePattern: get_CurrentState
	unitCharacter           = 0
	unitLine                = 3
	endpointStart           = 0
	endpointEnd             = 1
	maxSelectedText         = 1 << 16
	chromiumTreeWarmUpPolls = 24
	chromiumView            = "View"
	chromiumWarmUpPoll      = 25 * time.Millisecond
	// A field that shows text is wider than a character cell, even a large
	// one on a high-density screen: measured, VS Code's editor keeps a 0x0
	// element, its terminal and Tabby one cell of 7-8x18 pixels.
	minFieldWidth = 48
)

// object is the this pointer of a COM interface.
type object struct{ vtbl *[64]uintptr }

// cell allocates a value for a COM method to write its result into. The
// method receives a bare address, and a goroutine stack can move while the
// call is on its way, so the value is pinned, which also keeps it off the
// stack.
func cell[T any](pin *runtime.Pinner) *T {
	p := new(T)
	pin.Pin(p)
	return p
}

func (o *object) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uintptr(uint32(r)) //nolint:gosec // an HRESULT is 32 bits wide
}

func (o *object) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

// get calls a method whose last parameter receives an interface.
func (o *object) get(slot int, args ...uintptr) (*object, error) {
	var pin runtime.Pinner
	defer pin.Unpin()
	out := cell[*object](&pin)
	if hr := o.call(slot, append(args, uintptr(unsafe.Pointer(out)))...); hr != 0 || *out == nil {
		return nil, fmt.Errorf("COM call %d: %#x", slot, hr)
	}
	return *out, nil
}

// as asks for another interface of the same object.
func (o *object) as(iid *windows.GUID) (*object, error) {
	return o.get(slotQueryInterface, uintptr(unsafe.Pointer(iid)))
}

func (o *object) str(slot int, args ...uintptr) string {
	var pin runtime.Pinner
	defer pin.Unpin()
	bstr := cell[*uint16](&pin)
	if hr := o.call(slot, append(args, uintptr(unsafe.Pointer(bstr)))...); hr != 0 || *bstr == nil {
		return ""
	}
	defer procSysFreeString.Call(uintptr(unsafe.Pointer(*bstr)))
	return windows.UTF16PtrToString(*bstr)
}

func (o *object) integer(slot int) (int32, bool) {
	var pin runtime.Pinner
	defer pin.Unpin()
	v := cell[int32](&pin)
	ok := o.call(slot, uintptr(unsafe.Pointer(v))) == 0
	return *v, ok
}

// Client is a UI Automation client bound to the thread it was made on.
type Client struct{ automation *object }

// With runs fn with a client, on a thread of its own for the duration: COM is
// initialized per thread.
func With(fn func(*Client) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	switch hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); hr {
	case 0, sFalse:
		defer procCoUninitialize.Call()
	case rpcChangedMode:
		// Initialized as an apartment by someone else; that works too.
	default:
		return fmt.Errorf("CoInitializeEx: %#x", hr)
	}

	var automation *object
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if hr != 0 || automation == nil {
		return fmt.Errorf("UI Automation is unavailable: %#x", hr)
	}
	defer automation.release()
	return fn(&Client{automation: automation})
}

// Focused returns the element that has the keyboard focus. Chromium builds
// its accessibility tree only once a client asks, and reports its own views
// until then, so the question is repeated for a moment.
func (c *Client) Focused() (*Element, error) {
	for i := 0; ; i++ {
		obj, err := c.automation.get(slotGetFocusedElement)
		if err != nil {
			return nil, fmt.Errorf("no element has the focus: %w", err)
		}
		e := &Element{obj: obj}
		if i >= chromiumTreeWarmUpPolls || e.Class() != chromiumView {
			return e, nil
		}
		e.Release()
		time.Sleep(chromiumWarmUpPoll)
	}
}

// Parent returns the element above e in the raw view, or nil at the top.
func (c *Client) Parent(e *Element) *Element {
	walker, err := c.automation.get(slotRawViewWalker)
	if err != nil {
		return nil
	}
	defer walker.release()
	parent, err := walker.get(slotGetParentElement, uintptr(unsafe.Pointer(e.obj)))
	if err != nil {
		return nil
	}
	return &Element{obj: parent}
}

// Element is one node of the automation tree.
type Element struct{ obj *object }

// Release gives the element back; it is not used afterwards.
func (e *Element) Release() { e.obj.release() }

// ProcessID is the process the element belongs to.
func (e *Element) ProcessID() uint32 {
	pid, _ := e.obj.integer(slotCurrentProcessID)
	return uint32(pid) //nolint:gosec // a process id is a DWORD
}

// Class is the element's class name, which for web content is its CSS class.
func (e *Element) Class() string { return e.obj.str(slotCurrentClassName) }

// Framework names what draws the element: Win32, WPF, XAML, Chrome...
func (e *Element) Framework() string { return e.obj.str(slotCurrentFrameworkID) }

// ControlType is the element's UIA control type id.
func (e *Element) ControlType() int32 {
	t, _ := e.obj.integer(slotCurrentControlType)
	return t
}

// Password reports a field whose text is hidden.
func (e *Element) Password() bool {
	v, ok := e.obj.integer(slotCurrentIsPassword)
	return ok && v != 0
}

// Bounds is the element's rectangle on the screen, in physical pixels; empty
// when it has none.
func (e *Element) Bounds() Rect {
	var pin runtime.Pinner
	defer pin.Unpin()
	r := cell[Rect](&pin)
	if hr := e.obj.call(slotCurrentBounds, uintptr(unsafe.Pointer(r))); hr != 0 {
		return Rect{}
	}
	if r.Width() <= 0 || r.Height() <= 0 || r.Right-r.Left > maxBoundingRectangle {
		return Rect{}
	}
	return *r
}

// Hidden reports an element too narrow to show the text it holds: the text
// area an editor or a terminal keeps at its caret while it draws the text
// itself (VS Code, xterm.js). What such an element says its text is, is not
// what the user sees.
func (e *Element) Hidden() bool { return e.Bounds().Width() < minFieldWidth }

// Editable reports whether the element takes typed text: neither read-only
// nor disabled as Microsoft Active Accessibility describes it, nor read-only
// by its Value pattern. An element that answers neither question is not
// taken to be editable.
func (e *Element) Editable() bool {
	known := false
	if p, err := e.pattern(patternLegacyAccessible, &iidLegacyIAccessible); err == nil {
		state, ok := p.integer(slotLegacyCurrentState)
		p.release()
		if ok {
			if uint32(state)&(stateReadOnly|stateUnavailable|stateProtected) != 0 { //nolint:gosec // a state is a DWORD
				return false
			}
			known = true
		}
	}
	if p, err := e.pattern(patternValue, &iidValuePattern); err == nil {
		readOnly, ok := p.integer(slotValueIsReadOnly)
		p.release()
		if ok {
			if readOnly != 0 {
				return false
			}
			known = true
		}
	}
	return known
}

func (e *Element) pattern(id uintptr, iid *windows.GUID) (*object, error) {
	unknown, err := e.obj.get(slotGetCurrentPattern, id)
	if err != nil {
		return nil, err
	}
	defer unknown.release()
	return unknown.as(iid)
}

// Rect is a screen rectangle in physical pixels.
type Rect struct{ Left, Top, Right, Bottom int32 }

// Width is the rectangle's width.
func (r Rect) Width() float64 { return float64(r.Right - r.Left) }

// Height is the rectangle's height.
func (r Rect) Height() float64 { return float64(r.Bottom - r.Top) }

// Text is the element's Text pattern.
type Text struct{ obj *object }

// Text returns the element's Text pattern.
func (e *Element) Text() (*Text, error) {
	p, err := e.pattern(patternText, &iidTextPattern)
	if err != nil {
		return nil, errNoText
	}
	return &Text{obj: p}, nil
}

// Release gives the pattern back.
func (t *Text) Release() { t.obj.release() }

// selection is the first selected range, which is empty at the caret when
// nothing is selected.
func (t *Text) selection() (*object, error) {
	ranges, err := t.obj.get(slotGetSelection)
	if err != nil {
		return nil, errNoText
	}
	defer ranges.release()
	if n, ok := ranges.integer(slotArrayLength); !ok || n < 1 {
		return nil, errNoText
	}
	r, err := ranges.get(slotArrayElement, 0)
	if err != nil {
		return nil, errNoText
	}
	return r, nil
}

// Around reads the selection and up to before characters ahead of it and
// after characters behind it, within the element.
func (t *Text) Around(before, after int) (ahead, selected, behind string, err error) {
	sel, err := t.selection()
	if err != nil {
		return "", "", "", err
	}
	defer sel.release()
	doc, err := t.obj.get(slotDocumentRange)
	if err != nil {
		return "", "", "", errNoText
	}
	defer doc.release()

	selected = rangeText(sel, maxSelectedText)
	if ahead, err = stretch(sel, doc, endpointStart, -before); err != nil {
		return "", "", "", err
	}
	if behind, err = stretch(sel, doc, endpointEnd, after); err != nil {
		return "", "", "", err
	}
	return ahead, selected, behind, nil
}

// Rows returns the line the selection starts on and up to above lines before
// it, as one text.
func (t *Text) Rows(above int) (string, error) {
	sel, err := t.selection()
	if err != nil {
		return "", err
	}
	defer sel.release()
	if hr := sel.call(slotExpandToEnclosing, unitLine); hr != 0 {
		return "", fmt.Errorf("cannot expand the caret to its row: %#x", hr)
	}
	var pin runtime.Pinner
	defer pin.Unpin()
	moved := cell[int32](&pin)
	if hr := sel.call(slotMoveEndpointByUnit, endpointStart, unitLine, count(-above), uintptr(unsafe.Pointer(moved))); hr != 0 {
		return "", fmt.Errorf("cannot reach the rows above the caret: %#x", hr)
	}
	return rangeText(sel, -1), nil
}

// stretch reads the text between one end of sel and a point n characters
// away from it, never past the same end of doc.
func stretch(sel, doc *object, end uintptr, n int) (string, error) {
	r, err := sel.get(slotClone)
	if err != nil {
		return "", errNoText
	}
	defer r.release()
	// Collapse onto the end, then move the other endpoint away from it.
	other := endpointEnd - end
	if hr := r.call(slotMoveEndpointByRange, other, uintptr(unsafe.Pointer(sel)), end); hr != 0 {
		return "", fmt.Errorf("cannot collapse a range: %#x", hr)
	}
	var pin runtime.Pinner
	defer pin.Unpin()
	moved, order := cell[int32](&pin), cell[int32](&pin)
	if hr := r.call(slotMoveEndpointByUnit, end, unitCharacter, count(n), uintptr(unsafe.Pointer(moved))); hr != 0 {
		return "", fmt.Errorf("cannot move a range: %#x", hr)
	}
	// Some providers move past the element, into the page around it.
	if hr := r.call(slotCompareEndpoints, end, uintptr(unsafe.Pointer(doc)), end, uintptr(unsafe.Pointer(order))); hr == 0 &&
		(end == endpointStart && *order < 0 || end == endpointEnd && *order > 0) {
		r.call(slotMoveEndpointByRange, end, uintptr(unsafe.Pointer(doc)), end)
	}
	return rangeText(r, -1), nil
}

func rangeText(r *object, limit int) string {
	return r.str(slotGetText, count(limit))
}

// count passes a signed int in the 32-bit form the callee reads.
func count(n int) uintptr { return uintptr(uint32(int32(n))) } //nolint:gosec // bounded by the callers
