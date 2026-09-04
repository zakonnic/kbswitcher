// Command kbswitcher rewrites text that was typed with the wrong keyboard
// layout. It lives in the system tray and wakes up only for its two hotkeys;
// see internal/platform/hotkey for why it cannot observe anything else.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zakonnic/kbswitcher/internal/app"
	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/singleinstance"
	"github.com/zakonnic/kbswitcher/internal/updater"
)

func main() {
	// The GUI toolkit and, on macOS, the hotkey registration need the thread
	// the process started on.
	runtime.LockOSThread()

	verbose := flag.Bool("verbose", false, "log every conversion in detail")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println(buildinfo.String())
		return
	}
	os.Exit(run(*verbose))
}

func run(verbose bool) int {
	unlock, lockErr := singleinstance.Lock()
	if errors.Is(lockErr, singleinstance.ErrAlreadyRunning) {
		fmt.Fprintf(os.Stderr, "%s is already running\n", buildinfo.Name)
		return 0
	}
	defer unlock()

	log, closeLog := newLogger(verbose)
	defer closeLog()
	if lockErr != nil {
		log.Warn("a second instance cannot be detected", "error", lockErr)
	}

	updater.CleanUp()
	log.Info("starting", "version", buildinfo.Version, "commit", buildinfo.Commit)

	application, err := app.New(log)
	if err != nil {
		log.Error("cannot start", "error", err)
		fmt.Fprintf(os.Stderr, "%s: %v\n", buildinfo.Name, err)
		return 1
	}

	restart := application.Run()
	if err := application.Close(); err != nil {
		log.Warn("shutdown was not clean", "error", err)
	}
	log.Info("stopped")
	if !restart {
		return 0
	}

	// The new copy must find the hotkeys and the instance lock released.
	unlock()
	if err := updater.Relaunch(); err != nil {
		log.Error("cannot restart", "error", err)
		return 1
	}
	return 0
}

// newLogger writes to standard error and, when possible, to a file in the user
// cache directory that is rewritten at every start.
func newLogger(verbose bool) (*slog.Logger, func()) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	options := &slog.HandlerOptions{Level: level}

	file, err := openLogFile()
	if err != nil {
		logger := slog.New(slog.NewTextHandler(os.Stderr, options))
		logger.Warn("no log file", "error", err)
		return logger, func() {}
	}

	logger := slog.New(slog.NewTextHandler(io.MultiWriter(bestEffort{os.Stderr}, file), options))
	return logger, func() { _ = file.Close() }
}

// bestEffort discards write errors: a GUI process on Windows has no usable
// standard error, and its failure must not keep the log file from being
// written.
type bestEffort struct{ io.Writer }

func (w bestEffort) Write(p []byte) (int, error) {
	_, _ = w.Writer.Write(p)
	return len(p), nil
}

func openLogFile() (*os.File, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, buildinfo.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, buildinfo.Name+".log")
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // constant path under the user cache directory
}
