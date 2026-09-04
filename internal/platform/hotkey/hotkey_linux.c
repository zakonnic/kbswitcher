#include <errno.h>
#include <string.h>
#include <sys/select.h>

#include "hotkey_linux.h"

// X reports Caps Lock and Num Lock as modifiers of their own, so every
// combination of them has to be grabbed too.
static const unsigned int ignored_modifiers[] = {
    0,
    LockMask,
    Mod2Mask,
    LockMask | Mod2Mask,
};

#define IGNORED_COUNT (sizeof(ignored_modifiers) / sizeof(ignored_modifiers[0]))

// X reports a grab conflict asynchronously, through the error handler.
static int grab_failed = 0;

static int on_grab_error(Display *display, XErrorEvent *event) {
	(void)display;
	(void)event;
	grab_failed = 1;
	return 0;
}

int kbs_grab_key(Display *display, unsigned int keycode, unsigned int modifiers) {
	Window root = DefaultRootWindow(display);

	grab_failed = 0;
	XErrorHandler previous = XSetErrorHandler(on_grab_error);

	for (size_t i = 0; i < IGNORED_COUNT; i++) {
		XGrabKey(display, (int)keycode, modifiers | ignored_modifiers[i], root,
		         False, GrabModeAsync, GrabModeAsync);
	}
	// Let the server process the grabs so that a conflict is reported before
	// the handler is put back.
	XSync(display, False);
	XSetErrorHandler(previous);

	if (grab_failed) {
		kbs_ungrab_key(display, keycode, modifiers);
		return -1;
	}
	return 0;
}

void kbs_ungrab_key(Display *display, unsigned int keycode, unsigned int modifiers) {
	Window root = DefaultRootWindow(display);
	for (size_t i = 0; i < IGNORED_COUNT; i++) {
		XUngrabKey(display, (int)keycode, modifiers | ignored_modifiers[i], root);
	}
	XSync(display, False);
}

int kbs_next_hotkey(Display *display, int wake_fd, unsigned int *keycode, unsigned int *state) {
	int connection = ConnectionNumber(display);

	for (;;) {
		// Events Xlib has already read are not visible to select.
		while (XPending(display) > 0) {
			XEvent event;
			XNextEvent(display, &event);
			if (event.type == KeyPress) {
				*keycode = event.xkey.keycode;
				*state = event.xkey.state;
				return 1;
			}
		}

		fd_set readable;
		FD_ZERO(&readable);
		FD_SET(connection, &readable);
		FD_SET(wake_fd, &readable);

		int highest = connection > wake_fd ? connection : wake_fd;
		if (select(highest + 1, &readable, NULL, NULL, NULL) < 0) {
			if (errno == EINTR) {
				continue;
			}
			return -1;
		}
		if (FD_ISSET(wake_fd, &readable)) {
			return 0;
		}
	}
}
