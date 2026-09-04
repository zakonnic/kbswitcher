// System-wide hotkeys on X11 through XGrabKey: the server routes one key
// combination to this application wherever it is pressed and sends nothing
// else.

#ifndef KBSWITCHER_HOTKEY_LINUX_H
#define KBSWITCHER_HOTKEY_LINUX_H

#include <X11/Xlib.h>

// kbs_grab_key grabs a combination on the root window, together with the
// variants that have Caps Lock or Num Lock on. It returns 0 on success and -1
// when another client already holds the combination.
int kbs_grab_key(Display *display, unsigned int keycode, unsigned int modifiers);

// kbs_ungrab_key releases what kbs_grab_key took.
void kbs_ungrab_key(Display *display, unsigned int keycode, unsigned int modifiers);

// kbs_next_hotkey waits for a grabbed combination to be pressed, or for a byte
// on wake_fd. It returns 1 with keycode and state filled in, 0 when woken up,
// and -1 when the connection failed.
int kbs_next_hotkey(Display *display, int wake_fd, unsigned int *keycode, unsigned int *state);

#endif
