#include <Carbon/Carbon.h>
#include <dispatch/dispatch.h>
#include <pthread.h>

// Declares kbsHotkeyPressed, which is implemented in Go.
#include "_cgo_export.h"

#include "hotkey_darwin.h"

// Both are only ever touched on the main thread, which run_on_main guarantees.
static EventHandlerRef installed_handler = NULL;
static EventHotKeyRef bindings[KBS_MAX_HOTKEYS];

static OSStatus on_hotkey(EventHandlerCallRef next, EventRef event, void *context) {
	(void)next;
	(void)context;

	EventHotKeyID pressed;
	if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, NULL,
	                      sizeof(pressed), NULL, &pressed) == noErr) {
		kbsHotkeyPressed((int)pressed.id);
	}
	return noErr;
}

// run_on_main runs fn on the main thread, where the Carbon Event Manager
// expects to be called, and waits for it. The hotkeys are registered before
// the main run loop starts, so when this is already the main thread fn is
// called directly rather than dispatched to a loop that is not running yet.
static void run_on_main(dispatch_function_t fn, void *context) {
	if (pthread_main_np() != 0) {
		fn(context);
		return;
	}
	dispatch_sync_f(dispatch_get_main_queue(), context, fn);
}

typedef struct {
	unsigned int key;
	unsigned int modifiers;
	int id;
	int result;
} registration;

static void register_on_main(void *context) {
	registration *request = (registration *)context;

	if (request->id < 0 || request->id >= KBS_MAX_HOTKEYS) {
		request->result = -1;
		return;
	}
	if (installed_handler == NULL) {
		EventTypeSpec wanted = {kEventClassKeyboard, kEventHotKeyPressed};
		OSStatus status = InstallApplicationEventHandler(NewEventHandlerUPP(on_hotkey),
		                                                 1, &wanted, NULL, &installed_handler);
		if (status != noErr) {
			request->result = (int)status;
			return;
		}
	}

	EventHotKeyID identifier;
	identifier.signature = 'kbsw';
	identifier.id = (UInt32)request->id;

	EventHotKeyRef binding = NULL;
	OSStatus status = RegisterEventHotKey(request->key, request->modifiers, identifier,
	                                      GetApplicationEventTarget(), 0, &binding);
	if (status != noErr) {
		request->result = (int)status;
		return;
	}

	bindings[request->id] = binding;
	request->result = 0;
}

static void unregister_on_main(void *context) {
	int id = *(int *)context;
	if (id < 0 || id >= KBS_MAX_HOTKEYS || bindings[id] == NULL) {
		return;
	}
	UnregisterEventHotKey(bindings[id]);
	bindings[id] = NULL;
}

int kbs_register_hotkey(unsigned int key, unsigned int modifiers, int id) {
	registration request = {key, modifiers, id, -1};
	run_on_main(register_on_main, &request);
	return request.result;
}

void kbs_unregister_hotkey(int id) {
	int requested = id;
	run_on_main(unregister_on_main, &requested);
}
