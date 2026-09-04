// Reading the keyboard layouts of an X11 session. X calls a layout a "group":
// a session has up to four and cycles through them in a fixed order. The
// characters come from the keyboard mapping the X server holds.

#ifndef KBSWITCHER_LAYOUTS_LINUX_H
#define KBSWITCHER_LAYOUTS_LINUX_H

#include <X11/Xlib.h>

// KBS_MAX_LAYOUTS is XkbNumKbdGroups.
#define KBS_MAX_LAYOUTS 4
#define KBS_MAX_KEYS 64
#define KBS_TEXT_LENGTH 128
#define KBS_LEVELS 2

// kbs_layout is one group: its name and the character each requested key
// produces, unshifted at chars[key * KBS_LEVELS] and shifted at
// chars[key * KBS_LEVELS + 1].
typedef struct {
	char name[KBS_TEXT_LENGTH];
	unsigned int chars[KBS_MAX_KEYS * KBS_LEVELS];
} kbs_layout;

// kbs_list_layouts fills out with up to max groups, asking each what the
// keycount keycodes in keycodes produce. It returns the number of groups
// written, or a negative value on failure.
int kbs_list_layouts(Display *display, kbs_layout *out, int max,
                     const unsigned int *keycodes, int keycount);

// kbs_current_group returns the group in use, counting from zero, or a
// negative value on failure.
int kbs_current_group(Display *display);

// kbs_select_group makes a group current. It returns 0 on success.
int kbs_select_group(Display *display, int group);

#endif
