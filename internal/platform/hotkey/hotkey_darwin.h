// System-wide hotkeys on macOS through RegisterEventHotKey: the application
// names one key combination and is called back when it is pressed. It is not
// an event tap and cannot observe any other key.

#ifndef KBSWITCHER_HOTKEY_DARWIN_H
#define KBSWITCHER_HOTKEY_DARWIN_H

// KBS_MAX_HOTKEYS bounds how many combinations can be bound at once.
#define KBS_MAX_HOTKEYS 16

// kbs_register_hotkey binds a virtual key code with a Carbon modifier mask to
// an identifier that is handed back on every press. It returns 0 on success.
int kbs_register_hotkey(unsigned int key, unsigned int modifiers, int id);

// kbs_unregister_hotkey releases the combination bound to an identifier.
void kbs_unregister_hotkey(int id);

#endif
