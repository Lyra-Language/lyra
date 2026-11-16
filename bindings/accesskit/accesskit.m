// The C half of `bindings.accesskit`: each window's accessibility tree handed to AccessKit's
// macOS adapter — an adapter a window, its own snapshot and focus — and the actions assistive
// technology asks for queued for the program with the window they came from.
//
// Lyra cannot hand AccessKit a tree update the way AccessKit asks for one — from a callback,
// whenever VoiceOver starts listening — so the program hands this file a *snapshot* instead
// (`lyra_access_begin`, a `lyra_access_node` per element, `lyra_access_commit`), and this file
// builds AccessKit's nodes from it whenever AccessKit asks. AccessKit's action requests arrive
// on the main thread while SDL pumps AppKit's events; each is queued here, an SDL event is
// pushed so a program waiting for input wakes, and the program takes them with
// `lyra_access_next`.
//
// Objective-C only for `-fmodules`: `@import AppKit` autolinks the frameworks AccessKit's Rust
// needs, which `@link` cannot spell (bindings.menubar does the same); and the window's class
// name is read through the Objective-C runtime.

@import AppKit;
@import Foundation;
@import ObjectiveC.runtime;

#include <stdlib.h>
#include <string.h>
#include <SDL3/SDL.h>
#include "accesskit.h"

/// One element of the snapshot, as `lyra_access_node` gave it.
typedef struct {
  uint64_t id;
  int role;
  char *label;
  char *value;
  char *description;
  double x, y, w, h;
  int toggled;
  int has_numeric;
  double numeric, min, max, step;
  int actions;
  uint64_t *children;
  int64_t child_count;
} element;

/// A queued action: the window it came from (SDL's number), what, on which element, and its
/// value.
typedef struct {
  SDL_WindowID window;
  int kind;
  uint64_t target;
  double number;
  char *text;
} request;

/// One window AccessKit hears: its adapter and the snapshot last handed over for it. Each is
/// allocated alone, so the adapter's callbacks keep a pointer to it while others come and go.
typedef struct {
  SDL_Window *window;
  SDL_WindowID id;
  accesskit_macos_subclassing_adapter *adapter;
  element *elements;
  int64_t element_count, element_cap;
  uint64_t root_id, focus_id;
} surface;

static surface **surfaces = NULL;
static int64_t surface_count = 0, surface_cap = 0;
/// The window a snapshot is being handed over for, between `lyra_access_begin` and
/// `lyra_access_commit` — NULL for one AccessKit does not hear.
static surface *building = NULL;
static request *requests = NULL;
static int64_t request_count = 0, request_cap = 0;

static surface *surface_of(SDL_Window *window) {
  for (int64_t i = 0; i < surface_count; i++) {
    if (surfaces[i]->window == window) return surfaces[i];
  }
  return NULL;
}

/// `ui.backend`'s `AccessRole`, by the number `bindings.accesskit` passes.
static accesskit_role role_of(int role) {
  switch (role) {
    case 1: return ACCESSKIT_ROLE_WINDOW;
    case 2: return ACCESSKIT_ROLE_GROUP;
    case 3: return ACCESSKIT_ROLE_LABEL;
    case 4: return ACCESSKIT_ROLE_IMAGE;
    case 5: return ACCESSKIT_ROLE_BUTTON;
    case 6: return ACCESSKIT_ROLE_CHECK_BOX;
    case 7: return ACCESSKIT_ROLE_SLIDER;
    case 8: return ACCESSKIT_ROLE_TEXT_INPUT;
    case 9: return ACCESSKIT_ROLE_SCROLL_VIEW;
    case 10: return ACCESSKIT_ROLE_COMBO_BOX;
    case 11: return ACCESSKIT_ROLE_LIST_BOX;
    case 12: return ACCESSKIT_ROLE_LIST_BOX_OPTION;
    case 13: return ACCESSKIT_ROLE_TAB_LIST;
    case 14: return ACCESSKIT_ROLE_TAB;
    case 15: return ACCESSKIT_ROLE_RADIO_BUTTON;
    case 16: return ACCESSKIT_ROLE_RADIO_GROUP;
    case 17: return ACCESSKIT_ROLE_PROGRESS_INDICATOR;
    case 18: return ACCESSKIT_ROLE_TABLE;
    case 19: return ACCESSKIT_ROLE_ROW;
    case 20: return ACCESSKIT_ROLE_DIALOG;
    case 21: return ACCESSKIT_ROLE_TERMINAL;
    default: return ACCESSKIT_ROLE_GENERIC_CONTAINER;
  }
}

/// A window's whole snapshot as one tree update; `userdata` is its surface.
static accesskit_tree_update *snapshot(void *userdata) {
  surface *s = userdata;
  accesskit_tree_update *update =
      accesskit_tree_update_with_capacity_and_focus((size_t)s->element_count, s->focus_id);
  accesskit_tree_update_set_tree_info(update, accesskit_tree_info_new(s->root_id));
  for (int64_t i = 0; i < s->element_count; i++) {
    element *e = &s->elements[i];
    accesskit_node *node = accesskit_node_new(role_of(e->role));
    // `ui`'s one dialog is a modal's: nothing outside it takes input while it is open.
    if (e->role == 20) accesskit_node_set_modal(node);
    if (e->label[0]) accesskit_node_set_label(node, e->label);
    if (e->value[0]) accesskit_node_set_value(node, e->value);
    if (e->description[0]) accesskit_node_set_description(node, e->description);
    accesskit_rect bounds = {e->x, e->y, e->x + e->w, e->y + e->h};
    accesskit_node_set_bounds(node, bounds);
    if (e->toggled >= 0) {
      accesskit_node_set_toggled(node, e->toggled ? ACCESSKIT_TOGGLED_TRUE : ACCESSKIT_TOGGLED_FALSE);
    }
    if (e->has_numeric) {
      accesskit_node_set_numeric_value(node, e->numeric);
      accesskit_node_set_min_numeric_value(node, e->min);
      accesskit_node_set_max_numeric_value(node, e->max);
      if (e->step > 0) accesskit_node_set_numeric_value_step(node, e->step);
    }
    if (e->actions & 1) accesskit_node_add_action(node, ACCESSKIT_ACTION_FOCUS);
    if (e->actions & 2) accesskit_node_add_action(node, ACCESSKIT_ACTION_CLICK);
    if (e->actions & 4) accesskit_node_add_action(node, ACCESSKIT_ACTION_SET_VALUE);
    if (e->actions & 8) accesskit_node_add_action(node, ACCESSKIT_ACTION_INCREMENT);
    if (e->actions & 16) accesskit_node_add_action(node, ACCESSKIT_ACTION_DECREMENT);
    for (int64_t c = 0; c < e->child_count; c++) accesskit_node_push_child(node, e->children[c]);
    accesskit_tree_update_push_node(update, e->id, node);
  }
  return update;
}

static void queue(SDL_WindowID window, int kind, uint64_t target, double number,
                  const char *text) {
  if (request_count == request_cap) {
    request_cap = request_cap ? request_cap * 2 : 16;
    requests = realloc(requests, (size_t)request_cap * sizeof(request));
  }
  requests[request_count++] = (request){window, kind, target, number, strdup(text ? text : "")};
  // Wake a program waiting for input: its platform takes the queue after the wait.
  SDL_Event wake;
  SDL_zero(wake);
  wake.type = SDL_EVENT_USER;
  SDL_PushEvent(&wake);
}

/// An action asked of a window's tree; `userdata` is its surface.
static void on_action(accesskit_action_request *r, void *userdata) {
  SDL_WindowID window = ((surface *)userdata)->id;
  double number = 0;
  const char *text = NULL;
  if (r->data.has_value) {
    if (r->data.value.tag == ACCESSKIT_ACTION_DATA_VALUE) text = r->data.value.value;
    if (r->data.value.tag == ACCESSKIT_ACTION_DATA_NUMERIC_VALUE) number = r->data.value.numeric_value;
  }
  switch (r->action) {
    case ACCESSKIT_ACTION_FOCUS: queue(window, 1, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_CLICK: queue(window, 2, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_SET_VALUE: queue(window, text ? 3 : 6, r->target_node, number, text); break;
    case ACCESSKIT_ACTION_INCREMENT: queue(window, 4, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_DECREMENT: queue(window, 5, r->target_node, 0, NULL); break;
    default: break;
  }
  accesskit_action_request_free(r);
}

/// Start accessibility for `window` — the main window, or one opened beside it — whether
/// AccessKit took it. Once a window: a second start of one it hears is `true` and nothing more.
bool lyra_access_start(SDL_Window *window) {
  if (surface_of(window)) return true;
  void *ns = SDL_GetPointerProperty(SDL_GetWindowProperties(window),
                                    SDL_PROP_WINDOW_COCOA_WINDOW_POINTER, NULL);
  if (!ns) return false;
  // The window's own class, whatever SDL names it, forwards focus to AccessKit's view — once
  // a class: AccessKit panics adding the method to a class that has it, which a program's
  // second window (or a window opened again) would.
  const char *cls = object_getClassName((__bridge id)ns);
  static char patched[8][128];
  static int patched_count = 0;
  bool done = false;
  for (int i = 0; i < patched_count; i++) done = done || strcmp(patched[i], cls) == 0;
  if (!done) {
    accesskit_macos_add_focus_forwarder_to_window_class(cls);
    if (patched_count < 8) strncpy(patched[patched_count++], cls, 127);
  }
  surface *s = calloc(1, sizeof(surface));
  s->window = window;
  s->id = SDL_GetWindowID(window);
  s->adapter = accesskit_macos_subclassing_adapter_for_window(ns, snapshot, s, on_action, s);
  if (!s->adapter) {
    free(s);
    return false;
  }
  if (surface_count == surface_cap) {
    surface_cap = surface_cap ? surface_cap * 2 : 4;
    surfaces = realloc(surfaces, (size_t)surface_cap * sizeof(surface *));
  }
  surfaces[surface_count++] = s;
  // A window is started as it opens and shows, so as the one with the keyboard; each later
  // change is said by `lyra_access_focus`.
  accesskit_macos_queued_events *events =
      accesskit_macos_subclassing_adapter_update_view_focus_state(s->adapter, true);
  if (events) accesskit_macos_queued_events_raise(events);
  return true;
}

static void clear(surface *s) {
  for (int64_t i = 0; i < s->element_count; i++) {
    free(s->elements[i].label);
    free(s->elements[i].value);
    free(s->elements[i].description);
    free(s->elements[i].children);
  }
  s->element_count = 0;
}

/// `window`'s adapter and snapshot freed, before the window is destroyed; its queued requests
/// stay, and the program drops those of a window it has closed.
void lyra_access_stop(SDL_Window *window) {
  for (int64_t i = 0; i < surface_count; i++) {
    surface *s = surfaces[i];
    if (s->window != window) continue;
    if (building == s) building = NULL;
    accesskit_macos_subclassing_adapter_free(s->adapter);
    clear(s);
    free(s->elements);
    free(s);
    memmove(surfaces + i, surfaces + i + 1, (size_t)(surface_count - i - 1) * sizeof(surface *));
    surface_count--;
    return;
  }
}

/// Every window's adapter freed, as the program ends.
void lyra_access_stop_all(void) {
  while (surface_count > 0) lyra_access_stop(surfaces[surface_count - 1]->window);
}

/// Whether `window` has the keyboard now, for its adapter: VoiceOver follows the window with
/// it.
void lyra_access_focus(SDL_Window *window, bool focused) {
  surface *s = surface_of(window);
  if (!s) return;
  accesskit_macos_queued_events *events =
      accesskit_macos_subclassing_adapter_update_view_focus_state(s->adapter, focused);
  if (events) accesskit_macos_queued_events_raise(events);
}

/// A new snapshot of `window`'s tree, rooted at `root`, `focus` the element with the keyboard's
/// focus; nothing is kept for a window AccessKit does not hear.
void lyra_access_begin(SDL_Window *window, uint64_t root, uint64_t focus) {
  building = surface_of(window);
  if (!building) return;
  clear(building);
  building->root_id = root;
  building->focus_id = focus;
}

void lyra_access_node(uint64_t id, int32_t role, const char *label, const char *value,
                      const char *description, double x, double y, double w, double h, int32_t toggled,
                      int32_t has_numeric, double numeric, double min, double max,
                      double step, int32_t actions, const uint64_t *children,
                      int64_t child_count) {
  surface *s = building;
  if (!s) return;
  if (s->element_count == s->element_cap) {
    s->element_cap = s->element_cap ? s->element_cap * 2 : 64;
    s->elements = realloc(s->elements, (size_t)s->element_cap * sizeof(element));
  }
  uint64_t *kids = NULL;
  if (child_count > 0) {
    kids = malloc((size_t)child_count * sizeof(uint64_t));
    memcpy(kids, children, (size_t)child_count * sizeof(uint64_t));
  }
  s->elements[s->element_count++] = (element){
      id, role, strdup(label), strdup(value), strdup(description), x, y, w, h, toggled, has_numeric,
      numeric, min, max, step, actions, kids, child_count};
}

/// The snapshot begun to AccessKit, if assistive technology is listening to its window.
void lyra_access_commit(void) {
  surface *s = building;
  building = NULL;
  if (!s) return;
  accesskit_macos_queued_events *events =
      accesskit_macos_subclassing_adapter_update_if_active(s->adapter, snapshot, s);
  if (events) accesskit_macos_queued_events_raise(events);
}

/// The oldest queued action, if any: its kind (0 when the queue is empty), the window it came
/// from (SDL's number), its element, its number, and its text copied into `text` (at most `cap`
/// bytes, NUL-terminated).
int32_t lyra_access_next(uint32_t *window, uint64_t *target, double *number, char *text,
                         int64_t cap) {
  if (request_count == 0) return 0;
  request r = requests[0];
  memmove(requests, requests + 1, (size_t)(request_count - 1) * sizeof(request));
  request_count--;
  *window = r.window;
  *target = r.target;
  *number = r.number;
  if (cap > 0) {
    strncpy(text, r.text, (size_t)cap - 1);
    text[cap - 1] = 0;
  }
  free(r.text);
  return r.kind;
}
