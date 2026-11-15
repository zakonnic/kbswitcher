package field

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// wrapped is an edit control that wraps its lines every width units and
// ends them with brk ("\r\n" in an EDIT control, "\r" in a rich edit one).
type wrapped struct {
	rows  []row
	calls int
	// change, when set, replaces the rows after that many calls: keys the
	// control takes in between two reads.
	changeAfter int
	change      []row
}

func newWrapped(text, brk string, width int) *wrapped {
	return &wrapped{rows: layout(text, brk, width)}
}

func layout(text, brk string, width int) []row {
	var rows []row
	at := 0
	lines := strings.Split(text, brk)
	for i, line := range lines {
		units := utf16.Encode([]rune(line))
		for start := 0; ; start += width {
			end := min(start+width, len(units))
			last := end == len(units)
			rows = append(rows, row{
				index:  len(rows),
				start:  at + start,
				text:   units[start:end],
				broken: last && i < len(lines)-1,
			})
			if last {
				break
			}
		}
		at += len(units) + len(utf16.Encode([]rune(brk)))
	}
	return rows
}

func (w *wrapped) tick() {
	w.calls++
	if w.change != nil && w.calls > w.changeAfter {
		w.rows, w.change = w.change, nil
	}
}

func (w *wrapped) indexOf(position int) (int, bool) {
	w.tick()
	for _, r := range w.rows {
		if position >= r.start && position < r.end() || position == r.end() && !r.broken && r.index == len(w.rows)-1 {
			return r.index, true
		}
		if position == r.end() && r.broken {
			return r.index, true
		}
	}
	return 0, false
}

func (w *wrapped) rowAt(index int) (row, bool) {
	w.tick()
	if index < 0 || index >= len(w.rows) {
		return row{}, false
	}
	return w.rows[index], true
}

func TestReadRows(t *testing.T) {
	const text = "first line\r\nthe quick brown fox jumps over the lazy dog\r\nlast"
	pos := func(s string) int { return len(utf16.Encode([]rune(text[:strings.Index(text, s)]))) }
	tests := []struct {
		name                         string
		width, start, end            int
		before, after                int
		wantBefore, wantSel, wantAft string
	}{
		{"wrapped rows join up", 10, pos("lazy"), pos("lazy"), 300, 300,
			"the quick brown fox jumps over the ", "", "lazy dog"},
		{"a line break ends the reading", 80, pos("quick"), pos("quick"), 300, 300,
			"the ", "", "quick brown fox jumps over the lazy dog"},
		{"a selection over wrapped rows", 10, pos("brown"), pos(" over"), 300, 300,
			"the quick ", "brown fox jumps", " over the lazy dog"},
		{"a selection over a line break", 80, pos("line"), pos("quick"), 300, 300,
			"first ", "line\nthe ", "quick brown fox jumps over the lazy dog"},
		{"limits", 10, pos("jumps"), pos("jumps"), 6, 3, "n fox ", "", "jum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, ok := readRows(newWrapped(text, "\r\n", tt.width), tt.start, tt.end, tt.before, tt.after)
			if !ok || a.Before != tt.wantBefore || a.Selected != tt.wantSel || a.After != tt.wantAft || !a.Direct {
				t.Errorf("readRows = %q|%q|%q ok=%v, want %q|%q|%q", a.Before, a.Selected, a.After, ok, tt.wantBefore, tt.wantSel, tt.wantAft)
			}
		})
	}
}

func TestReadRowsOfARichEditControl(t *testing.T) {
	// A rich edit control ends a line with CR alone.
	w := newWrapped("one\rtwo three", "\r", 5)
	a, ok := readRows(w, len("one\rtwo three"), len("one\rtwo three"), 300, 0)
	if !ok || a.Before != "two three" {
		t.Errorf("readRows = %q ok=%v, want %q", a.Before, ok, "two three")
	}
}

func TestATornReadIsRefused(t *testing.T) {
	// The control takes a Backspace over the selection while it is read: the
	// text reflows, and the rows no longer hold what the selection said.
	const text = "the quick brown fox jumps over the lazy dog"
	for after := range 6 {
		w := newWrapped(text, "\r\n", 20)
		w.changeAfter = after
		w.change = layout("the q over the lazy dog", "\r\n", 20)
		start, end := strings.Index(text, "uick"), strings.Index(text, " over")
		if a, ok := readRows(w, start, end, 300, 300); ok && a.Selected != "uick brown fox jumps" {
			t.Errorf("change after %d calls: readRows = %q|%q|%q, accepted", after, a.Before, a.Selected, a.After)
		}
	}
}

// silent answers every message with 0, as a window that is no edit control
// does.
type silent struct{ calls int }

func (s *silent) indexOf(int) (int, bool) { s.calls++; return 0, true }
func (s *silent) rowAt(index int) (row, bool) {
	s.calls++
	return row{index: index}, index >= 0
}

func TestASilentWindowEndsTheReading(t *testing.T) {
	s := &silent{}
	readRows(s, 0, 0, 300, 300)
	if s.calls > 10 {
		t.Errorf("readRows asked %d times", s.calls)
	}
}
