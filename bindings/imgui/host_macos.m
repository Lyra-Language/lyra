// The macOS half of the host: let ImGui's torn-off windows cross onto a display above, and
// give a title bar the program draws (HOST_CUSTOM_TITLE_BAR) the system's double-click.
//
// **AppKit constrains a window's frame to keep its top below the menu bar** of the screen
// it is mostly on (`-[NSWindow constrainFrameRect:toScreen:]`). A title-bar drag is the
// window server's and crosses displays at the cursor, so the main window was never
// affected. A torn-off ImGui panel is a borderless window ImGui moves itself, a few pixels
// a frame through `SDL_SetWindowPosition` — and every step towards a monitor above
// straddles the two displays, so every step was pulled back to y = 34: the panel stopped
// under the menu bar, as if the monitor above were not there (09/27, found moving Vega's
// panels onto an external monitor above a MacBook).
//
// SDL's window class, `SDL3Window`, does not implement the method, so this adds one. **A
// borderless window is left where it was asked to go as long as its title bar can still be
// grabbed** — the point TITLE_GRAB below its top edge lies in some screen's visible area
// (`visibleFrame`, which excludes the menu bar and the Dock). A panel crossing onto the
// monitor above has its top up there, so it may straddle; one whose title bar would rest
// under the menu bar gets NSWindow's own rule and is pushed back down. The first version
// left every borderless frame alone, and a panel parked under the menu bar could not be
// grabbed again (09/27). A titled window always gets NSWindow's rule.
//
// Added rather than swizzled — if a future SDL defines the method, `class_addMethod`
// declines and SDL's answer stands.
//
// Compiled with `-fmodules`, so `@import` records AppKit and libobjc in the object file and
// the archive needs no framework flag (the menubar shim's arrangement).

@import AppKit;
@import ObjectiveC.runtime;

// How far below a panel's top edge its title bar is grabbed: the middle of ImGui's title
// bar (a 13-point font plus 3 points of padding each side, at scale 1).
static const CGFloat TITLE_GRAB = 10.0;

// Whether some screen's visible area holds the title bar's grab line within the frame's
// horizontal span. Cocoa's y grows upwards, so the grab line is below NSMaxY.
static BOOL title_bar_reachable(NSRect frame) {
    const CGFloat grab_y = NSMaxY(frame) - TITLE_GRAB;
    for (NSScreen *screen in [NSScreen screens]) {
        const NSRect visible = screen.visibleFrame;
        const BOOL spans = NSMaxX(frame) > NSMinX(visible) && NSMinX(frame) < NSMaxX(visible);
        if (spans && grab_y >= NSMinY(visible) && grab_y <= NSMaxY(visible))
            return YES;
    }
    return NO;
}

static NSRect lyra_constrain_frame(NSWindow *self, SEL cmd, NSRect frame, NSScreen *screen) {
    if (!(self.styleMask & NSWindowStyleMaskTitled) && title_bar_reachable(frame))
        return frame;
    IMP base = class_getMethodImplementation([NSWindow class], cmd);
    return ((NSRect (*)(id, SEL, NSRect, NSScreen *))base)(self, cmd, frame, screen);
}

// Called once from lyra_imgui_host_create, after SDL_Init has loaded SDL's classes.
void lyra_imgui_macos_allow_straddling(void) {
    Class sdl_window = objc_getClass("SDL3Window");
    if (sdl_window == Nil) {
        return;
    }
    SEL sel = @selector(constrainFrameRect:toScreen:);
    Method base = class_getInstanceMethod([NSWindow class], sel);
    class_addMethod(sdl_window, sel, (IMP)lyra_constrain_frame, method_getTypeEncoding(base));
}

// ── A title bar the program draws ────────────────────────────────────────────

// host.cpp: whether a point in the main window (top-left origin) is in the title bar's
// drag area as the last frame drew it.
extern bool lyra_imgui_host_in_drag_area(void *host, float x, float y);

static id title_bar_monitor = nil;

// **A double-click on a title bar is the person's to define** (System Settings > Desktop &
// Dock, "Double-click a window's title bar to": `AppleActionOnDoubleClick`, Fill by
// default, or Minimize, or nothing). A borderless window has no title bar for AppKit to
// do it on, and SDL takes a press on the drag area for a drag and never reports it, so
// this watches for the second click itself — before SDL sees the event — and does what the
// setting says.
void lyra_imgui_macos_watch_title_bar(void *nswindow, void *host) {
    NSWindow *window = (__bridge NSWindow *)nswindow;
    if (window == nil || title_bar_monitor != nil) {
        return;
    }
    title_bar_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown
                                                              handler:^NSEvent *(NSEvent *event) {
        if (event.window != window || event.clickCount != 2) {
            return event;
        }
        const NSPoint p = event.locationInWindow;
        const CGFloat height = window.contentView.frame.size.height;
        if (!lyra_imgui_host_in_drag_area(host, (float)p.x, (float)(height - p.y))) {
            return event;
        }
        NSString *action = [[NSUserDefaults standardUserDefaults] stringForKey:@"AppleActionOnDoubleClick"];
        if ([action isEqualToString:@"Minimize"]) {
            [window miniaturize:nil];
        } else if (![action isEqualToString:@"None"]) {
            [window zoom:nil];
        }
        return nil;
    }];
}

void lyra_imgui_macos_unwatch_title_bar(void) {
    if (title_bar_monitor != nil) {
        [NSEvent removeMonitor:title_bar_monitor];
        title_bar_monitor = nil;
    }
}
