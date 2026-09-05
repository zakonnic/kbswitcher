package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	buttonHeight   = 28
	buttonMinWidth = 88
	buttonPadding  = 20
	buttonRadius   = 7

	// textLift nudges a caption up: the toolkit measures text with its
	// descender space, so a box centered on the measurement sits visibly low.
	textLift = 0.1
)

// placeText puts a caption in a band of the given height so that its glyphs,
// not its measured box, look centered.
func placeText(text *canvas.Text, left, width, height float32) {
	textHeight := text.MinSize().Height
	text.Move(fyne.NewPos(left, (height-textHeight)/2-text.TextSize*textLift))
	text.Resize(fyne.NewSize(width, textHeight))
}

// button is a push button drawn the way macOS draws one: a continuous-corner
// face with a hairline border, filled with the accent color when it is the
// default action.
type button struct {
	widget.BaseWidget
	text     string
	onTapped func()
	primary  bool
	disabled bool
	pressed  bool
}

func newButton(text string, tapped func()) *button {
	b := &button{text: text, onTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *button) enable() {
	b.disabled = false
	b.Refresh()
}

func (b *button) disable() {
	b.disabled, b.pressed = true, false
	b.Refresh()
}

// Tapped runs the action.
func (b *button) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

// MouseDown darkens the face while the button is held.
func (b *button) MouseDown(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.pressed = true
	b.Refresh()
}

// MouseUp restores the face.
func (b *button) MouseUp(*desktop.MouseEvent) { b.release() }

// MouseIn is part of Hoverable; macOS buttons show nothing on hover.
func (b *button) MouseIn(*desktop.MouseEvent) {}

// MouseMoved is part of Hoverable.
func (b *button) MouseMoved(*desktop.MouseEvent) {}

// MouseOut restores the face when the pointer leaves while held.
func (b *button) MouseOut() { b.release() }

func (b *button) release() {
	if !b.pressed {
		return
	}
	b.pressed = false
	b.Refresh()
}

// CreateRenderer draws the face and the caption.
func (b *button) CreateRenderer() fyne.WidgetRenderer {
	r := &buttonRenderer{
		button: b,
		face:   newShape(buttonRadius),
		label:  canvas.NewText(b.text, color.Black),
	}
	r.label.Alignment = fyne.TextAlignCenter
	r.Refresh()
	return r
}

type buttonRenderer struct {
	button *button
	face   *shape
	label  *canvas.Text
}

func (r *buttonRenderer) MinSize() fyne.Size {
	text := fyne.MeasureText(r.button.text, theme.Size(theme.SizeNameText), fyne.TextStyle{})
	return fyne.NewSize(max(buttonMinWidth, text.Width+2*buttonPadding), buttonHeight)
}

func (r *buttonRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()
	placeText(r.label, 0, size.Width, size.Height)
}

func (r *buttonRenderer) Refresh() {
	fill := theme.Color(colorNameControl)
	ink := theme.Color(theme.ColorNameForeground)
	border := theme.Color(colorNameControlBorder)
	switch {
	case r.button.disabled:
		ink = theme.Color(theme.ColorNameDisabled)
	case r.button.primary:
		fill, ink, border = theme.Color(theme.ColorNamePrimary), color.White, color.Transparent
	}
	if r.button.pressed {
		fill = darken(fill, 0.12)
	}
	r.face.set(fill, border, true)

	r.label.Text = r.button.text
	r.label.Color = ink
	r.label.TextSize = theme.Size(theme.SizeNameText)
	r.label.Refresh()
	r.Layout(r.button.Size())
}

func (r *buttonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.face.Raster, r.label}
}

func (r *buttonRenderer) Destroy() {}
