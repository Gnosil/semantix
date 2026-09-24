#ifndef SEMANTIX_WEBKIT_DIAGNOSTICS_LINUX_H
#define SEMANTIX_WEBKIT_DIAGNOSTICS_LINUX_H

void semantix_install_webkit_observer(void);
extern void semantixWebKitRuntimeReady(int major, int minor, int micro, int gpu_mode);
extern void semantixWebKitProcessTerminated(int reason, int recovery, unsigned long long generation);

#ifdef SEMANTIX_WEBKIT_SMOKE
int semantix_test_webkit_run(int mode);
void semantix_test_webkit_event_seen(int reason, int recovery);
int semantix_test_webkit_reload_count(void);
#endif

#endif
