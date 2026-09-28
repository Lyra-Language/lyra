// The macOS half of the host: let ImGui's torn-off windows cross onto a display above.
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
// SDL's window class, `SDL3Window`, does not implement the method, so this adds one: a
// borderless window's frame is left where it was asked to go, and a titled window gets
// NSWindow's own rule. Added rather than swizzled — if a future SDL defines it,
// `class_addMethod` declines and SDL's answer stands.
//
// Compiled with `-fmodules`, so `@import` records AppKit and libobjc in the object file and
// the archive needs no framework flag (the menubar shim's arrangement).

@import AppKit;
@import ObjectiveC.runtime;

static NSRect lyra_constrain_frame(NSWindow *self, SEL cmd, NSRect frame, NSScreen *screen) {
    if (self.styleMask & NSWindowStyleMaskTitled) {
        IMP base = class_getMethodImplementation([NSWindow class], cmd);
        return ((NSRect (*)(id, SEL, NSRect, NSScreen *))base)(self, cmd, frame, screen);
    }
    return frame;
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
