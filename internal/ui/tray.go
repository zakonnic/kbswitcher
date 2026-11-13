package ui

import (
	"bytes"
	"image"
	"image/png"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	xdraw "golang.org/x/image/draw"

	"github.com/zakonnic/kbswitcher/internal/assets"
	"github.com/zakonnic/kbswitcher/internal/platform/window"
)

// trayIcon is the tray drawing for the size the notification area shows.
// Windows loads a tray icon at the large icon size and shrinks it to the small
// one, so the drawing is enlarged pixel for pixel to the large size, which the
// shrinking undoes exactly. Elsewhere the tray gets the application icon.
func trayIcon() fyne.Resource {
	small, large := window.IconSizes()
	if small == 0 {
		return icon()
	}
	src, err := trayImage(small)
	if err != nil {
		return icon()
	}
	dst := image.NewNRGBA(image.Rect(0, 0, large, large))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return icon()
	}
	return fyne.NewStaticResource("tray.png", buf.Bytes())
}

// trayImage decodes the tray drawing closest in size to size.
func trayImage(size int) (image.Image, error) {
	entries, err := assets.Tray.ReadDir("tray")
	if err != nil {
		return nil, err
	}
	best, bestSize := "", 0
	for _, e := range entries {
		n, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".png"))
		if err == nil && (best == "" || max(n-size, size-n) < max(bestSize-size, size-bestSize)) {
			best, bestSize = e.Name(), n
		}
	}
	data, err := assets.Tray.ReadFile("tray/" + best)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}
