// The C half of `bindings.menubar`: a native macOS menu bar for a program whose window
// belongs to someone else (SDL, GLFW), built from C calls and read back as a queue.
//
// **Why a shim at all.** Building an `NSMenu` is ordinary message sending, which `extern`s
// over `objc_msgSend` could manage. Choosing an item is not: AppKit calls a *method on a
// target object*, and Lyra cannot hand C a function to be called. So the target lives
// here, and what it does is the one thing that needs no callback — it queues the item's
// tag, and the program reads the queue with `lyra_menubar_next` after draining its window
// events. Everything happens on the main thread: SDL's event pump is what runs AppKit's
// event loop, so an item chosen (by click or by its key equivalent) is queued during the
// program's own `poll_event`.
//
// **It links with no framework flags.** `@import AppKit` under `-fmodules` records
// `-framework AppKit` (and `-lobjc`) in the object file, and the linker honours that from
// an archive member too — so the binding's `@link("lyra-menubar")` is the whole link line,
// which matters because `@link` has no way to name a framework.
//
// Built into `build/lib/liblyra-menubar.a` by `build.sh`; `menubar_stub.c` is what that
// archive holds on any other platform.

@import AppKit;
@import UniformTypeIdentifiers;

#include <stdbool.h>
#include <stdint.h>

// Modifier bits for `lyra_menubar_item`, as the Lyra side spells them.
enum { MOD_COMMAND = 1, MOD_SHIFT = 2, MOD_OPTION = 4, MOD_CONTROL = 8 };

// Chosen tags waiting to be read. A program reads them every frame, so a full queue means
// something has stopped reading; the newest choice is dropped rather than the oldest.
enum { QUEUE_SIZE = 64 };
static int32_t queue[QUEUE_SIZE];
static int queue_head = 0;
static int queue_count = 0;

@interface LyraMenubarTarget : NSObject
- (void)chosen:(NSMenuItem *)item;
@end

@implementation LyraMenubarTarget
- (void)chosen:(NSMenuItem *)item {
  if (queue_count == QUEUE_SIZE) return;
  queue[(queue_head + queue_count) % QUEUE_SIZE] = (int32_t)item.tag;
  queue_count++;
}
@end

// The bar being built, in order, and the menu items are going into.
static NSMutableArray<NSMenuItem *> *pending_menus;
static NSMenu *current_menu;
static NSMenu *windows_menu;
// Every program item by tag, for `set_checked`/`set_enabled` after installation.
static NSMutableDictionary<NSNumber *, NSMenuItem *> *items_by_tag;
static LyraMenubarTarget *target;

static NSString *string_of(const char *text) {
  return [NSString stringWithUTF8String:text] ?: @"";
}

static NSMenu *begin_menu(NSString *title) {
  if (pending_menus == nil) pending_menus = [NSMutableArray array];
  NSMenu *menu = [[NSMenu alloc] initWithTitle:title];
  NSMenuItem *holder = [[NSMenuItem alloc] initWithTitle:title action:nil keyEquivalent:@""];
  holder.submenu = menu;
  [pending_menus addObject:holder];
  current_menu = menu;
  return menu;
}

// An item AppKit itself answers (Hide, Quit, Minimize): target nil, so the action goes
// up the responder chain to NSApp or the key window.
static void add_standard(NSMenu *menu, NSString *title, SEL action, NSString *key,
                         NSEventModifierFlags mods) {
  NSMenuItem *item = [menu addItemWithTitle:title action:action keyEquivalent:key];
  item.keyEquivalentModifierMask = mods;
}

// Start a new top-level menu; items added after it go in it.
void lyra_menubar_menu(const char *title) {
  NSMenu *menu = begin_menu(string_of(title));
  // Enabled means what the program last said, not what AppKit infers from the target.
  menu.autoenablesItems = NO;
}

// Add an item to the current menu. `key` is its key equivalent ("" for none), `mods` the
// MOD_* bits held with it, and `tag` what `lyra_menubar_next` answers when it is chosen.
void lyra_menubar_item(const char *title, const char *key, uint32_t mods, int32_t tag) {
  if (current_menu == nil) lyra_menubar_menu("");
  if (target == nil) target = [[LyraMenubarTarget alloc] init];
  if (items_by_tag == nil) items_by_tag = [NSMutableDictionary dictionary];
  NSMenuItem *item = [current_menu addItemWithTitle:string_of(title)
                                             action:@selector(chosen:)
                                      keyEquivalent:string_of(key)];
  NSEventModifierFlags flags = 0;
  if (mods & MOD_COMMAND) flags |= NSEventModifierFlagCommand;
  if (mods & MOD_SHIFT) flags |= NSEventModifierFlagShift;
  if (mods & MOD_OPTION) flags |= NSEventModifierFlagOption;
  if (mods & MOD_CONTROL) flags |= NSEventModifierFlagControl;
  item.keyEquivalentModifierMask = flags;
  item.target = target;
  item.tag = tag;
  items_by_tag[@(tag)] = item;
}

void lyra_menubar_separator(void) {
  if (current_menu != nil) [current_menu addItem:[NSMenuItem separatorItem]];
}

// The standard Window menu, at this position in the bar.
void lyra_menubar_window_menu(void) {
  NSMenu *menu = begin_menu(@"Window");
  add_standard(menu, @"Minimize", @selector(performMiniaturize:), @"m",
               NSEventModifierFlagCommand);
  add_standard(menu, @"Zoom", @selector(performZoom:), @"", 0);
  [menu addItem:[NSMenuItem separatorItem]];
  add_standard(menu, @"Bring All to Front", @selector(arrangeInFront:), @"", 0);
  windows_menu = menu;
  current_menu = nil;
}

// Replace the application's menu bar — SDL's default one — with the application menu
// (Hide, Hide Others, Show All, Quit) followed by the menus built since the last install.
// Quit sends `terminate:`, which SDL answers with `SDL_EVENT_QUIT`. False where there is no
// application object to give a menu bar to.
bool lyra_menubar_install(const char *app_name) {
  if (NSApp == nil) return false;
  NSString *name = string_of(app_name);
  NSMenu *bar = [[NSMenu alloc] initWithTitle:@""];

  NSMenu *app = [[NSMenu alloc] initWithTitle:name];
  add_standard(app, [@"Hide " stringByAppendingString:name], @selector(hide:), @"h",
               NSEventModifierFlagCommand);
  add_standard(app, @"Hide Others", @selector(hideOtherApplications:), @"h",
               NSEventModifierFlagCommand | NSEventModifierFlagOption);
  add_standard(app, @"Show All", @selector(unhideAllApplications:), @"", 0);
  [app addItem:[NSMenuItem separatorItem]];
  add_standard(app, [@"Quit " stringByAppendingString:name], @selector(terminate:), @"q",
               NSEventModifierFlagCommand);
  NSMenuItem *app_holder = [[NSMenuItem alloc] initWithTitle:name action:nil keyEquivalent:@""];
  app_holder.submenu = app;
  [bar addItem:app_holder];

  for (NSMenuItem *holder in pending_menus) [bar addItem:holder];
  NSApp.mainMenu = bar;
  if (windows_menu != nil) NSApp.windowsMenu = windows_menu;

  pending_menus = nil;
  current_menu = nil;
  windows_menu = nil;
  return true;
}

// The tag of the oldest item chosen and not yet read, or -1.
int32_t lyra_menubar_next(void) {
  if (queue_count == 0) return -1;
  int32_t tag = queue[queue_head];
  queue_head = (queue_head + 1) % QUEUE_SIZE;
  queue_count--;
  return tag;
}

// Show a checkmark beside an item, or not. An unknown tag is ignored.
void lyra_menubar_set_checked(int32_t tag, bool checked) {
  items_by_tag[@(tag)].state = checked ? NSControlStateValueOn : NSControlStateValueOff;
}

// Let an item be chosen, or grey it out. An unknown tag is ignored.
void lyra_menubar_set_enabled(int32_t tag, bool enabled) {
  items_by_tag[@(tag)].enabled = enabled;
}

// The last path `lyra_menubar_choose_file` answered, kept until the next call so the
// caller can copy it out.
static char *chosen_path = NULL;

// A modal Open panel titled `title`, offering files with one of the comma-separated
// extensions in `types` ("" for any). Answers the chosen path — valid until the next call
// — or NULL if the panel was cancelled. Modal, so it runs on the main thread and answers
// before returning: no callback, unlike SDL's own file dialogs.
const char *lyra_menubar_choose_file(const char *title, const char *types) {
  free(chosen_path);
  chosen_path = NULL;
  NSOpenPanel *panel = [NSOpenPanel openPanel];
  panel.title = string_of(title);
  panel.canChooseFiles = YES;
  panel.canChooseDirectories = NO;
  panel.allowsMultipleSelection = NO;
  NSString *list = string_of(types);
  if (list.length > 0) {
    NSMutableArray<UTType *> *allowed = [NSMutableArray array];
    for (NSString *ext in [list componentsSeparatedByString:@","]) {
      UTType *type = [UTType typeWithFilenameExtension:ext];
      if (type != nil) [allowed addObject:type];
    }
    panel.allowedContentTypes = allowed;
  }
  NSWindow *key = NSApp.keyWindow;
  NSModalResponse response = [panel runModal];
  [key makeKeyAndOrderFront:nil];
  if (response != NSModalResponseOK || panel.URL == nil) return NULL;
  chosen_path = strdup(panel.URL.fileSystemRepresentation);
  return chosen_path;
}

// A modal alert: `message` in bold, `detail` beneath it, and an OK button.
void lyra_menubar_alert(const char *message, const char *detail) {
  NSAlert *alert = [[NSAlert alloc] init];
  alert.messageText = string_of(message);
  alert.informativeText = string_of(detail);
  [alert addButtonWithTitle:@"OK"];
  NSWindow *key = NSApp.keyWindow;
  [alert runModal];
  [key makeKeyAndOrderFront:nil];
}
