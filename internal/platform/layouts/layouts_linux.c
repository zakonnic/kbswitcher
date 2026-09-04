#include <X11/XKBlib.h>
#include <xkbcommon/xkbcommon.h>

#include <stdio.h>
#include <string.h>

#include "layouts_linux.h"

// group_count returns how many groups the session is configured with and
// writes their names. A group without a name is called after its number.
static int group_count(Display *display, char names[KBS_MAX_LAYOUTS][KBS_TEXT_LENGTH]) {
	XkbDescPtr keyboard = XkbAllocKeyboard();
	if (keyboard == NULL) {
		return -1;
	}
	keyboard->dpy = display;
	keyboard->device_spec = XkbUseCoreKbd;

	int count = 0;
	if (XkbGetControls(display, XkbAllControlsMask, keyboard) == Success && keyboard->ctrls != NULL) {
		count = keyboard->ctrls->num_groups;
	}
	if (count > KBS_MAX_LAYOUTS) {
		count = KBS_MAX_LAYOUTS;
	}

	for (int i = 0; i < count; i++) {
		snprintf(names[i], KBS_TEXT_LENGTH, "Group %d", i + 1);
	}
	if (XkbGetNames(display, XkbGroupNamesMask, keyboard) == Success && keyboard->names != NULL) {
		for (int i = 0; i < count; i++) {
			Atom atom = keyboard->names->groups[i];
			if (atom == None) {
				continue;
			}
			char *text = XGetAtomName(display, atom);
			if (text == NULL) {
				continue;
			}
			snprintf(names[i], KBS_TEXT_LENGTH, "%s", text);
			XFree(text);
		}
	}

	XkbFreeKeyboard(keyboard, 0, True);
	return count;
}

// character returns what one key produces in one group at one shift level.
// The core mapping stores two keysyms per group; when the shifted one is
// missing, X defines it as the upper case of the unshifted one.
static unsigned int character(const KeySym *symbols, int per_keycode, int group, int level) {
	int index = group * 2 + level;
	if (index >= per_keycode) {
		return 0;
	}

	KeySym symbol = symbols[index];
	if (symbol == NoSymbol && level == 1 && group * 2 < per_keycode) {
		KeySym lower = NoSymbol, upper = NoSymbol;
		XConvertCase(symbols[group * 2], &lower, &upper);
		symbol = upper;
	}
	if (symbol == NoSymbol) {
		return 0;
	}

	uint32_t produced = xkb_keysym_to_utf32(symbol);
	if (produced < 0x20 || produced == 0x7f) {
		return 0;
	}
	return produced;
}

int kbs_list_layouts(Display *display, kbs_layout *out, int max,
                     const unsigned int *keycodes, int keycount) {
	char names[KBS_MAX_LAYOUTS][KBS_TEXT_LENGTH];
	int groups = group_count(display, names);
	if (groups <= 0) {
		return groups;
	}
	if (groups > max) {
		groups = max;
	}

	int first = 0, last = 0;
	XDisplayKeycodes(display, &first, &last);

	int per_keycode = 0;
	KeySym *mapping = XGetKeyboardMapping(display, (KeyCode)first, last - first + 1, &per_keycode);
	if (mapping == NULL || per_keycode <= 0) {
		if (mapping != NULL) {
			XFree(mapping);
		}
		return -2;
	}

	for (int group = 0; group < groups; group++) {
		kbs_layout *entry = &out[group];
		memset(entry, 0, sizeof(*entry));
		snprintf(entry->name, KBS_TEXT_LENGTH, "%s", names[group]);

		for (int i = 0; i < keycount && i < KBS_MAX_KEYS; i++) {
			int keycode = (int)keycodes[i];
			if (keycode < first || keycode > last) {
				continue;
			}
			const KeySym *symbols = &mapping[(keycode - first) * per_keycode];
			for (int level = 0; level < KBS_LEVELS; level++) {
				entry->chars[i * KBS_LEVELS + level] = character(symbols, per_keycode, group, level);
			}
		}
	}

	XFree(mapping);
	return groups;
}

int kbs_current_group(Display *display) {
	XkbStateRec state;
	if (XkbGetState(display, XkbUseCoreKbd, &state) != Success) {
		return -1;
	}
	return (int)state.group;
}

int kbs_select_group(Display *display, int group) {
	if (!XkbLockGroup(display, XkbUseCoreKbd, (unsigned int)group)) {
		return -1;
	}
	XFlush(display);
	return 0;
}
