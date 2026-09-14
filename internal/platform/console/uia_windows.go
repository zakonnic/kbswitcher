package console

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The pane that has the keyboard focus is found through UI Automation: it
// reports the text around its caret, which the screen buffer of exactly one
// console should show.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procVariantClear     = oleaut32.NewProc("VariantClear")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

var (
	clsidCUIAutomation          = windows.GUID{Data1: 0xFF48DBA4, Data2: 0x60EF, Data3: 0x4201, Data4: [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation            = windows.GUID{Data1: 0x30CBE57D, Data2: 0xD9D0, Data3: 0x452A, Data4: [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
	iidIUIAutomationTextPattern = windows.GUID{Data1: 0x32EBA289, Data2: 0x3583, Data3: 0x42C9, Data4: [8]byte{0x9C, 0x59, 0x3B, 0x6D, 0x9A, 0x1E, 0x9B, 0x6A}}
)

// Slots of the virtual tables used, counted as in UIAutomationClient.h.
const (
	slotQueryInterface          = 0
	slotRelease                 = 2
	slotGetFocusedElement       = 8  // IUIAutomation
	slotGetCurrentPropertyValue = 10 // IUIAutomationElement
	slotGetCurrentPattern       = 16 // IUIAutomationElement
	slotGetSelection            = 5  // IUIAutomationTextPattern
	slotGetElement              = 4  // IUIAutomationTextRangeArray
	slotExpandToEnclosingUnit   = 6  // IUIAutomationTextRange
	slotGetText                 = 12 // IUIAutomationTextRange
	slotMoveEndpointByUnit      = 14 // IUIAutomationTextRange
)

const (
	coinitMultithreaded = 0x0
	clsctxInprocServer  = 0x1
	sFalse              = 0x1
	rpcChangedMode      = 0x80010106
	propertyProcessID   = 30002
	textPatternID       = 10014
	textUnitLine        = 3
	endpointStart       = 0
	vtI4                = 3
	wholeRange          = 0xFFFFFFFF // GetText(-1)
)

// comObject is the this pointer of a COM interface.
type comObject struct{ vtbl *[32]uintptr }

func (o *comObject) call(slot int, args ...uintptr) uint32 {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return uint32(r) //nolint:gosec // an HRESULT is 32 bits wide
}

func (o *comObject) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

// variant mirrors VARIANT on 64-bit Windows.
type variant struct {
	vt  uint16
	_   [6]byte
	val uint64
	_   uint64
}

var errNoFocusedText = errors.New("the focused element reports no text")

// focusedRows returns the row the caret is on in the pane that has the
// keyboard focus, preceded by up to above rows, right-trimmed. The pane must
// belong to process pid.
func focusedRows(pid uint32, above int) ([]string, error) {
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
		return nil, fmt.Errorf("CoInitializeEx: %#x", hr)
	}

	var automation *comObject
	hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if hr != 0 || automation == nil {
		return nil, fmt.Errorf("UI Automation is unavailable: %#x", hr)
	}
	defer automation.release()

	var focused *comObject
	if hr := automation.call(slotGetFocusedElement, uintptr(unsafe.Pointer(&focused))); hr != 0 || focused == nil {
		return nil, fmt.Errorf("no element has the focus: %#x", hr)
	}
	defer focused.release()

	var owner variant
	if hr := focused.call(slotGetCurrentPropertyValue, propertyProcessID, uintptr(unsafe.Pointer(&owner))); hr != 0 {
		return nil, fmt.Errorf("the focused element names no process: %#x", hr)
	}
	defer procVariantClear.Call(uintptr(unsafe.Pointer(&owner)))
	if owner.vt != vtI4 || uint32(owner.val) != pid { //nolint:gosec // VT_I4 holds 32 bits
		return nil, fmt.Errorf("the focused element belongs to process %d, not %d", uint32(owner.val), pid) //nolint:gosec // VT_I4 holds 32 bits
	}

	var pattern *comObject
	if hr := focused.call(slotGetCurrentPattern, textPatternID, uintptr(unsafe.Pointer(&pattern))); hr != 0 || pattern == nil {
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
	count := uintptr(uint32(int32(-above))) //nolint:gosec // a negative count in the 32-bit form the callee reads
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
