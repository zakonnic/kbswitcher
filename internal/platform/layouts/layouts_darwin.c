#include <Carbon/Carbon.h>
#include <string.h>

#include "layouts_darwin.h"

// copy_text copies a Core Foundation string into buf. It returns 1 on success
// and leaves an empty string behind on failure.
static int copy_text(CFStringRef text, char *buf, int len) {
	buf[0] = '\0';
	if (text == NULL) {
		return 0;
	}
	return CFStringGetCString(text, buf, len, kCFStringEncodingUTF8) ? 1 : 0;
}

static void read_chars(TISInputSourceRef source, kbs_layout *out,
                       const unsigned short *keycodes, int keycount) {
	CFDataRef data = (CFDataRef)TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData);
	if (data == NULL) {
		return; // an input method rather than a layout
	}
	const UCKeyboardLayout *table = (const UCKeyboardLayout *)CFDataGetBytePtr(data);
	if (table == NULL) {
		return;
	}

	UInt32 keyboard_type = LMGetKbdType();
	for (int i = 0; i < keycount && i < KBS_MAX_KEYS; i++) {
		for (int level = 0; level < KBS_LEVELS; level++) {
			// UCKeyTranslate takes the modifiers in the old Event Manager
			// format, hence the shift.
			UInt32 modifiers = (level == 1) ? ((shiftKey >> 8) & 0xff) : 0;
			UInt32 dead_key_state = 0;
			UniChar produced[8];
			UniCharCount length = 0;

			// kUCKeyTranslateNoDeadKeysMask asks for the standalone form of a
			// dead key, which is what a lone press shows. The similarly named
			// Bit constant is only its position and would mean "no options".
			OSStatus status = UCKeyTranslate(table, keycodes[i], kUCKeyActionDisplay,
			                                 modifiers, keyboard_type,
			                                 kUCKeyTranslateNoDeadKeysMask,
			                                 &dead_key_state,
			                                 sizeof(produced) / sizeof(produced[0]),
			                                 &length, produced);

			// Anything other than one printable character is not a single
			// key press.
			if (status != noErr || length != 1) {
				continue;
			}
			if (produced[0] < 0x20 || produced[0] == 0x7f) {
				continue;
			}
			out->chars[i * KBS_LEVELS + level] = (unsigned int)produced[0];
		}
	}
}

// keyboard_layout_filter selects keyboard layouts, as opposed to input methods
// such as the Chinese or Japanese ones.
static CFDictionaryRef keyboard_layout_filter(void) {
	CFMutableDictionaryRef filter = CFDictionaryCreateMutable(
	    kCFAllocatorDefault, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (filter == NULL) {
		return NULL;
	}
	CFDictionaryAddValue(filter, kTISPropertyInputSourceType, kTISTypeKeyboardLayout);
	return filter;
}

int kbs_list_layouts(kbs_layout *out, int max, const unsigned short *keycodes, int keycount) {
	CFDictionaryRef filter = keyboard_layout_filter();
	if (filter == NULL) {
		return -1;
	}

	// false: the layouts the user has enabled, not every installed one.
	CFArrayRef sources = TISCreateInputSourceList(filter, false);
	CFRelease(filter);
	if (sources == NULL) {
		return -2;
	}

	int written = 0;
	CFIndex total = CFArrayGetCount(sources);
	for (CFIndex i = 0; i < total && written < max; i++) {
		TISInputSourceRef source = (TISInputSourceRef)CFArrayGetValueAtIndex(sources, i);

		CFBooleanRef enabled = (CFBooleanRef)TISGetInputSourceProperty(source, kTISPropertyInputSourceIsEnabled);
		if (enabled != NULL && !CFBooleanGetValue(enabled)) {
			continue;
		}

		kbs_layout *entry = &out[written];
		memset(entry, 0, sizeof(*entry));
		if (!copy_text((CFStringRef)TISGetInputSourceProperty(source, kTISPropertyInputSourceID),
		               entry->id, KBS_TEXT_LENGTH)) {
			continue;
		}
		copy_text((CFStringRef)TISGetInputSourceProperty(source, kTISPropertyLocalizedName),
		          entry->name, KBS_TEXT_LENGTH);
		read_chars(source, entry, keycodes, keycount);
		written++;
	}

	CFRelease(sources);
	return written;
}

int kbs_current_layout(char *buf, int len) {
	// With a Chinese or Japanese input method active, this is still the layout
	// the keys follow.
	TISInputSourceRef source = TISCopyCurrentKeyboardLayoutInputSource();
	if (source == NULL) {
		return -1;
	}
	int ok = copy_text((CFStringRef)TISGetInputSourceProperty(source, kTISPropertyInputSourceID), buf, len);
	CFRelease(source);
	return ok ? 0 : -2;
}

int kbs_select_layout(const char *id) {
	CFStringRef wanted = CFStringCreateWithCString(kCFAllocatorDefault, id, kCFStringEncodingUTF8);
	if (wanted == NULL) {
		return -1;
	}

	CFMutableDictionaryRef filter = CFDictionaryCreateMutable(
	    kCFAllocatorDefault, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (filter == NULL) {
		CFRelease(wanted);
		return -2;
	}
	CFDictionaryAddValue(filter, kTISPropertyInputSourceID, wanted);

	CFArrayRef sources = TISCreateInputSourceList(filter, false);
	CFRelease(filter);
	CFRelease(wanted);
	if (sources == NULL) {
		return -3;
	}

	int result = -4;
	if (CFArrayGetCount(sources) > 0) {
		TISInputSourceRef source = (TISInputSourceRef)CFArrayGetValueAtIndex(sources, 0);
		result = (TISSelectInputSource(source) == noErr) ? 0 : -5;
	}

	CFRelease(sources);
	return result;
}
