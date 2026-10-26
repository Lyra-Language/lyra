// `bindings.gpu`'s window on macOS. `pin_contents`: a window's Metal layer told to keep a frame at its top-left
// corner, unscaled, until the next one is drawn. AppKit otherwise stretches the last frame to
// the window's new bounds while a resize waits for a new one (CAMetalLayer's contentsGravity
// defaults to resize), so a dragged window's contents grew and shrank between frames. Both
// renderers draw through SDL's Metal view on macOS, so both are pinned.
@import AppKit;
@import QuartzCore;
#include <SDL3/SDL.h>
#include <dlfcn.h>
#include <objc/message.h>
#include <objc/runtime.h>

static void pin(NSView *view) {
    if ([view.layer isKindOfClass:[CAMetalLayer class]]) {
        view.layer.contentsGravity = kCAGravityTopLeft;
        view.layerContentsPlacement = NSViewLayerContentsPlacementTopLeft;
    }
    for (NSView *sub in view.subviews) pin(sub);
}

static NSWindow *cocoa_window(SDL_Window *window) {
    return (__bridge NSWindow *)SDL_GetPointerProperty(SDL_GetWindowProperties(window),
                                                       SDL_PROP_WINDOW_COCOA_WINDOW_POINTER, NULL);
}

void lyra_gpu_pin_contents(SDL_Window *window) {
    NSWindow *ns = cocoa_window(window);
    if (ns && ns.contentView) pin(ns.contentView);
}

static void layers_opaque(NSView *view, BOOL opaque) {
    if ([view.layer isKindOfClass:[CAMetalLayer class]]) view.layer.opaque = opaque;
    for (NSView *sub in view.subviews) layers_opaque(sub, opaque);
}

// Keeps a layer's background clear: SDL's content view paints its layer black each time AppKit
// asks it to update (its `updateLayer`), unless SDL made the window transparent — which a
// window the GPU API draws into never is — and that black, under the Metal layer, hid what is
// behind the window everywhere but its title bar.
@interface LyraClearKeeper : NSObject
@end

@implementation LyraClearKeeper
- (void)observeValueForKeyPath:(NSString *)path ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
    CALayer *layer = object;
    if (layer.backgroundColor && CGColorGetAlpha(layer.backgroundColor) > 0) {
        layer.backgroundColor = CGColorGetConstantColor(kCGColorClear);
    }
}
@end

static char keeper_key;
static char titlebar_key;

// What a see-through window's title bar is drawn on: the window's own background colour,
// opaque, as the title bar of an opaque window shows — the window's background being clear, the
// title bar showed the desktop through. Drawn in the view's appearance, so light and dark follow
// the system; it takes no clicks, which go to the title bar's buttons and the window's drag.
@interface LyraTitlebarBacking : NSView
@end

@implementation LyraTitlebarBacking
- (BOOL)wantsUpdateLayer { return YES; }
- (void)updateLayer {
    [self.effectiveAppearance performAsCurrentDrawingAppearance:^{
        self.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
    }];
}
- (NSView *)hitTest:(NSPoint)point { return nil; }
@end

// The title bar's view — the close button's parent — given a backing (`on`) or rid of it.
static void titlebar_backing(NSWindow *ns, bool on) {
    NSView *bar = [ns standardWindowButton:NSWindowCloseButton].superview;
    if (!bar) return;
    LyraTitlebarBacking *backing = objc_getAssociatedObject(ns, &titlebar_key);
    if (on && !backing) {
        backing = [[LyraTitlebarBacking alloc] initWithFrame:bar.bounds];
        backing.wantsLayer = YES;
        backing.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        [bar addSubview:backing positioned:NSWindowBelow relativeTo:nil];
        objc_setAssociatedObject(ns, &titlebar_key, backing, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    } else if (!on && backing) {
        [backing removeFromSuperview];
        objc_setAssociatedObject(ns, &titlebar_key, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
}

// `set_see_through`: the window and its Metal layers made non-opaque (or opaque again), the
// content view's own layer kept clear, and the title bar kept opaque, so a frame's alpha shows
// what is behind the window's content.
// SDL's GPU API refuses a window made with SDL_WINDOW_TRANSPARENT, and SDL decides its views'
// opacity and background from that flag, so this is done to the claimed window after the fact.
bool lyra_gpu_set_see_through(SDL_Window *window, bool on) {
    NSWindow *ns = cocoa_window(window);
    if (!ns) return false;
    ns.opaque = !on;
    ns.backgroundColor = on ? NSColor.clearColor : NSColor.windowBackgroundColor;
    titlebar_backing(ns, on);
    NSView *content = ns.contentView;
    if (!content) return true;
    layers_opaque(content, !on);
    content.wantsLayer = YES;
    LyraClearKeeper *keeper = objc_getAssociatedObject(content, &keeper_key);
    if (on && !keeper) {
        keeper = [LyraClearKeeper new];
        objc_setAssociatedObject(content, &keeper_key, keeper, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        [content.layer addObserver:keeper forKeyPath:@"backgroundColor" options:0 context:NULL];
    } else if (!on && keeper) {
        [content.layer removeObserver:keeper forKeyPath:@"backgroundColor"];
        objc_setAssociatedObject(content, &keeper_key, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
    content.layer.backgroundColor = CGColorGetConstantColor(on ? kCGColorClear : kCGColorBlack);
    return true;
}

static bool any_metal_opaque(NSView *view) {
    if ([view.layer isKindOfClass:[CAMetalLayer class]] && view.layer.opaque) return true;
    for (NSView *sub in view.subviews) if (any_metal_opaque(sub)) return true;
    return false;
}

// For a test: after SDL's content view is made to update its layer (which painted it black),
// whether nothing of the window's content is opaque — the window, its Metal layers, the content
// view's layer's background — and its title bar has its opaque backing.
bool lyra_gpu_see_through_state(SDL_Window *window) {
    NSWindow *ns = cocoa_window(window);
    if (!ns || !ns.contentView) return false;
    // The title bar is opaque, though: its backing is in place.
    if (!objc_getAssociatedObject(ns, &titlebar_key)) return false;
    NSView *content = ns.contentView;
    [content.layer setNeedsDisplay];
    [content setNeedsDisplay:YES];
    [content displayIfNeeded];
    [content updateLayer];
    CGColorRef bg = content.layer.backgroundColor;
    return !ns.opaque && !any_metal_opaque(content) && (!bg || CGColorGetAlpha(bg) == 0);
}

// What a see-through window's content is drawn over to blur what is behind it: a view at the
// bottom of the content view whose layer is Core Animation's backdrop layer — what
// NSVisualEffectView draws with — sampling what is behind the window, through a Gaussian.
// Inside the window, it is cut to the window's rounded corners as everything in it is. The
// window server's own blur (CGSSetWindowBackgroundBlurRadius, as iTerm2 and Warp use) blurred
// a region whose corners are not the window's, and drew a rim round it: both showed past the
// window's corners on macOS 26. It takes no clicks.
@interface LyraBackdrop : NSView
@end

@implementation LyraBackdrop
- (CALayer *)makeBackingLayer {
    Class backdrop = NSClassFromString(@"CABackdropLayer");
    return backdrop ? [backdrop new] : [CALayer layer];
}
- (NSView *)hitTest:(NSPoint)point { return nil; }
@end

static char backdrop_key;

// The backdrop blurred by `radius` points, made where there is none; removed for 0. False where
// Core Animation's private classes are not there.
static bool backdrop_blur(NSWindow *ns, int32_t radius) {
    Class backdrop_class = NSClassFromString(@"CABackdropLayer");
    Class filter_class = NSClassFromString(@"CAFilter");
    SEL make_filter = NSSelectorFromString(@"filterWithType:");
    if (!backdrop_class || !filter_class || ![filter_class respondsToSelector:make_filter]) return false;
    NSView *content = ns.contentView;
    if (!content) return false;
    LyraBackdrop *view = objc_getAssociatedObject(ns, &backdrop_key);
    if (radius <= 0) {
        [view removeFromSuperview];
        objc_setAssociatedObject(ns, &backdrop_key, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        return true;
    }
    if (!view) {
        view = [[LyraBackdrop alloc] initWithFrame:content.bounds];
        view.wantsLayer = YES;
        view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        // At the very bottom: under any regions view (`set_backdrop_regions`) too.
        [content addSubview:view positioned:NSWindowBelow relativeTo:nil];
        objc_setAssociatedObject(ns, &backdrop_key, view, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
    CALayer *layer = view.layer;
    if (![layer isKindOfClass:backdrop_class]) return false;
    @try {
        // What is behind the window, not only what is behind the layer inside it.
        [layer setValue:@YES forKey:@"windowServerAware"];
        id (*make)(Class, SEL, NSString *) = (id (*)(Class, SEL, NSString *))objc_msgSend;
        id filter = make(filter_class, make_filter, @"gaussianBlur");
        [filter setValue:@(radius) forKey:@"inputRadius"];
        layer.filters = @[filter];
    } @catch (NSException *e) {
        return false;
    }
    return true;
}

// What blurs the desktop behind parts of a see-through window, each at its own radius: a
// program's frosted panels (`ui.gpu`'s `Backdrop` ops, cut to their clips). A view above the
// window's own backdrop and below its content, a backdrop layer a region in it, rounded as
// the region is; layers of one radius share one capture (`groupName`). It takes no clicks.
@interface LyraRegions : NSView
@end

@implementation LyraRegions
- (BOOL)isFlipped { return YES; }
- (NSView *)hitTest:(NSPoint)point { return nil; }
@end

static char regions_key;

// `set_backdrop_regions`: the regions, nine floats each — x, y, width, height in points from the
// window's content's top-left, the top-left, top-right, bottom-right and bottom-left radii,
// and the blur — the layers made, moved or let go to match. None on a window that is opaque,
// whose desktop does not show; false where Core Animation's private classes are not there.
bool lyra_gpu_set_backdrop_regions(SDL_Window *window, const float *regions, int32_t count) {
    NSWindow *ns = cocoa_window(window);
    if (!ns || !ns.contentView) return false;
    Class backdrop_class = NSClassFromString(@"CABackdropLayer");
    Class filter_class = NSClassFromString(@"CAFilter");
    SEL make_filter = NSSelectorFromString(@"filterWithType:");
    if (!backdrop_class || !filter_class || ![filter_class respondsToSelector:make_filter]) return false;
    if (ns.opaque) count = 0;
    LyraRegions *view = objc_getAssociatedObject(ns, &regions_key);
    if (count == 0 && !view) return true;
    NSView *content = ns.contentView;
    if (!view) {
        view = [[LyraRegions alloc] initWithFrame:content.bounds];
        view.wantsLayer = YES;
        // Its layers placed from the top-left, as the view's own points are.
        view.layer.geometryFlipped = YES;
        view.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        objc_setAssociatedObject(ns, &regions_key, view, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
    // Above the window's own backdrop, below everything else.
    LyraBackdrop *whole = objc_getAssociatedObject(ns, &backdrop_key);
    if (view.superview != content) {
        if (whole && whole.superview == content) {
            [content addSubview:view positioned:NSWindowAbove relativeTo:whole];
        } else {
            [content addSubview:view positioned:NSWindowBelow relativeTo:nil];
        }
    }
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    NSMutableArray<CALayer *> *layers = [NSMutableArray arrayWithArray:view.layer.sublayers ?: @[]];
    while ((int32_t)layers.count > count) {
        [layers.lastObject removeFromSuperlayer];
        [layers removeLastObject];
    }
    id (*make)(Class, SEL, NSString *) = (id (*)(Class, SEL, NSString *))objc_msgSend;
    for (int32_t i = 0; i < count; i++) {
        const float *r = regions + 9 * i;
        CALayer *layer;
        if (i < (int32_t)layers.count) {
            layer = layers[i];
        } else {
            layer = [backdrop_class new];
            @try {
                [layer setValue:@YES forKey:@"windowServerAware"];
            } @catch (NSException *e) {
                [CATransaction commit];
                return false;
            }
            layer.masksToBounds = YES;
            [view.layer addSublayer:layer];
            [layers addObject:layer];
        }
        layer.frame = CGRectMake(r[0], r[1], r[2], r[3]);
        // One radius, on the corners that have one: the view is flipped, so min-y is the top.
        float radius = fmaxf(fmaxf(r[4], r[5]), fmaxf(r[6], r[7]));
        layer.cornerRadius = radius;
        CACornerMask corners = 0;
        if (r[4] > 0) corners |= kCALayerMinXMinYCorner;
        if (r[5] > 0) corners |= kCALayerMaxXMinYCorner;
        if (r[6] > 0) corners |= kCALayerMaxXMaxYCorner;
        if (r[7] > 0) corners |= kCALayerMinXMaxYCorner;
        layer.maskedCorners = corners;
        NSNumber *blur = @(r[8]);
        id filter = layer.filters.firstObject;
        if (!filter || ![[filter valueForKey:@"inputRadius"] isEqual:blur]) {
            filter = make(filter_class, make_filter, @"gaussianBlur");
            [filter setValue:blur forKey:@"inputRadius"];
            layer.filters = @[filter];
            [layer setValue:[NSString stringWithFormat:@"lyra-blur-%g", r[8]] forKey:@"groupName"];
        }
    }
    [CATransaction commit];
    return true;
}

// `backdrop_regions_state`: how many regions the window blurs, -1 where it has no view for them
// — for a test of `set_backdrop_regions`.
int32_t lyra_gpu_backdrop_regions_state(SDL_Window *window) {
    NSWindow *ns = cocoa_window(window);
    LyraRegions *view = ns ? objc_getAssociatedObject(ns, &regions_key) : nil;
    if (!view || view.superview != ns.contentView) return -1;
    return (int32_t)view.layer.sublayers.count;
}

// `backdrop_state`: the radius the window's backdrop blurs by, -1 where it has none (a window
// blurred by the window server has none): for a test of `set_backdrop_blur`.
int32_t lyra_gpu_backdrop_state(SDL_Window *window) {
    NSWindow *ns = cocoa_window(window);
    LyraBackdrop *view = ns ? objc_getAssociatedObject(ns, &backdrop_key) : nil;
    if (!view || view.superview != ns.contentView || view.layer.filters.count == 0) return -1;
    return (int32_t)[[view.layer.filters[0] valueForKey:@"inputRadius"] intValue];
}

// `set_backdrop_blur`: what is behind the window blurred by `radius` points (0 for none), by the
// backdrop above; where Core Animation has no backdrop layer, by the window server, through
// CoreGraphics' private CGSSetWindowBackgroundBlurRadius, looked up at run time: false where
// neither is there.
typedef int32_t (*ConnectionFn)(void);
typedef int32_t (*BlurFn)(int32_t connection, int32_t window, int32_t radius);

bool lyra_gpu_set_backdrop_blur(SDL_Window *window, int32_t radius) {
    NSWindow *ns = cocoa_window(window);
    if (!ns) return false;
    if (backdrop_blur(ns, radius < 0 ? 0 : radius)) return true;
    ConnectionFn connection = (ConnectionFn)dlsym(RTLD_DEFAULT, "CGSDefaultConnectionForThread");
    BlurFn blur = (BlurFn)dlsym(RTLD_DEFAULT, "CGSSetWindowBackgroundBlurRadius");
    if (!connection || !blur) return false;
    return blur(connection(), (int32_t)ns.windowNumber, radius < 0 ? 0 : radius) == 0;
}
