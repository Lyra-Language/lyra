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

// Queue a tag — once, if it is already the newest waiting: a slider dragged while its menu
// is open sends a stream of changes the program can only read once the menu closes, and
// what it wants then is the value, which `lyra_menubar_slider_value` reads.
static void enqueue(int32_t tag) {
  if (queue_count > 0 && queue[(queue_head + queue_count - 1) % QUEUE_SIZE] == tag) return;
  if (queue_count == QUEUE_SIZE) return;
  queue[(queue_head + queue_count) % QUEUE_SIZE] = tag;
  queue_count++;
}

@interface LyraMenubarTarget : NSObject
- (void)chosen:(NSMenuItem *)item;
- (void)slid:(NSSlider *)slider;
@end

@implementation LyraMenubarTarget
- (void)chosen:(NSMenuItem *)item {
  enqueue((int32_t)item.tag);
}
- (void)slid:(NSSlider *)slider {
  enqueue((int32_t)slider.tag);
}
@end

// The bar being built, in order, and the menu items are going into.
static NSMutableArray<NSMenuItem *> *pending_menus;
static NSMenu *current_menu;
static NSMenu *windows_menu;
// The program's own items for the application menu (`lyra_menubar_app_menu`), above Hide.
static NSMenu *app_items;
// Every program item by tag, for `set_checked`/`set_enabled` after installation.
static NSMutableDictionary<NSNumber *, NSMenuItem *> *items_by_tag;
// Every slider by tag, for `lyra_menubar_slider_value`.
static NSMutableDictionary<NSNumber *, NSSlider *> *sliders_by_tag;
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
  NSString *name = string_of(title);
  // The menus are what the program described. AppKit finds "the Edit menu" by its title and
  // adds Writing Tools, AutoFill, Start Dictation and Emoji & Symbols to it — items for
  // AppKit's own text fields, which a program drawing its own has none of, and which its
  // in-window menus (ImGui's, elsewhere) would not have. An invisible word joiner after the
  // title (U+2060) is a title AppKit does not recognise, still shown and read as "Edit".
  if ([name isEqualToString:@"Edit"]) name = [name stringByAppendingString:@"\u2060"];
  NSMenu *menu = begin_menu(name);
  // Enabled means what the program last said, not what AppKit infers from the target.
  menu.autoenablesItems = NO;
}

// Items added after this go in the application menu, above Hide and Quit: About, Settings.
void lyra_menubar_app_menu(void) {
  app_items = [[NSMenu alloc] initWithTitle:@""];
  current_menu = app_items;
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

// A title over the items after it, greyed as macOS draws a section's.
void lyra_menubar_header(const char *title) {
  if (current_menu == nil) lyra_menubar_menu("");
  NSMenuItem *item;
  if (@available(macOS 14.0, *)) {
    item = [NSMenuItem sectionHeaderWithTitle:string_of(title)];
  } else {
    item = [[NSMenuItem alloc] initWithTitle:string_of(title) action:nil keyEquivalent:@""];
    item.enabled = NO;
  }
  [current_menu addItem:item];
}

// A slider from `min` to `max` at `value`, as an item of the current menu. Moving it
// queues `tag`, as choosing an item does; the value is read with `lyra_menubar_slider_value`.
void lyra_menubar_slider(double min, double max, double value, int32_t tag) {
  if (current_menu == nil) lyra_menubar_menu("");
  if (target == nil) target = [[LyraMenubarTarget alloc] init];
  if (sliders_by_tag == nil) sliders_by_tag = [NSMutableDictionary dictionary];
  NSSlider *slider = [NSSlider sliderWithValue:value
                                      minValue:min
                                      maxValue:max
                                        target:target
                                        action:@selector(slid:)];
  slider.tag = tag;
  slider.continuous = YES;
  slider.frame = NSMakeRect(20, 4, 180, 22);
  NSView *holder = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 220, 30)];
  [holder addSubview:slider];
  NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@"" action:nil keyEquivalent:@""];
  item.view = holder;
  [current_menu addItem:item];
  sliders_by_tag[@(tag)] = slider;
}

// The slider tagged `tag`'s value, or 0 for an unknown tag.
double lyra_menubar_slider_value(int32_t tag) {
  return sliders_by_tag[@(tag)].doubleValue;
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
  if (app_items != nil && app_items.numberOfItems > 0) {
    // Moved, not copied: an item belongs to one menu.
    while (app_items.numberOfItems > 0) {
      NSMenuItem *item = [app_items itemAtIndex:0];
      [app_items removeItemAtIndex:0];
      [app addItem:item];
    }
    [app addItem:[NSMenuItem separatorItem]];
  }
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
  app_items = nil;
  return true;
}

// The installed menu bar as text, a line per item, indented under its menu: a title, its key
// equivalent after a tab (⌘⇧ spelled out), `[x]` when checked, `(off)` when greyed, `---` for
// a separator. Valid until the next call. For a program's tests to read back what it installed.
static char *described = NULL;

const char *lyra_menubar_describe(void) {
  free(described);
  NSMutableString *out = [NSMutableString string];
  for (NSMenuItem *top in NSApp.mainMenu.itemArray) {
    [out appendFormat:@"%@\n", top.title];
    for (NSMenuItem *item in top.submenu.itemArray) {
      if (item.isSeparatorItem) {
        [out appendString:@"  ---\n"];
        continue;
      }
      NSMutableString *key = [NSMutableString string];
      if (item.keyEquivalent.length > 0) {
        NSEventModifierFlags m = item.keyEquivalentModifierMask;
        if (m & NSEventModifierFlagControl) [key appendString:@"Ctrl+"];
        if (m & NSEventModifierFlagOption) [key appendString:@"Option+"];
        if (m & NSEventModifierFlagShift) [key appendString:@"Shift+"];
        if (m & NSEventModifierFlagCommand) [key appendString:@"Cmd+"];
        [key appendString:item.keyEquivalent.uppercaseString];
      }
      [out appendFormat:@"  %@%@%@%@\n", item.title, key.length > 0 ? [@"\t" stringByAppendingString:key] : @"",
                        item.state == NSControlStateValueOn ? @" [x]" : @"", item.enabled ? @"" : @" (off)"];
    }
  }
  described = strdup(out.UTF8String);
  return described;
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
