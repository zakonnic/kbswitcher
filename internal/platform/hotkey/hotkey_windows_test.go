package hotkey

import "testing"

// TestRegisterAgainstWindows binds a real combination through the Win32 API and
// releases it again, without pressing anything.
func TestRegisterAgainstWindows(t *testing.T) {
	manager, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	// Not the default Pause, so a developer keeps their own hotkey meanwhile.
	combo := Combo{Key: "F9", Mods: ModCtrl | ModAlt}
	if err := manager.Register(combo, func() {}); err != nil {
		t.Fatalf("Register(%s): %v", combo, err)
	}
	if err := manager.Register(combo.With(ModShift), func() {}); err != nil {
		t.Fatalf("Register(%s): %v", combo.With(ModShift), err)
	}

	letter := Combo{Key: "K", Mods: ModCtrl | ModAlt | ModShift}
	if err := manager.Register(letter, func() {}); err != nil {
		t.Fatalf("Register(%s): %v", letter, err)
	}

	if err := manager.Register(Combo{Key: "NoSuchKey"}, func() {}); err == nil {
		t.Error("Register accepted a key that does not exist")
	}
}

func TestALetterIsNamedByTheLayout(t *testing.T) {
	// The US and Russian layouts both give the key at scan code 0x10 the
	// virtual key Q; a layout that does not (AZERTY) names it A instead.
	name, ok := KeyForScanCode(0x10)
	if !ok || (name != "Q" && name != "A") {
		t.Errorf("KeyForScanCode(0x10) = %q, %v", name, ok)
	}
	if name, ok := KeyForScanCode(0x45); !ok || name != "Pause" {
		t.Errorf("KeyForScanCode(0x45) = %q, %v; want Pause, which no layout moves", name, ok)
	}
}

func TestWhatWindowsCannotReport(t *testing.T) {
	if got := boundCode(Combo{Key: "Pause", Mods: ModCtrl}); got != vkCancel {
		t.Errorf("Ctrl+Pause binds %#x, want VK_CANCEL", got)
	}
	if err := (Combo{Key: "Numpad7", Mods: ModShift}).Check(); err == nil {
		t.Error("Shift+Num 7 passed the check, but Windows reports it as Home")
	}
	if err := (Combo{Key: "Numpad7", Mods: ModCtrl}).Check(); err != nil {
		t.Errorf("Ctrl+Num 7: %v", err)
	}
	if err := (Combo{Key: "NumpadAdd", Mods: ModShift}).Check(); err != nil {
		t.Errorf("Shift+Num +: %v", err)
	}
}
