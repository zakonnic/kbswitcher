package console

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReadConsoleOutputAttribute = kernel32.NewProc("ReadConsoleOutputAttribute")

// Edit shows change the shell's input line and has the shell replace the
// characters from..to with text; the caret ends up behind them. With whole
// unset the line stops at the caret, which saves the shell a key press.
//
// The input starts where the shell's own Home key puts the caret, and ends
// where End does, so nothing of the prompt is ever counted as typed. The edit
// goes to the shell in one burst and the line is watched until it shows the
// result; a shell that dropped a key event is set right by typing the
// difference.
func (s *Session) Edit(whole bool, change func(Line) (from, to int, text string)) error {
	in, err := s.input(whole)
	if err != nil {
		return err
	}
	n := len(in.text)
	from, to, text := change(in.Line)
	from = min(max(from, 0), n)
	to = min(max(to, from), n)
	if from == to && text == "" {
		return s.write(move(nil, in.at, in.Caret))
	}

	replacement := []rune(text)
	want := append(append(append([]rune(nil), in.text[:from]...), replacement...), in.text[to:]...)
	caret := from + len(replacement)
	if err := s.replace(in.at, from, to, replacement); err != nil {
		return err
	}
	if s.await(in.before, want[:caret]) {
		return nil
	}

	for range maxCorrections {
		now, err := s.input(whole)
		if err != nil {
			return err
		}
		head := commonPrefix(now.text, want)
		tail := min(commonSuffix(now.text, want), len(now.text)-head, len(want)-head)
		if err := s.replace(now.at, head, len(now.text)-tail, want[head:len(want)-tail]); err != nil {
			return err
		}
		if !s.await(now.before, want[:len(want)-tail]) {
			continue
		}
		return s.write(move(nil, len(want)-tail, caret))
	}
	return errors.New("the shell did not take the edit")
}

// move takes the caret from one character of the input to another.
func move(keys []inputRecord, from, to int) []inputRecord {
	if to < from {
		return navigate(keys, vkLeft, scanLeft, from-to)
	}
	return navigate(keys, vkRight, scanRight, to-from)
}

// replace, with the caret on character at of the input, swaps the characters
// from..to for text.
func (s *Session) replace(at, from, to int, text []rune) error {
	keys := move(nil, at, to)
	for range to - from {
		keys = press(keys, vkBack, scanBack, '\b', 0)
	}
	for _, unit := range windows.StringToUTF16(string(text)) {
		if unit != 0 {
			keys = press(keys, 0, 0, unit, 0)
		}
	}
	return s.write(keys)
}

// input is the line as the Home and End keys showed it.
type input struct {
	Line
	text   []rune
	before []rune // the cells just before the input, the end of the prompt
	at     int    // the character the caret was left on
}

// input presses Home, and End when the whole line is wanted, and reads the
// cells between the places the caret lands, or up to where it was.
func (s *Session) input(whole bool) (input, error) {
	info, err := s.screen()
	if err != nil {
		return input{}, err
	}
	width := int(info.Size.X)
	caret := offset(info.CursorPosition, width)
	// Moving the caret drops the shell's selection, so its highlight is read
	// first, around the caret, before the bounds of the input are known.
	first := max(caret-maxInput, 0)
	colors := s.attributes(first, 2*maxInput, width)

	if err := s.write(navigate(nil, vkHome, scanHome, 1)); err != nil {
		return input{}, err
	}
	home := s.awaitCaret(info.CursorPosition)
	start, end := offset(home, width), caret
	if whole {
		if err := s.write(navigate(nil, vkEnd, scanEnd, 1)); err != nil {
			return input{}, err
		}
		end = offset(s.awaitCaret(home), width)
	}
	if start > caret || caret > end || end-start > maxInput {
		return input{}, fmt.Errorf("the Home and End keys put the caret at cells %d and %d around %d; this is not a shell's input line", start, end, caret)
	}

	context := min(start, promptContext)
	cells := s.cells(start-context, end-start+context, width)
	if len(cells) != end-start+context {
		return input{}, errors.New("the console cannot be read")
	}
	in := input{before: cells[:context], text: cells[context:]}
	in.Line = Line{Text: string(in.text), Caret: caret - start}
	if whole {
		in.at = end - start
	}
	if whole && start > first && end-first <= len(colors) {
		in.SelStart, in.SelEnd = highlighted(colors[start-first:end-first], colors[start-first-1])
	}
	return in, nil
}

// Console character attributes: the background color and the flag that swaps
// it with the foreground.
const (
	backgroundMask = 0x00F0
	reverseVideo   = 0x4000
)

// highlighted finds the one run of cells drawn on another background than
// plain, the prompt's last cell, which is how a shell shows its selection.
// Syntax coloring changes the foreground only.
func highlighted(cells []uint16, plain uint16) (from, to int) {
	marked := func(cell uint16) bool {
		return (cell^plain)&(backgroundMask|reverseVideo) != 0
	}
	for from < len(cells) && !marked(cells[from]) {
		from++
	}
	to = from
	for to < len(cells) && marked(cells[to]) {
		to++
	}
	for _, cell := range cells[to:] {
		if marked(cell) {
			return 0, 0 // more than one run: not a selection
		}
	}
	return from, to
}

// attributes reads the attributes of up to n cells from a linear offset.
func (s *Session) attributes(from, n, width int) []uint16 {
	buf := make([]uint16, n)
	var got uint32
	ret, _, _ := procReadConsoleOutputAttribute.Call(uintptr(s.out),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(n), uintptr(coord(from, width)),
		uintptr(unsafe.Pointer(&got)))
	if ret == 0 {
		return nil
	}
	return buf[:got]
}

// awaitCaret waits for the shell to take the key events written and returns
// where the caret comes to rest.
func (s *Session) awaitCaret(from windows.Coord) windows.Coord {
	deadline := time.Now().Add(editTimeout)
	var taken time.Time
	last, stable := from, 0
	for time.Now().Before(deadline) {
		info, err := s.screen()
		if err != nil {
			return from
		}
		now := info.CursorPosition
		switch {
		case s.pending() > 0:
			stable = 0
		case taken.IsZero():
			taken = time.Now()
		case now != last:
			last, stable = now, 0
		case now != from:
			if stable++; stable >= stablePolls {
				return now
			}
		case time.Since(taken) >= quietTimeout:
			return from
		}
		time.Sleep(pollDelay)
	}
	s.flush()
	return last
}

// await waits until the shell shows want typed after the prompt, the caret
// right behind it. The comparison runs backwards from the caret, so a line
// that scrolled the screen still compares.
func (s *Session) await(before, want []rune) bool {
	expect := string(before) + string(want)
	n := len(before) + len(want)
	deadline := time.Now().Add(editTimeout)
	for time.Now().Before(deadline) {
		if s.pending() == 0 && s.tail(n) == expect {
			return true
		}
		time.Sleep(pollDelay)
	}
	s.flush()
	return false
}

// tail returns the n cells before the caret.
func (s *Session) tail(n int) string {
	info, err := s.screen()
	if err != nil {
		return ""
	}
	width := int(info.Size.X)
	caret := offset(info.CursorPosition, width)
	if n > caret {
		return ""
	}
	return string(s.cells(caret-n, n, width))
}

// flush discards key events the shell never took, so that a leftover cannot
// reach whatever reads the console next.
func (s *Session) flush() { _, _, _ = procFlushConsoleInputBuffer.Call(uintptr(s.in)) }

func commonPrefix(a, b []rune) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

func commonSuffix(a, b []rune) int {
	i := 0
	for i < len(a) && i < len(b) && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return i
}

func offset(c windows.Coord, width int) int { return int(c.Y)*width + int(c.X) }

// coord packs a linear cell offset into a COORD passed by value.
func coord(at, width int) uint32 {
	return uint32(uint16(at/width))<<16 | uint32(uint16(at%width)) //nolint:gosec // a screen buffer is at most 32767 cells either way
}

// Key codes of the keys this package presses; typed characters carry no key
// code at all.
const (
	vkBack    = 0x08
	scanBack  = 0x0E
	vkEnd     = 0x23
	scanEnd   = 0x4F
	vkHome    = 0x24
	scanHome  = 0x47
	vkLeft    = 0x25
	scanLeft  = 0x4B
	vkRight   = 0x27
	scanRight = 0x4D
	// enhancedKey marks the navigation keys between the main block and the
	// keypad, which is where Home, End and the arrows are on a real keyboard.
	enhancedKey = 0x0100
)

// keyEventRecord mirrors KEY_EVENT_RECORD.
type keyEventRecord struct {
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	unicodeChar     uint16
	controlKeyState uint32
}

// inputRecord mirrors INPUT_RECORD holding a key event.
type inputRecord struct {
	eventType uint16
	_         uint16
	event     keyEventRecord
}

const keyEvent = 0x0001

func press(records []inputRecord, vk, scan, char uint16, state uint32) []inputRecord {
	down := inputRecord{eventType: keyEvent, event: keyEventRecord{
		keyDown: 1, repeatCount: 1, virtualKeyCode: vk,
		virtualScanCode: scan, unicodeChar: char, controlKeyState: state}}
	up := down
	up.event.keyDown = 0
	return append(records, down, up)
}

func navigate(records []inputRecord, vk, scan uint16, times int) []inputRecord {
	for range times {
		records = press(records, vk, scan, 0, enhancedKey)
	}
	return records
}

func (s *Session) write(records []inputRecord) error {
	for len(records) > 0 {
		batch := records[:min(len(records), recordsPerWrite)]
		var written uint32
		ret, _, err := procWriteConsoleInputW.Call(uintptr(s.in),
			uintptr(unsafe.Pointer(&batch[0])), uintptr(len(batch)),
			uintptr(unsafe.Pointer(&written)))
		if ret == 0 || int(written) != len(batch) {
			return fmt.Errorf("the console accepted %d of %d key events: %w", written, len(batch), err)
		}
		records = records[len(batch):]
	}
	return nil
}
