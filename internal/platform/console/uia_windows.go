package console

import (
	"fmt"
	"strings"

	"github.com/zakonnic/kbswitcher/internal/platform/uia"
)

// The pane that has the keyboard focus is found through UI Automation. Windows
// Terminal reports the text around its caret, which the screen buffer of
// exactly one console shows; an xterm.js terminal (VS Code and its kin) keeps
// a hidden text area on the cursor cell of its grid, which gives the cursor
// position and the grid size to match against each console.

const (
	xtermTextArea          = "xterm-helper-textarea"
	xtermScreen            = "xterm-screen"
	xtermScreenSearchDepth = 4
)

// focus is what UI Automation reports about the element with the keyboard
// focus.
type focus struct {
	class string
	// hidden marks a text area too small to show text: one an editor or a
	// terminal keeps at its caret while it draws the text itself.
	hidden bool
	// caret and screen are the hidden text area and the grid of an xterm.js
	// terminal.
	caret, screen uia.Rect
	// rows are the caret row and the rows above it, right-trimmed, for an
	// element that exposes its text.
	rows []string
}

func (f focus) xterm() bool { return f.class == xtermTextArea && f.screen.Width() > 0 }

// focused describes the element that has the keyboard focus, which must belong
// to process pid.
func focused(pid uint32, withRows bool) (focus, error) {
	var f focus
	err := uia.With(func(c *uia.Client) error {
		e, err := c.Focused()
		if err != nil {
			return err
		}
		defer e.Release()
		if owner := e.ProcessID(); owner != pid {
			return fmt.Errorf("the focused element belongs to process %d, not %d", owner, pid)
		}
		f.class, f.hidden = e.Class(), e.Hidden()
		if f.class == xtermTextArea {
			f.caret = e.Bounds()
			f.screen = xtermGrid(c, e)
			return nil
		}
		if !withRows {
			return nil
		}
		t, err := e.Text()
		if err != nil {
			return err
		}
		defer t.Release()
		text, err := t.Rows(matchRowsAbove)
		f.rows = splitRows(text)
		return err
	})
	return f, err
}

// xtermGrid finds the grid an xterm.js text area sits in; it is a few levels
// up, past the helpers container.
func xtermGrid(c *uia.Client, e *uia.Element) uia.Rect {
	at := e
	defer func() {
		if at != e {
			at.Release()
		}
	}()
	for range xtermScreenSearchDepth {
		parent := c.Parent(at)
		if parent == nil {
			break
		}
		if at != e {
			at.Release()
		}
		at = parent
		if at.Class() == xtermScreen {
			return at.Bounds()
		}
	}
	return uia.Rect{}
}

// splitRows turns the text of a range into right-trimmed rows.
func splitRows(text string) []string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	rows := strings.Split(text, "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	return rows
}
