// `bindings.gpu` where its renderer is not built (no SDL3, or a platform its Metal shaders do
// not run on): `lyra_gpu_create` answers NULL and `ui.gpu` draws with SDL's 2D renderer.
#include <stdbool.h>
#include <stdint.h>

void *lyra_gpu_create(void *window) { (void)window; return 0; }
void lyra_gpu_destroy(void *g) { (void)g; }
int64_t lyra_gpu_upload(void *g, int64_t w, int64_t h, const uint32_t *argb, bool pixelated) {
  (void)g; (void)w; (void)h; (void)argb; (void)pixelated;
  return -1;
}
bool lyra_gpu_update(void *g, int64_t id, int64_t x, int64_t y, int64_t w, int64_t h, const uint32_t *argb) {
  (void)g; (void)id; (void)x; (void)y; (void)w; (void)h; (void)argb;
  return false;
}
bool lyra_gpu_texture_size(void *g, int64_t id, int64_t *w, int64_t *h) {
  (void)g; (void)id; (void)w; (void)h;
  return false;
}
bool lyra_gpu_frame(void *g, float r, float gr, float b, float a, const float *instances,
                    int64_t instance_count, const int64_t *commands, int64_t command_count) {
  (void)g; (void)r; (void)gr; (void)b; (void)a; (void)instances; (void)instance_count;
  (void)commands; (void)command_count;
  return false;
}
void lyra_gpu_keep_frames(void *g, bool keep) { (void)g; (void)keep; }
bool lyra_gpu_read(void *g, uint32_t *out, int64_t cap, int64_t *w, int64_t *h) {
  (void)g; (void)out; (void)cap; (void)w; (void)h;
  return false;
}
void lyra_gpu_pin_contents(void *window) { (void)window; }
bool lyra_gpu_set_see_through(void *window, bool on) { (void)window; (void)on; return false; }
bool lyra_gpu_set_backdrop_blur(void *window, int32_t radius) { (void)window; (void)radius; return false; }
bool lyra_gpu_see_through_state(void *window) { (void)window; return false; }
int32_t lyra_gpu_backdrop_state(void *window) { (void)window; return -1; }
bool lyra_gpu_release(void *g, int64_t id) { (void)g; (void)id; return false; }
bool lyra_gpu_pinned_state(void *window) { (void)window; return false; }
bool lyra_gpu_set_backdrop_regions(void *window, const float *regions, int32_t count) {
    (void)window; (void)regions; (void)count;
    return false;
}
int32_t lyra_gpu_backdrop_regions_state(void *window) { (void)window; return -1; }
