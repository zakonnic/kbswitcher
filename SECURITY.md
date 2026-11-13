# Security notes

This document is written for a reviewer. It describes what kbswitcher is able to
do, what it deliberately cannot do, and where to look in the code to check each
claim.

## What the program is

A tray utility that rewrites the last word, or the selection, as if it had been
typed with a different keyboard layout. It has no server, no account, no
telemetry and no background activity: between two hotkey presses the process
sleeps.

## It does not watch the keyboard

This is the claim worth checking first, because a tool of this kind could
plausibly be a keylogger, and this one is built so that it cannot be.

kbswitcher registers **two key combinations** with the operating system, the
hotkey and the same with the selection modifier, and is woken up only when one
of them is pressed:

| Platform | Call                                | File                                       |
|----------|-------------------------------------|--------------------------------------------|
| macOS    | `RegisterEventHotKey`               | `internal/platform/hotkey/hotkey_darwin.c` |
| Windows  | `RegisterHotKey` + a message loop   | `internal/platform/hotkey/hotkey_windows.go` |
| Linux    | `XGrabKey` on the root window       | `internal/platform/hotkey/hotkey_linux.c`  |

None of these give the process access to any other keystroke. At a hotkey
press kbswitcher asks which modifier keys are held at that moment, so as to
release them before it presses its own shortcuts: `GetAsyncKeyState` for the
eight modifier keys on Windows (`internal/platform/input/input_windows.go`),
`XkbGetState` on X11. It asks for nothing else and keeps nothing. In particular
kbswitcher does **not** install a `CGEventTap` (macOS), a `WH_KEYBOARD_LL` hook
(Windows) or an `XRecord` context (Linux) — the three APIs that would let it see
everything typed. `grep` the sources for them: there are no matches. There is
no setting that turns such a hook on; remembering keystrokes is out of scope by
design.

The Settings window records a new hotkey from the keys pressed while its
field has the focus, as any text field receives keys typed into it. The keys
are held in memory only until they are released, and kept only as the
configured combination; the hotkeys are released while the field listens, so
that the current ones reach it.

## What it reads, and when

Only on a hotkey press. In an ordinary application it goes through the
clipboard:

1. the current clipboard content is copied aside, so it can be given back
   (`internal/platform/clip`);
2. a random marker is put on the clipboard, so that a copy that does not happen
   cannot be mistaken for one that did;
3. the selection is extended and copied with synthetic key presses — the same
   shortcuts a user would press (`internal/platform/input`);
4. the text is converted in memory (`internal/keymap`, no I/O at all);
5. for the last word, the selection is narrowed down to the word and copied
   again: nothing is pasted until the application shows exactly that word
   selected;
6. the result is pasted and the original clipboard content restored.

The whole sequence lives in `internal/switcher/switcher.go` and takes about
100 ms. Text is never written to disk, never sent anywhere, and never logged:
the log records how many characters were converted and between which layouts,
not what they were, and no window title or console content either.

On Windows everything kbswitcher writes to the clipboard (the marker, the
converted text, the restored content) carries the formats
`ExcludeClipboardContentFromMonitorProcessing`, `CanIncludeInClipboardHistory`
and `CanUploadToCloudClipboard`, so clipboard history and cloud sync skip it
(`internal/platform/clip/clip_windows.go`). The copies the application itself
makes of the selected text (two for the last word: one to read it, one to
check the narrowed selection) are the application's, and are not marked: with
clipboard history on, they appear in it. A program that reads every clipboard
change regardless, as a remote desktop client or WSLg does, still sees what
passes through.

## It reads consoles through the Windows console API

A terminal answers `Ctrl+C` by cancelling the line, not by copying, so on
Windows the clipboard path cannot work there. Instead, **on a hotkey press
only**, kbswitcher does this (`internal/platform/console`):

1. it looks at the foreground window's class, and stops unless it is a console
   window, Windows Terminal, or a Chromium window whose process runs a shell
   (VS Code and other editors built on xterm.js);
2. it walks that window's process tree and calls `AttachConsole` on each
   process to find the consoles behind the window, noting each one's size and
   caret position, and for Windows Terminal its caret row and the three rows
   above it;
3. when there are several, or the window is an editor, it asks UI Automation
   which element has the keyboard focus: Windows Terminal reports the rows
   around its caret, which are compared with those of each console, an
   xterm.js terminal the position of its caret cell in its grid. Exactly one
   console must match, or nothing is touched; an editor whose focus is not on
   a terminal takes the clipboard path
   (`internal/platform/console/uia_windows.go`);
4. it writes a Home key (and End, for a selection) into that console's input
   buffer with `WriteConsoleInputW` and reads the cells between the places the
   shell puts the caret with `ReadConsoleOutputCharacterW` — the input line the
   shell is already showing, nothing scrolled back — together with the last
   16 cells of the prompt, which the result is checked against;
5. it writes arrows, backspaces and the converted characters the same way, and
   watches the line until it shows the result
   (`internal/platform/console/edit_windows.go`);
6. it detaches with `FreeConsole` before returning.

While attached it sets `SetConsoleCtrlHandler(NULL, TRUE)`, so a `Ctrl+C` in
the borrowed console cannot reach kbswitcher. The attachment lasts for the
length of one conversion, a few dozen milliseconds. Nothing is read between
hotkey presses. An elevated console refuses the attachment, and kbswitcher
does nothing there rather than ask for elevation. A shell that has not taken
the key events written to it within a second has its input buffer flushed, so
that they cannot reach whatever reads the console next; a key the user typed
in that second goes with them.

The first UI Automation question makes a Chromium application build its
accessibility tree, as any assistive tool does. kbswitcher asks only for the
focused element's class name, process and rectangle, the class names of the
few elements above it, and for Windows Terminal the rows around the caret; it
never reads the content of an editor that way.

## Permissions it asks for

- **macOS**: Accessibility, which is what posting key events requires. Nothing
  else — no Input Monitoring, no Full Disk Access, no camera or microphone.
- **Windows**: none. `SendInput` and `RegisterHotKey` are unprivileged.
- **Linux**: a connection to the X server, and the XTEST extension.

Nothing is installed system-wide. The autostart entry is written to the current
user's own launch agents, `HKEY_CURRENT_USER` Run key or XDG autostart folder,
and never to a machine-wide location (`internal/autostart`).

## Files it touches

| Path                                                | Purpose                       |
|-----------------------------------------------------|-------------------------------|
| `<user config>/kbswitcher/config.json`               | five settings, mode `0600`    |
| `<user cache>/kbswitcher/kbswitcher.log`             | the current run's log, `0600` |
| `<user cache>/kbswitcher/kbswitcher.lock`            | the instance lock on macOS and Linux (a named mutex, `Local\com.zakonnic.kbswitcher`, on Windows) |
| the autostart entry, when the user ticks the box     | see above                     |
| the program itself, when the user installs an update | see below                     |
| `<user config>/fyne/com.zakonnic.kbswitcher/preferences.json` | the GUI toolkit's preferences: the date of the last installed update |

The GUI toolkit also reads its own `<user config>/fyne/settings.json` when it
exists. Nothing else is created, read or deleted.

## Network access

There is exactly one network call, and only when the user presses **Check for
updates** in the About window. Nothing is contacted at start-up or on a timer.

`internal/updater` enforces:

- HTTPS only, including through redirects, which are refused if they leave
  HTTPS;
- one host for the release metadata, the one built into the binary
  (`buildinfo.APIBaseURL`, GitHub by default, overridable at build time for an
  internal instance); the archive and `checksums.txt` are fetched from the
  addresses that metadata names, which on GitHub redirect to its download
  hosts;
- one repository, `buildinfo.Repository`, a constant that no build flag can
  change;
- size limits on every response (1 MB metadata, 64 KB checksums, 128 MB
  archive);
- a SHA-256 check of the downloaded archive against the `checksums.txt` file
  published in the same release — a mismatch aborts the update;
- extraction of exactly one file, matched by base name, so no archive entry can
  decide where anything is written;
- replacement by writing next to the current program and renaming, so an
  interrupted update cannot leave a half-written binary.

No downloaded code is executed except the new program itself, after the user has
confirmed the update. No installer, no shell, no elevation.

**Trust model.** Integrity rests on TLS to the release host plus the checksum
published alongside the release. Whoever can publish a release can publish a
binary. A deployment that needs a stronger guarantee should distribute
kbswitcher through its own device management and leave the update button unused;
on macOS it should in any case, because replacing the executable inside a signed
application bundle invalidates its signature (the confirmation dialog says so).

## Native code

The macOS and Linux implementations use cgo. The C is kept in separate `.c` and
`.h` files rather than in Go comments so that it reads as C and can be reviewed
as such:

```
internal/platform/hotkey/hotkey_darwin.{c,h}    RegisterEventHotKey
internal/platform/hotkey/hotkey_linux.{c,h}     XGrabKey, waiting for events
internal/platform/layouts/layouts_darwin.{c,h}  reading the input sources
internal/platform/layouts/layouts_linux.{c,h}   reading the XKB keymap
internal/platform/input/input_darwin.{c,h}      posting key events
```

Every one of them is a thin wrapper over a documented system API. There is no
memory allocated in C that Go frees, or the other way round; buffers are
fixed-size structures allocated by the caller, with the limits declared in the
headers.

On Windows kbswitcher's own code has no cgo: the system calls go through
`golang.org/x/sys/windows` and `syscall`. The GUI toolkit does use cgo there,
for GLFW and OpenGL.

`tools/stand`, the developer's test stand, is not part of the program. It
presses keys with `SendInput` and brings windows to the front to drive real
terminals and editors, and is built only by hand.

## Dependencies

| Module                        | Why                                    |
|-------------------------------|----------------------------------------|
| `fyne.io/fyne/v2`             | tray icon, Settings and About windows  |
| `golang.design/x/clipboard`   | the clipboard on macOS and Linux       |
| `golang.org/x/sys`            | the Windows system calls               |

`go.mod` pins every version and `go.sum` records their hashes. `make lint` runs
golangci-lint with `gosec` enabled.

## Reporting a problem

Open an issue in the repository, or contact the team that owns it. Please do not
include the text you were converting when the problem happened.
