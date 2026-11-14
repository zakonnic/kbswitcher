package console

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// A window that says nothing about its focus through UI Automation (a
// JetBrains IDE, a terminal drawn by its own renderer) is asked with a key
// instead: Left moves the caret of the shell that has the focus one cell
// back, and Right returns it. A shell with an empty input line, or a program
// that takes the keys for its own, does not answer, and is then
// indistinguishable from an editor.

const (
	// probeTimeout is how long a shell is given to move its caret. A window
	// whose focus is on an editor waits this long before the clipboard path.
	probeTimeout = 120 * time.Millisecond
	probePoll    = 3 * time.Millisecond
)

// Probe presses a caret key in the focused window: Left, or with back set
// Right, which undoes it.
type Probe func(back bool) error

// errNoAnswer means no console moved its caret as a Left key moves it.
var errNoAnswer = errors.New("no shell under the window answered a caret key")

// byProbe picks the console whose caret a Left key moves.
func byProbe(consoles []found, press Probe) (found, error) {
	if press == nil {
		return found{}, errNoAnswer
	}
	// Reading a caret means attaching for a moment.
	ignoreCtrlC(true)
	defer ignoreCtrlC(false)
	if err := press(false); err != nil {
		return found{}, err
	}
	moved := awaitCarets(consoles, steppedBack)
	if err := press(true); err != nil {
		return found{}, err
	}
	switch len(moved) {
	case 0:
		return found{}, errNoAnswer
	case 1:
		// The edit starts from where the caret was. A caret that does not
		// come back was not moved by the keys.
		back := func(now, was windows.Coord) bool { return now == was }
		if len(awaitCarets(moved, back)) == 0 {
			return found{}, fmt.Errorf("%w: a caret moved and did not return", ErrAmbiguous)
		}
		return moved[0], nil
	default:
		return found{}, fmt.Errorf("%w: %d consoles moved their carets at once", ErrAmbiguous, len(moved))
	}
}

// steppedBack reports whether the caret is one character before where it
// was: a cell or two to the left, or from the first column somewhere on the
// row above.
func steppedBack(now, was windows.Coord) bool {
	if was.X == 0 {
		return now.Y == was.Y-1
	}
	return now.Y == was.Y && (now.X == was.X-1 || now.X == was.X-2)
}

// awaitCarets watches the carets of consoles until one satisfies done against
// where it was when found, and returns those that do.
func awaitCarets(consoles []found, done func(now, was windows.Coord) bool) []found {
	deadline := time.Now().Add(probeTimeout)
	for {
		var hit []found
		for _, console := range consoles {
			if now, ok := caretOf(console.pid); ok && done(now, console.caret) {
				hit = append(hit, console)
			}
		}
		if len(hit) > 0 || !time.Now().Before(deadline) {
			return hit
		}
		time.Sleep(probePoll)
	}
}

// caretOf reads the caret of the console a process is attached to.
func caretOf(pid uint32) (windows.Coord, bool) {
	if ret, _, _ := procAttachConsole.Call(uintptr(pid)); ret == 0 {
		return windows.Coord{}, false
	}
	defer detach()
	out, err := openConsoleFile("CONOUT$")
	if err != nil {
		return windows.Coord{}, false
	}
	defer func() { _ = windows.CloseHandle(out) }()
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(out, &info); err != nil {
		return windows.Coord{}, false
	}
	return info.CursorPosition, true
}
