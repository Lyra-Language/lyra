// bindings.hittest's C half: SDL's hit test answered from areas the program hands it. A window
// drawing its own title bar has no system frame, so SDL asks, at each press, what that point of
// the window is: an area that moves the window (a bar's empty stretch), one of the program's
// own controls inside it (which keeps the press), or — where the window is borderless,
// resizable and neither maximized nor fullscreen — an edge or corner it is resized from.
//
// **Why a shim**: SDL asks through a C function pointer, and Lyra cannot hand C a function. So
// the areas are copied here, a slot a window, and `answer` reads the slot SDL hands back with
// the press. A program's windows each draw their own bar or not; up to 16 at once draw theirs.
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
#define MAX_WINDOWS 16

// One window's areas, each four floats — x, y, width, height — in the window's coordinates,
// and the margin round its edges. `window` is NULL for a free slot.
typedef struct {
    void *window;
    float drags[MAX_AREAS * 4];
    int drag_count;
    float keeps[MAX_AREAS * 4];
    int keep_count;
    float margin;
} Areas;

// Slot 0 is no window's: the areas `lyra_hittest_set_areas` sets for `lyra_hittest_answer`, a
// test's. The rest are windows', each handed to SDL with the window's hit test.
static Areas slots[MAX_WINDOWS + 1];

// The slot of `window`, a free one taken for it if it has none — NULL when all are taken.
static Areas *slot_of(void *window) {
    Areas *free_slot = NULL;
    for (int i = 1; i <= MAX_WINDOWS; i++) {
        if (slots[i].window == window) return &slots[i];
        if (!slots[i].window && !free_slot) free_slot = &slots[i];
    }
    if (free_slot) {
        memset(free_slot, 0, sizeof(Areas));
        free_slot->window = window;
    }
    return free_slot;
}

static void copy_areas(Areas *a, const float *drag, int32_t drag_n, const float *keep,
                       int32_t keep_n, float edge) {
    if (drag_n > MAX_AREAS) drag_n = MAX_AREAS;
    if (keep_n > MAX_AREAS) keep_n = MAX_AREAS;
    a->drag_count = drag_n < 0 ? 0 : drag_n;
    a->keep_count = keep_n < 0 ? 0 : keep_n;
    if (a->drag_count > 0) memcpy(a->drags, drag, sizeof(float) * 4 * (size_t)a->drag_count);
    if (a->keep_count > 0) memcpy(a->keeps, keep, sizeof(float) * 4 * (size_t)a->keep_count);
    a->margin = edge;
}

static bool inside(const float *areas, int count, float x, float y) {
    for (int i = 0; i < count; i++) {
        const float *a = areas + i * 4;
        if (x >= a[0] && y >= a[1] && x < a[0] + a[2] && y < a[1] + a[3]) return true;
    }
    return false;
}

// What the point (x, y) of a window `w` × `h` with `flags` and areas `a` is: an edge or corner
// to resize from, one of the program's controls, an area that moves the window, or the rest.
static int32_t answer_in(const Areas *a, float x, float y, float w, float h, uint64_t flags) {
    float margin = a->margin;
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
    if (inside(a->keeps, a->keep_count, x, y)) return HIT_NORMAL;
    if (inside(a->drags, a->drag_count, x, y)) return HIT_DRAGGABLE;
    return HIT_NORMAL;
}

// The same, from the areas `lyra_hittest_set_areas` last set with no window — what a test asks.
int32_t lyra_hittest_answer(float x, float y, float w, float h, uint64_t flags) {
    return answer_in(&slots[0], x, y, w, h, flags);
}

// SDL's question for a press in `window`; `data` is the window's slot.
static int answer(void *window, const HitPoint *point, void *data) {
    int w = 0, h = 0;
    SDL_GetWindowSize(window, &w, &h);
    return answer_in(data, (float)point->x, (float)point->y, (float)w, (float)h,
                     SDL_GetWindowFlags(window));
}

// The areas that move the window (`drag_n` of them, four floats each) and those inside them that
// keep a press (`keep_n`), in the window's coordinates, at most 64 each; and the margin round a
// borderless window's edges it resizes from — set with no window, what `lyra_hittest_answer`
// answers from.
void lyra_hittest_set_areas(const float *drag, int32_t drag_n, const float *keep, int32_t keep_n,
                            float edge) {
    copy_areas(&slots[0], drag, drag_n, keep, keep_n, edge);
}

// `window`'s areas set, and its hit test installed the first time. False where SDL refuses a hit
// test (its dummy driver), or 16 windows have theirs already.
bool lyra_hittest_set(void *window, const float *drag, int32_t drag_n, const float *keep,
                      int32_t keep_n, float edge) {
    bool installed = false;
    for (int i = 1; i <= MAX_WINDOWS; i++) installed = installed || slots[i].window == window;
    Areas *a = slot_of(window);
    if (!a) return false;
    copy_areas(a, drag, drag_n, keep, keep_n, edge);
    if (installed) return true;
    if (!SDL_SetWindowHitTest(window, answer, a)) {
        a->window = NULL;
        return false;
    }
    return true;
}

// `window`'s slot given back, as the window is destroyed (SDL drops its hit test with it).
void lyra_hittest_forget(void *window) {
    for (int i = 1; i <= MAX_WINDOWS; i++) {
        if (slots[i].window == window) slots[i].window = NULL;
    }
}
