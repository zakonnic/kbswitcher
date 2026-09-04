// Package input sends the handful of key presses kbswitcher needs to the
// focused window: extend the selection, copy, paste. Replaying the shortcuts a
// user would press is what makes it work in every application, and it needs
// no access beyond the one permission the platform asks for (Accessibility on
// macOS, none on Windows, an X11 connection on Linux).
package input
