package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
	"github.com/zakonnic/kbswitcher/internal/singleinstance"
)

func TestTheLogGoesToAFileInTheCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	log, closeLog := newLogger(true)
	log.Debug("hello")
	closeLog()
	data, err := os.ReadFile(filepath.Join(dir, buildinfo.Name, buildinfo.Name+".log"))
	if err != nil || !strings.Contains(string(data), "hello") {
		t.Errorf("the log file holds %q, %v", data, err)
	}
}

func TestTheLogNeedsNoFile(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	log, closeLog := newLogger(false)
	defer closeLog()
	if log == nil {
		t.Fatal("no logger")
	}
	log.Info("still logged to standard error")
}

func TestBestEffortSwallowsErrors(t *testing.T) {
	n, err := bestEffort{failing{}}.Write([]byte("abc"))
	if n != 3 || err != nil {
		t.Errorf("Write = %d, %v", n, err)
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("no console") }

func TestASecondCopyExitsQuietly(t *testing.T) {
	// Should the lock ever let a second copy through, it must not reach the
	// user's log, configuration or tray.
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	unlock, err := singleinstance.Lock()
	if errors.Is(err, singleinstance.ErrAlreadyRunning) {
		unlock = func() {} // the user's own copy holds it: as good
	} else if err != nil {
		t.Skipf("no instance lock: %v", err)
	}
	defer unlock()
	if code := run(false); code != 0 {
		t.Errorf("run = %d, want 0", code)
	}
}
