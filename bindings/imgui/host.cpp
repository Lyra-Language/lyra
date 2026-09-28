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

struct LyraImGuiHost {
    SDL_Window* window;
    SDL_GPUDevice* device;
    bool quit;
};

// Flags for lyra_imgui_host_create; Lyra spells them HOST_DOCKING and HOST_VIEWPORTS.
enum {
    LYRA_IMGUI_DOCKING = 1 << 0,
    LYRA_IMGUI_VIEWPORTS = 1 << 1,
};

extern "C" {

// Open the main window and set ImGui up in it. NULL on failure; SDL_GetError says why.
// Initialises SDL's video and gamepad subsystems, which lyra_imgui_host_destroy quits.
LyraImGuiHost* lyra_imgui_host_create(const char* title, int32_t width, int32_t height, uint32_t flags) {
    if (!SDL_Init(SDL_INIT_VIDEO | SDL_INIT_GAMEPAD))
        return nullptr;

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
    return host;
}

// Pump events and start an ImGui frame. False once the user has asked to quit, and
// then no frame was started. While the main window is minimised this waits rather
// than returning, so every true answer is a frame worth drawing.
bool lyra_imgui_host_begin_frame(LyraImGuiHost* host) {
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

// Tear everything down, SDL included. The host must not be used afterwards.
void lyra_imgui_host_destroy(LyraImGuiHost* host) {
    SDL_WaitForGPUIdle(host->device);
    ImGui_ImplSDL3_Shutdown();
    ImGui_ImplSDLGPU3_Shutdown();
    ImGui::DestroyContext();
    SDL_ReleaseWindowFromGPUDevice(host->device, host->window);
    SDL_DestroyGPUDevice(host->device);
    SDL_DestroyWindow(host->window);
    free(host);
    SDL_Quit();
}

}
