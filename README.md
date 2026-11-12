# kbswitcher

Rewrites text that was typed with the wrong keyboard layout.

Type `ckjdj` when you meant `слово`, press the hotkey, and the word is replaced
with what the same keys would have produced in the next layout. Press it again
to move on to the layout after that: with three layouts installed, three presses
bring the text back to what you typed.

```
ckjdj   →  слово
штзге   →  input
;bpym   →  жизнь
```

- **Break** (**F12** on a Mac) rewrites the word before the caret and switches
  the keyboard to the layout that word now belongs to, so you can keep typing.
  The switch can be turned off in Settings.
- **Shift+Break** (**Shift+F12** on a Mac) rewrites the selected text and leaves
  the keyboard layout alone. In a Windows terminal it rewrites what the shell
  shows as selected, or the whole input line when nothing is.

Both are configurable. kbswitcher lives in the system tray, and runs on macOS,
Windows and Linux.

## How it works

kbswitcher does **not** watch what you type. It asks the operating system to
notify it about one key combination — `RegisterEventHotKey` on macOS,
`RegisterHotKey` on Windows, `XGrabKey` on X11 — and it is woken up only when
that combination is pressed. No other keystroke ever reaches the process.

When the hotkey fires, it does what you would do by hand:

1. extend the selection to cover the last word (skipped for the selection
   hotkey — you have already selected the text);
2. copy;
3. work out which layout the text was typed in, and rewrite it for the next one;
4. paste the result and give your clipboard back.

On Windows, what kbswitcher puts on the clipboard along the way is marked so
that clipboard history (Win+V) and cloud clipboard sync leave it out, and the
clipboard is given back the moment the application has read the paste.

The layouts themselves are read from the operating system at start-up, from the
same tables it uses to turn key presses into characters: `UCKeyTranslate` on
macOS, `ToUnicodeEx` on Windows, the XKB keymap on X11. Any layout you have
installed works — Dvorak, AZERTY, national variants, ISO and JIS keyboards —
without kbswitcher knowing anything about it in advance.

## Installing

Download the archive for your platform from the
[releases](https://github.com/zakonnic/kbswitcher/releases) page, or build it
yourself:

```bash
make build          # builds into bin/
make bundle         # macOS: wraps the binary in bin/kbswitcher.app
```

Build requirements:

| Platform | Needs                                                                     |
|----------|---------------------------------------------------------------------------|
| macOS    | Xcode command line tools                                                  |
| Windows  | a C compiler (MinGW-w64 or the MSVC build tools), for the GUI toolkit      |
| Linux    | `libx11-dev`, `libxtst-dev`, `libxkbcommon-dev`, `libgl1-mesa-dev`, `xorg-dev` |

### macOS

kbswitcher replaces text by pressing keys on your behalf, which macOS guards
with the **Accessibility** permission. The first start asks for it; grant it in
*System Settings → Privacy & Security → Accessibility*, then restart the app.
Until then the hotkeys do nothing.

On a MacBook, `F12` only reaches applications when *Use F1, F2, etc. keys as
standard function keys* is on, or when `fn` is held. If that does not suit you,
pick another key in Settings.

### Linux

kbswitcher needs an X11 session; under Wayland it works only for applications
running through XWayland, because Wayland deliberately offers no way to grab a
global hotkey or to type into another application's window.

## Settings

Right-click the tray icon:

- **About** — version, the layouts that were found and the order they are cycled
  in, and a button that checks for a newer release and installs it.
- **Settings** — three options: whether kbswitcher starts with your session,
  which key (with optional Ctrl, Alt or Cmd/Win) triggers it, and whether
  converting a word also switches the keyboard layout. To change the key, click
  the Hotkey field and press the key; the hotkey is released while the field
  listens, so the current key can be pressed there too. Shift is reserved: it is
  what turns the hotkey into its "convert the selection" form. Save puts the
  settings in force at once; Cancel discards the changes.
- **Quit**.

Only one copy runs at a time: starting kbswitcher while it is already running
exits quietly, so an autostart entry and a manual launch never fight over the
hotkeys.

The configuration is a small JSON file:

| Platform | Location                                                       |
|----------|----------------------------------------------------------------|
| macOS    | `~/Library/Application Support/kbswitcher/config.json`         |
| Windows  | `%AppData%\kbswitcher\config.json`                             |
| Linux    | `~/.config/kbswitcher/config.json`                             |

A log of the current run is written next to it, in the user cache directory
(`~/Library/Caches/kbswitcher/kbswitcher.log`, `%LocalAppData%\kbswitcher\`,
`~/.cache/kbswitcher/`). It is rewritten at every start. Run with `-verbose` to
log every conversion.

## What it cannot do

- **Terminals kbswitcher cannot read.** On Windows a console is rewritten
  directly rather than through the clipboard, which covers the classic console
  window, Windows Terminal, the terminal pane of VS Code and other editors
  built on xterm.js, and the shells inside them: PowerShell, cmd, Git Bash, WSL.
  Git Bash in its own window (mintty) has no Windows console at all, and there
  the hotkey does nothing. A JetBrains IDE does not say whether its terminal
  pane has the focus, so the hotkey takes the clipboard path there, which a
  terminal pane does not answer. Two terminals of one editor that are the same
  size and have their carets on the same cell cannot be told apart, and neither
  is touched. On macOS and Linux every terminal takes the clipboard path.
- **Selections in a terminal.** A selection made with the mouse belongs to the
  terminal window and no shell knows of it, so `Shift`+hotkey converts what the
  shell itself shows as selected (`Shift`+arrows in PowerShell), and the whole
  input line otherwise.
- **Formatting.** The replacement is pasted as plain text. In a rich text editor
  the word that was replaced loses its formatting; the rest of the line does
  not, because the selection is narrowed to the word before pasting.
- **Characters that need AltGr.** Only unshifted and shifted key presses are
  reproduced. Anything typed with a third or fourth level is copied through
  unchanged.
- **Clipboard formats other than text, rich text, images and file lists** are
  not restored after a conversion, and content larger than 8 MB is left behind
  rather than copied aside.

## Project layout

```
cmd/kbswitcher      the program itself: flags, logging, start-up
internal/keymap     the conversion core — no operating system anywhere in it
internal/switcher   what a hotkey press does, start to finish
internal/platform   everything the operating system provides:
    hotkey            registering a system-wide key combination
    input             pressing keys in the focused window
    console           reading and rewriting a Windows console input line
    layouts           reading the installed layouts and their characters
    clip              borrowing and giving back the clipboard
internal/config     the settings file
internal/autostart  the "start with the session" entry
internal/singleinstance  the lock that keeps a second copy from running
internal/updater    checking for and installing a release
internal/ui         tray icon, Settings and About
```

The conversion core has no dependency on any operating system and is covered by
tests, including a simulated text field that exercises select-copy-convert-paste
end to end. `make check` formats, vets and runs everything.

## Security

`SECURITY.md` describes what kbswitcher can reach, what it deliberately cannot,
and the trust model of the update button. It is the place to start for a review.

Third-party dependencies: [Fyne](https://fyne.io) for the tray icon and the two
windows, [golang.design/x/clipboard](https://github.com/golang-design/clipboard)
for the clipboard on macOS and Linux, and `golang.org/x/sys` for the Windows
system calls.

`tools/stand` is a Windows test stand: it opens a real console, Windows
Terminal, an isolated VS Code or Notepad, types a word, converts it and checks
what the window shows. `go run ./tools/stand -help` lists its options.
