//go:build darwin && cgo

package main

/*
#include <stdint.h>
#include <dispatch/dispatch.h>

extern void semantixDesktopMainHeartbeat(void);

static dispatch_source_t semantix_main_heartbeat_timer;

static void semantix_main_heartbeat_handler(void *ctx) {
	semantixDesktopMainHeartbeat();
}

static void semantix_start_main_heartbeat(uint64_t interval_ms) {
	if (semantix_main_heartbeat_timer != NULL) {
		return;
	}
	semantix_main_heartbeat_timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
	dispatch_set_context(semantix_main_heartbeat_timer, NULL);
	dispatch_source_set_event_handler_f(semantix_main_heartbeat_timer, semantix_main_heartbeat_handler);
	dispatch_source_set_timer(semantix_main_heartbeat_timer, dispatch_time(DISPATCH_TIME_NOW, 0), interval_ms * NSEC_PER_MSEC, 100 * NSEC_PER_MSEC);
	dispatch_resume(semantix_main_heartbeat_timer);
}

static void semantix_stop_main_heartbeat(void) {
	if (semantix_main_heartbeat_timer == NULL) {
		return;
	}
	dispatch_source_cancel(semantix_main_heartbeat_timer);
	semantix_main_heartbeat_timer = NULL;
}
*/
import "C"

import "time"

func mainThreadWatchdogSupported() bool {
	return true
}

func startNativeMainThreadHeartbeat(intervalMS uint64) {
	C.semantix_start_main_heartbeat(C.uint64_t(intervalMS))
}

func stopNativeMainThreadHeartbeat() {
	C.semantix_stop_main_heartbeat()
}

//export semantixDesktopMainHeartbeat
func semantixDesktopMainHeartbeat() {
	recordMainThreadHeartbeat(time.Now())
}
