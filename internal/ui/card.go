package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	cardRadius  = 10
	cardPadding = 12
	rowGap      = 10
)

// card groups rows on a panel with hairlines between them.
func card(rows ...fyne.CanvasObject) fyne.CanvasObject {
	return titledCard("", rows...)
}

// titledCard is a card with a small caption above its first row.
func titledCard(title string, rows ...fyne.CanvasObject) fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, 2*len(rows)+1)
	if title != "" {
		caption := label(strings.ToUpper(title), 11, theme.Color(colorNameSecondary), true)
		objects = append(objects, container.New(layout.NewCustomPaddedLayout(10, 0, cardPadding, cardPadding), caption))
	}
	for i, row := range rows {
		if i > 0 {
			objects = append(objects, hairline())
		}
		objects = append(objects, row)
	}
	return container.NewStack(newPanel(), container.New(column{}, objects...))
}

// row is one line of a card, at least height tall: one object at the left
// and one at the right, both centered vertically.
func row(height float32, left, right fyne.CanvasObject) fyne.CanvasObject {
	return container.New(rowLayout{height}, left, right)
}

type rowLayout struct{ height float32 }

func (l rowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	left, right := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(2*cardPadding+left.Width+rowGap+right.Width, max(l.height, left.Height+8, right.Height+8))
}

func (l rowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	left, right := objects[0].MinSize(), objects[1].MinSize()
	objects[0].Move(fyne.NewPos(cardPadding, (size.Height-left.Height)/2))
	objects[0].Resize(left)
	objects[1].Move(fyne.NewPos(size.Width-cardPadding-right.Width, (size.Height-right.Height)/2))
	objects[1].Resize(right)
}

// label is a single line of text.
func label(text string, size float32, ink color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(text, ink)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// caption is a setting name with a smaller explanation under it.
func caption(name, detail string) fyne.CanvasObject {
	return container.New(column{},
		label(name, theme.Size(theme.SizeNameText), theme.Color(theme.ColorNameForeground), false),
		label(detail, 11.5, theme.Color(colorNameSecondary), false))
}

func hairline() fyne.CanvasObject {
	line := canvas.NewRectangle(theme.Color(colorNameHairline))
	line.SetMinSize(fyne.NewSize(1, 1))
	return container.New(layout.NewCustomPaddedLayout(0, 0, cardPadding, 0), line)
}

// gap is empty space of a fixed size.
func gap(width, height float32) fyne.CanvasObject {
	g := canvas.NewRectangle(color.Transparent)
	g.SetMinSize(fyne.NewSize(width, height))
	return g
}

// line puts objects side by side with a fixed gap, centered vertically.
func line(spacing float32, objects ...fyne.CanvasObject) fyne.CanvasObject {
	return container.New(lineLayout{spacing}, objects...)
}

type lineLayout struct{ gap float32 }

func (l lineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for i, o := range objects {
		if i > 0 {
			size.Width += l.gap
		}
		size.Width += o.MinSize().Width
		size.Height = max(size.Height, o.MinSize().Height)
	}
	return size
}

func (l lineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var x float32
	for _, o := range objects {
		m := o.MinSize()
		o.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
		o.Resize(m)
		x += m.Width + l.gap
	}
}

// columns shares the width equally between objects with a fixed gap.
type columns struct{ gap float32 }

func (c columns) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var widest, height float32
	for _, o := range objects {
		widest = max(widest, o.MinSize().Width)
		height = max(height, o.MinSize().Height)
	}
	n := float32(len(objects))
	return fyne.NewSize(n*widest+(n-1)*c.gap, height)
}

func (c columns) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := float32(len(objects))
	width := (size.Width - (n-1)*c.gap) / n
	for i, o := range objects {
		o.Move(fyne.NewPos(float32(i)*(width+c.gap), 0))
		o.Resize(fyne.NewSize(width, size.Height))
	}
}

// column stacks objects top to bottom with no space between them, which the
// toolkit's box layout cannot do.
type column struct{}

func (column) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		size.Width = max(size.Width, o.MinSize().Width)
		size.Height += o.MinSize().Height
	}
	return size
}

func (column) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		height := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, height))
		y += height
	}
}

// panel is the background of a card: a rounded rectangle with a hairline.
type panel struct{ widget.BaseWidget }

func newPanel() *panel {
	p := &panel{}
	p.ExtendBaseWidget(p)
	return p
}

// CreateRenderer draws the panel.
func (p *panel) CreateRenderer() fyne.WidgetRenderer {
	r := &panelRenderer{face: newShape(cardRadius)}
	r.Refresh()
	return r
}

type panelRenderer struct{ face *shape }

func (r *panelRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *panelRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()
}

func (r *panelRenderer) Refresh() {
	r.face.set(theme.Color(colorNameCard), theme.Color(colorNameHairline), false)
}

func (r *panelRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.face.Raster} }

func (r *panelRenderer) Destroy() {}
