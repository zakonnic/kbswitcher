package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	popUpHeight   = 22
	popUpMinWidth = 120
	popUpRadius   = 6
	popUpPadding  = 9
	badgeSize     = 16
	badgeRadius   = 4
	badgeInset    = 3
)

// popUpButton picks one of a list of options from a menu, the way a macOS
// pop-up button does: a white face showing the choice, and an accent-colored
// badge with two chevrons at its right end.
type popUpButton struct {
	widget.BaseWidget
	options   []string
	selected  string
	onChanged func(string)
}

func newPopUpButton(options []string, changed func(string)) *popUpButton {
	p := &popUpButton{options: options, onChanged: changed}
	p.ExtendBaseWidget(p)
	return p
}

func (p *popUpButton) setSelected(option string) {
	if p.selected == option {
		return
	}
	p.selected = option
	p.Refresh()
	if p.onChanged != nil {
		p.onChanged(option)
	}
}

// Tapped opens the menu of options under the button.
func (p *popUpButton) Tapped(*fyne.PointEvent) {
	items := make([]*fyne.MenuItem, 0, len(p.options))
	for _, option := range p.options {
		item := fyne.NewMenuItem(option, func() { p.setSelected(option) })
		item.Checked = option == p.selected
		items = append(items, item)
	}
	driver := fyne.CurrentApp().Driver()
	menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), driver.CanvasForObject(p))
	menu.ShowAtPosition(driver.AbsolutePositionForObject(p).AddXY(0, p.Size().Height+2))
	menu.Resize(fyne.NewSize(max(p.Size().Width, menu.MinSize().Width), menu.MinSize().Height))
}

// CreateRenderer draws the face, the choice and the badge.
func (p *popUpButton) CreateRenderer() fyne.WidgetRenderer {
	r := &popUpRenderer{
		popUp: p,
		face:  newShape(popUpRadius),
		label: canvas.NewText(p.selected, color.Black),
		badge: newShape(badgeRadius),
	}
	for i := range r.chevrons {
		r.chevrons[i] = canvas.NewLine(color.White)
		r.chevrons[i].StrokeWidth = 1.5
	}
	r.Refresh()
	return r
}

type popUpRenderer struct {
	popUp    *popUpButton
	face     *shape
	label    *canvas.Text
	badge    *shape
	chevrons [4]*canvas.Line
}

func (r *popUpRenderer) MinSize() fyne.Size {
	var widest float32
	for _, option := range r.popUp.options {
		widest = max(widest, fyne.MeasureText(option, theme.Size(theme.SizeNameText), fyne.TextStyle{}).Width)
	}
	return fyne.NewSize(max(popUpMinWidth, widest+2*popUpPadding+badgeSize+badgeInset), popUpHeight)
}

func (r *popUpRenderer) Layout(size fyne.Size) {
	r.face.Resize(size)
	r.face.Refresh()

	badge := fyne.NewPos(size.Width-badgeInset-badgeSize, (size.Height-badgeSize)/2)
	r.badge.Move(badge)
	r.badge.Resize(fyne.NewSize(badgeSize, badgeSize))
	r.badge.Refresh()
	cx, cy := badge.X+badgeSize/2, badge.Y+badgeSize/2
	r.chevrons[0].Position1, r.chevrons[0].Position2 = fyne.NewPos(cx-2.5, cy-1.2), fyne.NewPos(cx, cy-3.7)
	r.chevrons[1].Position1, r.chevrons[1].Position2 = fyne.NewPos(cx, cy-3.7), fyne.NewPos(cx+2.5, cy-1.2)
	r.chevrons[2].Position1, r.chevrons[2].Position2 = fyne.NewPos(cx-2.5, cy+1.2), fyne.NewPos(cx, cy+3.7)
	r.chevrons[3].Position1, r.chevrons[3].Position2 = fyne.NewPos(cx, cy+3.7), fyne.NewPos(cx+2.5, cy+1.2)

	placeText(r.label, popUpPadding, badge.X-popUpPadding-badgeInset, size.Height)
}

func (r *popUpRenderer) Refresh() {
	r.face.set(theme.Color(colorNameControl), theme.Color(colorNameControlBorder), true)
	r.badge.set(theme.Color(theme.ColorNamePrimary), color.Transparent, false)
	r.label.Text = r.popUp.selected
	r.label.Color = theme.Color(theme.ColorNameForeground)
	r.label.TextSize = theme.Size(theme.SizeNameText)
	r.label.Refresh()
	r.Layout(r.popUp.Size())
}

func (r *popUpRenderer) Objects() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{r.face.Raster, r.label, r.badge.Raster}
	for _, line := range r.chevrons {
		objects = append(objects, line)
	}
	return objects
}

func (r *popUpRenderer) Destroy() {}
