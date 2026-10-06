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

@interface LyraMenubarTarget : NSObject <NSWindowDelegate>
- (void)chosen:(NSMenuItem *)item;
- (void)slid:(NSSlider *)slider;
- (void)pressed:(NSButton *)button;
@end

// The grid window's close tag, queued when the window is closed.
static int32_t grid_close_tag = -1;

@implementation LyraMenubarTarget
- (void)chosen:(NSMenuItem *)item {
  enqueue((int32_t)item.tag);
}
- (void)slid:(NSSlider *)slider {
  enqueue((int32_t)slider.tag);
}
- (void)pressed:(NSButton *)button {
  enqueue((int32_t)button.tag);
}
- (void)windowWillClose:(NSNotification *)note {
  (void)note;
  if (grid_close_tag >= 0) enqueue(grid_close_tag);
}
@end

// The bar being built, in order, and the menu items are going into.
static NSMutableArray<NSMenuItem *> *pending_menus;
static NSMenu *current_menu;
// The menus a submenu was begun in, innermost last: `lyra_menubar_end_submenu` returns to it.
static NSMutableArray<NSMenu *> *parent_menus;
static NSMenu *windows_menu;
// The program's own items for the application menu (`lyra_menubar_app_menu`), above Hide.
static NSMenu *app_items;
// Every program item by tag, for `set_checked`/`set_enabled` after installation.
static NSMutableDictionary<NSNumber *, NSMenuItem *> *items_by_tag;
// The grid window (`lyra_menubar_grid_window`) and its rows, each its label and buttons.
static NSWindow *grid_window;
static NSMutableArray<NSMutableArray<NSView *> *> *grid_rows;
// Every grid window button by tag, as items are (`lyra_menubar_grid_cell`).
static NSMutableDictionary<NSNumber *, NSButton *> *cells_by_tag;
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
  parent_menus = nil;
}

// Start a submenu titled `title` in the current menu; items added after it go in it, until
// `lyra_menubar_end_submenu`.
void lyra_menubar_submenu(const char *title) {
  if (current_menu == nil) lyra_menubar_menu("");
  NSMenu *sub = [[NSMenu alloc] initWithTitle:string_of(title)];
  sub.autoenablesItems = NO;
  NSMenuItem *holder = [current_menu addItemWithTitle:string_of(title) action:nil keyEquivalent:@""];
  holder.submenu = sub;
  if (parent_menus == nil) parent_menus = [NSMutableArray array];
  [parent_menus addObject:current_menu];
  current_menu = sub;
}

// Back to the menu the current submenu was begun in.
void lyra_menubar_end_submenu(void) {
  if (parent_menus.count == 0) return;
  current_menu = parent_menus.lastObject;
  [parent_menus removeLastObject];
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
  parent_menus = nil;
  windows_menu = nil;
  app_items = nil;
  return true;
}

// The installed menu bar as text, a line per item, indented under its menu: a title, its key
// equivalent after a tab (⌘⇧ spelled out), `[x]` when checked, `(off)` when greyed, `---` for
// a separator. Valid until the next call. For a program's tests to read back what it installed.
static char *described = NULL;

// A menu's items, `indent` deep, a submenu's beneath its title two spaces further.
static void describe_items(NSMutableString *out, NSMenu *menu, NSString *indent) {
  for (NSMenuItem *item in menu.itemArray) {
    if (item.isSeparatorItem) {
      [out appendFormat:@"%@---\n", indent];
      continue;
    }
    if (item.submenu != nil) {
      [out appendFormat:@"%@%@ >%@\n", indent, item.title, item.enabled ? @"" : @" (off)"];
      describe_items(out, item.submenu, [indent stringByAppendingString:@"  "]);
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
    [out appendFormat:@"%@%@%@%@%@\n", indent, item.title, key.length > 0 ? [@"\t" stringByAppendingString:key] : @"",
                      item.state == NSControlStateValueOn ? @" [x]" : @"", item.enabled ? @"" : @" (off)"];
  }
}

const char *lyra_menubar_describe(void) {
  free(described);
  NSMutableString *out = [NSMutableString string];
  for (NSMenuItem *top in NSApp.mainMenu.itemArray) {
    [out appendFormat:@"%@\n", top.title];
    describe_items(out, top.submenu, @"  ");
  }
  // The grid window, if there is one: a line a row, its label then its buttons.
  if (grid_window != nil) {
    [out appendFormat:@"window %@\n", grid_window.title];
    for (NSArray<NSView *> *row in grid_rows) {
      NSMutableArray<NSString *> *parts = [NSMutableArray array];
      for (NSView *view in row) {
        if ([view isKindOfClass:[NSButton class]]) {
          NSButton *button = (NSButton *)view;
          [parts addObject:[NSString stringWithFormat:@"[%@]%@%@", button.title,
                                     button.state == NSControlStateValueOn ? @" (on)" : @"",
                                     button.enabled ? @"" : @" (off)"]];
        } else if ([view isKindOfClass:[NSTextField class]]) {
          [parts addObject:((NSTextField *)view).stringValue];
        }
      }
      [out appendFormat:@"  %@\n", [parts componentsJoinedByString:@" "]];
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
  NSControlStateValue state = checked ? NSControlStateValueOn : NSControlStateValueOff;
  items_by_tag[@(tag)].state = state;
  // A pushed-in button is drawn only a shade darker; the accent colour says it plainly.
  NSButton *cell = cells_by_tag[@(tag)];
  cell.state = state;
  cell.bezelColor = checked ? NSColor.controlAccentColor : nil;
}

// Let an item be chosen, or grey it out. An unknown tag is ignored.
void lyra_menubar_set_enabled(int32_t tag, bool enabled) {
  items_by_tag[@(tag)].enabled = enabled;
  cells_by_tag[@(tag)].enabled = enabled;
}

// ── The grid window ──────────────────────────────────────────────────────────
//
// A window of push buttons in a grid — a settings window's rows of choices — built like the
// menus: `lyra_menubar_grid_window` starts it, `_grid_row` a row (its label first),
// `_grid_cell` a button in that row, queueing its tag when pressed, as a menu item does. It
// is laid out the first time it is shown, so its buttons exist, and take titles and states,
// before that.
//
// **Keys typed into it can be captured** (`lyra_menubar_capture_keys`): a key pressed while
// the window is key is queued as an SDL scancode, for `lyra_menubar_next_key`, instead of
// reaching AppKit. SDL never sees a key pressed in a window it does not own, which is the
// whole reason this is here: a program asking "which key?" must hear it from AppKit.

static NSMutableArray<NSString *> *grid_headers;
static NSString *grid_note_text;
static bool grid_laid_out = false;

enum { KEY_QUEUE_SIZE = 16 };
static uint32_t key_queue[KEY_QUEUE_SIZE];
static int key_head = 0;
static int key_count = 0;
static bool capturing = false;
static id key_monitor;

static void enqueue_key(uint32_t scancode) {
  if (key_count == KEY_QUEUE_SIZE) return;
  key_queue[(key_head + key_count) % KEY_QUEUE_SIZE] = scancode;
  key_count++;
}

// A Mac virtual key code (`kVK_…`, the key's position) as the USB HID usage SDL's scancodes
// are — position for position, as SDL's own Cocoa table maps them. 0 for a key with none.
static uint32_t scancode_of(unsigned short code) {
  static const uint8_t table[128] = {
    [0x00] = 4,   [0x01] = 22,  [0x02] = 7,   [0x03] = 9,   [0x04] = 11,  [0x05] = 10,
    [0x06] = 29,  [0x07] = 27,  [0x08] = 6,   [0x09] = 25,  [0x0A] = 100, [0x0B] = 5,
    [0x0C] = 20,  [0x0D] = 26,  [0x0E] = 8,   [0x0F] = 21,  [0x10] = 28,  [0x11] = 23,
    [0x12] = 30,  [0x13] = 31,  [0x14] = 32,  [0x15] = 33,  [0x16] = 35,  [0x17] = 34,
    [0x18] = 46,  [0x19] = 38,  [0x1A] = 36,  [0x1B] = 45,  [0x1C] = 37,  [0x1D] = 39,
    [0x1E] = 48,  [0x1F] = 18,  [0x20] = 24,  [0x21] = 47,  [0x22] = 12,  [0x23] = 19,
    [0x24] = 40,  [0x25] = 15,  [0x26] = 13,  [0x27] = 52,  [0x28] = 14,  [0x29] = 51,
    [0x2A] = 49,  [0x2B] = 54,  [0x2C] = 56,  [0x2D] = 17,  [0x2E] = 16,  [0x2F] = 55,
    [0x30] = 43,  [0x31] = 44,  [0x32] = 53,  [0x33] = 42,  [0x35] = 41,  [0x36] = 231,
    [0x37] = 227, [0x38] = 225, [0x39] = 57,  [0x3A] = 226, [0x3B] = 224, [0x3C] = 229,
    [0x3D] = 230, [0x3E] = 228, [0x40] = 108, [0x41] = 99,  [0x43] = 85,  [0x45] = 87,
    [0x47] = 83,  [0x48] = 128, [0x49] = 129, [0x4A] = 127, [0x4B] = 84,  [0x4C] = 88,
    [0x4E] = 86,  [0x4F] = 109, [0x50] = 110, [0x51] = 103, [0x52] = 98,  [0x53] = 89,
    [0x54] = 90,  [0x55] = 91,  [0x56] = 92,  [0x57] = 93,  [0x58] = 94,  [0x59] = 95,
    [0x5A] = 111, [0x5B] = 96,  [0x5C] = 97,  [0x60] = 62,  [0x61] = 63,  [0x62] = 64,
    [0x63] = 60,  [0x64] = 65,  [0x65] = 66,  [0x67] = 68,  [0x69] = 104, [0x6A] = 107,
    [0x6B] = 105, [0x6D] = 67,  [0x6F] = 69,  [0x71] = 106, [0x72] = 73,  [0x73] = 74,
    [0x74] = 75,  [0x75] = 76,  [0x76] = 61,  [0x77] = 77,  [0x78] = 59,  [0x79] = 78,
    [0x7A] = 58,  [0x7B] = 80,  [0x7C] = 79,  [0x7D] = 81,  [0x7E] = 82,
  };
  return code < 128 ? table[code] : 0;
}

// The modifier flag a modifier key's code sets, or 0 for a key that is not one. Caps Lock
// is left out: it reports its lock, not whether it is held.
static NSEventModifierFlags flag_of(unsigned short code) {
  switch (code) {
    case 0x38: case 0x3C: return NSEventModifierFlagShift;
    case 0x3A: case 0x3D: return NSEventModifierFlagOption;
    case 0x3B: case 0x3E: return NSEventModifierFlagControl;
    case 0x37: case 0x36: return NSEventModifierFlagCommand;
    default: return 0;
  }
}

// Watch the grid window's keys, once. A key held with ⌘ is passed on — ⌘W closes the
// window and ⌘Q quits, capturing or not — and so is everything while nothing is captured.
static void watch_keys(void) {
  if (key_monitor != nil) return;
  NSEventMask mask = NSEventMaskKeyDown | NSEventMaskFlagsChanged;
  key_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask
                                                      handler:^NSEvent *(NSEvent *event) {
    if (!capturing || event.window != grid_window) return event;
    if (event.type == NSEventTypeFlagsChanged) {
      NSEventModifierFlags flag = flag_of(event.keyCode);
      // Pressed, not released: the flag is set now.
      if (flag != 0 && (event.modifierFlags & flag) != 0) enqueue_key(scancode_of(event.keyCode));
      return event;
    }
    if (event.modifierFlags & NSEventModifierFlagCommand) return event;
    if (!event.isARepeat) {
      uint32_t scancode = scancode_of(event.keyCode);
      if (scancode != 0) enqueue_key(scancode);
    }
    return nil;
  }];
}

// Start the grid window, titled `title`, its columns headed by the tab-separated `columns`
// (the row labels' column first, unheaded). Closing it queues `close_tag`. Replaces any grid
// window built before.
void lyra_menubar_grid_window(const char *title, const char *columns, int32_t close_tag) {
  if (target == nil) target = [[LyraMenubarTarget alloc] init];
  if (grid_window != nil) [grid_window close];
  grid_window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 400, 300)
                                            styleMask:NSWindowStyleMaskTitled |
                                                      NSWindowStyleMaskClosable
                                              backing:NSBackingStoreBuffered
                                                defer:YES];
  grid_window.title = string_of(title);
  grid_window.releasedWhenClosed = NO;
  grid_window.delegate = target;
  grid_close_tag = close_tag;
  grid_headers = [NSMutableArray array];
  NSString *list = string_of(columns);
  if (list.length > 0) [grid_headers addObjectsFromArray:[list componentsSeparatedByString:@"\t"]];
  grid_rows = [NSMutableArray array];
  grid_note_text = nil;
  grid_laid_out = false;
  cells_by_tag = [NSMutableDictionary dictionary];
  watch_keys();
}

// A row of the grid window, labelled `label`; cells added after it go in it.
void lyra_menubar_grid_row(const char *label) {
  if (grid_window == nil) return;
  NSTextField *text = [NSTextField labelWithString:string_of(label)];
  text.alignment = NSTextAlignmentRight;
  [grid_rows addObject:[NSMutableArray arrayWithObject:text]];
}

// A button in the current row, titled `title`; pressing it queues `tag`. Shown pressed in
// while `lyra_menubar_set_checked(tag, true)` — the button listening for a key, say.
void lyra_menubar_grid_cell(const char *title, int32_t tag) {
  if (grid_rows.count == 0) return;
  NSButton *button = [NSButton buttonWithTitle:string_of(title)
                                        target:target
                                        action:@selector(pressed:)];
  [button setButtonType:NSButtonTypePushOnPushOff];
  button.tag = tag;
  [button.widthAnchor constraintGreaterThanOrEqualToConstant:130].active = YES;
  [grid_rows.lastObject addObject:button];
  cells_by_tag[@(tag)] = button;
}

// A line of small text under the grid.
void lyra_menubar_grid_note(const char *text) { grid_note_text = string_of(text); }

// The grid window laid out, once: a header row, the rows, the note beneath.
static void lay_out_grid(void) {
  NSUInteger columns = grid_headers.count;
  for (NSArray *row in grid_rows) columns = MAX(columns, row.count);
  NSMutableArray<NSArray<NSView *> *> *views = [NSMutableArray array];
  if (grid_headers.count > 0) {
    NSMutableArray<NSView *> *header = [NSMutableArray array];
    for (NSString *title in grid_headers) {
      NSTextField *text = [NSTextField labelWithString:title];
      text.font = [NSFont boldSystemFontOfSize:NSFont.systemFontSize];
      text.alignment = NSTextAlignmentCenter;
      [header addObject:text];
    }
    while (header.count < columns) [header addObject:NSGridCell.emptyContentView];
    [views addObject:header];
  }
  for (NSMutableArray<NSView *> *row in grid_rows) {
    while (row.count < columns) [row addObject:NSGridCell.emptyContentView];
    [views addObject:row];
  }
  NSGridView *grid = [NSGridView gridViewWithViews:views];
  grid.rowSpacing = 6;
  grid.columnSpacing = 10;
  // Each label level with its buttons' titles, and against them.
  grid.rowAlignment = NSGridRowAlignmentFirstBaseline;
  [grid columnAtIndex:0].xPlacement = NSGridCellPlacementTrailing;
  for (NSUInteger c = 1; c < grid.numberOfColumns; c++) {
    [grid columnAtIndex:c].xPlacement = NSGridCellPlacementFill;
  }
  NSStackView *stack = [NSStackView stackViewWithViews:@[ grid ]];
  stack.orientation = NSUserInterfaceLayoutOrientationVertical;
  stack.alignment = NSLayoutAttributeLeading;
  stack.spacing = 14;
  stack.edgeInsets = NSEdgeInsetsMake(20, 20, 20, 20);
  if (grid_note_text != nil) {
    NSTextField *note = [NSTextField wrappingLabelWithString:grid_note_text];
    note.font = [NSFont systemFontOfSize:NSFont.smallSystemFontSize];
    note.textColor = NSColor.secondaryLabelColor;
    // Wrapped to the grid's width: a label's own width is its text on one line, which
    // would stretch the window, and the grid's label column with it.
    note.preferredMaxLayoutWidth = grid.fittingSize.width;
    [note setContentCompressionResistancePriority:NSLayoutPriorityDefaultLow
                                   forOrientation:NSLayoutConstraintOrientationHorizontal];
    [stack addArrangedSubview:note];
  }
  grid_window.contentView = stack;
  [grid_window setContentSize:stack.fittingSize];
  [grid_window center];
  grid_laid_out = true;
}

// Show the grid window, laid out the first time, in front and key.
void lyra_menubar_show_grid(void) {
  if (grid_window == nil) return;
  if (!grid_laid_out) lay_out_grid();
  [grid_window makeKeyAndOrderFront:nil];
}

// Whether keys pressed in the grid window are captured for `lyra_menubar_next_key` rather
// than passed on. Turning it off forgets any not yet read.
void lyra_menubar_capture_keys(bool on) {
  capturing = on;
  if (!on) key_count = 0;
}

// The SDL scancode of the oldest key captured and not yet read, or 0.
uint32_t lyra_menubar_next_key(void) {
  if (key_count == 0) return 0;
  uint32_t scancode = key_queue[key_head];
  key_head = (key_head + 1) % KEY_QUEUE_SIZE;
  key_count--;
  return scancode;
}

// A menu item's or a grid button's title. An unknown tag is ignored.
void lyra_menubar_set_title(int32_t tag, const char *title) {
  NSString *text = string_of(title);
  items_by_tag[@(tag)].title = text;
  cells_by_tag[@(tag)].title = text;
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
