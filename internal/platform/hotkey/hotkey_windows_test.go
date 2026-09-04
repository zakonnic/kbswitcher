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

	if err := manager.Register(Combo{Key: "NoSuchKey"}, func() {}); err == nil {
		t.Error("Register accepted a key that does not exist")
	}
}
