// Package field reads the text field that has the keyboard focus without a
// key press and without the clipboard: through Microsoft UI Automation, the
// interface screen readers use, or a system edit control's own messages. A
// system edit control is also written to through its messages. The field is
// asked only when a hotkey is pressed, only about the focused field, and only
// for the text around the caret.
package field

import "errors"

var (
	// ErrNoText means the focus is not on an editable field that tells its
	// text truthfully.
	ErrNoText = errors.New("the focus is not on a text field that tells its text")
	// ErrPassword means the focused field hides its text.
	ErrPassword = errors.New("the focused field hides its text")
	// ErrChanged means the field does not hold the text a replacement was
	// meant for; nothing was replaced.
	ErrChanged = errors.New("the field no longer holds the text to replace")
)

// Around is the selection of a text field and the text on either side of it.
// With nothing selected, Selected is empty and the caret lies between Before
// and After.
type Around struct {
	Before, Selected, After string
	// Direct marks a system edit control: one whose text Replace changes
	// through the control's own messages, with no key press.
	Direct bool
}
