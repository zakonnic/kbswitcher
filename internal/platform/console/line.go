package console

// Line is a shell's input line at the moment of a hotkey press, prompt
// excluded, whole or up to the caret. Positions count characters from the
// start of the input.
type Line struct {
	Text  string
	Caret int
	// SelStart and SelEnd bound the text the shell shows as selected; they
	// are equal when nothing is.
	SelStart, SelEnd int
}
