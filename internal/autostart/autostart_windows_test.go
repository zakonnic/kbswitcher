package autostart

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

// scratchKey points the Run key at a key of the test's own under the user's
// Software key, and removes it afterwards; the real Run key is not touched.
func scratchKey(t *testing.T) string {
	t.Helper()
	parent := fmt.Sprintf(`Software\kbswitcher-test-%d`, os.Getpid())
	previous := runKeyPath
	runKeyPath = parent + `\Run`
	t.Cleanup(func() {
		runKeyPath = previous
		_ = registry.DeleteKey(registry.CURRENT_USER, parent+`\Run`)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent)
	})
	return runKeyPath
}

func TestEnableAndDisable(t *testing.T) {
	path := scratchKey(t)

	if on, err := Enabled(); on || err != nil {
		t.Fatalf("Enabled before anything = %v, %v", on, err)
	}
	if err := Disable(); err != nil {
		t.Errorf("disabling with no key: %v", err)
	}
	if err := Set(true); err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	if on, err := Enabled(); !on || err != nil {
		t.Errorf("Enabled after Set(true) = %v, %v", on, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := key.GetStringValue(buildinfo.Name)
	_ = key.Close()
	exe, _ := executablePath()
	if err != nil || value != `"`+exe+`"` {
		t.Errorf("the entry is %q, %v; want the quoted test binary %q", value, err, exe)
	}

	if err := Set(false); err != nil {
		t.Fatalf("Set(false): %v", err)
	}
	if on, err := Enabled(); on || err != nil {
		t.Errorf("Enabled after Set(false) = %v, %v", on, err)
	}
	if err := Disable(); err != nil {
		t.Errorf("disabling twice: %v", err)
	}
}

func TestAnEmptyEntryIsNotEnabled(t *testing.T) {
	path := scratchKey(t)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	_ = key.SetStringValue(buildinfo.Name, "  ")
	_ = key.Close()
	if on, err := Enabled(); on || err != nil {
		t.Errorf("Enabled with a blank entry = %v, %v", on, err)
	}
}

func TestExecutablePath(t *testing.T) {
	path, err := executablePath()
	if err != nil || !strings.HasSuffix(strings.ToLower(path), ".exe") {
		t.Errorf("executablePath = %q, %v", path, err)
	}
}
