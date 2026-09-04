#include <ApplicationServices/ApplicationServices.h>

#include "input_darwin.h"

// The modifiers are set on the events themselves rather than by pressing the
// modifier keys, so whatever the user is holding cannot change the shortcut.
static void post_key(CGEventSourceRef source, CGKeyCode key, CGEventFlags flags) {
	CGEventRef down = CGEventCreateKeyboardEvent(source, key, true);
	if (down != NULL) {
		CGEventSetFlags(down, flags);
		CGEventPost(kCGHIDEventTap, down);
		CFRelease(down);
	}

	CGEventRef up = CGEventCreateKeyboardEvent(source, key, false);
	if (up != NULL) {
		CGEventSetFlags(up, flags);
		CGEventPost(kCGHIDEventTap, up);
		CFRelease(up);
	}
}

void kbs_send_key(unsigned short key, unsigned long long flags, int times) {
	// A private source keeps these presses apart from the real keyboard state.
	CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStatePrivate);

	for (int i = 0; i < times; i++) {
		post_key(source, (CGKeyCode)key, (CGEventFlags)flags);
	}

	if (source != NULL) {
		CFRelease(source);
	}
}

int kbs_accessibility_trusted(int prompt) {
	if (!prompt) {
		return AXIsProcessTrusted() ? 1 : 0;
	}

	const void *keys[] = {kAXTrustedCheckOptionPrompt};
	const void *values[] = {kCFBooleanTrue};
	CFDictionaryRef options = CFDictionaryCreate(
	    kCFAllocatorDefault, keys, values, 1,
	    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (options == NULL) {
		return AXIsProcessTrusted() ? 1 : 0;
	}

	int trusted = AXIsProcessTrustedWithOptions(options) ? 1 : 0;
	CFRelease(options);
	return trusted;
}
