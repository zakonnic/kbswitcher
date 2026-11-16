# kbswitcher

[![Release](https://img.shields.io/github/v/release/zakonnic/kbswitcher?sort=semver)](https://github.com/zakonnic/kbswitcher/releases)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/zakonnic/kbswitcher/badges/coverage.json)](https://github.com/zakonnic/kbswitcher/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/zakonnic/kbswitcher)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-informational)](#installation)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**Rewrites text that was typed with the wrong keyboard layout.**

**No keylogger.** kbswitcher does not watch what you type. The operating system wakes it only when its hotkey is
pressed; no other keystroke ever reaches the process, and nothing is buffered. See [How it works](#how-it-works).

<p align="center">
  <img src="docs/about.png" alt="The kbswitcher About window: hotkeys, installed layouts and the update check" width="640">
</p>

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

Download the archive for your machine from the [latest release](https://github.com/zakonnic/kbswitcher/releases/latest):

| System                 | Archive                                  | Contains          |
|------------------------|------------------------------------------|-------------------|
| Windows (x64 and ARM)  | `kbswitcher_vX.Y.Z_windows_amd64.zip`    | `kbswitcher.exe`  |
| macOS, Apple silicon   | `kbswitcher_vX.Y.Z_darwin_arm64.tar.gz`  | `kbswitcher.app`  |
| macOS, Intel           | `kbswitcher_vX.Y.Z_darwin_amd64.tar.gz`  | `kbswitcher.app`  |
| Linux, x86-64          | `kbswitcher_vX.Y.Z_linux_amd64.tar.gz`   | `kbswitcher`      |
| Linux, ARM64           | `kbswitcher_vX.Y.Z_linux_arm64.tar.gz`   | `kbswitcher`      |

Unpack it into a folder you own and start the program; the steps for each system follow. kbswitcher lives in the
tray (the menu bar on a Mac). Settings → *Start when I log in* adds it to autostart; About → *Check for updates*
installs a newer release in place, which is why the folder must be writable without administrator rights. Only one
copy runs at a time: starting kbswitcher while it is already running exits quietly, so an autostart entry and a
manual launch never fight over the hotkeys.

Or build it yourself (Go 1.26+, cgo):

```sh
make build          # builds into bin/
make bundle         # macOS: wraps the binary in bin/kbswitcher.app

# make help         # list all targets
```

| Platform | Build needs                                                                                       |
|----------|---------------------------------------------------------------------------------------------------|
| Windows  | a C compiler (MinGW-w64 or the MSVC build tools), for the GUI toolkit                             |
| macOS    | Xcode command line tools                                                                          |
| Linux    | `gcc`, `libx11-dev`, `libxtst-dev`, `libxkbcommon-dev`, `libwayland-dev`, `libgl1-mesa-dev`, `xorg-dev` |

### Windows

1. Unpack the zip into a folder of your own, for example `%LOCALAPPDATA%\Programs\kbswitcher` (not
   `Program Files`: the update button replaces the program in place).
2. Start `kbswitcher.exe`. The program is not signed, so SmartScreen may say *Windows protected your PC* on the first
   start: *More info → Run anyway*.

Nothing to grant: a hotkey, key presses and the clipboard need no permission. Windows on ARM runs the x64 build.

### macOS

macOS 12 or later.

1. Open the archive (Finder unpacks it) and move `kbswitcher.app` to *Applications*.
2. Start it. The app is not signed by an Apple developer, so macOS refuses the first start; open
   *System Settings → Privacy & Security*, press *Open Anyway* next to the message about kbswitcher, and confirm.
   The same from a terminal: `xattr -dr com.apple.quarantine /Applications/kbswitcher.app`.
3. Grant **Accessibility**: kbswitcher replaces text by pressing keys on your behalf, which macOS guards with that
   permission. The first start asks for it; switch kbswitcher on in *System Settings → Privacy & Security →
   Accessibility*, then restart the app. Until then the hotkeys do nothing.

On a MacBook, `F12` only reaches applications when *Use F1, F2, etc. keys as standard function keys* is on, or when
`fn` is held. If that does not suit you, pick another key in Settings.

### Linux

```sh
mkdir -p ~/.local/bin
tar -xzf kbswitcher_vX.Y.Z_linux_amd64.tar.gz -C ~/.local/bin
~/.local/bin/kbswitcher &
```

A desktop installation already has the libraries it loads: X11 (`libX11`, `libXtst`), `libxkbcommon`,
`libwayland-client` and OpenGL. On a minimal Debian or Ubuntu:
`sudo apt install libx11-6 libxtst6 libxkbcommon0 libwayland-client0 libgl1`. The archives are built on Ubuntu 22.04
and need its C library (glibc 2.35) or a newer one; on an older distribution, build from source.

kbswitcher needs an X11 session. Under Wayland it works only for applications running through XWayland, because
Wayland deliberately offers no way to grab a global hotkey or to type into another application's window.

## Usage

| Hotkey                                | What it does                                                                                     |
|---------------------------------------|--------------------------------------------------------------------------------------------------|
| **Break** (**F12** on a Mac)          | Rewrites the word before the caret and switches the keyboard to the layout that word now belongs to, so you can keep typing. |
| **Shift+Break** (**Shift+F12** on a Mac) | Rewrites the selected text and switches the keyboard the same way. In a Windows terminal it rewrites what the shell shows as selected, or the whole input line when nothing is. |

Press the same hotkey again to move on to the next layout. The hotkey, the modifier that turns it to the selection,
and each of the two layout switches can be changed in Settings.

## Settings

Right-click the tray icon:

- **About** — version, the layouts that were found and the order they are cycled in, and a button that checks for a
  newer release and installs it.
- **Settings** — whether kbswitcher starts with your session; whether converting the last word, and converting a
  selection, also switch the keyboard layout (both on by default); the hotkey; and the modifier that, held with the
  hotkey, converts the selection instead (Shift by default). To change the hotkey, click its field and press a key,
  or a modifier and a key together: at most two keys, and a key used in typing (a letter, a digit, Space, an
  arrow…) needs a modifier. On Windows a letter is the one the layout in use puts on the key, as for any Windows
  shortcut; on macOS and Linux a key is bound by its position. Escape stops listening.
  The hotkeys are released while a field listens, so the current ones can be pressed there too. Save puts the
  settings in force at once; Cancel discards the changes.
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

kbswitcher asks the operating system to notify it about two key combinations, the hotkey and the same with the
selection modifier — `RegisterHotKey` on Windows, `RegisterEventHotKey` on macOS, `XGrabKey` on X11 — and is woken
up only when one of them is pressed.

When the hotkey fires, it does what you would do by hand, in one of three ways, depending on where the caret is.

**A text field that tells its text** (on Windows: browsers and Electron applications, WPF, WinUI, Notepad and other
RichEdit fields, the classic edit controls of dialogs and Windows Forms). kbswitcher reads the line around the caret
the way a screen reader does — UI Automation, or the edit control's own messages — and then writes the fix. The
clipboard is not touched and no shortcut is pressed. How the fix is written depends on what the field is, which
Windows reports by the class of the focused window:

- A system edit control (`Edit`, `RichEdit`) is changed through its own messages: the word is selected by position
  and replaced, after checking that it holds exactly that word. No character is typed, so nothing depends on how the
  application handles typed input (Notepad turns typed characters that queue up while it is busy into copies of the
  last one). One `Ctrl`+`Z` undoes the conversion of a word.
- Any other field gets keys: Backspace over the word, and the word in the right layout.

The selection hotkey erases the selection with Backspace and puts its conversion in its place the same way.

A field may not show yet what was typed into it a moment ago: a busy page takes in keys late, and its accessibility
tree is later still (up to 150 ms measured). So kbswitcher presses `Shift`+`Left` and waits for the field to show one
character selected. That key queues behind everything typed before it, and nothing else you type or press selects a
single character, so the field then shows all of it. The `Right` key that drops the selection opens the burst that
retypes the word. A field that does not show the selection within a second is left alone.

**A terminal** (on Windows). The shell's input line is read and rewritten in place through the console API; no
key reaches the shell but the ones it would get from you.

**Anything else** (editors that draw their own text, such as VS Code's and the JetBrains IDEs', and every
application on macOS and Linux) goes through the clipboard:

1. select from the caret to the start of the line, with one key press, and copy (skipped for the selection hotkey —
   you have already selected the text);
2. work out which layout the last word was typed in, and rewrite it for the next one;
3. select exactly that word and cut it;
4. paste the result where the word was, and give your clipboard back.

The line is selected by a single key press and the word is cut rather than pasted over because of editors driven by
scripts: they learn of a keyboard selection late, and until then copy one range and paste over another. A selection
made by one press cannot be caught halfway, and what a cut puts on the clipboard is what it removed. On Windows,
what kbswitcher puts on the clipboard along the way is marked so that clipboard history and cloud clipboard sync
leave it out, and the clipboard is given back a quarter of a second after the pasted text was read off it.

The layouts come from the same tables the operating system uses to turn key presses into characters: `ToUnicodeEx`
on Windows, `UCKeyTranslate` on macOS, the XKB keymap on X11.

## Limitations

- **Terminals kbswitcher cannot read.** Git Bash in its own window (mintty) has no Windows console at all, and there
  the hotkey does nothing. A JetBrains IDE, Tabby, or a terminal that draws its own window, does not say where its
  focus is, so kbswitcher presses Left and Right once to see whether a shell moves its caret; with the focus in the
  IDE's editor that costs a tenth of a second. A Tabby pane whose shell does not answer (an empty line, a program
  running in it) is left alone. With the caret at the very start of a shell's line that probe leaves it one
  character to the right. On macOS and Linux every terminal takes the clipboard path.
- **Retyping is typing.** In a field that tells its text and is not a system edit control, the fix is typed, so an
  editor treats it as typed: a code editor may close a bracket or open its completion list, as it would for you. A
  word with combining marks or emoji in it goes through the clipboard instead there. A selection over several lines
  goes through the clipboard in any field.
- **Clipboard readers.** On the clipboard path, a program that opens the clipboard on every change (WSLg, remote
  desktop clients, clipboard managers) can hold it at the moment the application copies or pastes, and a classic
  edit control does not try again. Fields that tell their text avoid the clipboard for exactly this reason.
- **The last word is looked for on the caret's line only.** With the caret at the very start of a line the hotkey
  does nothing.
- **Selections in a terminal.** A selection made with the mouse belongs to the terminal window and no shell knows of
  it, so `Shift`+hotkey converts what the shell itself shows as selected (`Shift`+arrows in PowerShell), and the
  whole input line otherwise. In the terminal of a JetBrains IDE the whole line takes most of a second, because the
  window is tried as an editor first.
- **Formatting.** The replacement is pasted as plain text. In a rich text editor the word that was replaced loses its
  formatting; the rest of the line does not, because only the word is cut.
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
    field                  reading the focused text field around the caret, without the clipboard
    uia                    a small UI Automation client
    console                reading and rewriting a Windows console input line
    layouts                reading the installed layouts and their characters
    clip                   borrowing and giving back the clipboard
internal/config          the settings file
internal/autostart       the "start with the session" entry
internal/singleinstance  the lock that keeps a second copy from running
internal/updater         checking for and installing a release
internal/ui              tray icon, Settings and About
tools/stand              Windows test stand for terminals, editors and text fields
tools/uiaread            prints what UI Automation reports about the focused field
```

The conversion core has no dependency on any operating system and is covered by tests, including a simulated text
field that exercises select-copy-convert-paste end to end.

```sh
make check          # format, vet and test
make test-race      # tests under the race detector
make cover-gen      # coverage of the application's code, the number of the badge above
make lint           # golangci-lint, settings in .golangci.yml
make dist           # package bin/ and append to bin/checksums.txt, as a release expects
```

`tools/stand` opens a real console, Windows Terminal, an isolated VS Code or Notepad, types a word, converts it and
checks what the window shows. `go run ./tools/stand -help` lists its options.

### Releasing

```sh
make release V=1.2.3          # asks before the push; YES=1 does not
```

On a clean `main`, `make release` checks formatting, vets, lints and runs the tests under the race detector, tags
`v1.2.3` (refusing a version not newer than the last tag), asks, and pushes `main` and the tag together. The tag's
CI run then builds every platform on its own runner (the build needs cgo, which cannot cross to macOS) and, once the
tests and all five builds pass, publishes the release with the archives and one `checksums.txt`. `make release` waits
for that run (about a quarter of an hour) and finishes by checking the release as the updater will use it: it is the
latest published one, every archive is attached, and each downloads with the checksum listed for it.

Anything that stops before the push deletes the tag again. When CI fails, nothing is published and the updater sees
nothing, and `make release` prints the way back. A cause that needs no new commit (a flaky test, a runner outage):
re-run the failed jobs on the run's page, then `make release-verify V=1.2.3`. A fix: commit it, delete the tag from
origin and locally (`git push origin :refs/tags/v1.2.3`, `make untag V=1.2.3`) and run `make release` again; should a
draft of the release be left over from a broken upload, delete it on GitHub first. `make release-verify V=1.2.3`
repeats the final check; `GITHUB_TOKEN` lifts the GitHub API's limit of 60 requests an hour. `make dist` packages the current platform alone, as CI does on
each runner; `make release-check`, `make tag`, `make untag` and `make push` are the other steps on their own.

## Security

[SECURITY.md](SECURITY.md) describes what kbswitcher can reach, what it deliberately cannot, and the trust model of
the update button. It is the place to start for a review.

Third-party dependencies: [Fyne](https://fyne.io) for the tray icon and the two windows,
[golang.design/x/clipboard](https://github.com/golang-design/clipboard) for the clipboard on macOS and Linux, and
`golang.org/x/sys` for the Windows system calls.

## License

[MIT](LICENSE)
