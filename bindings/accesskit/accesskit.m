// The C half of `bindings.accesskit`: a window's accessibility tree handed to AccessKit's
// macOS adapter, and the actions assistive technology asks for queued for the program.
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

/// A queued action: what, on which element, and its value.
typedef struct {
  int kind;
  uint64_t target;
  double number;
  char *text;
} request;

static accesskit_macos_subclassing_adapter *adapter = NULL;
static element *elements = NULL;
static int64_t element_count = 0, element_cap = 0;
static uint64_t root_id = 0, focus_id = 0;
static request *requests = NULL;
static int64_t request_count = 0, request_cap = 0;

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
    default: return ACCESSKIT_ROLE_GENERIC_CONTAINER;
  }
}

/// The whole snapshot as one tree update.
static accesskit_tree_update *snapshot(void *userdata) {
  (void)userdata;
  accesskit_tree_update *update =
      accesskit_tree_update_with_capacity_and_focus((size_t)element_count, focus_id);
  accesskit_tree_update_set_tree_info(update, accesskit_tree_info_new(root_id));
  for (int64_t i = 0; i < element_count; i++) {
    element *e = &elements[i];
    accesskit_node *node = accesskit_node_new(role_of(e->role));
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

static void queue(int kind, uint64_t target, double number, const char *text) {
  if (request_count == request_cap) {
    request_cap = request_cap ? request_cap * 2 : 16;
    requests = realloc(requests, (size_t)request_cap * sizeof(request));
  }
  requests[request_count++] = (request){kind, target, number, strdup(text ? text : "")};
  // Wake a program waiting for input: its platform takes the queue after the wait.
  SDL_Event wake;
  SDL_zero(wake);
  wake.type = SDL_EVENT_USER;
  SDL_PushEvent(&wake);
}

static void on_action(accesskit_action_request *r, void *userdata) {
  (void)userdata;
  double number = 0;
  const char *text = NULL;
  if (r->data.has_value) {
    if (r->data.value.tag == ACCESSKIT_ACTION_DATA_VALUE) text = r->data.value.value;
    if (r->data.value.tag == ACCESSKIT_ACTION_DATA_NUMERIC_VALUE) number = r->data.value.numeric_value;
  }
  switch (r->action) {
    case ACCESSKIT_ACTION_FOCUS: queue(1, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_CLICK: queue(2, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_SET_VALUE: queue(text ? 3 : 6, r->target_node, number, text); break;
    case ACCESSKIT_ACTION_INCREMENT: queue(4, r->target_node, 0, NULL); break;
    case ACCESSKIT_ACTION_DECREMENT: queue(5, r->target_node, 0, NULL); break;
    default: break;
  }
  accesskit_action_request_free(r);
}

/// Start accessibility for `window`; whether AccessKit took it.
bool lyra_access_start(SDL_Window *window) {
  if (adapter) return true;
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
  adapter = accesskit_macos_subclassing_adapter_for_window(ns, snapshot, NULL, on_action, NULL);
  if (!adapter) return false;
  accesskit_macos_queued_events *events =
      accesskit_macos_subclassing_adapter_update_view_focus_state(adapter, true);
  if (events) accesskit_macos_queued_events_raise(events);
  return true;
}

void lyra_access_stop(void) {
  if (adapter) accesskit_macos_subclassing_adapter_free(adapter);
  adapter = NULL;
}

static void clear(void) {
  for (int64_t i = 0; i < element_count; i++) {
    free(elements[i].label);
    free(elements[i].value);
    free(elements[i].description);
    free(elements[i].children);
  }
  element_count = 0;
}

/// A new snapshot, rooted at `root`, `focus` the element with the keyboard's focus.
void lyra_access_begin(uint64_t root, uint64_t focus) {
  clear();
  root_id = root;
  focus_id = focus;
}

void lyra_access_node(uint64_t id, int32_t role, const char *label, const char *value,
                      const char *description, double x, double y, double w, double h, int32_t toggled,
                      int32_t has_numeric, double numeric, double min, double max,
                      double step, int32_t actions, const uint64_t *children,
                      int64_t child_count) {
  if (element_count == element_cap) {
    element_cap = element_cap ? element_cap * 2 : 64;
    elements = realloc(elements, (size_t)element_cap * sizeof(element));
  }
  uint64_t *kids = NULL;
  if (child_count > 0) {
    kids = malloc((size_t)child_count * sizeof(uint64_t));
    memcpy(kids, children, (size_t)child_count * sizeof(uint64_t));
  }
  elements[element_count++] = (element){
      id, role, strdup(label), strdup(value), strdup(description), x, y, w, h, toggled, has_numeric,
      numeric, min, max, step, actions, kids, child_count};
}

/// The snapshot to AccessKit, if assistive technology is listening.
void lyra_access_commit(void) {
  if (!adapter) return;
  accesskit_macos_queued_events *events =
      accesskit_macos_subclassing_adapter_update_if_active(adapter, snapshot, NULL);
  if (events) accesskit_macos_queued_events_raise(events);
}

/// The oldest queued action, if any: its kind (0 when the queue is empty), its element, its
/// number, and its text copied into `text` (at most `cap` bytes, NUL-terminated).
int32_t lyra_access_next(uint64_t *target, double *number, char *text, int64_t cap) {
  if (request_count == 0) return 0;
  request r = requests[0];
  memmove(requests, requests + 1, (size_t)(request_count - 1) * sizeof(request));
  request_count--;
  *target = r.target;
  *number = r.number;
  if (cap > 0) {
    strncpy(text, r.text, (size_t)cap - 1);
    text[cap - 1] = 0;
  }
  free(r.text);
  return r.kind;
}
