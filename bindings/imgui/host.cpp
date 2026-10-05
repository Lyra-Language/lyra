// The C half of `bindings.imgui`: one window, an SDL_GPU device, and Dear ImGui with
// docking and multi-viewports, driven a frame at a time from Lyra.
//
// **Why a host rather than binding SDL_GPU.** ImGui's SDL_GPU backend needs a device, a
// claimed window, a command buffer, a swapchain texture and a render pass every frame —
// five SDL_GPU types and a dozen calls Lyra would bind only to hand straight back. The
// loop is ImGui's own example (`examples/example_sdl3_sdlgpu3/main.cpp`), and Lyra calls
// four functions around it. SDL_GPU is the renderer because it is the SDL backend with
// multi-viewport support (SDL_Renderer's has none): a panel dragged out of the main
// window becomes an OS window of its own.
//
// **Built without the C++ runtime** (`-fno-exceptions -fno-rtti -fno-threadsafe-statics`):
// ImGui uses no STL, so the archive links with the plain C driver `lyrac` runs, on every
// platform, with no `-lc++`/`-lstdc++` to choose between.

#include "imgui.h"
#include "imgui_internal.h"
#include "imgui_impl_sdl3.h"
#include "imgui_impl_sdlgpu3.h"
#include <SDL3/SDL.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// A file dialog's answer, waiting for Lyra to poll it: `kind` is one of the
// LYRA_DIALOG_* values, `text` the chosen path or the error (NULL when cancelled).
struct LyraDialogAnswer {
    int32_t tag;
    int32_t kind;
    char* text;
    LyraDialogAnswer* next;
};

enum {
    LYRA_DIALOG_CHOSEN = 0,
    LYRA_DIALOG_CANCELLED = 1,
    LYRA_DIALOG_FAILED = 2,
};

// A font a host loaded from a file, and how: the same file at another size or offset is
// another font. `font` is NULL for a file that could not be read, so it is not tried again.
enum { LYRA_IMGUI_MAX_FONT_FILES = 8, LYRA_IMGUI_FONT_PATH = 1024 };
struct LyraFontFile {
    char path[LYRA_IMGUI_FONT_PATH];
    float size;
    float offset_y;
    ImFont* font;
};

struct LyraImGuiHost {
    SDL_Window* window;
    SDL_GPUDevice* device;
    bool quit;
    // A headless host has no window and no GPU (both NULL): ImGui runs a frame at a time
    // at a fixed size and step, and its input is only what the program injects — how a
    // program tests its interface. See lyra_imgui_host_create_headless.
    bool headless;
    float width, height;
    // Headless only: the last file dialog asked for, which is recorded, not shown.
    bool dialog_requested;
    int32_t dialog_tag;
    bool dialog_save;
    // Answers from file dialogs, oldest first. SDL may call a dialog's callback on
    // another thread, so the queue is the lock's; `polled` is the last answer handed to
    // Lyra, freed on the next poll.
    SDL_Mutex* dialog_lock;
    LyraDialogAnswer* answers;
    LyraDialogAnswer* polled;
    // The style as the host set it up, the display's density applied, and the program's
    // scale on top of it (lyra_imgui_host_set_ui_scale). Kept so a new scale starts from
    // the base: ScaleAllSizes multiplies and rounds, so scaling the scaled style drifts.
    ImGuiStyle base_style;
    float ui_scale;
    // HOST_CUSTOM_TITLE_BAR: the main window has no title bar of the system's, and the
    // program draws one (lyra_imgui_title_bar). `drag_area` is the part of that bar a
    // press moves the window by — between its menus and its buttons, in the window's
    // coordinates — as the last frame drew it; the hit test reads it.
    bool custom_title_bar;
    bool drag_area_set;
    SDL_FRect drag_area;
    // Fonts loaded from files (lyra_imgui_host_font_file), each once: a font belongs to
    // the context that loaded it, so it is kept with the host that owns that context and
    // goes with it — a program-wide cache handed a later host the last one's freed font.
    LyraFontFile fonts[LYRA_IMGUI_MAX_FONT_FILES];
    int font_count;
    // The window's buttons drawn as macOS's traffic lights — red, yellow and green circles —
    // rather than as glyphs on the bar (lyra_imgui_host_set_traffic_lights).
    bool traffic_lights;
};

// Flags for lyra_imgui_host_create; Lyra spells them HOST_DOCKING and HOST_VIEWPORTS.
enum {
    LYRA_IMGUI_DOCKING = 1 << 0,
    LYRA_IMGUI_VIEWPORTS = 1 << 1,
    LYRA_IMGUI_CUSTOM_TITLE_BAR = 1 << 2,
};

// Which of the window's buttons lyra_imgui_window_button draws.
enum {
    LYRA_WINDOW_MINIMIZE = 0,
    LYRA_WINDOW_MAXIMIZE = 1,
    LYRA_WINDOW_CLOSE = 2,
};

#if defined(__APPLE__)
// host_macos.m: lets a borderless (torn-off) window straddle two displays.
extern "C" void lyra_imgui_macos_allow_straddling(void);
// host_macos.m: a double-click on the title bar's drag area does what the person's
// "double-click a window's title bar" setting says; and the monitor doing it removed.
// `nswindow` is the main window's NSWindow.
extern "C" void lyra_imgui_macos_watch_title_bar(void* nswindow, LyraImGuiHost* host);
extern "C" void lyra_imgui_macos_unwatch_title_bar(void);
#endif

// How far in from the window's edge a press resizes it, in window coordinates, where the
// system draws no frame (HOST_CUSTOM_TITLE_BAR). Not on macOS: AppKit resizes a borderless
// window at its edges itself, and SDL's Cocoa hit test knows only dragging.
static const int RESIZE_BORDER = 5;

// Whether (x, y), in the main window's coordinates, is in the title bar's drag area.
extern "C" bool lyra_imgui_host_in_drag_area(LyraImGuiHost* host, float x, float y) {
    const SDL_FRect& a = host->drag_area;
    return host->drag_area_set && x >= a.x && x < a.x + a.w && y >= a.y && y < a.y + a.h;
}

// SDL's hit test for a window drawing its own title bar: its edges resize it (but not
// while it fills the screen), and the empty stretch of the bar moves it — the system's
// own drag, so it snaps and crosses displays as a title bar's does.
static SDL_HitTestResult title_bar_hit_test(SDL_Window* window, const SDL_Point* p, void* data) {
    LyraImGuiHost* host = (LyraImGuiHost*)data;
#if !defined(__APPLE__)
    if (!(SDL_GetWindowFlags(window) & (SDL_WINDOW_MAXIMIZED | SDL_WINDOW_FULLSCREEN))) {
        int w = 0, h = 0;
        SDL_GetWindowSize(window, &w, &h);
        const bool left = p->x < RESIZE_BORDER, right = p->x >= w - RESIZE_BORDER;
        const bool top = p->y < RESIZE_BORDER, bottom = p->y >= h - RESIZE_BORDER;
        if (top && left)
            return SDL_HITTEST_RESIZE_TOPLEFT;
        if (top && right)
            return SDL_HITTEST_RESIZE_TOPRIGHT;
        if (bottom && left)
            return SDL_HITTEST_RESIZE_BOTTOMLEFT;
        if (bottom && right)
            return SDL_HITTEST_RESIZE_BOTTOMRIGHT;
        if (top)
            return SDL_HITTEST_RESIZE_TOP;
        if (bottom)
            return SDL_HITTEST_RESIZE_BOTTOM;
        if (left)
            return SDL_HITTEST_RESIZE_LEFT;
        if (right)
            return SDL_HITTEST_RESIZE_RIGHT;
    }
#else
    (void)window;
#endif
    if (lyra_imgui_host_in_drag_area(host, (float)p->x, (float)p->y))
        return SDL_HITTEST_DRAGGABLE;
    return SDL_HITTEST_NORMAL;
}

extern "C" {

// Open the main window and set ImGui up in it. NULL on failure; SDL_GetError says why.
// Initialises SDL's video and gamepad subsystems, which lyra_imgui_host_destroy quits.
LyraImGuiHost* lyra_imgui_host_create(const char* title, int32_t width, int32_t height, uint32_t flags) {
    if (!SDL_Init(SDL_INIT_VIDEO | SDL_INIT_GAMEPAD))
        return nullptr;
#if defined(__APPLE__)
    lyra_imgui_macos_allow_straddling();
#endif

    float scale = SDL_GetDisplayContentScale(SDL_GetPrimaryDisplay());
    if (scale <= 0.0f)
        scale = 1.0f;
    SDL_WindowFlags window_flags = SDL_WINDOW_RESIZABLE | SDL_WINDOW_HIDDEN | SDL_WINDOW_HIGH_PIXEL_DENSITY;
    if (flags & LYRA_IMGUI_CUSTOM_TITLE_BAR)
        window_flags |= SDL_WINDOW_BORDERLESS;
    SDL_Window* window = SDL_CreateWindow(title, (int)(width * scale), (int)(height * scale), window_flags);
    if (window == nullptr) {
        SDL_Quit();
        return nullptr;
    }
    SDL_SetWindowPosition(window, SDL_WINDOWPOS_CENTERED, SDL_WINDOWPOS_CENTERED);
    SDL_ShowWindow(window);

    SDL_GPUDevice* device = SDL_CreateGPUDevice(
        SDL_GPU_SHADERFORMAT_SPIRV | SDL_GPU_SHADERFORMAT_DXIL | SDL_GPU_SHADERFORMAT_MSL | SDL_GPU_SHADERFORMAT_METALLIB,
        false, nullptr);
    if (device == nullptr || !SDL_ClaimWindowForGPUDevice(device, window)) {
        if (device != nullptr)
            SDL_DestroyGPUDevice(device);
        SDL_DestroyWindow(window);
        SDL_Quit();
        return nullptr;
    }
    SDL_SetGPUSwapchainParameters(device, window, SDL_GPU_SWAPCHAINCOMPOSITION_SDR, SDL_GPU_PRESENTMODE_VSYNC);

    IMGUI_CHECKVERSION();
    ImGui::CreateContext();
    ImGuiIO& io = ImGui::GetIO();
    io.ConfigFlags |= ImGuiConfigFlags_NavEnableKeyboard | ImGuiConfigFlags_NavEnableGamepad;
    if (flags & LYRA_IMGUI_DOCKING)
        io.ConfigFlags |= ImGuiConfigFlags_DockingEnable;
    if (flags & LYRA_IMGUI_VIEWPORTS)
        io.ConfigFlags |= ImGuiConfigFlags_ViewportsEnable;

    ImGui::StyleColorsDark();
    ImGuiStyle& style = ImGui::GetStyle();
    style.ScaleAllSizes(scale);
    style.FontScaleDpi = scale;
    io.ConfigDpiScaleFonts = true;
    io.ConfigDpiScaleViewports = true;
    if (io.ConfigFlags & ImGuiConfigFlags_ViewportsEnable) {
        // A torn-off panel is an OS window; square corners and an opaque background
        // are what make it look like one.
        style.WindowRounding = 0.0f;
        style.Colors[ImGuiCol_WindowBg].w = 1.0f;
    }

    ImGui_ImplSDL3_InitForSDLGPU(window);
    ImGui_ImplSDLGPU3_InitInfo init_info = {};
    init_info.Device = device;
    init_info.ColorTargetFormat = SDL_GetGPUSwapchainTextureFormat(device, window);
    init_info.MSAASamples = SDL_GPU_SAMPLECOUNT_1;
    init_info.SwapchainComposition = SDL_GPU_SWAPCHAINCOMPOSITION_SDR;
    init_info.PresentMode = SDL_GPU_PRESENTMODE_VSYNC;
    ImGui_ImplSDLGPU3_Init(&init_info);

    LyraImGuiHost* host = (LyraImGuiHost*)calloc(1, sizeof(LyraImGuiHost));
    host->window = window;
    host->device = device;
    host->dialog_lock = SDL_CreateMutex();
    host->base_style = ImGui::GetStyle();
    host->ui_scale = 1.0f;
    if (flags & LYRA_IMGUI_CUSTOM_TITLE_BAR) {
        host->custom_title_bar = true;
        SDL_SetWindowHitTest(window, title_bar_hit_test, host);
#if defined(__APPLE__)
        lyra_imgui_macos_watch_title_bar(
            SDL_GetPointerProperty(SDL_GetWindowProperties(window), SDL_PROP_WINDOW_COCOA_WINDOW_POINTER, nullptr), host);
#endif
    }
    return host;
}

// A host with no window and no GPU, for tests: ImGui frames of `width` × `height` points at
// a fixed 1/60 s, with input only from io.Add*Event. No SDL video or GPU — it runs where
// there is no display (its one SDL object, the dialog queue's mutex, needs no SDL_Init). The layout is not read from or written to imgui.ini, so a test never sees
// (or changes) a person's; and Cmd/Ctrl are not swapped on macOS, so an injected Ctrl+Z
// means one thing everywhere. Multi-viewports need a platform window and are left off.
LyraImGuiHost* lyra_imgui_host_create_headless(int32_t width, int32_t height, uint32_t flags) {
    IMGUI_CHECKVERSION();
    ImGui::CreateContext();
    ImGuiIO& io = ImGui::GetIO();
    io.IniFilename = nullptr;
    io.ConfigMacOSXBehaviors = false;
    if (flags & LYRA_IMGUI_DOCKING)
        io.ConfigFlags |= ImGuiConfigFlags_DockingEnable;
    // What imgui_impl_null.cpp's renderer declares: it takes textures (and marks each one
    // done at end_frame), so the font atlas is built as it would be with a GPU.
    io.BackendFlags |= ImGuiBackendFlags_RendererHasVtxOffset | ImGuiBackendFlags_RendererHasTextures;
    io.BackendPlatformName = "lyra_headless";
    io.BackendRendererName = "lyra_headless";
    ImGui::StyleColorsDark();

    LyraImGuiHost* host = (LyraImGuiHost*)calloc(1, sizeof(LyraImGuiHost));
    host->headless = true;
    host->custom_title_bar = (flags & LYRA_IMGUI_CUSTOM_TITLE_BAR) != 0;
    host->dialog_lock = SDL_CreateMutex();
    host->width = (float)width;
    host->height = (float)height;
    host->base_style = ImGui::GetStyle();
    host->ui_scale = 1.0f;
    return host;
}

// Scale the whole interface by `scale` (1 is as the host set it up): text through
// FontScaleMain, which 1.92's dynamic fonts render sharp at any size, and every padding,
// spacing and rounding through ScaleAllSizes — both from the base style, so scales do not
// compound. Clamped to 0.5–3; the display's density stays applied beneath it.
void lyra_imgui_host_set_ui_scale(LyraImGuiHost* host, float scale) {
    if (!(scale >= 0.5f))
        scale = 0.5f;
    if (scale > 3.0f)
        scale = 3.0f;
    ImGuiStyle& style = ImGui::GetStyle();
    style = host->base_style;
    style.ScaleAllSizes(scale);
    style.FontScaleMain = host->base_style.FontScaleMain * scale;
    host->ui_scale = scale;
}

float lyra_imgui_host_ui_scale(LyraImGuiHost* host) {
    return host->ui_scale;
}

// A bar across the bottom of the main window, a line of text tall, which the dock space
// and other windows leave room for — ImGui's own BeginViewportSideBar, an internal API the
// generated bindings leave out. True when its contents may be drawn; end it either way.
static bool status_bar_open = false;

bool lyra_imgui_begin_status_bar(const char* name) {
    ImGuiWindowFlags flags = ImGuiWindowFlags_NoScrollbar | ImGuiWindowFlags_NoSavedSettings | ImGuiWindowFlags_MenuBar;
    status_bar_open = false;
    if (ImGui::BeginViewportSideBar(name, ImGui::GetMainViewport(), ImGuiDir_Down, ImGui::GetFrameHeight(), flags))
        status_bar_open = ImGui::BeginMenuBar();
    return status_bar_open;
}

void lyra_imgui_end_status_bar(void) {
    if (status_bar_open)
        ImGui::EndMenuBar();
    ImGui::End();
    status_bar_open = false;
}

// ── Fonts from files ───────────────────────────────────────────────────────────

// A TrueType/OpenType font from a file, for PushFont, loaded into the host's context the
// first time it is asked for and kept with the host after — AddFontFromFileTTF, which the
// generated bindings leave out (its glyph-range array). NULL when the file cannot be read:
// ImGui asserts on a missing file rather than answering, so it is opened here first.
// `size` is the size `offset_y` is written for — 1.92's fonts are dynamic, PushFont chooses
// the size drawn, and the offset scales with it — and `offset_y` moves every glyph down
// (negative: up), for a font whose line metrics set its letters low or high.
ImFont* lyra_imgui_host_font_file(LyraImGuiHost* host, const char* path, float size, float offset_y) {
    if (strlen(path) >= LYRA_IMGUI_FONT_PATH)
        return NULL;
    for (int i = 0; i < host->font_count; i++) {
        LyraFontFile* known = &host->fonts[i];
        if (strcmp(known->path, path) == 0 && known->size == size && known->offset_y == offset_y)
            return known->font;
    }
    ImFont* font = NULL;
    FILE* f = fopen(path, "rb");
    if (f) {
        fclose(f);
        ImFontConfig config;
        config.GlyphOffset = ImVec2(0.0f, offset_y);
        font = ImGui::GetIO().Fonts->AddFontFromFileTTF(path, size, &config);
    }
    if (host->font_count < LYRA_IMGUI_MAX_FONT_FILES) {
        LyraFontFile* kept = &host->fonts[host->font_count++];
        strcpy(kept->path, path);
        kept->size = size;
        kept->offset_y = offset_y;
        kept->font = font;
    }
    return font;
}

// ── The window's own title bar (HOST_CUSTOM_TITLE_BAR) ───────────────────────

bool lyra_imgui_host_custom_title_bar(LyraImGuiHost* host) {
    return host->custom_title_bar;
}

// The window's title as the system shows it (the Dock, the Window menu, the taskbar).
void lyra_imgui_host_set_title(LyraImGuiHost* host, const char* title) {
    if (host->headless)
        return;
    const char* now = SDL_GetWindowTitle(host->window);
    if (now == nullptr || strcmp(now, title) != 0)
        SDL_SetWindowTitle(host->window, title);
}

bool lyra_imgui_host_maximized(LyraImGuiHost* host) {
    return !host->headless && (SDL_GetWindowFlags(host->window) & SDL_WINDOW_MAXIMIZED) != 0;
}

void lyra_imgui_host_minimize(LyraImGuiHost* host) {
    if (!host->headless)
        SDL_MinimizeWindow(host->window);
}

// Maximise the window, or restore it when it is maximised.
void lyra_imgui_host_toggle_maximized(LyraImGuiHost* host) {
    if (host->headless)
        return;
    if (SDL_GetWindowFlags(host->window) & SDL_WINDOW_MAXIMIZED)
        SDL_RestoreWindow(host->window);
    else
        SDL_MaximizeWindow(host->window);
}

// Whether the window's buttons go at the title bar's left end — on macOS, where they
// always have (headless too, so a test there sees the bar as the window shows it).
bool lyra_imgui_host_window_buttons_left(LyraImGuiHost* host) {
    (void)host;
#if defined(__APPLE__)
    return true;
#else
    return false;
#endif
}

// First in the main menu bar, where the buttons go at its left end: the cursor at the
// bar's very edge, so the first button sits in the corner.
void lyra_imgui_title_bar_start(LyraImGuiHost* host) {
    ImGuiWindow* window = ImGui::GetCurrentWindow();
    // Traffic lights stand a little in from the corner, as macOS's do.
    const float inset = host->traffic_lights ? IM_ROUND(ImGui::GetFontSize() * 0.4f) : 0.0f;
    window->DC.CursorPos.x = window->MenuBarRect().Min.x + inset;
}

// A button's width: half as wide again as the bar is tall — or, as traffic lights, a
// circle and the gap to the next.
static float window_button_width(LyraImGuiHost* host) {
    if (host->traffic_lights)
        return IM_ROUND(ImGui::GetFontSize() * 1.55f);
    return IM_ROUND(ImGui::GetFrameHeight() * 1.5f);
}

// The window's buttons as macOS's traffic lights from now on, or as glyphs on the bar.
void lyra_imgui_host_set_traffic_lights(LyraImGuiHost* host, bool on) {
    host->traffic_lights = on;
}

// In the menu bar being drawn, after the program's menus (and the window's buttons, where
// they lead): the title centred on the bar (moved right of the menus when they reach past
// where it would start, left out when it does not fit before the buttons), the stretch
// between the menus and `buttons` window buttons at the right end (0 where they lead)
// kept as the drag area, and the cursor where the first of those goes.
void lyra_imgui_title_bar(LyraImGuiHost* host, const char* title, int32_t buttons) {
    ImGuiWindow* window = ImGui::GetCurrentWindow();
    const ImGuiStyle& style = ImGui::GetStyle();
    const ImRect bar = window->MenuBarRect();
    const float left = window->DC.CursorPos.x;
    const float right = bar.Max.x - (float)buttons * window_button_width(host);

    const ImVec2 size = ImGui::CalcTextSize(title);
    float x = IM_ROUND((bar.Min.x + bar.Max.x - size.x) * 0.5f);
    if (x < left + style.ItemSpacing.x)
        x = left + style.ItemSpacing.x;
    if (x + size.x + style.ItemSpacing.x <= right) {
        const ImVec2 at(x, IM_ROUND(bar.Min.y + (bar.GetHeight() - size.y) * 0.5f));
        const bool focused = ImGui::IsWindowFocused(ImGuiFocusedFlags_AnyWindow) || host->headless;
        window->DrawList->AddText(at, ImGui::GetColorU32(focused ? ImGuiCol_Text : ImGuiCol_TextDisabled), title);
    }

    const ImVec2 origin = ImGui::GetMainViewport()->Pos;
    host->drag_area = SDL_FRect{left - origin.x, bar.Min.y - origin.y, ImMax(right - left, 0.0f), bar.GetHeight()};
    host->drag_area_set = true;
    window->DC.CursorPos.x = right;
}

// One of the window's buttons (LYRA_WINDOW_*) at the cursor, the bar's height and
// window_button_width() wide, its glyph drawn with lines (the default font has none);
// whether it was clicked. Close turns red under the pointer, as Windows' and GNOME's do.
bool lyra_imgui_window_button(LyraImGuiHost* host, int32_t kind) {
    ImGuiWindow* window = ImGui::GetCurrentWindow();
    const ImRect bar = window->MenuBarRect();
    const float w = window_button_width(host);
    const ImVec2 min(window->DC.CursorPos.x, bar.Min.y);
    ImGui::SetCursorScreenPos(min);
    ImGui::PushID(kind);
    const bool pressed = ImGui::InvisibleButton("##window_button", ImVec2(w, bar.GetHeight()));
    const bool hovered = ImGui::IsItemHovered(), held = ImGui::IsItemActive();
    ImGui::PopID();
    window->DC.CursorPos.x = min.x + w;

    ImDrawList* draw = window->DrawList;
    const ImVec2 max(min.x + w, bar.Max.y);
    if (host->traffic_lights) {
        // macOS's: close red, minimize yellow, zoom green, grey while the window is not the
        // one in use; under the pointer, darker, with its glyph.
        const float font = ImGui::GetFontSize();
        const float r = IM_ROUND(font * 0.46f);
        const ImVec2 c(IM_ROUND((min.x + max.x) * 0.5f), IM_ROUND((min.y + max.y) * 0.5f));
        const bool focused = ImGui::IsWindowFocused(ImGuiFocusedFlags_AnyWindow) || host->headless;
        ImU32 fill = kind == LYRA_WINDOW_CLOSE ? IM_COL32(255, 95, 87, 255)
            : kind == LYRA_WINDOW_MINIMIZE    ? IM_COL32(254, 188, 46, 255)
                                              : IM_COL32(40, 200, 64, 255);
        ImU32 rim = kind == LYRA_WINDOW_CLOSE ? IM_COL32(224, 68, 62, 255)
            : kind == LYRA_WINDOW_MINIMIZE   ? IM_COL32(222, 161, 35, 255)
                                             : IM_COL32(29, 173, 43, 255);
        if (!focused && !hovered) {
            fill = IM_COL32(200, 200, 200, 255);
            rim = IM_COL32(180, 180, 180, 255);
        }
        if (held) {
            const ImVec4 v = ImGui::ColorConvertU32ToFloat4(fill);
            fill = ImGui::GetColorU32(ImVec4(v.x * 0.8f, v.y * 0.8f, v.z * 0.8f, 1.0f));
        }
        draw->AddCircleFilled(c, r, fill, 24);
        draw->AddCircle(c, r - 0.5f, rim, 24, 1.0f);
        if (hovered || held) {
            const ImU32 ink = IM_COL32(0, 0, 0, 150);
            const float g = IM_ROUND(r * 0.5f);
            const float t = ImMax(1.0f, IM_ROUND(font / 13.0f));
            if (kind == LYRA_WINDOW_CLOSE) {
                draw->AddLine(ImVec2(c.x - g, c.y - g), ImVec2(c.x + g, c.y + g), ink, t);
                draw->AddLine(ImVec2(c.x - g, c.y + g), ImVec2(c.x + g, c.y - g), ink, t);
            } else if (kind == LYRA_WINDOW_MINIMIZE) {
                draw->AddLine(ImVec2(c.x - g, c.y), ImVec2(c.x + g, c.y), ink, t);
            } else {
                draw->AddLine(ImVec2(c.x - g, c.y), ImVec2(c.x + g, c.y), ink, t);
                draw->AddLine(ImVec2(c.x, c.y - g), ImVec2(c.x, c.y + g), ink, t);
            }
        }
        return pressed;
    }
    ImU32 ink = ImGui::GetColorU32(ImGuiCol_Text);
    if (kind == LYRA_WINDOW_CLOSE && (hovered || held)) {
        draw->AddRectFilled(min, max, held ? IM_COL32(150, 30, 30, 255) : IM_COL32(196, 43, 28, 255));
        ink = IM_COL32_WHITE;
    } else if (hovered || held) {
        draw->AddRectFilled(min, max, ImGui::GetColorU32(held ? ImGuiCol_ButtonActive : ImGuiCol_ButtonHovered));
    }

    const float font = ImGui::GetFontSize();
    const float g = IM_ROUND(font * 0.38f);  // half the glyph's width
    const float t = ImMax(1.0f, IM_ROUND(font / 13.0f));
    const ImVec2 c(IM_ROUND((min.x + max.x) * 0.5f), IM_ROUND((min.y + max.y) * 0.5f));
    switch (kind) {
    case LYRA_WINDOW_MINIMIZE:
        draw->AddLine(ImVec2(c.x - g, c.y), ImVec2(c.x + g, c.y), ink, t);
        break;
    case LYRA_WINDOW_MAXIMIZE:
        if (lyra_imgui_host_maximized(host)) {
            // Restore: a window behind a window.
            const float s = IM_ROUND(g * 0.4f);
            draw->AddRect(ImVec2(c.x - g, c.y - g + s), ImVec2(c.x + g - s, c.y + g), ink, 0.0f, 0, t);
            draw->PathLineTo(ImVec2(c.x - g + s, c.y - g + s));
            draw->PathLineTo(ImVec2(c.x - g + s, c.y - g));
            draw->PathLineTo(ImVec2(c.x + g, c.y - g));
            draw->PathLineTo(ImVec2(c.x + g, c.y + g - s));
            draw->PathLineTo(ImVec2(c.x + g - s, c.y + g - s));
            draw->PathStroke(ink, 0, t);
        } else {
            draw->AddRect(ImVec2(c.x - g, c.y - g), ImVec2(c.x + g, c.y + g), ink, 0.0f, 0, t);
        }
        break;
    default:
        draw->AddLine(ImVec2(c.x - g, c.y - g), ImVec2(c.x + g, c.y + g), ink, t);
        draw->AddLine(ImVec2(c.x - g, c.y + g), ImVec2(c.x + g, c.y - g), ink, t);
        break;
    }
    return pressed;
}

// Ask the host to quit, as closing the window would: the next begin_frame answers false.
// How a test reaches a program's "unsaved changes?" path.
void lyra_imgui_host_request_quit(LyraImGuiHost* host) {
    host->quit = true;
}

static bool begin_headless_frame(LyraImGuiHost* host) {
    if (host->quit)
        return false;
    ImGuiIO& io = ImGui::GetIO();
    io.DisplaySize = ImVec2(host->width, host->height);
    io.DisplayFramebufferScale = ImVec2(1.0f, 1.0f);
    io.DeltaTime = 1.0f / 60.0f;
    ImGui::NewFrame();
    return true;
}

static void end_headless_frame() {
    ImGui::Render();
    // imgui_impl_null.cpp's texture handling: every request done, nothing uploaded.
    ImDrawData* draw_data = ImGui::GetDrawData();
    if (draw_data->Textures != nullptr)
        for (ImTextureData* tex : *draw_data->Textures) {
            if (tex->Status == ImTextureStatus_WantDestroy) {
                tex->SetTexID(ImTextureID_Invalid);
                tex->SetStatus(ImTextureStatus_Destroyed);
            } else if (tex->Status != ImTextureStatus_OK) {
                tex->SetStatus(ImTextureStatus_OK);
            }
        }
}

// Pump events and start an ImGui frame. False once the user has asked to quit, and
// then no frame was started. While the main window is minimised this waits rather
// than returning, so every true answer is a frame worth drawing.
bool lyra_imgui_host_begin_frame(LyraImGuiHost* host) {
    if (host->headless)
        return begin_headless_frame(host);
    for (;;) {
        SDL_Event event;
        while (SDL_PollEvent(&event)) {
            ImGui_ImplSDL3_ProcessEvent(&event);
            if (event.type == SDL_EVENT_QUIT)
                host->quit = true;
            if (event.type == SDL_EVENT_WINDOW_CLOSE_REQUESTED && event.window.windowID == SDL_GetWindowID(host->window))
                host->quit = true;
        }
        if (host->quit)
            return false;
        // Events are in: the drag area is redrawn by the frame about to start, if at all.
        host->drag_area_set = false;
        if (!(SDL_GetWindowFlags(host->window) & SDL_WINDOW_MINIMIZED))
            break;
        SDL_WaitEventTimeout(nullptr, 100);
    }
    ImGui_ImplSDLGPU3_NewFrame();
    ImGui_ImplSDL3_NewFrame();
    ImGui::NewFrame();
    return true;
}

// Render the frame into the main window — cleared to the given colour behind ImGui —
// and into every torn-off window, then present.
void lyra_imgui_host_end_frame(LyraImGuiHost* host, float r, float g, float b) {
    if (host->headless) {
        end_headless_frame();
        return;
    }
    ImGui::Render();
    ImDrawData* draw_data = ImGui::GetDrawData();
    const bool minimized = draw_data->DisplaySize.x <= 0.0f || draw_data->DisplaySize.y <= 0.0f;

    SDL_GPUCommandBuffer* commands = SDL_AcquireGPUCommandBuffer(host->device);
    SDL_GPUTexture* swapchain = nullptr;
    SDL_WaitAndAcquireGPUSwapchainTexture(commands, host->window, &swapchain, nullptr, nullptr);
    if (swapchain != nullptr && !minimized) {
        ImGui_ImplSDLGPU3_PrepareDrawData(draw_data, commands);
        SDL_GPUColorTargetInfo target = {};
        target.texture = swapchain;
        target.clear_color = SDL_FColor{r, g, b, 1.0f};
        target.load_op = SDL_GPU_LOADOP_CLEAR;
        target.store_op = SDL_GPU_STOREOP_STORE;
        SDL_GPURenderPass* pass = SDL_BeginGPURenderPass(commands, &target, 1, nullptr);
        ImGui_ImplSDLGPU3_RenderDrawData(draw_data, commands, pass);
        SDL_EndGPURenderPass(pass);
    }
    if (ImGui::GetIO().ConfigFlags & ImGuiConfigFlags_ViewportsEnable) {
        ImGui::UpdatePlatformWindows();
        ImGui::RenderPlatformWindowsDefault();
    }
    SDL_SubmitGPUCommandBuffer(commands);
}

// ── Text input over a growable buffer ───────────────────────────────────────
//
// ImGui edits a `char *` in place and a Lyra string is immutable, so the text is copied
// into a buffer the host owns and grows through ImGui's resize callback (imgui_stdlib's
// pattern, without the STL). One buffer serves every call: its contents are the widget's
// value only until the next call, and the Lyra wrapper copies it out at once when it
// changed. Never freed; it is the size of the longest text edited.

static char* text_buf = nullptr;
static int text_cap = 0;

static bool text_reserve(int size) {
    if (size <= text_cap)
        return true;
    int cap = text_cap > 0 ? text_cap : 256;
    while (cap < size)
        cap *= 2;
    char* grown = (char*)realloc(text_buf, (size_t)cap);
    if (grown == nullptr)
        return false;
    text_buf = grown;
    text_cap = cap;
    return true;
}

static int text_resize(ImGuiInputTextCallbackData* data) {
    if (data->EventFlag == ImGuiInputTextFlags_CallbackResize) {
        if (!text_reserve(data->BufSize))
            data->BufSize = text_cap; // refused: ImGui truncates to what fits
        data->Buf = text_buf;
    }
    return 0;
}

// Edit `text` in an InputText (multiline when `multiline`, with a grey `hint` when not
// NULL). Answers the buffer holding the value after this frame's edit — the host's, valid
// until the next call — and sets `*changed` when the user changed it.
const char* lyra_imgui_input_text(const char* label, const char* hint, const char* text, int32_t flags,
                                  bool multiline, ImVec2 size, bool* changed) {
    size_t len = strlen(text);
    if (!text_reserve((int)len + 1)) {
        *changed = false;
        return text;
    }
    memcpy(text_buf, text, len + 1);
    ImGuiInputTextFlags f = (ImGuiInputTextFlags)flags | ImGuiInputTextFlags_CallbackResize;
    if (multiline)
        *changed = ImGui::InputTextMultiline(label, text_buf, (size_t)text_cap, size, f, text_resize);
    else if (hint != nullptr)
        *changed = ImGui::InputTextWithHint(label, hint, text_buf, (size_t)text_cap, f, text_resize);
    else
        *changed = ImGui::InputText(label, text_buf, (size_t)text_cap, f, text_resize);
    return text_buf;
}

// ── Textures ───────────────────────────────────────────────────────────────────
//
// An RGBA texture the program fills from Lyra and ImGui draws: tiles, sprites, a canvas.
// ImGui's SDL_GPU backend takes an `SDL_GPUTexture *` as the texture ID, so that is what
// Lyra holds. Pixel art wants nearest-neighbour sampling, which the image functions below
// switch to around one draw with ImGui's standard sampler callbacks.

// A width × height RGBA texture, contents undefined until updated; NULL on failure.
SDL_GPUTexture* lyra_imgui_texture_create(LyraImGuiHost* host, int32_t width, int32_t height) {
    if (host->device == nullptr)
        return nullptr; // headless: no GPU, so no texture (Lyra answers None)
    SDL_GPUTextureCreateInfo info = {};
    info.type = SDL_GPU_TEXTURETYPE_2D;
    info.format = SDL_GPU_TEXTUREFORMAT_R8G8B8A8_UNORM;
    info.usage = SDL_GPU_TEXTUREUSAGE_SAMPLER;
    info.width = (Uint32)width;
    info.height = (Uint32)height;
    info.layer_count_or_depth = 1;
    info.num_levels = 1;
    info.sample_count = SDL_GPU_SAMPLECOUNT_1;
    return SDL_CreateGPUTexture(host->device, &info);
}

// Replace the whole texture with `rgba` (width × height × 4 bytes, rows top to bottom).
// Submitted at once, ahead of the frame being built, so this frame draws the new pixels.
bool lyra_imgui_texture_update(LyraImGuiHost* host, SDL_GPUTexture* texture, const uint8_t* rgba,
                               int32_t width, int32_t height) {
    if (host->device == nullptr)
        return false;
    const Uint32 size = (Uint32)width * (Uint32)height * 4;
    SDL_GPUTransferBufferCreateInfo transfer_info = {};
    transfer_info.usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD;
    transfer_info.size = size;
    SDL_GPUTransferBuffer* transfer = SDL_CreateGPUTransferBuffer(host->device, &transfer_info);
    if (transfer == nullptr)
        return false;
    void* mapped = SDL_MapGPUTransferBuffer(host->device, transfer, false);
    if (mapped == nullptr) {
        SDL_ReleaseGPUTransferBuffer(host->device, transfer);
        return false;
    }
    memcpy(mapped, rgba, size);
    SDL_UnmapGPUTransferBuffer(host->device, transfer);

    SDL_GPUCommandBuffer* commands = SDL_AcquireGPUCommandBuffer(host->device);
    SDL_GPUCopyPass* copy = SDL_BeginGPUCopyPass(commands);
    SDL_GPUTextureTransferInfo source = {};
    source.transfer_buffer = transfer;
    source.pixels_per_row = (Uint32)width;
    source.rows_per_layer = (Uint32)height;
    SDL_GPUTextureRegion destination = {};
    destination.texture = texture;
    destination.w = (Uint32)width;
    destination.h = (Uint32)height;
    destination.d = 1;
    SDL_UploadToGPUTexture(copy, &source, &destination, false);
    SDL_EndGPUCopyPass(copy);
    const bool submitted = SDL_SubmitGPUCommandBuffer(commands);
    SDL_ReleaseGPUTransferBuffer(host->device, transfer); // freed once the upload is done
    return submitted;
}

// Release a texture. Not while the frame being built still draws it: the draw runs at
// end_frame, after this.
void lyra_imgui_texture_destroy(LyraImGuiHost* host, SDL_GPUTexture* texture) {
    if (host->device == nullptr)
        return;
    SDL_ReleaseGPUTexture(host->device, texture);
}

// Switch `list`'s sampling for the draws after this point: nearest-neighbour or linear.
static void set_sampler(ImDrawList* list, bool nearest) {
    ImGuiPlatformIO& platform_io = ImGui::GetPlatformIO();
    ImDrawCallback callback = nearest ? platform_io.DrawCallback_SetSamplerNearest : platform_io.DrawCallback_SetSamplerLinear;
    if (callback != nullptr)
        list->AddCallback(callback, nullptr);
}

static ImTextureRef texture_ref(SDL_GPUTexture* texture) {
    return ImTextureRef((ImTextureID)(intptr_t)texture);
}

void lyra_imgui_image(SDL_GPUTexture* texture, ImVec2 size, ImVec2 uv0, ImVec2 uv1, bool nearest) {
    ImDrawList* list = ImGui::GetWindowDrawList();
    if (nearest)
        set_sampler(list, true);
    ImGui::Image(texture_ref(texture), size, uv0, uv1);
    if (nearest)
        set_sampler(list, false);
}

bool lyra_imgui_image_button(const char* id, SDL_GPUTexture* texture, ImVec2 size, ImVec2 uv0, ImVec2 uv1,
                             bool nearest) {
    ImDrawList* list = ImGui::GetWindowDrawList();
    if (nearest)
        set_sampler(list, true);
    bool pressed = ImGui::ImageButton(id, texture_ref(texture), size, uv0, uv1);
    if (nearest)
        set_sampler(list, false);
    return pressed;
}

void lyra_imgui_draw_list_add_image(ImDrawList* list, SDL_GPUTexture* texture, ImVec2 p_min, ImVec2 p_max,
                                    ImVec2 uv0, ImVec2 uv1, ImU32 col, bool nearest) {
    if (nearest)
        set_sampler(list, true);
    list->AddImage(texture_ref(texture), p_min, p_max, uv0, uv1, col);
    if (nearest)
        set_sampler(list, false);
}

// ── Quitting ───────────────────────────────────────────────────────────────────

// Take back a quit the user asked for, so the next begin_frame starts a frame: how a
// program with unsaved work asks first. begin_frame answering false is what reports the
// request, and it stays requested until this.
void lyra_imgui_host_cancel_quit(LyraImGuiHost* host) {
    host->quit = false;
}

// ── File dialogs ───────────────────────────────────────────────────────────────
//
// SDL's open and save dialogs are asynchronous: they return at once and answer through a
// callback, possibly on another thread, with a file list freed when the callback returns.
// Lyra cannot hand C a function, so — as `bindings/menubar` does for menu items — the
// callback copies the answer onto the host's queue and Lyra polls for it each frame,
// matching it to its request by the tag it chose.

// One dialog in flight: the filters SDL reads until the callback, and where to answer.
struct LyraDialogRequest {
    LyraImGuiHost* host;
    int32_t tag;
    SDL_DialogFileFilter* filters;
    int nfilters;
    char* filter_text; // the names and patterns `filters` points into
};

static char* copy_string(const char* s) {
    size_t n = strlen(s) + 1;
    char* out = (char*)malloc(n);
    if (out != nullptr)
        memcpy(out, s, n);
    return out;
}

static void dialog_answered(void* userdata, const char* const* filelist, int filter) {
    (void)filter;
    LyraDialogRequest* request = (LyraDialogRequest*)userdata;
    LyraDialogAnswer* answer = (LyraDialogAnswer*)calloc(1, sizeof(LyraDialogAnswer));
    if (answer != nullptr) {
        answer->tag = request->tag;
        if (filelist == nullptr) {
            answer->kind = LYRA_DIALOG_FAILED;
            answer->text = copy_string(SDL_GetError());
        } else if (filelist[0] == nullptr) {
            answer->kind = LYRA_DIALOG_CANCELLED;
        } else {
            answer->kind = LYRA_DIALOG_CHOSEN;
            answer->text = copy_string(filelist[0]);
        }
        LyraImGuiHost* host = request->host;
        SDL_LockMutex(host->dialog_lock);
        LyraDialogAnswer** tail = &host->answers;
        while (*tail != nullptr)
            tail = &(*tail)->next;
        *tail = answer;
        SDL_UnlockMutex(host->dialog_lock);
    }
    free(request->filters);
    free(request->filter_text);
    free(request);
}

// Parse `filters` — "name\tpattern" lines, a pattern being extensions joined by `;`
// (`vega;json`) or `*` — into SDL's array, owned by the request.
static bool dialog_filters(LyraDialogRequest* request, const char* filters) {
    request->filter_text = copy_string(filters);
    if (request->filter_text == nullptr)
        return false;
    int lines = 0;
    for (const char* c = filters; *c != '\0'; c++)
        if (*c == '\n')
            lines++;
    if (filters[0] != '\0' && filters[strlen(filters) - 1] != '\n')
        lines++;
    if (lines == 0)
        return true;
    request->filters = (SDL_DialogFileFilter*)calloc((size_t)lines, sizeof(SDL_DialogFileFilter));
    if (request->filters == nullptr)
        return false;
    char* line = request->filter_text;
    while (line != nullptr && *line != '\0') {
        char* end = strchr(line, '\n');
        if (end != nullptr)
            *end = '\0';
        char* tab = strchr(line, '\t');
        if (tab != nullptr) {
            *tab = '\0';
            request->filters[request->nfilters].name = line;
            request->filters[request->nfilters].pattern = tab + 1;
            request->nfilters++;
        }
        line = end != nullptr ? end + 1 : nullptr;
    }
    return true;
}

// Show an open (`save` false) or save dialog, modal to the main window; its answer is
// queued under `tag`. `default_location` may be NULL. False only when out of memory,
// and then no answer will come.
bool lyra_imgui_show_file_dialog(LyraImGuiHost* host, int32_t tag, bool save, const char* filters,
                                 const char* default_location) {
    if (host->headless) {
        // Recorded, not shown: a test reads the request and queues the answer.
        host->dialog_requested = true;
        host->dialog_tag = tag;
        host->dialog_save = save;
        return true;
    }
    LyraDialogRequest* request = (LyraDialogRequest*)calloc(1, sizeof(LyraDialogRequest));
    if (request == nullptr)
        return false;
    request->host = host;
    request->tag = tag;
    if (!dialog_filters(request, filters)) {
        free(request->filters);
        free(request->filter_text);
        free(request);
        return false;
    }
    SDL_DialogFileFilter* list = request->nfilters > 0 ? request->filters : nullptr;
    if (save)
        SDL_ShowSaveFileDialog(dialog_answered, request, host->window, list, request->nfilters, default_location);
    else
        SDL_ShowOpenFileDialog(dialog_answered, request, host->window, list, request->nfilters, default_location,
                               false);
    return true;
}

// Headless: the dialog last asked for (and forget it), false when none was.
bool lyra_imgui_host_take_dialog_request(LyraImGuiHost* host, int32_t* tag, bool* save) {
    if (!host->dialog_requested)
        return false;
    host->dialog_requested = false;
    *tag = host->dialog_tag;
    *save = host->dialog_save;
    return true;
}

// Queue an answer as a dialog's callback would — how a test answers a recorded request.
// `text` is the path or error, NULL when cancelled.
void lyra_imgui_host_queue_dialog_answer(LyraImGuiHost* host, int32_t tag, int32_t kind, const char* text) {
    LyraDialogAnswer* answer = (LyraDialogAnswer*)calloc(1, sizeof(LyraDialogAnswer));
    if (answer == nullptr)
        return;
    answer->tag = tag;
    answer->kind = kind;
    answer->text = text != nullptr ? copy_string(text) : nullptr;
    SDL_LockMutex(host->dialog_lock);
    LyraDialogAnswer** tail = &host->answers;
    while (*tail != nullptr)
        tail = &(*tail)->next;
    *tail = answer;
    SDL_UnlockMutex(host->dialog_lock);
}

// The oldest queued answer: its tag and kind through the pointers, its text (path or
// error) as the result — owned by the host until the next poll. `*kind` is -1 when no
// answer is waiting.
const char* lyra_imgui_poll_file_dialog(LyraImGuiHost* host, int32_t* tag, int32_t* kind) {
    if (host->polled != nullptr) {
        free(host->polled->text);
        free(host->polled);
        host->polled = nullptr;
    }
    SDL_LockMutex(host->dialog_lock);
    LyraDialogAnswer* answer = host->answers;
    if (answer != nullptr)
        host->answers = answer->next;
    SDL_UnlockMutex(host->dialog_lock);
    if (answer == nullptr) {
        *kind = -1;
        return nullptr;
    }
    host->polled = answer;
    *tag = answer->tag;
    *kind = answer->kind;
    return answer->text;
}

// Tear everything down, SDL included. The host must not be used afterwards.
void lyra_imgui_host_destroy(LyraImGuiHost* host) {
    if (host->headless) {
        ImGui::DestroyContext();
        for (LyraDialogAnswer* a = host->answers; a != nullptr;) {
            LyraDialogAnswer* next = a->next;
            free(a->text);
            free(a);
            a = next;
        }
        if (host->polled != nullptr) {
            free(host->polled->text);
            free(host->polled);
        }
        SDL_DestroyMutex(host->dialog_lock);
        free(host);
        return;
    }
    SDL_WaitForGPUIdle(host->device);
#if defined(__APPLE__)
    if (host->custom_title_bar)
        lyra_imgui_macos_unwatch_title_bar();
#endif
    ImGui_ImplSDL3_Shutdown();
    ImGui_ImplSDLGPU3_Shutdown();
    ImGui::DestroyContext();
    SDL_ReleaseWindowFromGPUDevice(host->device, host->window);
    SDL_DestroyGPUDevice(host->device);
    SDL_DestroyWindow(host->window);
    // Answers nobody polled. A dialog still open now answers into a freed host, so a
    // program closes its dialogs (or waits for them) before it destroys the host.
    for (LyraDialogAnswer* a = host->answers; a != nullptr;) {
        LyraDialogAnswer* next = a->next;
        free(a->text);
        free(a);
        a = next;
    }
    if (host->polled != nullptr) {
        free(host->polled->text);
        free(host->polled);
    }
    SDL_DestroyMutex(host->dialog_lock);
    free(host);
    SDL_Quit();
}

}
