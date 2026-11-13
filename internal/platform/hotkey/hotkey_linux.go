package hotkey

/*
#cgo pkg-config: x11
#include "hotkey_linux.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// X11 modifier masks.
const (
	maskShift   = 1 << 0
	maskLock    = 1 << 1 // Caps Lock
	maskControl = 1 << 2
	maskAlt     = 1 << 3 // Mod1
	maskNumLock = 1 << 4 // Mod2
	maskSuper   = 1 << 6 // Mod4
)

const maxPendingRegistrations = 8

// DefaultKey is the key bound when no configuration exists yet.
func DefaultKey() string { return "Pause" }

// binding identifies one grabbed combination.
type binding struct {
	keycode   uint32
	modifiers uint32
}

// request is a message to the goroutine that owns the X connection.
type request struct {
	binding binding
	// keysym names the key when the binding has no keycode.
	keysym  uint32
	handler func()
	stop    bool
	result  chan error
}

// Manager owns the hotkey grabs of the application.
//
// Xlib connections must not be used from two threads at once, so a single
// goroutine owns the connection: it waits for key presses and performs the
// grabs that Register sends through a channel.
type Manager struct {
	display  *C.Display
	wakeRead *os.File
	wakeSend *os.File

	requests chan request
	done     chan struct{}

	mu       sync.Mutex
	closed   bool
	handlers map[binding]func()
}

// Open connects to the X server and starts waiting for hotkeys.
func Open() (*Manager, error) {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return nil, errors.New("cannot open the X display: kbswitcher needs an X11 session " +
			"(a Wayland session must be running XWayland)")
	}

	// The pipe is how other goroutines interrupt the wait for X events.
	read, write, err := os.Pipe()
	if err != nil {
		C.XCloseDisplay(display)
		return nil, fmt.Errorf("hotkey: %w", err)
	}

	m := &Manager{
		display:  display,
		wakeRead: read,
		wakeSend: write,
		requests: make(chan request, maxPendingRegistrations),
		done:     make(chan struct{}),
		handlers: make(map[binding]func()),
	}
	go m.loop()
	return m, nil
}

// Register binds a combination. The handler runs on a goroutine of its own.
func (m *Manager) Register(combo Combo, handler func()) error {
	key, ok := keys[combo.Key]
	if !ok {
		return fmt.Errorf("hotkey: key %q is not available on this system", combo.Key)
	}

	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("hotkey: manager is closed")
	}

	// The key is grabbed where it sits, as the settings window recorded it;
	// a keysym is turned into a keycode on the goroutine that owns the
	// connection.
	return m.ask(request{
		binding: binding{keycode: uint32(key.scan), modifiers: nativeModifiers(combo.Mods)}, //nolint:gosec // an X keycode is a byte
		keysym:  key.code,
		handler: handler,
	})
}

// Close releases every grab and closes the connection.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	err := m.ask(request{stop: true})
	<-m.done
	m.wakeRead.Close()
	m.wakeSend.Close()
	return err
}

// ask hands a request to the goroutine that owns the connection and waits for
// its answer.
func (m *Manager) ask(req request) error {
	req.result = make(chan error, 1)
	select {
	case m.requests <- req:
	default:
		return errors.New("hotkey: too many requests in flight")
	}

	if _, err := m.wakeSend.Write([]byte{1}); err != nil {
		return fmt.Errorf("hotkey: cannot wake the event loop: %w", err)
	}
	select {
	case err := <-req.result:
		return err
	case <-m.done:
		return errors.New("hotkey: manager is closed")
	}
}

// loop owns the X connection: it waits for key presses and carries out the
// requests that arrive through the channel.
func (m *Manager) loop() {
	defer close(m.done)
	defer C.XCloseDisplay(m.display)

	for {
		var keycode, state C.uint
		switch C.kbs_next_hotkey(m.display, C.int(m.wakeRead.Fd()), &keycode, &state) {
		case 1:
			m.dispatch(uint32(keycode), uint32(state))
		case 0:
			m.drainWake()
			if stop := m.serve(); stop {
				m.ungrabAll()
				return
			}
		default:
			// The connection is gone; the session is ending.
			return
		}
	}
}

// dispatch runs the handler of a pressed combination. Caps Lock, Num Lock and
// the layout group the state also carries are not part of it.
func (m *Manager) dispatch(keycode, state uint32) {
	pressed := binding{keycode: keycode, modifiers: state & (maskShift | maskControl | maskAlt | maskSuper)}

	m.mu.Lock()
	handler := m.handlers[pressed]
	m.mu.Unlock()

	if handler != nil {
		go handler()
	}
}

// serve carries out the pending requests and reports whether the manager was
// asked to stop.
func (m *Manager) serve() bool {
	for {
		select {
		case req := <-m.requests:
			if req.stop {
				req.result <- nil
				return true
			}
			req.result <- m.grab(req)
		default:
			return false
		}
	}
}

// grab takes one combination on the goroutine that owns the connection.
func (m *Manager) grab(req request) error {
	keycode := req.binding.keycode
	if keycode == 0 {
		keycode = uint32(C.XKeysymToKeycode(m.display, C.KeySym(req.keysym)))
	}
	if keycode == 0 {
		return errors.New("hotkey: this keyboard has no such key")
	}
	grabbed := binding{keycode: keycode, modifiers: req.binding.modifiers}

	if C.kbs_grab_key(m.display, C.uint(grabbed.keycode), C.uint(grabbed.modifiers)) != 0 {
		return errors.New("hotkey: the combination is already taken by another application")
	}

	m.mu.Lock()
	m.handlers[grabbed] = req.handler
	m.mu.Unlock()
	return nil
}

// ungrabAll releases every grab, just before the connection is closed.
func (m *Manager) ungrabAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for grabbed := range m.handlers {
		C.kbs_ungrab_key(m.display, C.uint(grabbed.keycode), C.uint(grabbed.modifiers))
		delete(m.handlers, grabbed)
	}
}

// drainWake empties the pipe that woke the loop up. select reported the pipe
// readable, so this does not block.
func (m *Manager) drainWake() {
	var buffer [maxPendingRegistrations]byte
	m.wakeRead.Read(buffer[:])
}

func nativeModifiers(mods Modifier) uint32 {
	var mask uint32
	if mods&ModCtrl != 0 {
		mask |= maskControl
	}
	if mods&ModAlt != 0 {
		mask |= maskAlt
	}
	if mods&ModShift != 0 {
		mask |= maskShift
	}
	if mods&ModSuper != 0 {
		mask |= maskSuper
	}
	return mask
}
