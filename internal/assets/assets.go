// Package assets holds the files compiled into the binary.
package assets

import "embed"

// IconPNG is the application icon.
//
//go:embed icon.png
var IconPNG []byte

// Tray holds the notification-area icon drawn for each size in pixels, as
// tray/<size>.png; tools/icons draws them.
//
//go:embed tray
var Tray embed.FS
