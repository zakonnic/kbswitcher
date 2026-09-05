package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	checkboxSize   = 14
	checkboxRadius = 3
	checkboxGap    = 5
)

// checkbox is a small square box with a caption, drawn like the one macOS
// shows in a form: white with a border, or the accent color with a tick.
type checkbox struct {
	widget.BaseWidget
	text      string
	checked   bool
	onChanged func(bool)
}

func newCheckbox(text string, changed func(bool)) *checkbox {
	c := &checkbox{text: text, onChanged: changed}
	c.ExtendBaseWidget(c)
	return c
}

func (c *checkbox) setChecked(on bool) {
	if c.checked == on {
		return
	}
	c.checked = on
	c.Refresh()
	if c.onChanged != nil {
		c.onChanged(on)
	}
}

// Tapped flips the box.
func (c *checkbox) Tapped(*fyne.PointEvent) { c.setChecked(!c.checked) }

// CreateRenderer draws the box, the tick and the caption.
func (c *checkbox) CreateRenderer() fyne.WidgetRenderer {
	r := &checkboxRenderer{
		checkbox: c,
		box:      newShape(checkboxRadius),
		label:    canvas.NewText(c.text, color.Black),
		tick:     [2]*canvas.Line{canvas.NewLine(color.White), canvas.NewLine(color.White)},
	}
	for _, line := range r.tick {
		line.StrokeWidth = 1.6
	}
	r.Refresh()
	return r
}

type checkboxRenderer struct {
	checkbox *checkbox
	box      *shape
	tick     [2]*canvas.Line
	label    *canvas.Text
}

func (r *checkboxRenderer) MinSize() fyne.Size {
	text := fyne.MeasureText(r.checkbox.text, theme.Size(theme.SizeNameText), fyne.TextStyle{})
	return fyne.NewSize(checkboxSize+checkboxGap+text.Width, max(checkboxSize, text.Height))
}

func (r *checkboxRenderer) Layout(size fyne.Size) {
	top := (size.Height - checkboxSize) / 2
	r.box.Move(fyne.NewPos(0, top))
	r.box.Resize(fyne.NewSize(checkboxSize, checkboxSize))
	r.box.Refresh()

	// The tick runs from the left edge down to the bottom and up to the right.
	left, bottom, right := fyne.NewPos(3.5, top+7.3), fyne.NewPos(6, top+9.8), fyne.NewPos(10.8, top+4.4)
	r.tick[0].Position1, r.tick[0].Position2 = left, bottom
	r.tick[1].Position1, r.tick[1].Position2 = bottom, right

	placeText(r.label, checkboxSize+checkboxGap, size.Width-checkboxSize-checkboxGap, size.Height)
}

func (r *checkboxRenderer) Refresh() {
	if r.checkbox.checked {
		r.box.set(theme.Color(theme.ColorNamePrimary), color.Transparent, false)
	} else {
		r.box.set(theme.Color(colorNameControl), theme.Color(colorNameControlBorder), false)
	}
	for _, line := range r.tick {
		if r.checkbox.checked {
			line.Show()
		} else {
			line.Hide()
		}
	}
	r.label.Text = r.checkbox.text
	r.label.Color = theme.Color(theme.ColorNameForeground)
	r.label.TextSize = theme.Size(theme.SizeNameText)
	r.label.Refresh()
	r.Layout(r.checkbox.Size())
}

func (r *checkboxRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.box.Raster, r.tick[0], r.tick[1], r.label}
}

func (r *checkboxRenderer) Destroy() {}
