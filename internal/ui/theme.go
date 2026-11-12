package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Colors the controls of this package need beyond what the toolkit names.
const (
	colorNameCard          fyne.ThemeColorName = "card"
	colorNameHairline      fyne.ThemeColorName = "hairline"
	colorNameControl       fyne.ThemeColorName = "control"
	colorNameControlBorder fyne.ThemeColorName = "controlBorder"
	colorNameSecondary     fyne.ThemeColorName = "secondary"
	colorNameTrackOff      fyne.ThemeColorName = "trackOff"
	colorNameKnob          fyne.ThemeColorName = "knob"
	colorNameFrame         fyne.ThemeColorName = "frame"
)

// colors is the one palette of the application: light text on a dark window.
var colors = map[fyne.ThemeColorName]color.Color{
	theme.ColorNameBackground:        rgba(0x10, 0x13, 0x1B, 0xFF),
	theme.ColorNameForeground:        rgba(0xFF, 0xFF, 0xFF, 0xEB),
	theme.ColorNamePrimary:           rgba(0x0A, 0x84, 0xFF, 0xFF),
	theme.ColorNameHyperlink:         rgba(0x8A, 0xB4, 0xFF, 0xFF),
	theme.ColorNameMenuBackground:    rgba(0x1C, 0x20, 0x30, 0xFF),
	theme.ColorNameOverlayBackground: rgba(0x1C, 0x20, 0x30, 0xFF),
	theme.ColorNameInputBackground:   rgba(0xFF, 0xFF, 0xFF, 0x0F),
	theme.ColorNameHover:             rgba(0xFF, 0xFF, 0xFF, 0x14),
	theme.ColorNamePressed:           rgba(0xFF, 0xFF, 0xFF, 0x26),
	theme.ColorNameSeparator:         rgba(0xFF, 0xFF, 0xFF, 0x14),
	theme.ColorNameShadow:            rgba(0x00, 0x00, 0x00, 0x80),
	theme.ColorNameScrollBar:         rgba(0xFF, 0xFF, 0xFF, 0x4D),
	theme.ColorNameFocus:             rgba(0x0A, 0x84, 0xFF, 0x66),
	theme.ColorNameSelection:         rgba(0x0A, 0x84, 0xFF, 0x40),
	theme.ColorNameDisabled:          rgba(0xFF, 0xFF, 0xFF, 0x4D),
	colorNameCard:                    rgba(0xFF, 0xFF, 0xFF, 0x0D),
	colorNameHairline:                rgba(0xFF, 0xFF, 0xFF, 0x14),
	colorNameControl:                 rgba(0xFF, 0xFF, 0xFF, 0x1A),
	colorNameControlBorder:           rgba(0xFF, 0xFF, 0xFF, 0x24),
	colorNameSecondary:               rgba(0xFF, 0xFF, 0xFF, 0x94),
	colorNameTrackOff:                rgba(0xFF, 0xFF, 0xFF, 0x29),
	colorNameKnob:                    rgba(0xF2, 0xF2, 0xF2, 0xFF),
	colorNameFrame:                   rgba(0x00, 0x00, 0x00, 0xFF),
}

// appTheme gives the windows their colors, sizes and the host's interface
// font. The controls themselves are drawn by this package.
type appTheme struct {
	fyne.Theme
	regular, bold fyne.Resource
}

func newTheme() fyne.Theme {
	t := &appTheme{Theme: theme.DefaultTheme()}
	t.regular, t.bold = systemFont()
	return t
}

// Color ignores the system variant: the windows are dark by design.
func (t *appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if c, ok := colors[name]; ok {
		return c
	}
	return t.Theme.Color(name, theme.VariantDark)
}

func (t *appTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 20
	case theme.SizeNameSubHeadingText:
		return 15
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 8
	case theme.SizeNameScrollBarSmall:
		return 4
	default:
		return t.Theme.Size(name)
	}
}

func (t *appTheme) Font(style fyne.TextStyle) fyne.Resource {
	switch {
	case t.regular == nil || style.Monospace || style.Symbol:
		return t.Theme.Font(style)
	case style.Bold && t.bold != nil:
		return t.bold
	default:
		return t.regular
	}
}

// systemFont loads the interface font of the host, which is most of what makes
// a window look native. Only Windows keeps it in plain TrueType files.
func systemFont() (regular, bold fyne.Resource) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	dir := filepath.Join(os.Getenv("SystemRoot"), "Fonts")
	return fontFile(dir, "segoeui.ttf"), fontFile(dir, "segoeuib.ttf")
}

func fontFile(dir, name string) fyne.Resource {
	data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a fixed file in the system font directory
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource(name, data)
}

func rgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: a} }

func nrgba(c color.Color) color.NRGBA { return color.NRGBAModel.Convert(c).(color.NRGBA) }

// withAlpha scales the opacity of a color.
func withAlpha(c color.Color, alpha float32) color.Color {
	n := nrgba(c)
	n.A = uint8(float32(n.A)*alpha + 0.5)
	return n
}

// darken moves a color towards black by a fraction of the way.
func darken(c color.Color, amount float32) color.Color {
	n := nrgba(c)
	keep := 1 - amount
	n.R = uint8(float32(n.R)*keep + 0.5)
	n.G = uint8(float32(n.G)*keep + 0.5)
	n.B = uint8(float32(n.B)*keep + 0.5)
	return n
}

// blend mixes two colors; t is 0 for the first and 1 for the second.
func blend(from, to color.Color, t float32) color.Color {
	a, b := nrgba(from), nrgba(to)
	mix := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: mix(a.A, b.A)}
}
