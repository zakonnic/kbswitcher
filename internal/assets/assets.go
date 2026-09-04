// Package assets holds the files compiled into the binary.
package assets

import _ "embed"

// IconPNG is the tray icon.
//
//go:embed icon.png
var IconPNG []byte
