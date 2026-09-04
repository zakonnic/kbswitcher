//go:build !windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

// Lock takes an advisory lock on a file in the user cache directory. The kernel
// drops the lock when the process ends, so a crash cannot leave it behind. The
// returned function releases it and may be called more than once.
func Lock() (release func(), err error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return noop, err
	}
	dir = filepath.Join(dir, buildinfo.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return noop, err
	}

	path := filepath.Join(dir, buildinfo.Name+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // constant path under the user cache directory
	if err != nil {
		return noop, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return noop, ErrAlreadyRunning
		}
		return noop, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return sync.OnceFunc(func() { _ = file.Close() }), nil
}
