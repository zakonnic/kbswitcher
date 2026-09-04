// Package console rewrites the input line of a Windows console without asking
// it to copy or paste. A terminal answers Ctrl+C by canceling the line, so on
// a hotkey press kbswitcher instead borrows the console of the shell behind the
// focused window, reads the row the caret is on out of the screen buffer, and
// puts backspaces and characters back into the input buffer.
//
// Nothing is read between hotkey presses, and the console is released again
// straight away. The shell refuses to erase its own prompt, which is what
// keeps a wrong guess about the caret from destroying anything.
package console
