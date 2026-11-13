package hotkey

/*
#cgo LDFLAGS: -framework Carbon
#include "hotkey_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
)

// Carbon modifier masks, as RegisterEventHotKey expects them.
const (
	cmdKey     = 0x0100
	shiftKey   = 0x0200
	optionKey  = 0x0800
	controlKey = 0x1000
)

// DefaultKey is the key bound when no configuration exists yet: Mac keyboards
// have no Break key, and F12 is the closest thing that is free by default.
func DefaultKey() string { return "F12" }

// macOS calls back with an identifier, so the handlers of the whole process
// live here, indexed by the identifier handed to C.
var (
	bindingsMu sync.Mutex
	bindings   [C.KBS_MAX_HOTKEYS]func()
)

//export kbsHotkeyPressed
func kbsHotkeyPressed(id C.int) {
	bindingsMu.Lock()
	var handler func()
	if id >= 0 && int(id) < len(bindings) {
		handler = bindings[id]
	}
	bindingsMu.Unlock()

	// This returns into the event handler of the main thread, which must not
	// wait for a conversion.
	if handler != nil {
		go handler()
	}
}

// Manager owns the hotkey registrations of the application.
type Manager struct {
	mu     sync.Mutex
	owned  []int
	closed bool
}

// Open prepares the manager. The bindings themselves are made by Register.
func Open() (*Manager, error) { return &Manager{}, nil }

// Register binds a combination. The handler runs on a goroutine of its own.
func (m *Manager) Register(combo Combo, handler func()) error {
	key, ok := keys[combo.Key]
	if !ok {
		return fmt.Errorf("hotkey: key %q is not available on this system", combo.Key)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("hotkey: manager is closed")
	}

	id, err := reserve(handler)
	if err != nil {
		return err
	}
	if status := C.kbs_register_hotkey(C.uint(key.code), C.uint(nativeModifiers(combo.Mods)), C.int(id)); status != 0 {
		release(id)
		return fmt.Errorf("hotkey: %s could not be bound, another application may already use it (code %d)",
			combo, int(status))
	}

	m.owned = append(m.owned, id)
	return nil
}

// Close releases every binding this manager made.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true

	for _, id := range m.owned {
		C.kbs_unregister_hotkey(C.int(id))
		release(id)
	}
	m.owned = nil
	return nil
}

// reserve takes the first free identifier for a handler.
func reserve(handler func()) (int, error) {
	bindingsMu.Lock()
	defer bindingsMu.Unlock()

	for id, taken := range bindings {
		if taken == nil {
			bindings[id] = handler
			return id, nil
		}
	}
	return 0, errors.New("hotkey: no free slot for another binding")
}

func release(id int) {
	bindingsMu.Lock()
	defer bindingsMu.Unlock()
	if id >= 0 && id < len(bindings) {
		bindings[id] = nil
	}
}

func nativeModifiers(mods Modifier) uint32 {
	var mask uint32
	if mods&ModCtrl != 0 {
		mask |= controlKey
	}
	if mods&ModAlt != 0 {
		mask |= optionKey
	}
	if mods&ModShift != 0 {
		mask |= shiftKey
	}
	if mods&ModSuper != 0 {
		mask |= cmdKey
	}
	return mask
}
