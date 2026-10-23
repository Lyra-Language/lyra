// bindings.hittest's C half: SDL's hit test answered from areas the program hands it. A window
// drawing its own title bar has no system frame, so SDL asks, at each press, what that point of
// the window is: an area that moves the window (a bar's empty stretch), one of the program's
// own controls inside it (which keeps the press), or — where the window is borderless,
// resizable and neither maximized nor fullscreen — an edge or corner it is resized from.
//
// **Why a shim**: SDL asks through a C function pointer, and Lyra cannot hand C a function. So
// the areas are copied here, and `answer` reads them. One window's at a time — a program with
// a drawn title bar has one window.
//
// SDL's functions are declared here rather than taken from its headers, as bindings.wake's are:
// the program that links this links SDL3, and the archive builds with nothing but a C compiler.

#include <stdbool.h>
#include <stdint.h>
#include <string.h>

typedef struct { int x, y; } HitPoint;
typedef int (*HitTest)(void *window, const HitPoint *point, void *data);

extern bool SDL_SetWindowHitTest(void *window, HitTest callback, void *data);
extern uint64_t SDL_GetWindowFlags(void *window);
extern bool SDL_GetWindowSize(void *window, int *w, int *h);

// SDL_WindowFlags, and SDL_HitTestResult in its order.
#define FLAG_FULLSCREEN 0x1ull
#define FLAG_BORDERLESS 0x10ull
#define FLAG_RESIZABLE 0x20ull
#define FLAG_MAXIMIZED 0x80ull
enum {
    HIT_NORMAL,
    HIT_DRAGGABLE,
    HIT_RESIZE_TOPLEFT,
    HIT_RESIZE_TOP,
    HIT_RESIZE_TOPRIGHT,
    HIT_RESIZE_RIGHT,
    HIT_RESIZE_BOTTOMRIGHT,
    HIT_RESIZE_BOTTOM,
    HIT_RESIZE_BOTTOMLEFT,
    HIT_RESIZE_LEFT,
};

#define MAX_AREAS 64

// Each area four floats — x, y, width, height — in the window's coordinates.
static float drags[MAX_AREAS * 4];
static int drag_count = 0;
static float keeps[MAX_AREAS * 4];
static int keep_count = 0;
static float margin = 0.0f;
static void *installed = NULL;

static bool inside(const float *areas, int count, float x, float y) {
    for (int i = 0; i < count; i++) {
        const float *a = areas + i * 4;
        if (x >= a[0] && y >= a[1] && x < a[0] + a[2] && y < a[1] + a[3]) return true;
    }
    return false;
}

// What the point (x, y) of a window `w` × `h` with `flags` is: an edge or corner to resize from,
// one of the program's controls, an area that moves the window, or the rest of it.
int32_t lyra_hittest_answer(float x, float y, float w, float h, uint64_t flags) {
    bool frameless = (flags & FLAG_BORDERLESS) && (flags & FLAG_RESIZABLE) &&
                     !(flags & (FLAG_MAXIMIZED | FLAG_FULLSCREEN));
    if (frameless && margin > 0.0f) {
        bool left = x < margin, right = x >= w - margin, top = y < margin, bottom = y >= h - margin;
        if (top && left) return HIT_RESIZE_TOPLEFT;
        if (top && right) return HIT_RESIZE_TOPRIGHT;
        if (bottom && left) return HIT_RESIZE_BOTTOMLEFT;
        if (bottom && right) return HIT_RESIZE_BOTTOMRIGHT;
        if (top) return HIT_RESIZE_TOP;
        if (bottom) return HIT_RESIZE_BOTTOM;
        if (left) return HIT_RESIZE_LEFT;
        if (right) return HIT_RESIZE_RIGHT;
    }
    if (inside(keeps, keep_count, x, y)) return HIT_NORMAL;
    if (inside(drags, drag_count, x, y)) return HIT_DRAGGABLE;
    return HIT_NORMAL;
}

static int answer(void *window, const HitPoint *point, void *data) {
    (void)data;
    int w = 0, h = 0;
    SDL_GetWindowSize(window, &w, &h);
    return lyra_hittest_answer((float)point->x, (float)point->y, (float)w, (float)h,
                               SDL_GetWindowFlags(window));
}

// The areas that move the window (`drag_n` of them, four floats each) and those inside them that
// keep a press (`keep_n`), in the window's coordinates, at most 64 each; and the margin round a
// borderless window's edges it resizes from — what `lyra_hittest_answer` answers from.
void lyra_hittest_set_areas(const float *drag, int32_t drag_n, const float *keep, int32_t keep_n,
                            float edge) {
    if (drag_n > MAX_AREAS) drag_n = MAX_AREAS;
    if (keep_n > MAX_AREAS) keep_n = MAX_AREAS;
    drag_count = drag_n < 0 ? 0 : drag_n;
    keep_count = keep_n < 0 ? 0 : keep_n;
    if (drag_count > 0) memcpy(drags, drag, sizeof(float) * 4 * (size_t)drag_count);
    if (keep_count > 0) memcpy(keeps, keep, sizeof(float) * 4 * (size_t)keep_count);
    margin = edge;
}

// The areas set, and the hit test installed on `window` the first time. False where SDL refuses
// a hit test (its dummy driver).
bool lyra_hittest_set(void *window, const float *drag, int32_t drag_n, const float *keep,
                      int32_t keep_n, float edge) {
    lyra_hittest_set_areas(drag, drag_n, keep, keep_n, edge);
    if (window == installed) return true;
    if (!SDL_SetWindowHitTest(window, answer, NULL)) return false;
    installed = window;
    return true;
}
