package keymap

import "testing"

const numberRowKeys = 13

// testLayouts returns the two built-in layouts plus a synthetic third one.
func testLayouts() (us, ru, synthetic *Layout) {
	var chars Chars
	for i, k := range ansiKeys {
		if i < numberRowKeys {
			// The number row is shared, as on real layouts.
			chars[k][LevelNormal] = builtinUS.Rune(k, LevelNormal)
			chars[k][LevelShift] = builtinUS.Rune(k, LevelShift)
			continue
		}
		// Greek, because it overlaps neither of the other two.
		chars[k][LevelNormal] = rune(0x03B1 + i)
		chars[k][LevelShift] = rune(0x0391 + i)
	}
	return builtinUS, builtinRU, NewLayout("test.GR", "Synthetic", chars)
}

func TestConvert(t *testing.T) {
	us, ru, _ := testLayouts()

	tests := []struct {
		name     string
		text     string
		from, to *Layout
		want     string
	}{
		{"latin to cyrillic", "ckjdj", us, ru, "слово"},
		{"cyrillic to latin", "штзге", ru, us, "input"},
		{"keeps case", "Ghbdtn", us, ru, "Привет"},
		{"punctuation keys are letters", ";bpym", us, ru, "жизнь"},
		{"letters typed as punctuation", "жизнь", ru, us, ";bpym"},
		{"shifted punctuation", "yt ,ele!", us, ru, "не буду!"},
		{"unknown characters survive", "ckjdj 🙂 42", us, ru, "слово 🙂 42"},
		{"same layout is a no-op", "ckjdj", us, us, "ckjdj"},
		{"empty input", "", us, ru, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Convert(tt.text, tt.from, tt.to); got != tt.want {
				t.Errorf("Convert(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestConvertCycle(t *testing.T) {
	us, ru, gr := testLayouts()
	layouts := []*Layout{us, ru, gr}

	const original = "Ghbdtn, ckjdj!"
	text := original
	for i := range layouts {
		text = Convert(text, layouts[i], layouts[(i+1)%len(layouts)])
	}
	if text != original {
		t.Errorf("after a full cycle text = %q, want %q", text, original)
	}
}

func TestDetect(t *testing.T) {
	us, ru, gr := testLayouts()
	layouts := []*Layout{us, ru, gr}

	tests := []struct {
		name      string
		text      string
		preferred int
		want      int
	}{
		{"latin text", "ckjdj", 1, 0},
		{"cyrillic text", "слово", 0, 1},
		{"synthetic text", "ξοπ", 0, 2},
		{"digits alone keep the active layout", "1234", 2, 2},
		{"spaces alone keep the active layout", "   ", 1, 1},
		{"mixed text follows the majority", "ckjdj слово ckjdj", 1, 0},
		{"out of range preferred index is clamped", "1234", 99, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.text, layouts, tt.preferred); got != tt.want {
				t.Errorf("Detect(%q, preferred=%d) = %d, want %d", tt.text, tt.preferred, got, tt.want)
			}
		})
	}

	t.Run("no layouts", func(t *testing.T) {
		if got := Detect("ckjdj", nil, 3); got != 3 {
			t.Errorf("Detect with no layouts = %d, want 3", got)
		}
	})
}

func TestSplitLastWord(t *testing.T) {
	tests := []struct {
		text             string
		head, word, tail string
	}{
		{"ckjdj", "", "ckjdj", ""},
		{"one ckjdj", "one ", "ckjdj", ""},
		{"one ckjdj ", "one ", "ckjdj", " "},
		{"ckjdj\n", "", "ckjdj", "\n"},
		{"two words\there", "two words\t", "here", ""},
		{";bpym", "", ";bpym", ""},
		{"", "", "", ""},
		{"   ", "", "", "   "},
		{"слово", "", "слово", ""},
		{"один слово", "один ", "слово", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			head, word, tail := SplitLastWord(tt.text)
			if head != tt.head || word != tt.word || tail != tt.tail {
				t.Errorf("SplitLastWord(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.text, head, word, tail, tt.head, tt.word, tt.tail)
			}
		})
	}
}

func TestLayoutAccessors(t *testing.T) {
	us, _, _ := testLayouts()

	if got := us.Rune(KeyA, LevelShift); got != 'A' {
		t.Errorf("Rune(KeyA, LevelShift) = %q, want %q", got, 'A')
	}
	if got := us.Rune(KeyIntlYen, LevelNormal); got != 0 {
		t.Errorf("Rune(KeyIntlYen, LevelNormal) = %q, want 0 for a key the layout does not use", got)
	}
	if got := us.Rune(KeyCount, LevelNormal); got != 0 {
		t.Errorf("Rune with an out-of-range key = %q, want 0", got)
	}
	if p, ok := us.Press('?'); !ok || p.Key != KeySlash || p.Level != LevelShift {
		t.Errorf("Press('?') = %v, %v; want {KeySlash, LevelShift}, true", p, ok)
	}
	if us.CanType('щ') {
		t.Error("CanType('щ') = true for the US layout, want false")
	}
	if us.Size() != 2*len(ansiKeys) {
		t.Errorf("Size() = %d, want %d", us.Size(), 2*len(ansiKeys))
	}
	if us.String() != "English (US) (builtin.US)" {
		t.Errorf("String() = %q", us.String())
	}
}
