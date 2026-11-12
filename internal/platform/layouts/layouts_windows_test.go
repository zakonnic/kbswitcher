package layouts

import (
	"testing"

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
