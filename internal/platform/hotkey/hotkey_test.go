package hotkey

import (
	"slices"
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

func TestSupportedKeys(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("this platform reports no bindable key")
	}
	if !slices.Contains(keys, DefaultKey()) {
		t.Errorf("default key %q is missing from %v", DefaultKey(), keys)
	}
	if !Supported(DefaultKey()) {
		t.Errorf("Supported(%q) = false", DefaultKey())
	}
	if Supported("NoSuchKey") {
		t.Error(`Supported("NoSuchKey") = true`)
	}
	if slices.Contains(Modifiers(), "Shift") {
		t.Errorf("Modifiers() offers Shift: %v", Modifiers())
	}
}
