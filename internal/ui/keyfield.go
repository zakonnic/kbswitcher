package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/zakonnic/kbswitcher/internal/platform/hotkey"
)

const (
	keyFieldWidth   = 120
	keyFieldHeight  = 24
	keyFieldRadius  = 6
	keyFieldPadding = 10
)

// keyField takes the bindable key pressed while it has the focus. It shows
// the chosen key, in grey while that is still the key in force.
type keyField struct {
	widget.BaseWidget
	current, selected string
	focused           bool
	onChanged         func(string)
	// onFocus reports that the field starts or stops listening.
	onFocus func(bool)
}

func newKeyField() *keyField {
	f := &keyField{}
	f.ExtendBaseWidget(f)
	return f
}

// reset makes key both the key in force and the one shown.
func (f *keyField) reset(key string) {
	f.current, f.selected = key, key
	f.Refresh()
}

// Tapped starts listening.
func (f *keyField) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Focus(f)
	}
}

func (f *keyField) FocusGained() { f.setFocused(true) }

func (f *keyField) FocusLost() { f.setFocused(false) }

func (f *keyField) setFocused(on bool) {
	f.focused = on
	f.Refresh()
	if f.onFocus != nil {
		f.onFocus(on)
	}
}

// KeyDown takes the key if it can be bound. It sees every key, including the
// ones the toolkit only knows by scan code and the ones shortcuts would eat.
func (f *keyField) KeyDown(e *fyne.KeyEvent) {
	name, ok := bindableKey(e)
	if !ok || name == f.selected {
		return
	}
	f.selected = name
	f.Refresh()
	if f.onChanged != nil {
		f.onChanged(name)
	}
}

func (f *keyField) KeyUp(*fyne.KeyEvent) {}

// TypedKey lets Escape end listening without closing the window.
func (f *keyField) TypedKey(e *fyne.KeyEvent) {
	if e.Name != fyne.KeyEscape {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Unfocus()
	}
}

// TypedRune drops characters: nothing typed is ever kept.
func (f *keyField) TypedRune(rune) {}

func bindableKey(e *fyne.KeyEvent) (string, bool) {
	if name := string(e.Name); hotkey.Supported(name) {
		return name, true
	}
	return hotkey.KeyForScanCode(e.Physical.ScanCode)
}

var _ desktop.Keyable = (*keyField)(nil)

// CreateRenderer draws the face and the key name.
func (f *keyField) CreateRenderer() fyne.WidgetRenderer {
	r := &keyFieldRenderer{field: f, face: newShape(keyFieldRadius), text: canvas.NewText("", color.White)}
	r.Refresh()
	return r
}

type keyFieldRenderer struct {
	field *keyField
	face  *shape
	text  *canvas.Text
}

func (r *keyFieldRenderer) MinSize() fyne.Size { return fyne.NewSize(keyFieldWidth, keyFieldHeight) }

func (r *keyFieldRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()
	placeText(r.text, keyFieldPadding, size.Width-2*keyFieldPadding, size.Height)
}

func (r *keyFieldRenderer) Refresh() {
	border := theme.Color(colorNameControlBorder)
	if r.field.focused {
		border = theme.Color(theme.ColorNamePrimary)
	}
	r.face.set(theme.Color(colorNameControl), border, false)

	r.text.Text = r.field.selected
	r.text.TextSize = theme.Size(theme.SizeNameText)
	r.text.Color = theme.Color(theme.ColorNameForeground)
	if r.field.selected == r.field.current {
		r.text.Color = theme.Color(colorNameSecondary)
	}
	r.text.Refresh()
	r.Layout(r.field.Size())
}

func (r *keyFieldRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.face.Raster, r.text}
}

func (r *keyFieldRenderer) Destroy() {}
