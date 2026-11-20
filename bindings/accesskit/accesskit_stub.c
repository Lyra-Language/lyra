// `bindings.accesskit` where AccessKit is not built in (another platform, or a build that
// could not download it): the same symbols, doing nothing — no assistive technology hears the
// program, which otherwise runs as it would. Only the ask for less motion is answered, on Linux.
#include <stdbool.h>
#include <stdint.h>
#if defined(__linux__)
#include <dlfcn.h>
#include <stddef.h>
#endif

bool lyra_access_start(void *window) { (void)window; return false; }
void lyra_access_stop(void *window) { (void)window; }
void lyra_access_stop_all(void) {}
void lyra_access_focus(void *window, bool focused) { (void)window; (void)focused; }
void lyra_access_begin(void *window, uint64_t root, uint64_t focus) {
  (void)window; (void)root; (void)focus;
}
void lyra_access_node(uint64_t id, int32_t role, const char *label, const char *value,
                      const char *description, double x, double y, double w, double h, int32_t toggled,
                      int32_t has_numeric, double numeric, double min, double max,
                      double step, int32_t actions, const uint64_t *children,
                      int64_t child_count, int64_t color, int32_t states) {
  (void)color; (void)states;
  (void)id; (void)role; (void)label; (void)value; (void)description; (void)x; (void)y; (void)w; (void)h;
  (void)toggled; (void)has_numeric; (void)numeric; (void)min; (void)max; (void)step;
  (void)actions; (void)children; (void)child_count;
}
void lyra_access_commit(void) {}

#if defined(__linux__)
// GNOME's "Reduce animation" (Settings → Accessibility → Seeing) is `enable-animations` off in
// `org.gnome.desktop.interface`, read through GIO, which is opened at the first call rather
// than linked: a desktop without it, or without that schema or key, never asks for less. GIO
// would abort on a schema or key it lacks, so both are looked up first. Read each call: dconf
// sees another process's write with no main loop running.
typedef int (*get_boolean_fn)(void *settings, const char *key);
static void *settings;
static get_boolean_fn get_boolean;

static void open_settings(void) {
  void *gio = dlopen("libgio-2.0.so.0", RTLD_LAZY | RTLD_LOCAL);
  if (!gio) return;
  void *(*source_default)(void) = (void *(*)(void))dlsym(gio, "g_settings_schema_source_get_default");
  void *(*lookup)(void *, const char *, int) =
      (void *(*)(void *, const char *, int))dlsym(gio, "g_settings_schema_source_lookup");
  int (*has_key)(void *, const char *) = (int (*)(void *, const char *))dlsym(gio, "g_settings_schema_has_key");
  void (*schema_unref)(void *) = (void (*)(void *))dlsym(gio, "g_settings_schema_unref");
  void *(*settings_new)(const char *) = (void *(*)(const char *))dlsym(gio, "g_settings_new");
  get_boolean = (get_boolean_fn)dlsym(gio, "g_settings_get_boolean");
  if (!source_default || !lookup || !has_key || !schema_unref || !settings_new || !get_boolean) return;
  void *source = source_default();
  void *schema = source ? lookup(source, "org.gnome.desktop.interface", 1) : NULL;
  if (!schema) return;
  if (has_key(schema, "enable-animations")) settings = settings_new("org.gnome.desktop.interface");
  schema_unref(schema);
}

bool lyra_access_reduce_motion(void) {
  static bool opened = false;
  if (!opened) {
    opened = true;
    open_settings();
  }
  return settings && !get_boolean(settings, "enable-animations");
}
#else
bool lyra_access_reduce_motion(void) { return false; }
#endif

int32_t lyra_access_next(uint32_t *window, uint64_t *target, double *number, char *text,
                         int64_t cap) {
  (void)window; (void)target; (void)number; (void)text; (void)cap;
  return 0;
}
