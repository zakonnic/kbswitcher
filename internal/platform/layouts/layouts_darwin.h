// Reading the enabled keyboard layouts on macOS through the Text Input Sources
// API and UCKeyTranslate, the call the system itself makes to turn a key press
// into a character. Only kbs_select_layout changes anything.

#ifndef KBSWITCHER_LAYOUTS_DARWIN_H
#define KBSWITCHER_LAYOUTS_DARWIN_H

#define KBS_MAX_LAYOUTS 32
#define KBS_MAX_KEYS 64
#define KBS_TEXT_LENGTH 128
#define KBS_LEVELS 2

// kbs_layout is one keyboard layout: its identifier, the name shown in the
// input menu, and the character every requested key produces, unshifted at
// chars[key * KBS_LEVELS] and shifted at chars[key * KBS_LEVELS + 1]. Zero
// means the key produces nothing.
typedef struct {
	char id[KBS_TEXT_LENGTH];
	char name[KBS_TEXT_LENGTH];
	unsigned int chars[KBS_MAX_KEYS * KBS_LEVELS];
} kbs_layout;

// kbs_list_layouts fills out with up to max enabled keyboard layouts, asking
// each what the keycount virtual key codes in keycodes produce. It returns the
// number of layouts written, or a negative value on failure.
int kbs_list_layouts(kbs_layout *out, int max, const unsigned short *keycodes, int keycount);

// kbs_current_layout writes the identifier of the layout in use into buf,
// which has room for len bytes. It returns 0 on success.
int kbs_current_layout(char *buf, int len);

// kbs_select_layout makes the layout with the given identifier current. It
// returns 0 on success.
int kbs_select_layout(const char *id);

#endif
