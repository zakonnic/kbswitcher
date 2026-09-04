package hotkey

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey    = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey  = user32.NewProc("UnregisterHotKey")
	procGetMessage        = user32.NewProc("GetMessageW")
	procPeekMessage       = user32.NewProc("PeekMessageW")
	procPostThreadMessage = user32.NewProc("PostThreadMessageW")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmHotkey = 0x0312
	// Private messages of the hotkey thread, in the WM_APP range.
	wmRegister = 0x8000 + 1
	wmQuit     = 0x8000 + 2

	maxPendingRegistrations = 8
)

// keyCodes maps the portable key names to virtual key codes. Only keys that
// exist on a PC keyboard are listed.
var keyCodes = map[string]uint32{
	"Pause":      0x13,
	"ScrollLock": 0x91,
	"Insert":     0x2D,
	"Home":       0x24,
	"End":        0x23,
	"F1":         0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73,
	"F5": 0x74, "F6": 0x75, "F7": 0x76, "F8": 0x77,
	"F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,
}

func keyCode(name string) (uint32, bool) {
	code, ok := keyCodes[name]
	return code, ok
}

// DefaultKey is the key bound when no configuration exists yet.
func DefaultKey() string { return "Pause" }

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

// registration is a request to bind one combination, served by the hotkey
// thread.
type registration struct {
	combo   Combo
	code    uint32
	handler func()
	result  chan error
}

// Manager owns the hotkey registrations of the process.
//
// Windows delivers WM_HOTKEY to the thread that called RegisterHotKey, so the
// manager keeps a thread of its own with a message loop that does nothing
// else.
type Manager struct {
	threadID uint32
	requests chan registration
	loopDone chan struct{}

	mu       sync.Mutex
	closed   bool
	nextID   uintptr
	handlers map[uintptr]func()
}

// Open starts the hotkey thread.
func Open() (*Manager, error) {
	m := &Manager{
		requests: make(chan registration, maxPendingRegistrations),
		loopDone: make(chan struct{}),
		nextID:   1,
		handlers: make(map[uintptr]func()),
	}

	ready := make(chan uint32, 1)
	go m.loop(ready)
	m.threadID = <-ready
	if m.threadID == 0 {
		return nil, errors.New("hotkey: could not start the message thread")
	}
	return m, nil
}

// Register binds a combination. The handler runs on a goroutine of its own.
func (m *Manager) Register(combo Combo, handler func()) error {
	code, ok := keyCode(combo.Key)
	if !ok {
		return fmt.Errorf("hotkey: key %q is not available on this system", combo.Key)
	}

	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("hotkey: manager is closed")
	}

	request := registration{combo: combo, code: code, handler: handler, result: make(chan error, 1)}
	select {
	case m.requests <- request:
	default:
		return errors.New("hotkey: too many registrations in flight")
	}
	if err := m.post(wmRegister); err != nil {
		return err
	}
	select {
	case err := <-request.result:
		return err
	case <-m.loopDone:
		return errors.New("hotkey: manager closed while registering")
	}
}

// Close unregisters every hotkey and stops the thread.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	if err := m.post(wmQuit); err != nil {
		return err
	}
	<-m.loopDone
	return nil
}

func (m *Manager) post(message uint32) error {
	ret, _, err := procPostThreadMessage.Call(uintptr(m.threadID), uintptr(message), 0, 0)
	if ret == 0 {
		return fmt.Errorf("hotkey: PostThreadMessage: %w", err)
	}
	return nil
}

// loop runs the message pump on a thread of its own. RegisterHotKey binds to
// the calling thread, so the goroutine stays on it for its whole life.
func (m *Manager) loop(ready chan<- uint32) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.loopDone)

	// A thread gets a message queue only once it has called a message
	// function; PostThreadMessage fails until then.
	var message msg
	_, _, _ = procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, wmRegister, wmRegister, 0)
	ready <- windows.GetCurrentThreadId()

	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) == -1 { //nolint:gosec // GetMessage answers -1, 0 or a positive int32
			m.unregisterAll()
			return
		}
		switch message.message {
		case wmHotkey:
			m.mu.Lock()
			handler := m.handlers[message.wParam]
			m.mu.Unlock()
			if handler != nil {
				go handler()
			}
		case wmRegister:
			m.serveRegistrations()
		case wmQuit:
			m.unregisterAll()
			return
		}
	}
}

func (m *Manager) serveRegistrations() {
	for {
		select {
		case request := <-m.requests:
			request.result <- m.bind(request)
		default:
			return
		}
	}
}

// bind calls RegisterHotKey for one request on the hotkey thread.
func (m *Manager) bind(request registration) error {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.mu.Unlock()

	flags := uintptr(modNoRepeat) | nativeModifiers(request.combo.Mods)
	ret, _, err := procRegisterHotKey.Call(0, id, flags, uintptr(request.code))
	if ret == 0 {
		return fmt.Errorf("hotkey: %s is already taken by another application: %w",
			request.combo, err)
	}

	m.mu.Lock()
	m.handlers[id] = request.handler
	m.mu.Unlock()
	return nil
}

// unregisterAll releases every binding. Only the hotkey thread may do so.
func (m *Manager) unregisterAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.handlers {
		_, _, _ = procUnregisterHotKey.Call(0, id)
		delete(m.handlers, id)
	}
}

func nativeModifiers(mods Modifier) uintptr {
	var flags uintptr
	if mods&ModCtrl != 0 {
		flags |= modControl
	}
	if mods&ModAlt != 0 {
		flags |= modAlt
	}
	if mods&ModShift != 0 {
		flags |= modShift
	}
	if mods&ModSuper != 0 {
		flags |= modWin
	}
	return flags
}
