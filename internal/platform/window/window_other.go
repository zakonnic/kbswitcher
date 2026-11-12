//go:build !windows

// Package window gives frameless windows what a title bar normally provides:
// rounded corners, a border and a way to move them.
package window

import "image/color"

// Frameless reports whether windows on this platform are drawn without the
// system title bar. Elsewhere they keep it, so nothing below is ever called.
const Frameless = false

func Frame(uintptr, color.Color) (int, error) { return 0, nil }

func Drag(uintptr) {}
