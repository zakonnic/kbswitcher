package app

import (
	"github.com/zakonnic/kbswitcher/internal/platform/field"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/switcher"
)

// textField reads the focused text field without the clipboard and edits it
// through a system edit control's messages or with the keyboard.
type textField struct{ *input.Keyboard }

func newField(keyboard *input.Keyboard) switcher.Field { return textField{keyboard} }

func (textField) Read(around int) (field.Around, error) { return field.Read(around, around) }

func (textField) Replace(right bool, erased, text string) error {
	return field.Replace(right, erased, text)
}
