//go:build !windows

package console

import "errors"

// ErrNotConsole means the focused window is not a console. Only Windows has
// the API this package needs, so elsewhere nothing ever is.
var ErrNotConsole = errors.New("reading a console is only possible on Windows")

// Session is never created on this platform.
type Session struct{}

// Open always fails away from Windows.
func Open() (*Session, error) { return nil, ErrNotConsole }

// Edit is never reached on this platform.
func (s *Session) Edit(bool, func(Line) (int, int, string)) error { return ErrNotConsole }

// Close has nothing to release.
func (s *Session) Close() error { return nil }
