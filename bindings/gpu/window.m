// `bindings.gpu`'s `pin_contents`: a window's Metal layer told to keep a frame at its top-left
// corner, unscaled, until the next one is drawn. AppKit otherwise stretches the last frame to
// the window's new bounds while a resize waits for a new one (CAMetalLayer's contentsGravity
// defaults to resize), so a dragged window's contents grew and shrank between frames. Both
// renderers draw through SDL's Metal view on macOS, so both are pinned.
@import AppKit;
@import QuartzCore;
#include <SDL3/SDL.h>

static void pin(NSView *view) {
    if ([view.layer isKindOfClass:[CAMetalLayer class]]) {
        view.layer.contentsGravity = kCAGravityTopLeft;
        view.layerContentsPlacement = NSViewLayerContentsPlacementTopLeft;
    }
    for (NSView *sub in view.subviews) pin(sub);
}

void lyra_gpu_pin_contents(SDL_Window *window) {
    NSWindow *ns = (__bridge NSWindow *)SDL_GetPointerProperty(SDL_GetWindowProperties(window),
                                                                SDL_PROP_WINDOW_COCOA_WINDOW_POINTER, NULL);
    if (ns && ns.contentView) pin(ns.contentView);
}
