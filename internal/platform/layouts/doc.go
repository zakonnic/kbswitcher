// Package layouts reads the keyboard layouts installed on the machine and the
// characters each of them produces, from the same data the operating system
// uses to turn key presses into text: UCKeyTranslate on macOS, ToUnicodeEx on
// Windows, the XKB keymap on X11. Any installed layout therefore works.
//
// Every implementation exposes the same Reader: List reads the installed
// layouts, Active reports the layout of the focused window, and Activate
// switches to another one.
package layouts
