// `bindings.accesskit` where AccessKit is not built in (another platform, or a build that
// could not download it): the same symbols, doing nothing — no assistive technology hears the
// program, which otherwise runs as it would.
#include <stdbool.h>
#include <stdint.h>

bool lyra_access_start(void *window) { (void)window; return false; }
void lyra_access_stop(void) {}
void lyra_access_begin(uint64_t root, uint64_t focus) { (void)root; (void)focus; }
void lyra_access_node(uint64_t id, int32_t role, const char *label, const char *value,
                      double x, double y, double w, double h, int32_t toggled,
                      int32_t has_numeric, double numeric, double min, double max,
                      double step, int32_t actions, const uint64_t *children,
                      int64_t child_count) {
  (void)id; (void)role; (void)label; (void)value; (void)x; (void)y; (void)w; (void)h;
  (void)toggled; (void)has_numeric; (void)numeric; (void)min; (void)max; (void)step;
  (void)actions; (void)children; (void)child_count;
}
void lyra_access_commit(void) {}
int32_t lyra_access_next(uint64_t *target, double *number, char *text, int64_t cap) {
  (void)target; (void)number; (void)text; (void)cap;
  return 0;
}
