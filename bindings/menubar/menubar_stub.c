// `bindings.menubar` where there is no native menu bar: every call does nothing,
// `lyra_menubar_install` answers false and nothing is ever chosen. It exists so a program
// using the binding links on every platform and asks `install` whether it got a menu,
// rather than failing to link off macOS. `menubar.m` is the real one.

#include <stdbool.h>
#include <stdint.h>

void lyra_menubar_menu(const char *title) { (void)title; }

void lyra_menubar_item(const char *title, const char *key, uint32_t mods, int32_t tag) {
  (void)title;
  (void)key;
  (void)mods;
  (void)tag;
}

void lyra_menubar_separator(void) {}

void lyra_menubar_window_menu(void) {}

bool lyra_menubar_install(const char *app_name) {
  (void)app_name;
  return false;
}

int32_t lyra_menubar_next(void) { return -1; }

void lyra_menubar_set_checked(int32_t tag, bool checked) {
  (void)tag;
  (void)checked;
}

void lyra_menubar_set_enabled(int32_t tag, bool enabled) {
  (void)tag;
  (void)enabled;
}
