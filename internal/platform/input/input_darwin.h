// Sending key presses on macOS through CGEventCreateKeyboardEvent and
// CGEventPost, which need the Accessibility permission.

#ifndef KBSWITCHER_INPUT_DARWIN_H
#define KBSWITCHER_INPUT_DARWIN_H

// kbs_send_key presses and releases a key, times times, with the given
// kCGEventFlagMask modifier flags.
void kbs_send_key(unsigned short key, unsigned long long flags, int times);

// kbs_accessibility_trusted returns 1 when this application may post key
// events. When prompt is non-zero and it may not, macOS shows the panel that
// leads to the relevant settings page.
int kbs_accessibility_trusted(int prompt);

#endif
