// Package singleinstance keeps a second copy of the program from running next
// to the first one, which would otherwise fight over the hotkeys and show a
// second tray icon.
package singleinstance

import "errors"

// ErrAlreadyRunning means another instance holds the lock.
var ErrAlreadyRunning = errors.New("another instance is already running")

func noop() {}
