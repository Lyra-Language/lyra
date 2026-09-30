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
};

// Flags for lyra_imgui_host_create; Lyra spells them HOST_DOCKING and HOST_VIEWPORTS.
enum {
    LYRA_IMGUI_DOCKING = 1 << 0,
    LYRA_IMGUI_VIEWPORTS = 1 << 1,
};

#if defined(__APPLE__)
// host_macos.m: lets a borderless (torn-off) window straddle two displays.
extern "C" void lyra_imgui_macos_allow_straddling(void);
#endif

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
    host->dialog_lock = SDL_CreateMutex();
    host->width = (float)width;
    host->height = (float)height;
    return host;
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
