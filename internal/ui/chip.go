package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

const (
	lipHeight = 1.5
	dotSize   = 6
	dotGap    = 5
)

// chipStyle is the look of a chip.
type chipStyle struct {
	fill, border, ink color.Color
	// lip is a ledge under the face that makes a key cap; nil for none.
	lip                                         color.Color
	height, radius, padding, minWidth, textSize float32
	bold, dot                                   bool
}

var (
	keycapStyle = chipStyle{
		fill: rgba(0xFF, 0xFF, 0xFF, 0x1F), border: rgba(0xFF, 0xFF, 0xFF, 0x2E), ink: color.White,
		lip:    rgba(0xFF, 0xFF, 0xFF, 0x2E),
		height: 24, radius: 6, padding: 8, minWidth: 26, textSize: 12, bold: true,
	}
	versionStyle = chipStyle{
		fill: rgba(0xFF, 0xFF, 0xFF, 0x1A), border: color.Transparent, ink: rgba(0xFF, 0xFF, 0xFF, 0xCC),
		height: 20, radius: 10, padding: 10, textSize: 11.5,
	}
	pillOK      = pillStyle(rgba(0x4C, 0xD2, 0x7F, 0x26), rgba(0x6F, 0xE1, 0x9A, 0xFF))
	pillWarn    = pillStyle(rgba(0xFF, 0x9F, 0x0A, 0x26), rgba(0xFF, 0xB3, 0x40, 0xFF))
	pillNeutral = pillStyle(rgba(0xFF, 0xFF, 0xFF, 0x1A), rgba(0xFF, 0xFF, 0xFF, 0xBF))
)

func pillStyle(fill, ink color.Color) chipStyle {
	return chipStyle{
		fill: fill, border: color.Transparent, ink: ink,
		height: 22, radius: 11, padding: 9, textSize: 11.5, bold: true, dot: true,
	}
}

func badgeStyle(fill color.Color) chipStyle {
	return chipStyle{
		fill: fill, border: color.Transparent, ink: color.White,
		height: 20, radius: 5, minWidth: 28, padding: 4, textSize: 10.5, bold: true,
	}
}

// chip is a short text on a rounded face: a key cap, a status pill, a badge.
type chip struct {
	widget.BaseWidget
	text  string
	style chipStyle
}

func newChip(text string, style chipStyle) *chip {
	c := &chip{text: text, style: style}
	c.ExtendBaseWidget(c)
	return c
}

func (c *chip) set(text string, style chipStyle) {
	c.text, c.style = text, style
	c.Refresh()
}

// CreateRenderer draws the lip, the face, the dot and the text.
func (c *chip) CreateRenderer() fyne.WidgetRenderer {
	r := &chipRenderer{
		chip:  c,
		lip:   newShape(0),
		face:  newShape(0),
		dot:   canvas.NewCircle(color.Transparent),
		label: canvas.NewText("", color.White),
	}
	r.Refresh()
	return r
}

type chipRenderer struct {
	chip      *chip
	lip, face *shape
	dot       *canvas.Circle
	label     *canvas.Text
}

func (r *chipRenderer) textWidth() float32 {
	s := r.chip.style
	return fyne.MeasureText(r.chip.text, s.textSize, fyne.TextStyle{Bold: s.bold}).Width
}

func (r *chipRenderer) contentWidth() float32 {
	w := r.textWidth()
	if r.chip.style.dot {
		w += dotSize + dotGap
	}
	return w
}

func (r *chipRenderer) MinSize() fyne.Size {
	s := r.chip.style
	height := s.height
	if s.lip != nil {
		height += lipHeight
	}
	return fyne.NewSize(max(s.minWidth, r.contentWidth()+2*s.padding), height)
}

func (r *chipRenderer) Layout(size fyne.Size) {
	s := r.chip.style
	face := fyne.NewSize(size.Width, s.height)
	r.face.Resize(face)
	r.face.Refresh()
	r.lip.Move(fyne.NewPos(0, lipHeight))
	r.lip.Resize(face)
	r.lip.Refresh()

	left := (size.Width - r.contentWidth()) / 2
	if s.dot {
		top := (s.height - dotSize) / 2
		r.dot.Position1 = fyne.NewPos(left, top)
		r.dot.Position2 = fyne.NewPos(left+dotSize, top+dotSize)
		left += dotSize + dotGap
	}
	placeText(r.label, left, r.textWidth(), s.height)
}

func (r *chipRenderer) Refresh() {
	s := r.chip.style
	r.face.radius, r.lip.radius = s.radius, s.radius
	r.face.set(s.fill, s.border, false)
	if s.lip != nil {
		r.lip.set(s.lip, color.Transparent, false)
		r.lip.Show()
	} else {
		r.lip.Hide()
	}
	r.dot.FillColor = s.ink
	r.dot.Hidden = !s.dot
	r.dot.Refresh()

	r.label.Text = r.chip.text
	r.label.Color = s.ink
	r.label.TextSize = s.textSize
	r.label.TextStyle = fyne.TextStyle{Bold: s.bold}
	r.label.Refresh()
	r.Layout(r.chip.Size())
}

func (r *chipRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.lip.Raster, r.face.Raster, r.dot, r.label}
}

func (r *chipRenderer) Destroy() {}
