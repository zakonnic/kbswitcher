# kbswitcher

[![Release](https://img.shields.io/github/v/release/zakonnic/kbswitcher?sort=semver)](https://github.com/zakonnic/kbswitcher/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/zakonnic/kbswitcher)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-informational)](#installation)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**Fixes text typed in the wrong keyboard layout. One key, and nothing else you type ever reaches it.**

**No keylogger.** kbswitcher does not watch what you type. The operating system wakes it only when its hotkey is
pressed; no other keystroke ever reaches the process, and nothing is buffered. See [How it works](#how-it-works).

## Table of Contents

- [Installation](#installation)
  - [Windows](#windows)
  - [macOS](#macos)
  - [Linux](#linux)
- [Usage](#usage)
- [Settings](#settings)
- [How it works](#how-it-works)
- [Limitations](#limitations)
- [Development](#development)
- [Security](#security)
- [License](#license)

## Installation

Download the archive for your platform from the [releases](https://github.com/zakonnic/kbswitcher/releases) page,
unpack it anywhere and start `kbswitcher`. Settings → *Start when I log in* adds it to autostart; About →
*Check for updates* installs a newer release in place. Only one copy runs at a time: starting kbswitcher while it is
already running exits quietly, so an autostart entry and a manual launch never fight over the hotkeys.

Or build it yourself (Go 1.26+, cgo):

```sh
make build          # builds into bin/
make bundle         # macOS: wraps the binary in bin/kbswitcher.app

# make help         # list all targets
```

| Platform | Build needs                                                                    |
|----------|--------------------------------------------------------------------------------|
| Windows  | a C compiler (MinGW-w64 or the MSVC build tools), for the GUI toolkit          |
| macOS    | Xcode command line tools                                                       |
| Linux    | `libx11-dev`, `libxtst-dev`, `libxkbcommon-dev`, `libgl1-mesa-dev`, `xorg-dev` |

### Windows

Nothing to grant: a hotkey, key presses and the clipboard need no permission. SmartScreen may warn about an unsigned
download on the first start.

### macOS

kbswitcher replaces text by pressing keys on your behalf, which macOS guards with the **Accessibility** permission.
The first start asks for it; grant it in *System Settings → Privacy & Security → Accessibility*, then restart the
app. Until then the hotkeys do nothing.

On a MacBook, `F12` only reaches applications when *Use F1, F2, etc. keys as standard function keys* is on, or when
`fn` is held. If that does not suit you, pick another key in Settings.

### Linux

kbswitcher needs an X11 session. Under Wayland it works only for applications running through XWayland, because
Wayland deliberately offers no way to grab a global hotkey or to type into another application's window.

## Usage

| Hotkey                                | What it does                                                                                     |
|---------------------------------------|--------------------------------------------------------------------------------------------------|
| **Break** (**F12** on a Mac)          | Rewrites the word before the caret and switches the keyboard to the layout that word now belongs to, so you can keep typing. The switch can be turned off in Settings. |
| **Shift+Break** (**Shift+F12** on a Mac) | Rewrites the selected text and leaves the keyboard layout alone. In a Windows terminal it rewrites what the shell shows as selected, or the whole input line when nothing is. |

Press the same hotkey again to move on to the next layout. Both hotkeys are configurable.

## Settings

Right-click the tray icon:

- **About** — version, the layouts that were found and the order they are cycled in, and a button that checks for a
  newer release and installs it.
- **Settings** — whether kbswitcher starts with your session, which key (with optional Ctrl, Alt or Cmd/Win)
  triggers it, and whether converting a word also switches the keyboard layout. To change the key, click the Hotkey
  field and press the key; the hotkey is released while the field listens, so the current key can be pressed there
  too. Shift is reserved: it turns the hotkey into its "convert the selection" form. Save puts the settings in force
  at once; Cancel discards the changes.
- **Quit**.

The configuration is a small JSON file:

| Platform | Location                                               |
|----------|--------------------------------------------------------|
| Windows  | `%AppData%\kbswitcher\config.json`                     |
| macOS    | `~/Library/Application Support/kbswitcher/config.json` |
| Linux    | `~/.config/kbswitcher/config.json`                     |

A log of the current run is written to the user cache directory (`%LocalAppData%\kbswitcher\`,
`~/Library/Caches/kbswitcher/kbswitcher.log`, `~/.cache/kbswitcher/`) and rewritten at every start. Run with
`-verbose` to log every conversion.

## How it works

kbswitcher asks the operating system to notify it about one key combination — `RegisterHotKey` on Windows,
`RegisterEventHotKey` on macOS, `XGrabKey` on X11 — and is woken up only when that combination is pressed.

When the hotkey fires, it does what you would do by hand:

1. extend the selection to cover the last word (skipped for the selection hotkey — you have already selected the
   text);
2. copy;
3. work out which layout the text was typed in, and rewrite it for the next one;
4. paste the result and give your clipboard back.

On Windows, what kbswitcher puts on the clipboard along the way is marked so that clipboard history and cloud
clipboard sync leave it out, and the clipboard is given back the moment the application has read the paste. A
Windows console skips the clipboard altogether: its input line is read and rewritten in place.

The layouts come from the same tables the operating system uses to turn key presses into characters: `ToUnicodeEx`
on Windows, `UCKeyTranslate` on macOS, the XKB keymap on X11.

## Limitations

- **Terminals kbswitcher cannot read.** Git Bash in its own window (mintty) has no Windows console at all, and there
  the hotkey does nothing. A JetBrains IDE does not say whether its terminal pane has the focus, so the hotkey takes
  the clipboard path there, which a terminal pane does not answer. Two terminals of one editor that are the same
  size and have their carets on the same cell cannot be told apart, and neither is touched. On macOS and Linux
  every terminal takes the clipboard path.
- **Selections in a terminal.** A selection made with the mouse belongs to the terminal window and no shell knows of
  it, so `Shift`+hotkey converts what the shell itself shows as selected (`Shift`+arrows in PowerShell), and the
  whole input line otherwise.
- **Formatting.** The replacement is pasted as plain text. In a rich text editor the word that was replaced loses its
  formatting; the rest of the line does not, because the selection is narrowed to the word before pasting.
- **Characters that need AltGr.** Only unshifted and shifted key presses are reproduced. Anything typed with a third
  or fourth level is copied through unchanged.
- **Clipboard formats other than text, rich text, images and file lists** are not restored after a conversion, and
  content larger than 8 MB is left behind rather than copied aside.

## Development

```
cmd/kbswitcher           the program itself: flags, logging, start-up
internal/keymap          the conversion core — no operating system anywhere in it
internal/switcher        what a hotkey press does, start to finish
internal/platform        everything the operating system provides:
    hotkey                 registering a system-wide key combination
    input                  pressing keys in the focused window
    console                reading and rewriting a Windows console input line
    layouts                reading the installed layouts and their characters
    clip                   borrowing and giving back the clipboard
internal/config          the settings file
internal/autostart       the "start with the session" entry
internal/singleinstance  the lock that keeps a second copy from running
internal/updater         checking for and installing a release
internal/ui              tray icon, Settings and About
tools/stand              Windows test stand for terminals and editors
```

The conversion core has no dependency on any operating system and is covered by tests, including a simulated text
field that exercises select-copy-convert-paste end to end.

```sh
make check          # format, vet and test
make test-race      # tests under the race detector
make lint           # golangci-lint, settings in .golangci.yml
make dist           # package bin/ and append to bin/checksums.txt, as a release expects
```

`tools/stand` opens a real console, Windows Terminal, an isolated VS Code or Notepad, types a word, converts it and
checks what the window shows. `go run ./tools/stand -help` lists its options.

### Releasing

```sh
make release VERSION=1.2.3
```

On a clean `main`, this vets and tests, tags `v1.2.3` (refusing a version not newer than the last tag), packages the
archive with `checksums.txt`, pushes `main` and the tag, and prints what is left to do on GitHub: publish a release
from the tag with both files attached. The updater reads only the latest published release; drafts and pre-releases
are invisible to it, and it refuses a release without `checksums.txt`. The build needs cgo, so each platform's archive
is made on that platform with `make dist VERSION=v1.2.3` and added to the same release, its line appended to the same
`checksums.txt`. `make tag`, `make untag` and `make push` are the steps on their own.

## Security

[SECURITY.md](SECURITY.md) describes what kbswitcher can reach, what it deliberately cannot, and the trust model of
the update button. It is the place to start for a review.

Third-party dependencies: [Fyne](https://fyne.io) for the tray icon and the two windows,
[golang.design/x/clipboard](https://github.com/golang-design/clipboard) for the clipboard on macOS and Linux, and
`golang.org/x/sys` for the Windows system calls.

## License

[MIT](LICENSE)
