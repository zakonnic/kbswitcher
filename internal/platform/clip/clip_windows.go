package clip

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the clipboard is driven through user32 directly, by a hidden
// window of its own: owning what is written lets the paste be offered with
// delayed rendering, so the system says exactly when the application has read
// it, and the privacy formats keep history and cloud sync away from it.

var (
	user32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardOwner          = user32.NewProc("GetClipboardOwner")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procPeekMessageW               = user32.NewProc("PeekMessageW")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDrop       = 15
	cfLocale      = 16
	cfDIBV5       = 17

	gmemMoveable       = 0x0002
	wmRenderFormat     = 0x0305
	wmRenderAllFormats = 0x0306
	wmRun              = 0x8000 + 1  // WM_APP + 1
	hwndMessage        = ^uintptr(2) // HWND_MESSAGE, (HWND)-3

	// poll is how often the clipboard sequence number is looked at while a
	// copy is awaited; reading it opens nothing.
	poll = 2 * time.Millisecond
	// openAttempts and openRetry bound the wait for an application that holds
	// the clipboard open.
	openAttempts = 100
	openRetry    = 2 * time.Millisecond
)

var (
	// Formats that tell clipboard history, cloud sync and clipboard managers
	// to leave the content alone.
	fmtExcludeFromMonitors = registered("ExcludeClipboardContentFromMonitorProcessing")
	fmtHistory             = registered("CanIncludeInClipboardHistory")
	fmtCloud               = registered("CanUploadToCloudClipboard")

	// preserved are the formats given back after a conversion: text, rich
	// text, images and copied files. Handle formats (bitmaps, metafiles)
	// cannot be copied aside and are left out; their DIB form survives.
	preserved = []uint32{
		cfUnicodeText, cfLocale,
		registered("HTML Format"), registered("Rich Text Format"),
		registered("PNG"), cfDIBV5, cfDIB,
		cfHDrop, registered("Shell IDList Array"), registered("FileNameW"), registered("Preferred DropEffect"),
	}
)

func registered(name string) uint32 {
	id, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(name))))
	return uint32(id) //nolint:gosec // clipboard formats are 16-bit values
}

// Snapshot is the clipboard content found before the clipboard was borrowed.
type Snapshot struct {
	items []item
	// unread means the clipboard could not be read, so nothing is put back.
	unread bool
}

type item struct {
	format uint32
	data   []byte
}

// Empty reports whether the snapshot holds nothing.
func (s Snapshot) Empty() bool { return len(s.items) == 0 }

// Clipboard is the system clipboard, used through a window of its own.
type Clipboard struct {
	hwnd  uintptr
	calls chan func()
	// callMu admits one call at a time to the window's thread.
	callMu sync.Mutex

	// Owned by the window's thread.
	seq   uint32 // the sequence number the clipboard had when last seen
	offer *offer // text waiting for an application to ask for it
}

type offer struct {
	text []byte
	read chan struct{}
}

var (
	// clipboards maps a window to its Clipboard for the window procedure.
	clipboards   = map[uintptr]*Clipboard{}
	clipboardsMu sync.Mutex
	windowProc   = windows.NewCallback(dispatch)
	classOnce    sync.Once
	classErr     error
	className    = windows.StringToUTF16Ptr("kbswitcher.clipboard")
)

// Open creates the window the clipboard is driven from.
func Open() (*Clipboard, error) {
	c := &Clipboard{calls: make(chan func(), 1)}
	ready := make(chan error, 1)
	go c.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return c, nil
}

// Close destroys the window; delayed text not read yet is rendered first.
func (c *Clipboard) Close() error {
	c.do(func() {
		_, _, _ = procDestroyWindow.Call(c.hwnd)
		_, _, _ = procPostQuitMessage.Call(0)
	})
	clipboardsMu.Lock()
	delete(clipboards, c.hwnd)
	clipboardsMu.Unlock()
	return nil
}

// Snapshot copies aside what is on the clipboard, in the formats worth
// restoring, up to maxPreservedBytes.
func (c *Clipboard) Snapshot(context.Context) Snapshot {
	var s Snapshot
	c.do(func() {
		if c.open() != nil {
			s.unread = true
			return
		}
		defer closeClipboard()
		total := 0
		image := false
		for _, format := range preserved {
			isImage := format == cfDIB || format == cfDIBV5
			if isImage && image {
				continue // one device-independent bitmap is enough
			}
			data := getData(format)
			if data == nil || total+len(data) > maxPreservedBytes {
				continue
			}
			total += len(data)
			image = image || isImage
			s.items = append(s.items, item{format, data})
		}
		c.seq = sequence()
	})
	return s
}

// Restore puts a snapshot back, unless something other than the conversion
// has written to the clipboard since: what the user copied meanwhile stays.
func (c *Clipboard) Restore(_ context.Context, s Snapshot) error {
	if s.unread {
		return nil
	}
	var err error
	c.do(func() {
		if sequence() != c.seq {
			return
		}
		err = c.put(s.items, false)
	})
	return err
}

// WriteText puts plain text on the clipboard.
func (c *Clipboard) WriteText(_ context.Context, text string) error {
	var err error
	c.do(func() { err = c.put([]item{{cfUnicodeText, utf16Bytes(text)}}, false) })
	return err
}

// Offer puts text on the clipboard for a paste. The channel is closed when
// something reads it: the application pasting, or a clipboard monitor ahead
// of it.
func (c *Clipboard) Offer(_ context.Context, text string) (<-chan struct{}, error) {
	read := make(chan struct{})
	var err error
	c.do(func() {
		if err = c.put(nil, true); err == nil {
			c.offer = &offer{text: utf16Bytes(text), read: read}
		}
	})
	if err != nil {
		return nil, err
	}
	return read, nil
}

// AwaitText waits until the clipboard changes to text other than exclude,
// the marker primed before the copy shortcut, and returns it.
func (c *Clipboard) AwaitText(ctx context.Context, exclude string) (string, error) {
	var since uint32
	c.do(func() { since = c.seq })
	deadline := time.Now().Add(DefaultWait)
	for {
		if now := sequence(); now != since {
			since = now
			var text string
			var err error
			c.do(func() {
				text, err = c.readText()
				c.seq = sequence()
			})
			if err != nil {
				return "", err
			}
			if text != "" && text != exclude {
				return text, nil
			}
		}
		if !time.Now().Before(deadline) {
			return "", ErrNoCopy
		}
		select {
		case <-time.After(poll):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// put replaces the clipboard content with items, marked private, plus the
// text format with delayed rendering when asked for. Window thread only.
func (c *Clipboard) put(items []item, delayedText bool) error {
	if err := c.open(); err != nil {
		return err
	}
	c.offer = nil
	_, _, _ = procEmptyClipboard.Call()
	for _, it := range items {
		setData(it.format, it.data)
	}
	if delayedText {
		_, _, _ = procSetClipboardData.Call(cfUnicodeText, 0)
	}
	markPrivate()
	closeClipboard()
	c.seq = sequence()
	return nil
}

// render answers an application's request for the offered text. The
// clipboard is already open on the requester's behalf. Window thread only.
func (c *Clipboard) render() {
	if c.offer == nil {
		return
	}
	setData(cfUnicodeText, c.offer.text)
	close(c.offer.read)
	c.offer = nil
	c.seq = sequence()
}

func (c *Clipboard) readText() (string, error) {
	if err := c.open(); err != nil {
		return "", err
	}
	defer closeClipboard()
	data := getData(cfUnicodeText)
	if len(data) < 2 {
		return "", nil
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
	return windows.UTF16ToString(units), nil
}

// open opens the clipboard for the window, waiting a little for an
// application that holds it. Messages sent to the window meanwhile are
// answered, so an application waiting for delayed text is never stalled.
func (c *Clipboard) open() error {
	var m msg
	for range openAttempts {
		if ok, _, _ := procOpenClipboard.Call(c.hwnd); ok != 0 {
			return nil
		}
		_, _, _ = procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, wmRun, wmRun, 0)
		time.Sleep(openRetry)
	}
	return errors.New("another application holds the clipboard")
}

func closeClipboard() { _, _, _ = procCloseClipboard.Call() }

func sequence() uint32 {
	n, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(n) //nolint:gosec // a DWORD
}

// markPrivate keeps clipboard history, cloud sync and well-behaved clipboard
// managers away from the content just written.
func markPrivate() {
	zero := []byte{0, 0, 0, 0}
	setData(fmtExcludeFromMonitors, zero)
	setData(fmtHistory, zero)
	setData(fmtCloud, zero)
}

// getData copies a format off the open clipboard, or returns nil.
func getData(format uint32) []byte {
	if ok, _, _ := procIsClipboardFormatAvailable.Call(uintptr(format)); ok == 0 {
		return nil
	}
	h, _, _ := procGetClipboardData.Call(uintptr(format))
	if h == 0 {
		return nil
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 || size > maxPreservedBytes {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	return append([]byte(nil), unsafe.Slice((*byte)(pointer(p)), size)...)
}

// setData hands a copy of data to the open clipboard.
func setData(format uint32, data []byte) {
	if len(data) == 0 {
		return
	}
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return
	}
	copy(unsafe.Slice((*byte)(pointer(p)), len(data)), data)
	_, _, _ = procGlobalUnlock.Call(h)
	if ok, _, _ := procSetClipboardData.Call(uintptr(format), h); ok == 0 {
		_, _, _ = procGlobalFree.Call(h)
	}
}

// pointer turns the address of a locked global memory block, which the
// garbage collector knows nothing about, into a pointer.
func pointer(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

func utf16Bytes(text string) []byte {
	units, err := windows.UTF16FromString(text)
	if err != nil {
		units = []uint16{0} // text with a NUL in it is not text
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&units[0])), len(units)*2)
}

// msg mirrors the Win32 MSG structure.
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	priv    uint32
}

// wndClassEx mirrors WNDCLASSEXW.
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

// run owns the window for its whole life: Windows sends its messages to the
// thread that created it.
func (c *Clipboard) run(ready chan<- error) {
	runtime.LockOSThread() // never unlocked: the thread ends with the window

	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		ready <- err
		return
	}
	classOnce.Do(func() {
		class := wndClassEx{wndProc: windowProc, instance: instance, className: className}
		class.size = uint32(unsafe.Sizeof(class))
		if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			classErr = err
		}
	})
	if classErr != nil {
		ready <- classErr
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0, hwndMessage, 0, uintptr(instance), 0)
	if hwnd == 0 {
		ready <- err
		return
	}
	c.hwnd = hwnd
	clipboardsMu.Lock()
	clipboards[hwnd] = c
	clipboardsMu.Unlock()
	ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { //nolint:gosec // GetMessage answers -1, 0 or a positive int32
			return
		}
		if m.message == wmRun && m.hwnd == hwnd {
			select {
			case call := <-c.calls:
				call()
			default:
			}
			continue
		}
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// do runs call on the window's thread and waits for it.
func (c *Clipboard) do(call func()) {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	done := make(chan struct{})
	c.calls <- func() {
		defer close(done)
		call()
	}
	_, _, _ = procPostMessageW.Call(c.hwnd, wmRun, 0, 0)
	<-done
}

func dispatch(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmRenderFormat:
		if c := lookup(hwnd); c != nil && wParam == cfUnicodeText {
			c.render()
		}
		return 0
	case wmRenderAllFormats:
		if c := lookup(hwnd); c != nil {
			if ok, _, _ := procOpenClipboard.Call(hwnd); ok != 0 {
				if owner, _, _ := procGetClipboardOwner.Call(); owner == hwnd {
					c.render()
				}
				closeClipboard()
			}
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func lookup(hwnd uintptr) *Clipboard {
	clipboardsMu.Lock()
	defer clipboardsMu.Unlock()
	return clipboards[hwnd]
}
