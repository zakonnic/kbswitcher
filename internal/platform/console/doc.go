// Package console rewrites the input line of a Windows console without asking
// it to copy or paste. A terminal answers Ctrl+C by canceling the line, so on
// a hotkey press kbswitcher instead borrows the console of the shell behind the
// focused window (a console window, Windows Terminal, or the terminal pane of
// an editor built on xterm.js), has the shell's own Home and End keys show
// where its input line lies, and writes the edit into the input buffer.
//
// Nothing is read between hotkey presses, and the console is released again
// straight away. Only what lies between Home and End is ever replaced, which
// is what keeps the prompt and everything above it out of reach.
package console
