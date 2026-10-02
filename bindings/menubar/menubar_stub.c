// `bindings.menubar` where there is no native menu bar: every call does nothing,
// `lyra_menubar_install` answers false and nothing is ever chosen. It exists so a program
// using the binding links on every platform and asks `install` whether it got a menu,
// rather than failing to link off macOS. `menubar.m` is the real one.

#include <stdbool.h>
#include <stdint.h>

void lyra_menubar_menu(const char *title) { (void)title; }

void lyra_menubar_app_menu(void) {}

void lyra_menubar_submenu(const char *title) { (void)title; }

void lyra_menubar_end_submenu(void) {}

const char *lyra_menubar_describe(void) { return ""; }

void lyra_menubar_item(const char *title, const char *key, uint32_t mods, int32_t tag) {
  (void)title;
  (void)key;
  (void)mods;
  (void)tag;
}

void lyra_menubar_separator(void) {}

void lyra_menubar_header(const char *title) { (void)title; }

void lyra_menubar_slider(double min, double max, double value, int32_t tag) {
  (void)min;
  (void)max;
  (void)value;
  (void)tag;
}

double lyra_menubar_slider_value(int32_t tag) {
  (void)tag;
  return 0;
}

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

const char *lyra_menubar_choose_file(const char *title, const char *types) {
  (void)title;
  (void)types;
  return 0;
}

void lyra_menubar_alert(const char *message, const char *detail) {
  (void)message;
  (void)detail;
}
