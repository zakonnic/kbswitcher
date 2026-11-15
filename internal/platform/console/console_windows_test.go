package console

import (
	"testing"

	"github.com/zakonnic/kbswitcher/internal/platform/uia"
)

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

func TestOnCaret(t *testing.T) {
	// Measured in VS Code: a 98x15 terminal, the caret on cell (15, 1).
	f := focus{
		class:  xtermTextArea,
		caret:  uia.Rect{Left: 1511, Top: 1416, Right: 1524, Bottom: 1446},
		screen: uia.Rect{Left: 1303, Top: 1386, Right: 2630, Bottom: 1836},
	}
	tests := []struct {
		name string
		v    view
		want bool
	}{
		{"same grid, same caret", view{cols: 98, lines: 15, caretX: 15, caretY: 1}, true},
		{"caret one cell off", view{cols: 98, lines: 15, caretX: 16, caretY: 1}, false},
		{"caret on another row", view{cols: 98, lines: 15, caretX: 15, caretY: 2}, false},
		{"taller grid", view{cols: 98, lines: 30, caretX: 15, caretY: 2}, false},
		{"empty console", view{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := onCaret(f, tt.v); got != tt.want {
				t.Errorf("onCaret = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHighlighted(t *testing.T) {
	const plain, colored, selected = 0x07, 0x0E, 0x70
	tests := []struct {
		name     string
		cells    []uint16
		from, to int
	}{
		{"nothing selected", []uint16{plain, colored, colored}, 3, 3},
		{"a word in the middle", []uint16{colored, plain, selected, selected, plain}, 2, 4},
		{"reverse video", []uint16{plain, plain | reverseVideo, plain}, 1, 2},
		{"two runs are not a selection", []uint16{selected, plain, selected}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := highlighted(tt.cells, plain)
			if from != tt.from || to != tt.to {
				t.Errorf("highlighted = %d..%d, want %d..%d", from, to, tt.from, tt.to)
			}
		})
	}
}

func TestNavigationKeysAreEnhanced(t *testing.T) {
	keys := navigate(nil, vkLeft, scanLeft, 2)
	if len(keys) != 4 || keys[0].event.keyDown != 1 || keys[1].event.keyDown != 0 {
		t.Fatalf("two presses made %d records", len(keys))
	}
	if keys[0].event.controlKeyState != enhancedKey || keys[0].event.virtualKeyCode != vkLeft {
		t.Error("an arrow must be marked as an enhanced key, as a real one is")
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]rune("приве"), []rune("привет")); got != 5 {
		t.Errorf("commonPrefix = %d, want 5", got)
	}
	if got := commonSuffix([]rune("ghbпривет tail"), []rune("привет tail")); got != 11 {
		t.Errorf("commonSuffix = %d, want 11", got)
	}
}
