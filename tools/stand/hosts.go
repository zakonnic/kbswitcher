//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zakonnic/kbswitcher/internal/platform/field"
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
	if len(h.owned) == 0 {
		h.clear() // a shell that is not the stand's own gets its line back empty
	}
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
  "chat.disableAIFeatures": true,
  "editor.autoClosingBrackets": "never",
  "editor.autoClosingQuotes": "never",
  "editor.autoSurround": "never"
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
	// wipe empties the field where Ctrl+A does not select all.
	wipe func()
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
	if h.wipe != nil {
		h.wipe()
		return
	}
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

const edgeExe = `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`

// launchBrowser opens editors.html, next to this file, in an Edge app window
// with a profile of its own. ProseMirror stands for the script-driven editors
// (Claude, ChatGPT) whose selection lags the browser's; the page mirrors the
// editor's text into the window title.
func launchBrowser(profile, editor string, load int) *editorHost {
	_, self, _, _ := runtime.Caller(0)
	page := fmt.Sprintf("file:///%s?editor=%s&load=%d", filepath.ToSlash(filepath.Join(filepath.Dir(self), "editors.html")), editor, load)
	check(os.MkdirAll(profile, 0o700), "edge profile")
	before := topLevel("Chrome_WidgetWin_1", 0)
	createProcess(fmt.Sprintf(`"%s" --user-data-dir="%s" --no-first-run --no-default-browser-check --app=%s`, edgeExe, profile, page), 0)
	var hwnd uintptr
	deadline := time.Now().Add(20 * time.Second)
	for hwnd == 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		for h := range topLevel("Chrome_WidgetWin_1", 0) {
			if !before[h] && strings.HasPrefix(windowText(h), "stand:") {
				hwnd = h
			}
		}
	}
	if hwnd == 0 {
		fatal("the %s page did not load", editor)
	}
	return &editorHost{hwnd: hwnd,
		read: func() (string, error) {
			title, ok := strings.CutPrefix(windowText(hwnd), "stand:")
			if !ok {
				return "", fmt.Errorf("the page title is %q", windowText(hwnd))
			}
			return strings.ReplaceAll(strings.TrimSuffix(title, "¶"), "⏎", "\n"), nil
		},
		cleanup: func() { _, _, _ = procPostMessage.Call(hwnd, 0x0010, 0, 0) }, // WM_CLOSE
	}
}

// launchNotepad opens a fresh Notepad window or tab and works in its editor
// only: Windows 11 Notepad restores the tabs of the last session, the user's
// unsaved text among them. The editor is emptied by message, never by keys
// that would reach whichever tab has the focus, and its tab is closed only
// while the focus is in it.
func launchNotepad() *editorHost {
	before := topLevel("Notepad", 0)
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
			if c != 0 && controlText(c) == "" && (!before[h] || h == fg()) {
				hwnd, edit = h, c
			}
		}
	}
	if edit == 0 {
		fatal("Notepad shows no empty edit control")
	}
	guardFocus = edit
	wipe := func() { setControlText(edit, "") }
	return &editorHost{hwnd: hwnd, read: func() (string, error) { return controlText(edit), nil }, wipe: wipe,
		cleanup: func() {
			wipe()
			if forceForeground(hwnd) && focusedControl(hwnd) == edit && controlText(edit) == "" {
				chord('W', vkControl) // an empty tab closes without asking
			}
		}}
}

// findBox moves to Notepad's search box, a XAML text box of Windows App SDK,
// and reads it through UI Automation.
func findBox(notepad *editorHost) *editorHost {
	if !forceForeground(notepad.hwnd) {
		fatal("Notepad cannot be brought to the front")
	}
	guard, guardFocus = notepad.hwnd, 0
	chord('F', vkControl)
	time.Sleep(700 * time.Millisecond)
	read := func() (string, error) {
		a, err := field.Read(4000, 4000)
		return a.Before + a.Selected + a.After, err
	}
	if _, err := read(); err != nil {
		fatal("the search box cannot be read: %v", err)
	}
	return &editorHost{hwnd: notepad.hwnd, read: read,
		wipe: func() {
			chord('A', vkControl)
			chord(vkDelete)
		},
		cleanup: func() {
			chord('A', vkControl)
			chord(vkDelete)
			chord(vkEscape)
			notepad.close()
		}}
}

// attachShell works in a terminal that is already open, such as the one of a
// JetBrains IDE: the window is the first one up the shell's process tree. A
// click puts the focus in the terminal; nothing is closed afterwards.
func attachShell(pid uint32, shell, click string) *shellHost {
	all := processes()
	var hwnd uintptr
	for at := all[pid].parent; at != 0 && hwnd == 0; at = all[at].parent {
		for h := range topLevelOf(at) {
			if windowText(h) != "" {
				hwnd = h
			}
		}
	}
	if hwnd == 0 {
		fatal("no window above process %d", pid)
	}
	if !forceForeground(hwnd) {
		fatal("the window of process %d cannot be brought to the front", pid)
	}
	guard = hwnd
	if click != "" {
		var x, y int
		if _, err := fmt.Sscanf(click, "%d,%d", &x, &y); err != nil {
			fatal("-click wants x,y: %v", err)
		}
		clickAt(hwnd, x, y)
		time.Sleep(300 * time.Millisecond)
	}
	return &shellHost{hwnd: hwnd, shell: pid, unixy: isUnixy(shell)}
}

// openInIDE opens a scratch file in a running JetBrains IDE through its own
// launcher and reads the file back after asking the IDE to save it. shell is
// a terminal of the same IDE that must stay as it is meanwhile.
func openInIDE(launcher, file, click string) *editorHost {
	// The file is never written from outside once the IDE has it open: a
	// reload in the middle of typing moves the caret.
	if launcher != "" { // without one, the file is expected to be open already
		check(os.WriteFile(file, nil, 0o600), "scratch file")
		createProcess(fmt.Sprintf(`"%s" "%s"`, launcher, file), 0)
	}
	var hwnd uintptr
	deadline := time.Now().Add(20 * time.Second)
	for hwnd == 0 && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		for h := range topLevel("SunAwtFrame", 0) {
			if strings.Contains(windowText(h), filepath.Base(file)) {
				hwnd = h
			}
		}
	}
	if hwnd == 0 {
		fatal("no IDE window shows %s", file)
	}
	if !forceForeground(hwnd) {
		fatal("the IDE cannot be brought to the front")
	}
	guard = hwnd
	if click != "" {
		var x, y int
		if _, err := fmt.Sscanf(click, "%d,%d", &x, &y); err != nil {
			fatal("-click wants x,y: %v", err)
		}
		clickAt(hwnd, x, y)
		time.Sleep(300 * time.Millisecond)
	}
	var saved time.Time
	return &editorHost{hwnd: hwnd,
		read: func() (string, error) {
			if time.Since(saved) > 200*time.Millisecond {
				chord('S', vkControl)
				saved = time.Now()
				time.Sleep(60 * time.Millisecond)
			}
			data, err := os.ReadFile(file)
			return string(data), err
		},
		cleanup: func() {
			chord('A', vkControl)
			chord(vkDelete)
			chord('S', vkControl)
			chord(0x73, vkControl) // Ctrl+F4 closes the tab
			_ = os.Remove(file)
		}}
}
