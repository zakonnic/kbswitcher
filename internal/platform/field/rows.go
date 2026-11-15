package field

import (
	"slices"
	"unicode/utf16"
)

// An edit control tells its text one screen row at a time. A line it wraps
// takes several rows with nothing between them; a line break (CR LF, or CR
// in a rich edit control) lies between two rows and belongs to neither.

// maxRows bounds the rows read for one side of the caret or for a selection.
const maxRows = 64

// row is one screen row: its index, the position of its first character,
// its text, and whether a line break ends it.
type row struct {
	index, start int
	text         []uint16
	broken       bool
}

func (r row) end() int { return r.start + len(r.text) }

// span is the part of the row from position from to position to; false when
// they do not lie on it, as when the control changed between two reads.
func (r row) span(from, to int) ([]uint16, bool) {
	if from < r.start || to < from || to > r.end() {
		return nil, false
	}
	return r.text[from-r.start : to-r.start], true
}

// rowSource reads the rows of one edit control.
type rowSource interface {
	// indexOf is the index of the row a position lies on.
	indexOf(position int) (int, bool)
	// rowAt reads a row; false past either end.
	rowAt(index int) (row, bool)
}

func rowOf(src rowSource, position int) (row, bool) {
	index, ok := src.indexOf(position)
	if !ok {
		return row{}, false
	}
	r, ok := src.rowAt(index)
	return r, ok && r.start <= position && position <= r.end()
}

// readRows reads the selection start..end and up to before and after units
// on either side of it on its line. It is false when a row does not hold
// what an earlier one said it would: the control took keys in between.
func readRows(src rowSource, start, end, before, after int) (Around, bool) {
	first, ok := rowOf(src, start)
	if !ok {
		return Around{}, false
	}
	last, ok := rowOf(src, end)
	if !ok || last.index < first.index || last.index-first.index > maxRows {
		return Around{}, false
	}

	head, ok := first.span(first.start, start)
	if !ok {
		return Around{}, false
	}
	// A wrapped line goes on in the row before, which ends where this one
	// starts and holds text; anything else ends the reading.
	for at, n := first, 0; len(head) < before && n < maxRows; n++ {
		previous, ok := src.rowAt(at.index - 1)
		if !ok || previous.broken || len(previous.text) == 0 || previous.end() != at.start {
			break
		}
		head, at = slices.Concat(previous.text, head), previous
	}

	var selected []uint16
	for at := first; ; {
		if at.index == last.index {
			part, ok := at.span(max(start, at.start), end)
			if !ok {
				return Around{}, false
			}
			selected = append(selected, part...)
			break
		}
		part, ok := at.span(max(start, at.start), at.end())
		if !ok {
			return Around{}, false
		}
		selected = append(selected, part...)
		if at.broken {
			selected = append(selected, '\n')
		}
		if at, ok = src.rowAt(at.index + 1); !ok {
			return Around{}, false
		}
	}

	rest, ok := last.span(end, last.end())
	if !ok {
		return Around{}, false
	}
	for at, n := last, 0; len(rest) < after && !at.broken && len(at.text) > 0 && n < maxRows; n++ {
		next, ok := src.rowAt(at.index + 1)
		if !ok || next.start != at.end() {
			break
		}
		rest, at = slices.Concat(rest, next.text), next
	}
	return Around{
		Before:   units(head[max(len(head)-before, 0):]),
		Selected: units(selected),
		After:    units(rest[:min(after, len(rest))]),
		Direct:   true,
	}, true
}

// units decodes UTF-16; a surrogate cut off at the far end of a read turns
// into U+FFFD, never one next to the caret.
func units(text []uint16) string { return string(utf16.Decode(text)) }
