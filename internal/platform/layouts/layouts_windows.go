package layouts

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/zakonnic/kbswitcher/internal/keymap"
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procGetKeyboardLayoutList  = user32.NewProc("GetKeyboardLayoutList")
	procActivateKeyboardLayout = user32.NewProc("ActivateKeyboardLayout")
	procGetKeyboardLayoutName  = user32.NewProc("GetKeyboardLayoutNameW")
	procMapVirtualKeyEx        = user32.NewProc("MapVirtualKeyExW")
	procPostMessage            = user32.NewProc("PostMessageW")
	procLCIDToLocaleName       = windows.NewLazySystemDLL("kernel32.dll").NewProc("LCIDToLocaleName")
)

const (
	mapvkVSCToVKEx           = 3
	wmInputLangChangeRequest = 0x0050
	vkShift                  = 0x10
	// toUnicodeNoState keeps ToUnicodeEx from touching the kernel keyboard
	// state (Windows 10 1607 and later).
	toUnicodeNoState = 0x4
	// klidBufferLen fits the eight hex digits of a layout identifier and NUL.
	klidBufferLen       = 9
	layoutsRegistryPath = `SYSTEM\CurrentControlSet\Control\Keyboard Layouts`
)

// scanCodes maps every tracked key position to its PS/2 set 1 scan code, which
// names a physical key independently of the active layout.
var scanCodes = [keymap.KeyCount]uint32{
	keymap.KeyBackquote: 0x29, keymap.Key1: 0x02, keymap.Key2: 0x03, keymap.Key3: 0x04,
	keymap.Key4: 0x05, keymap.Key5: 0x06, keymap.Key6: 0x07, keymap.Key7: 0x08,
	keymap.Key8: 0x09, keymap.Key9: 0x0A, keymap.Key0: 0x0B,
	keymap.KeyMinus: 0x0C, keymap.KeyEqual: 0x0D,

	keymap.KeyQ: 0x10, keymap.KeyW: 0x11, keymap.KeyE: 0x12, keymap.KeyR: 0x13,
	keymap.KeyT: 0x14, keymap.KeyY: 0x15, keymap.KeyU: 0x16, keymap.KeyI: 0x17,
	keymap.KeyO: 0x18, keymap.KeyP: 0x19,
	keymap.KeyBracketLeft: 0x1A, keymap.KeyBracketRight: 0x1B, keymap.KeyBackslash: 0x2B,

	keymap.KeyA: 0x1E, keymap.KeyS: 0x1F, keymap.KeyD: 0x20, keymap.KeyF: 0x21,
	keymap.KeyG: 0x22, keymap.KeyH: 0x23, keymap.KeyJ: 0x24, keymap.KeyK: 0x25,
	keymap.KeyL: 0x26, keymap.KeySemicolon: 0x27, keymap.KeyQuote: 0x28,

	keymap.KeyZ: 0x2C, keymap.KeyX: 0x2D, keymap.KeyC: 0x2E, keymap.KeyV: 0x2F,
	keymap.KeyB: 0x30, keymap.KeyN: 0x31, keymap.KeyM: 0x32,
	keymap.KeyComma: 0x33, keymap.KeyPeriod: 0x34, keymap.KeySlash: 0x35,

	keymap.KeyIntlBackslash: 0x56, keymap.KeyIntlRo: 0x73, keymap.KeyIntlYen: 0x7D,
}

// Reader reads keyboard layouts from Windows.
type Reader struct {
	mu      sync.Mutex
	idByHKL map[windows.Handle]string
	hklByID map[string]windows.Handle
}

// Open prepares a reader. Windows needs no handle of its own.
func Open() (*Reader, error) {
	return &Reader{
		idByHKL: make(map[windows.Handle]string),
		hklByID: make(map[string]windows.Handle),
	}, nil
}

// Close releases the reader.
func (r *Reader) Close() error { return nil }

// List returns the installed layouts in the order the language bar cycles
// through them.
func (r *Reader) List() ([]*keymap.Layout, error) {
	handles, err := keyboardLayoutList()
	if err != nil {
		return nil, err
	}

	// GetKeyboardLayoutNameW reports the layout of the calling thread, so
	// each layout is made current for a moment. ActivateKeyboardLayout is
	// thread local, which keeps the change invisible to the rest of the
	// process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	previous := windows.GetKeyboardLayout(0)
	defer func() { _ = activateLayout(previous) }()

	r.mu.Lock()
	defer r.mu.Unlock()

	layouts := make([]*keymap.Layout, 0, len(handles))
	for _, hkl := range handles {
		id, err := layoutID(hkl)
		if err != nil {
			return nil, err
		}
		layout := keymap.NewLayout(id, layoutName(id), layoutChars(hkl))
		if layout.Size() == 0 {
			continue // an input method rather than a keyboard layout
		}
		layouts = append(layouts, layout)
		r.idByHKL[hkl] = id
		r.hklByID[id] = hkl
	}
	if len(layouts) == 0 {
		return nil, errors.New("no usable keyboard layout among those installed")
	}
	return layouts, nil
}

// Active returns the identifier of the layout used by the focused window.
func (r *Reader) Active() (string, error) {
	var tid uint32
	if hwnd := windows.GetForegroundWindow(); hwnd != 0 {
		// When the window went away, thread id zero gives the process-wide
		// default layout, which is a good enough answer.
		if id, err := windows.GetWindowThreadProcessId(hwnd, nil); err == nil {
			tid = id
		}
	}
	hkl := windows.GetKeyboardLayout(tid)

	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.idByHKL[hkl]
	if !ok {
		return "", fmt.Errorf("layout %#x was not installed at start-up", uintptr(hkl))
	}
	return id, nil
}

// Activate asks the focused window to switch to the given layout, as the
// language bar does; Windows offers no way to set another process's layout
// directly.
func (r *Reader) Activate(id string) error {
	r.mu.Lock()
	hkl, ok := r.hklByID[id]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown layout %q", id)
	}

	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return errors.New("no focused window to switch")
	}
	ret, _, err := procPostMessage.Call(uintptr(hwnd), wmInputLangChangeRequest, 0, uintptr(hkl))
	if ret == 0 {
		return fmt.Errorf("PostMessage(WM_INPUTLANGCHANGEREQUEST): %w", err)
	}
	return nil
}

func keyboardLayoutList() ([]windows.Handle, error) {
	count, _, err := procGetKeyboardLayoutList.Call(0, 0)
	if count == 0 {
		return nil, fmt.Errorf("GetKeyboardLayoutList: %w", err)
	}
	handles := make([]windows.Handle, count)
	got, _, err := procGetKeyboardLayoutList.Call(count, uintptr(unsafe.Pointer(&handles[0])))
	if got == 0 {
		return nil, fmt.Errorf("GetKeyboardLayoutList: %w", err)
	}
	return handles[:got], nil
}

// layoutID returns the registry identifier of a layout, such as "00000409".
func layoutID(hkl windows.Handle) (string, error) {
	if err := activateLayout(hkl); err != nil {
		return "", err
	}
	var buf [klidBufferLen]uint16
	ret, _, err := procGetKeyboardLayoutName.Call(uintptr(unsafe.Pointer(&buf[0])))
	if ret == 0 {
		return "", fmt.Errorf("GetKeyboardLayoutNameW: %w", err)
	}
	return windows.UTF16ToString(buf[:]), nil
}

// activateLayout makes hkl current for the calling thread only.
func activateLayout(hkl windows.Handle) error {
	ret, _, err := procActivateKeyboardLayout.Call(uintptr(hkl), 0)
	if ret == 0 {
		return fmt.Errorf("ActivateKeyboardLayout: %w", err)
	}
	return nil
}

// Locale names the language of a layout identifier, such as en-US for
// 00000409, from the language identifier in its low word.
func Locale(id string) (string, bool) {
	klid, err := strconv.ParseUint(id, 16, 32)
	if err != nil {
		return "", false
	}
	var name [85]uint16 // LOCALE_NAME_MAX_LENGTH
	n, _, _ := procLCIDToLocaleName.Call(uintptr(klid&0xFFFF), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0)
	if n <= 1 { // the count includes the terminating NUL
		return "", false
	}
	return windows.UTF16ToString(name[:]), true
}

// layoutName returns the display name of a layout identifier, or the
// identifier itself when the registry has no entry.
func layoutName(id string) string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, layoutsRegistryPath+`\`+id, registry.QUERY_VALUE)
	if err != nil {
		return id
	}
	defer key.Close()

	if text, _, err := key.GetStringValue("Layout Text"); err == nil && text != "" {
		return text
	}
	return id
}

// layoutChars asks Windows which character every tracked key produces in the
// given layout, unshifted and shifted.
func layoutChars(hkl windows.Handle) keymap.Chars {
	var chars keymap.Chars
	var state [256]byte
	var buf [8]uint16

	for key := keymap.Key(0); key < keymap.KeyCount; key++ {
		scan := scanCodes[key]
		if scan == 0 {
			continue
		}
		vk, _, _ := procMapVirtualKeyEx.Call(uintptr(scan), mapvkVSCToVKEx, uintptr(hkl))
		if vk == 0 || vk > 0xFF {
			continue
		}
		for _, level := range []keymap.Level{keymap.LevelNormal, keymap.LevelShift} {
			state[vkShift] = 0
			if level == keymap.LevelShift {
				state[vkShift] = 0x80
			}
			n := windows.ToUnicodeEx(uint32(vk), scan, &state[0],
				&buf[0], int32(len(buf)), toUnicodeNoState, hkl)
			switch {
			case n == 1:
				chars[key][level] = rune(buf[0])
			case n < 0:
				// A dead key: its spacing form is what a lone press shows. The
				// second call clears the pending state on builds that ignore
				// toUnicodeNoState.
				chars[key][level] = rune(buf[0])
				windows.ToUnicodeEx(uint32(vk), scan, &state[0],
					&buf[0], int32(len(buf)), toUnicodeNoState, hkl)
			}
			// n == 0 types nothing and n > 1 types a ligature; neither is a
			// single key press.
			if r := chars[key][level]; r < 0x20 || r == 0x7F {
				chars[key][level] = 0
			}
		}
	}
	return chars
}
