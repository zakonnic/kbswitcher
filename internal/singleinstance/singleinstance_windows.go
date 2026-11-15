package singleinstance

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

// lockName names the mutex. A test takes a name of its own, so that it can
// run while kbswitcher does.
var lockName = buildinfo.ID

// Lock claims a named mutex that lives as long as the process. The returned
// function releases it and may be called more than once.
func Lock() (release func(), err error) {
	name, err := windows.UTF16PtrFromString(`Local\` + lockName)
	if err != nil {
		return noop, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return noop, ErrAlreadyRunning
	}
	if err != nil {
		return noop, fmt.Errorf("cannot create the instance mutex: %w", err)
	}
	return sync.OnceFunc(func() { _ = windows.CloseHandle(handle) }), nil
}
