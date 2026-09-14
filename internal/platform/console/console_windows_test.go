package console

import "testing"

func TestTailDiff(t *testing.T) {
	tests := []struct {
		name           string
		before, after  string
		removed, added int
	}{
		{"word erased at the prompt", "PS C:\\> ghbdtn", "PS C:\\> ", 6, 0},
		{"one character erased", "PS C:\\> ckjdj", "PS C:\\> ckjd", 1, 0},
		{"nothing changed", "PS C:\\> ", "PS C:\\> ", 0, 0},
		{"text typed", "PS C:\\> ", "PS C:\\> привет", 0, 6},
		{"word replaced", "PS C:\\> ghbdtn", "PS C:\\> привет", 6, 6},
		{"unicode counts by rune", "жизнь", "жиз", 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed, added := tailDiff(tt.before, tt.after)
			if removed != tt.removed || added != tt.added {
				t.Errorf("tailDiff(%q, %q) = (%d, %d), want (%d, %d)",
					tt.before, tt.after, removed, added, tt.removed, tt.added)
			}
		})
	}
}

func TestSplitRows(t *testing.T) {
	got := splitRows("Windows PowerShell   \r\nPS C:\\> ghbdtn   \r\n")
	want := []string{"Windows PowerShell", "PS C:\\> ghbdtn"}
	if len(got) != len(want) {
		t.Fatalf("splitRows returned %d rows %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSameRows(t *testing.T) {
	// The screen buffer pads and the terminal joins wrapped rows, so the match
	// is on the non-space characters, from the caret row backwards.
	buffer := []string{"PS C:\\Users\\Zak>", "ghbdtn"}
	focused := []string{"", "", "PS C:\\Users\\Zak> ghbdtn"}
	if !sameRows(buffer, focused) {
		t.Error("rows that share their tail were not matched")
	}
	other := []string{"PS C:\\Users\\Zak> ckjdj"}
	if sameRows(buffer, other) {
		t.Error("rows with different tails were matched")
	}
	if sameRows(nil, focused) || sameRows(buffer, []string{"   "}) {
		t.Error("empty rows must never match")
	}
}
