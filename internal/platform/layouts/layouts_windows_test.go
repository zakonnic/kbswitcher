package layouts

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/zakonnic/kbswitcher/internal/keymap"
)

// TestListReadsRealLayouts reads the layouts of the running Windows session.
func TestListReadsRealLayouts(t *testing.T) {
	reader, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()

	list, err := reader.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, layout := range list {
		t.Logf("layout %s: %d characters, KeyA types %q/%q", layout,
			layout.Size(),
			layout.Rune(keymap.KeyA, keymap.LevelNormal),
			layout.Rune(keymap.KeyA, keymap.LevelShift))

		if layout.ID() == "" {
			t.Error("layout has an empty identifier")
		}
		if layout.Size() < 20 {
			t.Errorf("layout %s produced only %d characters, the table looks broken",
				layout, layout.Size())
		}
	}

	active, err := reader.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	t.Logf("active layout: %s", active)
}

func TestLocaleNamesTheLanguageOfALayout(t *testing.T) {
	tests := []struct {
		id, want string
		ok       bool
	}{
		{"00000409", "en-US", true},
		{"00000419", "ru-RU", true},
		{"00010409", "en-US", true}, // a variant: the high word is not the language
		{"E0010411", "ja-JP", true},
		{"0000007F", "", false}, // the invariant locale has an empty name
		{"zz", "", false},
	}
	for _, tt := range tests {
		got, ok := Locale(tt.id)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Locale(%q) = %q, %v; want %q, %v", tt.id, got, ok, tt.want, tt.ok)
		}
	}
}

func TestActivateRefusesALayoutItDoesNotKnow(t *testing.T) {
	reader, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// An unknown layout is refused before any window is asked to switch.
	if err := reader.Activate("not a layout"); err == nil {
		t.Error("an unknown layout was activated")
	}
}

func TestActivateAsksTheForegroundWindow(t *testing.T) {
	reader, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	list, err := reader.List()
	if err != nil || len(list) == 0 {
		t.Fatalf("List = %v, %v", list, err)
	}
	previous := foregroundWindow
	t.Cleanup(func() { foregroundWindow = previous })

	// A window of the test's own that nothing reads: no window of the user's
	// is asked to switch.
	own := ownWindow(t)
	foregroundWindow = func() windows.HWND { return own }
	if err := reader.Activate(list[0].ID()); err != nil {
		t.Errorf("Activate: %v", err)
	}
	foregroundWindow = func() windows.HWND { return 0 }
	if err := reader.Activate(list[0].ID()); err == nil {
		t.Error("a layout was switched with no window to switch")
	}
}

// ownWindow is a message-only window of the test's thread, which runs no
// messages: what is posted to it stays in the test.
func ownWindow(t *testing.T) windows.HWND {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	user32 := windows.NewLazySystemDLL("user32.dll")
	class, _ := windows.UTF16PtrFromString("Static")
	const messageOnly = ^uintptr(2) // HWND_MESSAGE, -3
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, messageOnly, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("no window")
	}
	t.Cleanup(func() { _, _, _ = user32.NewProc("DestroyWindow").Call(hwnd) })
	return windows.HWND(hwnd)
}
