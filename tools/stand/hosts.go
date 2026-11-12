//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// host is a window whose text the stand types into and reads back.
type host interface {
	window() uintptr
	// line is the text the conversion works on: a shell's input row up to the
	// caret, prompt included, or an editor's last line.
	line() (string, error)
	clear()
	// selectLine selects what a selection conversion should rewrite.
	selectLine()
	close()
}

// shellHost is a shell whose console the stand can read.
type shellHost struct {
	hwnd   uintptr
	shell  uint32
	owned  []uint32
	unixy  bool // bash or WSL: readline keys instead of the Windows ones
	reaper func()
}

func (h *shellHost) window() uintptr       { return h.hwnd }
func (h *shellHost) line() (string, error) { return consoleLine(h.shell) }

// selectWords is how many words back the shell is asked to select.
var selectWords int

func (h *shellHost) selectLine() {
	for range selectWords {
		chord(0x25, vkControl, vkShift) // Ctrl+Shift+Left
	}
}

// clear empties the input line through the console itself: a shortcut sent
// to the window could be taken by the host instead (Ctrl+E is VS Code's Quick
// Open).
func (h *shellHost) clear() {
	if h.unixy {
		writeConsole(h.shell, "\x05\x15") // Ctrl+E, Ctrl+U
		return
	}
	writeConsole(h.shell, "\x1b")
}

func (h *shellHost) close() {
	closeShells(h.owned)
	if h.reaper != nil {
		h.reaper()
	}
}

func isUnixy(shell string) bool {
	exe := strings.ToLower(filepath.Base(strings.Fields(shell)[0]))
	return exe == "bash.exe" || exe == "wsl.exe" || exe == "sh.exe"
}

// launchConhost starts the shell in a console window of its own.
func launchConhost(shell string) *shellHost {
	conhost := createProcess("conhost.exe "+shell, 0x10) // CREATE_NEW_CONSOLE
	exe := filepath.Base(strings.Fields(shell)[0])
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for pid := range descendants(conhost, exe) {
			if ret, _, _ := procAttachConsole.Call(uintptr(pid)); ret != 0 {
				hwnd, _, _ := procGetConsoleWindow.Call()
				_, _, _ = procFreeConsole.Call()
				if hwnd != 0 {
					return &shellHost{hwnd: hwnd, shell: pid, owned: []uint32{pid}, unixy: isUnixy(shell)}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fatal("the console of %d never appeared", conhost)
	return nil
}

// launchWT opens a new Windows Terminal window running the shell and, if asked,
// more tabs of it; the last tab is the one worked in.
func launchWT(shell string, tabs int) *shellHost {
	exe := filepath.Base(strings.Fields(shell)[0])
	wt, err := exec.LookPath("wt.exe")
	check(err, "wt.exe")

	windowsBefore := topLevel("CASCADIA_HOSTING_WINDOW_CLASS", 0)
	var hwnd uintptr
	var pids []uint32
	for i := range tabs {
		before := processesNamed(exe)
		if i == 0 {
			createProcess(fmt.Sprintf(`"%s" -w -1 %s`, wt, shell), 0)
		} else {
			createProcess(fmt.Sprintf(`"%s" -w last nt %s`, wt, shell), 0)
		}
		var fresh []uint32
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			fresh = topmost(newPIDs(before, processesNamed(exe)))
			if hwnd == 0 {
				for h := range topLevel("CASCADIA_HOSTING_WINDOW_CLASS", 0) {
					if !windowsBefore[h] {
						hwnd = h
					}
				}
			}
			if hwnd != 0 && len(fresh) == 1 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if hwnd == 0 || len(fresh) != 1 {
			fatal("Windows Terminal tab %d did not open: hwnd=%#x pids=%v", i+1, hwnd, fresh)
		}
		pids = append(pids, fresh[0])
	}
	time.Sleep(500 * time.Millisecond)
	return &shellHost{hwnd: hwnd, shell: pids[len(pids)-1], owned: pids, unixy: isUnixy(shell)}
}

// topmost drops the processes started by another one of the list, as WSL's
// relay and Git Bash's launcher are.
func topmost(pids []uint32) []uint32 {
	all := processes()
	fresh := make(map[uint32]bool, len(pids))
	for _, pid := range pids {
		fresh[pid] = true
	}
	var top []uint32
	for _, pid := range pids {
		if !fresh[all[pid].parent] {
			top = append(top, pid)
		}
	}
	return top
}

const vscodeExe = `C:\Program Files\Microsoft VS Code\Code.exe`

// vscode is a VS Code instance with a user data directory of its own, so that
// the stand never touches the user's editor.
type vscode struct {
	dir  string
	pid  uint32
	hwnd uintptr
}

var vscodeProfiles = map[string]string{
	"powershell.exe": "PowerShell",
	"cmd.exe":        "Command Prompt",
	"bash.exe":       "Git Bash",
	"wsl.exe":        "Ubuntu (WSL)",
}

func openVSCode(dir, shell string) *vscode {
	exe := filepath.Base(strings.Fields(shell)[0])
	profile, ok := vscodeProfiles[strings.ToLower(exe)]
	if !ok {
		fatal("no VS Code profile for %s", exe)
	}
	for _, sub := range []string{"user/User", "ext", "work"} {
		check(os.MkdirAll(filepath.Join(dir, sub), 0o700), "mkdir")
	}
	settings := fmt.Sprintf(`{
  "workbench.startupEditor": "none",
  "security.workspace.trust.enabled": false,
  "update.mode": "none",
  "telemetry.telemetryLevel": "off",
  "extensions.autoCheckUpdates": false,
  "workbench.tips.enabled": false,
  "window.restoreWindows": "none",
  "workbench.enableExperiments": false,
  "terminal.integrated.enablePersistentSessions": false,
  "terminal.integrated.initialHint": false,
  "terminal.integrated.defaultProfile.windows": %q,
  "files.autoSave": "afterDelay",
  "files.autoSaveDelay": 50,
  "chat.disableAIFeatures": true
}
`, profile)
	check(os.WriteFile(filepath.Join(dir, "user/User/settings.json"), []byte(settings), 0o600), "settings")

	v := &vscode{dir: dir}
	if data, err := os.ReadFile(filepath.Join(dir, "main.pid")); err == nil {
		var pid uint32
		fmt.Sscan(string(data), &pid)
		if alive(pid) && strings.EqualFold(processes()[pid].exe, "Code.exe") {
			v.pid = pid
		}
	}
	if v.pid == 0 {
		v.pid = createProcess(fmt.Sprintf(`"%s" --user-data-dir "%s" --extensions-dir "%s" --new-window --disable-workspace-trust "%s"`,
			vscodeExe, filepath.Join(dir, "user"), filepath.Join(dir, "ext"), filepath.Join(dir, "work")), 0)
		check(os.WriteFile(filepath.Join(dir, "main.pid"), []byte(fmt.Sprint(v.pid)), 0o600), "main.pid")
	}
	deadline := time.Now().Add(30 * time.Second)
	for v.hwnd == 0 && time.Now().Before(deadline) {
		for h := range topLevel("Chrome_WidgetWin_1", v.pid) {
			if strings.Contains(windowText(h), "Visual Studio Code") {
				v.hwnd = h
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if v.hwnd == 0 {
		fatal("VS Code %d shows no window", v.pid)
	}
	if !forceForeground(v.hwnd) {
		fatal("VS Code cannot be brought to the foreground")
	}
	guard = v.hwnd
	time.Sleep(500 * time.Millisecond) // the settings file is watched, give it a moment
	return v
}

// newTerminal opens a terminal of the default profile and returns its shell.
func (v *vscode) newTerminal(shell string) *shellHost {
	exe := filepath.Base(strings.Fields(shell)[0])
	before := descendants(v.pid, exe)
	var fresh []uint32
	// A workbench that is still loading ignores the shortcut, so it is
	// repeated until a shell appears.
	for range 5 {
		forceForeground(v.hwnd)
		chord(0xC0, vkControl, vkShift) // Ctrl+Shift+`, a new terminal
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if fresh = topmost(newPIDs(before, descendants(v.pid, exe))); len(fresh) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if len(fresh) > 0 {
			break
		}
	}
	if len(fresh) != 1 {
		fatal("VS Code opened no %s terminal: %v", exe, fresh)
	}
	return &shellHost{hwnd: v.hwnd, shell: fresh[0], owned: fresh, unixy: isUnixy(shell)}
}

// editorHost is a text field whose content the stand reads back: Notepad's
// edit control, or a file VS Code saves on every change.
type editorHost struct {
	hwnd    uintptr
	read    func() (string, error)
	cleanup func()
}

func (h *editorHost) window() uintptr { return h.hwnd }

func (h *editorHost) line() (string, error) {
	text, err := h.read()
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return text, err
}

func (h *editorHost) clear() {
	chord('A', vkControl)
	chord(vkDelete)
}

func (h *editorHost) selectLine() { chord(0x24, vkShift) } // Shift+Home

func (h *editorHost) close() {
	if h.cleanup != nil {
		h.cleanup()
	}
}

func (v *vscode) editor() *editorHost {
	file := filepath.Join(v.dir, "work", "stand.txt")
	check(os.WriteFile(file, nil, 0o600), "stand.txt")
	createProcess(fmt.Sprintf(`"%s" --user-data-dir "%s" --extensions-dir "%s" -r "%s"`,
		vscodeExe, filepath.Join(v.dir, "user"), filepath.Join(v.dir, "ext"), file), 0)
	time.Sleep(1500 * time.Millisecond)
	forceForeground(v.hwnd)
	chord('1', vkControl) // focus the first editor group
	return &editorHost{hwnd: v.hwnd, read: func() (string, error) {
		data, err := os.ReadFile(file)
		return string(data), err
	}}
}

// launchNotepad opens a fresh Notepad tab (Windows 11 Notepad adds it to a
// window already open) and closes only that tab afterwards.
func launchNotepad() *editorHost {
	createProcess("notepad.exe", 0)
	var hwnd, edit uintptr
	deadline := time.Now().Add(10 * time.Second)
	for edit == 0 && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		for h := range topLevel("Notepad", 0) {
			c := childOfClass(h, "RichEdit")
			if c == 0 {
				c = childOfClass(h, "Edit")
			}
			if c != 0 && controlText(c) == "" {
				hwnd, edit = h, c
			}
		}
	}
	if edit == 0 {
		fatal("Notepad shows no empty edit control")
	}
	return &editorHost{hwnd: hwnd, read: func() (string, error) { return controlText(edit), nil },
		cleanup: func() {
			if !forceForeground(hwnd) {
				return
			}
			chord('A', vkControl)
			chord(vkDelete)
			chord('W', vkControl) // an empty tab closes without asking
		}}
}
