package hotkey

import (
	"errors"
	"testing"
)

func TestParseModifiers(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    Modifier
		wantErr bool
	}{
		{"empty", nil, 0, false},
		{"single", []string{"Ctrl"}, ModCtrl, false},
		{"several", []string{"Ctrl", "Alt"}, ModCtrl | ModAlt, false},
		{"case insensitive", []string{"ctrl", "ALT"}, ModCtrl | ModAlt, false},
		{"mac spelling", []string{"Cmd", "Option"}, ModSuper | ModAlt, false},
		{"windows spelling", []string{"Win", "Control"}, ModSuper | ModCtrl, false},
		{"blank entries are ignored", []string{"", "  "}, 0, false},
		{"unknown", []string{"Fn"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModifiers(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseModifiers(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseModifiers(%v) = %b, want %b", tt.input, got, tt.want)
			}
		})
	}
}

func TestModifierNamesRoundTrip(t *testing.T) {
	mods := ModCtrl | ModShift | ModSuper
	names := mods.Names()
	got, err := ParseModifiers(names)
	if err != nil {
		t.Fatalf("ParseModifiers(%v): %v", names, err)
	}
	if got != mods {
		t.Errorf("round trip through %v gave %b, want %b", names, got, mods)
	}
}

func TestComboString(t *testing.T) {
	tests := []struct {
		combo Combo
		want  string
	}{
		{Combo{Key: "Pause"}, "Pause"},
		{Combo{Key: "F12", Mods: ModShift}, "Shift+F12"},
		{Combo{Key: "F12", Mods: ModCtrl | ModAlt}, "Ctrl+Alt+F12"},
	}
	for _, tt := range tests {
		if got := tt.combo.String(); got != tt.want {
			t.Errorf("Combo%+v.String() = %q, want %q", tt.combo, got, tt.want)
		}
	}
}

func TestComboWith(t *testing.T) {
	base := Combo{Key: "Pause", Mods: ModCtrl}
	selection := base.With(ModShift)

	if !selection.Mods.Has(ModShift | ModCtrl) {
		t.Errorf("With(ModShift) = %v, want it to keep Ctrl and add Shift", selection)
	}
	if base.Mods.Has(ModShift) {
		t.Error("With must not modify the original combo")
	}
}

func TestDefaultKeyIsSupported(t *testing.T) {
	if !Supported(DefaultKey()) {
		t.Errorf("Supported(%q) = false", DefaultKey())
	}
	if Supported("NoSuchKey") {
		t.Error(`Supported("NoSuchKey") = true`)
	}
}

func TestEveryScanCodeNamesOneKey(t *testing.T) {
	named := 0
	for name, k := range keys {
		if k.scan == 0 {
			continue
		}
		named++
		if got, ok := byScanCode()[k.scan]; !ok || got != name {
			t.Errorf("scan code %#x names %q, want %q", k.scan, got, name)
		}
	}
	if len(byScanCode()) != named {
		t.Errorf("%d keys have a scan code but only %d codes are distinct", named, len(byScanCode()))
	}
}

func TestTyping(t *testing.T) {
	for _, key := range []string{"A", "Z", "0", "9", "Semicolon", "Space", "Enter", "Tab", "Backspace", "Delete", "Left", "PageDown", "Numpad5", "NumpadAdd"} {
		if !Typing(key) {
			t.Errorf("Typing(%q) = false", key)
		}
	}
	for _, key := range []string{"Pause", "F1", "F12", "Home", "End", "Insert", "Escape", "PrintScreen"} {
		if Typing(key) {
			t.Errorf("Typing(%q) = true", key)
		}
	}
}

func TestComboCheck(t *testing.T) {
	tests := []struct {
		combo Combo
		ok    bool
	}{
		{Combo{Key: DefaultKey()}, true},
		{Combo{Key: "Q", Mods: ModCtrl}, true},
		{Combo{Key: "Q"}, false},
		{Combo{Key: "Space"}, false},
		{Combo{Key: "Space", Mods: ModAlt}, true},
		{Combo{Key: ""}, false},
		{Combo{Key: "NoSuchKey", Mods: ModCtrl}, false},
	}
	for _, tt := range tests {
		if err := tt.combo.Check(); (err == nil) != tt.ok {
			t.Errorf("%v.Check() = %v, want ok=%v", tt.combo, err, tt.ok)
		}
	}
	if err := (Combo{Key: "Q"}).Check(); !errors.Is(err, ErrTypingKey) {
		t.Errorf("a bare letter gives %v, want ErrTypingKey", err)
	}
}

func TestLabel(t *testing.T) {
	for name, want := range map[string]string{"Semicolon": ";", "Numpad7": "Num 7", "Q": "Q", "PageUp": "PgUp"} {
		if got := Label(name); got != want {
			t.Errorf("Label(%q) = %q, want %q", name, got, want)
		}
	}
}
