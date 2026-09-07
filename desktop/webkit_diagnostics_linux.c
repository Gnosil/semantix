#include "webkit_diagnostics_linux.h"

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

#define SEMANTIX_RECOVERY_COOLDOWN_US (30 * G_USEC_PER_SEC)
#ifdef SEMANTIX_WEBKIT_SMOKE
#define SEMANTIX_RECOVERY_TIMEOUT_SECONDS 5
#else
#define SEMANTIX_RECOVERY_TIMEOUT_SECONDS 30
#endif

static WebKitWebView *semantix_web_view = NULL;
static gboolean semantix_recovery_pending = FALSE;
static gboolean semantix_recovery_load_started = FALSE;
static gboolean semantix_recovery_load_failed = FALSE;
static gint64 semantix_last_recovery_at = 0;
static guint semantix_recovery_timeout_id = 0;
static guint64 semantix_generation = 0;
static guint64 semantix_pending_generation = 0;
static WebKitWebProcessTerminationReason semantix_pending_reason = WEBKIT_WEB_PROCESS_CRASHED;

#ifdef SEMANTIX_WEBKIT_SMOKE
enum {
  SEMANTIX_WEBKIT_SMOKE_SUCCESS = 1,
  SEMANTIX_WEBKIT_SMOKE_FAILURE = 2,
  SEMANTIX_WEBKIT_SMOKE_TIMEOUT = 3,
  SEMANTIX_WEBKIT_SMOKE_COOLDOWN = 4
};
static GMainLoop *semantix_test_loop = NULL;
static WebKitWebView *semantix_test_web_view = NULL;
static int semantix_test_mode = 0;
static int semantix_test_event_count = 0;
static int semantix_test_reload_count_value = 0;
static gboolean semantix_test_initial_termination = FALSE;
static gboolean semantix_test_timed_out = FALSE;
static guint semantix_test_safety_timeout_id = 0;
#endif

static GtkWidget *semantix_find_web_view(GtkWidget *widget) {
  if (WEBKIT_IS_WEB_VIEW(widget)) return widget;
  if (!GTK_IS_CONTAINER(widget)) return NULL;
  GList *children = gtk_container_get_children(GTK_CONTAINER(widget));
  GtkWidget *found = NULL;
  for (GList *item = children; item != NULL && found == NULL; item = item->next) {
    found = semantix_find_web_view(GTK_WIDGET(item->data));
  }
  g_list_free(children);
  return found;
}

static void semantix_finish_recovery(int outcome) {
  if (!semantix_recovery_pending) return;
  semantix_recovery_pending = FALSE;
  semantix_recovery_load_started = FALSE;
  semantix_recovery_load_failed = FALSE;
  if (semantix_recovery_timeout_id != 0) {
    g_source_remove(semantix_recovery_timeout_id);
    semantix_recovery_timeout_id = 0;
  }
  semantixWebKitProcessTerminated((int)semantix_pending_reason, outcome,
                                  (unsigned long long)semantix_pending_generation);
}

static gboolean semantix_recovery_timeout(gpointer data) {
  (void)data;
  semantix_recovery_timeout_id = 0;
  if (semantix_recovery_pending) {
    semantix_recovery_pending = FALSE;
    semantix_recovery_load_started = FALSE;
    semantix_recovery_load_failed = FALSE;
    semantixWebKitProcessTerminated((int)semantix_pending_reason, 2,
                                    (unsigned long long)semantix_pending_generation);
  }
  return G_SOURCE_REMOVE;
}

static gboolean semantix_reload_after_termination(gpointer data) {
  WebKitWebView *web_view = WEBKIT_WEB_VIEW(data);
  if (!semantix_recovery_pending || web_view != semantix_web_view) {
    return G_SOURCE_REMOVE;
  }
#ifdef SEMANTIX_WEBKIT_SMOKE
  semantix_test_reload_count_value++;
#endif
  webkit_web_view_reload(web_view);
  return G_SOURCE_REMOVE;
}

static void semantix_web_process_terminated(WebKitWebView *web_view,
                                            WebKitWebProcessTerminationReason reason,
                                            gpointer data) {
  (void)data;
  gint64 now = g_get_monotonic_time();
  guint64 generation = ++semantix_generation;
  if (semantix_recovery_pending ||
      (semantix_last_recovery_at != 0 && now - semantix_last_recovery_at < SEMANTIX_RECOVERY_COOLDOWN_US)) {
    semantixWebKitProcessTerminated((int)reason, 0, (unsigned long long)generation);
    return;
  }
  semantix_last_recovery_at = now;
  semantix_pending_reason = reason;
  semantix_pending_generation = generation;
  semantix_recovery_pending = TRUE;
  semantix_recovery_load_started = FALSE;
  semantix_recovery_load_failed = FALSE;
  semantix_recovery_timeout_id = g_timeout_add_seconds(SEMANTIX_RECOVERY_TIMEOUT_SECONDS,
                                                        semantix_recovery_timeout, NULL);
  // Let WebKit finish dispatching the termination signal before starting a
  // replacement process. Reloading synchronously from this callback is not
  // reliable across WebKitGTK versions.
  g_idle_add_full(G_PRIORITY_DEFAULT_IDLE, semantix_reload_after_termination,
                  g_object_ref(web_view), g_object_unref);
}

static gboolean semantix_load_failed(WebKitWebView *web_view, WebKitLoadEvent event,
                                     const gchar *uri, GError *error, gpointer data) {
  (void)web_view;
  (void)event;
  (void)uri;
  (void)error;
  (void)data;
  // WebKit may deliver load-failed for the terminated navigation after the
  // process-terminated signal. Only failures belonging to the reload that we
  // started are recovery failures.
  if (semantix_recovery_pending && semantix_recovery_load_started) {
    semantix_recovery_load_failed = TRUE;
  }
  return FALSE;
}

static void semantix_load_changed(WebKitWebView *web_view, WebKitLoadEvent event, gpointer data) {
  (void)web_view;
  (void)data;
  if (!semantix_recovery_pending) return;
  if (event == WEBKIT_LOAD_STARTED) {
    semantix_recovery_load_started = TRUE;
    semantix_recovery_load_failed = FALSE;
    return;
  }
  if (semantix_recovery_load_started && event == WEBKIT_LOAD_FINISHED) {
#ifdef SEMANTIX_WEBKIT_SMOKE
    if (semantix_test_mode == SEMANTIX_WEBKIT_SMOKE_TIMEOUT) return;
    if (semantix_test_mode == SEMANTIX_WEBKIT_SMOKE_FAILURE) {
      semantix_recovery_load_failed = TRUE;
    }
#endif
    semantix_finish_recovery(semantix_recovery_load_failed ? 2 : 1);
  }
}

static void semantix_web_view_destroyed(GtkWidget *widget, gpointer data) {
  (void)widget;
  (void)data;
  if (semantix_recovery_pending) semantix_finish_recovery(2);
  semantix_web_view = NULL;
}

static gboolean semantix_attach_webkit_observer(gpointer data) {
  (void)data;
  if (semantix_web_view != NULL) return G_SOURCE_REMOVE;
  GList *windows = gtk_window_list_toplevels();
  GtkWidget *found = NULL;
  for (GList *item = windows; item != NULL && found == NULL; item = item->next) {
    found = semantix_find_web_view(GTK_WIDGET(item->data));
  }
  g_list_free(windows);
  if (found == NULL) return G_SOURCE_REMOVE;
  if (g_signal_lookup("web-process-terminated", WEBKIT_TYPE_WEB_VIEW) == 0) {
    return G_SOURCE_REMOVE;
  }

  WebKitWebView *web_view = WEBKIT_WEB_VIEW(found);
  if (g_signal_connect(web_view, "web-process-terminated",
                       G_CALLBACK(semantix_web_process_terminated), NULL) == 0) {
    return G_SOURCE_REMOVE;
  }
  semantix_web_view = web_view;
  g_signal_connect(semantix_web_view, "load-failed", G_CALLBACK(semantix_load_failed), NULL);
  g_signal_connect(semantix_web_view, "load-changed", G_CALLBACK(semantix_load_changed), NULL);
  g_signal_connect(semantix_web_view, "destroy", G_CALLBACK(semantix_web_view_destroyed), NULL);

  WebKitSettings *settings = webkit_web_view_get_settings(semantix_web_view);
  WebKitHardwareAccelerationPolicy policy = WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND;
  if (settings != NULL) policy = webkit_settings_get_hardware_acceleration_policy(settings);
  semantixWebKitRuntimeReady((int)webkit_get_major_version(), (int)webkit_get_minor_version(),
                            (int)webkit_get_micro_version(), (int)policy);
  return G_SOURCE_REMOVE;
}

void semantix_install_webkit_observer(void) {
  g_main_context_invoke(NULL, semantix_attach_webkit_observer, NULL);
}

#ifdef SEMANTIX_WEBKIT_SMOKE
static gboolean semantix_test_terminate_again(gpointer data) {
  (void)data;
  if (semantix_test_web_view != NULL) {
    webkit_web_view_terminate_web_process(semantix_test_web_view);
  }
  return G_SOURCE_REMOVE;
}

static void semantix_test_initial_load_changed(WebKitWebView *web_view,
                                                WebKitLoadEvent event,
                                                gpointer data) {
  (void)data;
  if (event != WEBKIT_LOAD_FINISHED || semantix_test_initial_termination) return;
  semantix_test_initial_termination = TRUE;
  webkit_web_view_terminate_web_process(web_view);
}

static gboolean semantix_test_safety_timeout(gpointer data) {
  (void)data;
  semantix_test_safety_timeout_id = 0;
  semantix_test_timed_out = TRUE;
  if (semantix_test_loop != NULL) g_main_loop_quit(semantix_test_loop);
  return G_SOURCE_REMOVE;
}

void semantix_test_webkit_event_seen(int reason, int recovery) {
  (void)reason;
  semantix_test_event_count++;
  if (semantix_test_mode == SEMANTIX_WEBKIT_SMOKE_COOLDOWN &&
      semantix_test_event_count == 1 && recovery == 1) {
    g_idle_add(semantix_test_terminate_again, NULL);
    return;
  }
  if (semantix_test_loop != NULL) g_main_loop_quit(semantix_test_loop);
}

int semantix_test_webkit_reload_count(void) {
  return semantix_test_reload_count_value;
}

int semantix_test_webkit_run(int mode) {
  if (mode < SEMANTIX_WEBKIT_SMOKE_SUCCESS || mode > SEMANTIX_WEBKIT_SMOKE_COOLDOWN) return -1;
  if (!gtk_init_check(NULL, NULL)) return -2;

  if (semantix_recovery_timeout_id != 0) {
    g_source_remove(semantix_recovery_timeout_id);
    semantix_recovery_timeout_id = 0;
  }
  semantix_web_view = NULL;
  semantix_recovery_pending = FALSE;
  semantix_recovery_load_started = FALSE;
  semantix_recovery_load_failed = FALSE;
  semantix_last_recovery_at = 0;
  semantix_generation = 0;
  semantix_pending_generation = 0;
  semantix_test_mode = mode;
  semantix_test_event_count = 0;
  semantix_test_reload_count_value = 0;
  semantix_test_initial_termination = FALSE;
  semantix_test_timed_out = FALSE;

  GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  GtkWidget *widget = webkit_web_view_new();
  if (window == NULL || widget == NULL) return -3;
  gtk_container_add(GTK_CONTAINER(window), widget);
  gtk_widget_show_all(window);
  if (semantix_attach_webkit_observer(NULL) != G_SOURCE_REMOVE || semantix_web_view == NULL) {
    gtk_widget_destroy(window);
    return -4;
  }
  semantix_test_web_view = WEBKIT_WEB_VIEW(widget);
  g_signal_connect(widget, "load-changed", G_CALLBACK(semantix_test_initial_load_changed), NULL);
  semantix_test_loop = g_main_loop_new(NULL, FALSE);
  semantix_test_safety_timeout_id = g_timeout_add_seconds(15, semantix_test_safety_timeout, NULL);
  webkit_web_view_load_uri(
      semantix_test_web_view,
      "data:text/html,%3Chtml%3E%3Cbody%3ESemantix%20WebKit%20native%20smoke%3C%2Fbody%3E%3C%2Fhtml%3E");
  g_main_loop_run(semantix_test_loop);

  if (semantix_test_safety_timeout_id != 0) {
    g_source_remove(semantix_test_safety_timeout_id);
    semantix_test_safety_timeout_id = 0;
  }
  g_main_loop_unref(semantix_test_loop);
  semantix_test_loop = NULL;
  gtk_widget_destroy(window);
  semantix_test_web_view = NULL;
  semantix_web_view = NULL;
  return semantix_test_timed_out ? -5 : 0;
}
#endif
