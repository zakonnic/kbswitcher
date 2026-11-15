package hotkey

import (
	"testing"
	"time"
)

func TestAPressReachesItsHandler(t *testing.T) {
	m, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	pressed := make(chan struct{}, 1)
	if err := m.Register(Combo{Key: "F15", Mods: ModCtrl | ModAlt | ModShift}, func() { pressed <- struct{}{} }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Windows posts this when the combination is pressed.
	if ret, _, err := procPostThreadMessage.Call(uintptr(m.threadID), wmHotkey, 1, 0); ret == 0 {
		t.Fatal(err)
	}
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was not called")
	}
}

func TestACombinationTakenElsewhereIsReported(t *testing.T) {
	first, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	combo := Combo{Key: "F15", Mods: ModCtrl | ModAlt}
	if err := first.Register(combo, func() {}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := second.Register(combo, func() {}); err == nil {
		t.Error("the same combination was bound twice")
	}
}

func TestAClosedManagerTakesNothing(t *testing.T) {
	m, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	if err := m.Register(Combo{Key: "F15", Mods: ModCtrl}, func() {}); err == nil {
		t.Error("a closed manager registered a combination")
	}
}

func TestTheSystemKeyIsAModifier(t *testing.T) {
	if nativeModifiers(ModSuper)&modWin == 0 {
		t.Error("the system key is not passed on")
	}
	if !IsModifier("ctrl") || IsModifier("Q") {
		t.Error("IsModifier")
	}
	if Label("Q") != "Q" {
		t.Errorf("Label(Q) = %q", Label("Q"))
	}
	if names := (Combo{Key: "Q", Mods: ModCtrl}).Names(); len(names) != 2 || names[1] != "Q" {
		t.Errorf("Names = %v", names)
	}
	if _, ok := KeyForScanCode(0x1000); ok {
		t.Error("a scan code past the keyboard has a key")
	}
}
