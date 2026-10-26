// `bindings.gpu`'s C half: a renderer for `ui`'s display list over SDL3's GPU API. Lyra hands
// it a frame as instances — one per shape, glyph, shadow or picture, 20 float4s each (layout
// below, and `ui.gpu`'s `instance`) — and a list of commands that draw runs of them and push
// and pop layers; this file owns the device, the pipelines, the textures and the passes.
//
// The shaders are Metal Shading Language, compiled by SDL at start-up (no toolchain at build
// time), so the renderer is macOS's; elsewhere `lyra_gpu_create` answers NULL and `ui.gpu`
// falls back to SDL's 2D renderer. Each fragment computes what it covers from the shape's
// signed distance (per-corner radii, a one-pixel soft edge), so a rounded corner, a border,
// a clip and a shadow are exact at any scale; a shadow is the analytic Gaussian of a rounded
// box (Evan Wallace's erf method). A frame is drawn into a scene texture and blitted to the
// window, so a backdrop can sample what is behind it and a test can read the frame back.

#include <SDL3/SDL.h>
#include <math.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

enum { FLOATS_PER_INSTANCE = 80 };
enum { CMD_DRAW = 1, CMD_PUSH_LAYER = 2, CMD_POP_LAYER = 3, CMD_BACKDROP = 4 };
enum { CMD_WORDS = 6 };
enum { MAX_LAYERS = 16 };

static const char *SHADERS =
    "#include <metal_stdlib>\n"
    "using namespace metal;\n"
    "struct Instance { float4 quad, shape, radii, color, color2, params, uv, extra, clip, clip_radii, at0, at1, stops[8]; };\n"
    "struct Frame { float2 size; float first; float pad; };\n"
    "struct Out { float4 pos [[position]]; float2 p; uint i [[flat]]; };\n"
    "vertex Out quad_vs(uint v [[vertex_id]], uint inst [[instance_id]], constant Frame &f [[buffer(0)]],\n"
    "                   const device Instance *ins [[buffer(1)]]) {\n"
    "  uint i = uint(f.first) + inst;\n"
    "  float4 q = ins[i].quad;\n"
    "  float2 corner = float2((v == 1 || v == 2 || v == 4) ? 1.0 : 0.0, (v == 2 || v == 4 || v == 5) ? 1.0 : 0.0);\n"
    "  float2 p = q.xy + corner * q.zw;\n"
    "  Out o; o.pos = float4(p.x / f.size.x * 2.0 - 1.0, 1.0 - p.y / f.size.y * 2.0, 0.0, 1.0); o.p = p; o.i = i; return o;\n"
    "}\n"
    // Signed distance to a rounded box: `r` is (top-left, top-right, bottom-right, bottom-left).
    "float sd_box(float2 p, float4 box, float4 r) {\n"
    "  float2 h = box.zw * 0.5; float2 c = box.xy + h; float2 d = p - c;\n"
    "  float rad = d.x > 0.0 ? (d.y > 0.0 ? r.z : r.y) : (d.y > 0.0 ? r.w : r.x);\n"
    "  rad = min(rad, min(h.x, h.y));\n"
    "  float2 q = abs(d) - h + rad;\n"
    "  return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - rad;\n"
    "}\n"
    "float cover(float2 p, float4 box, float4 r) { return clamp(0.5 - sd_box(p, box, r), 0.0, 1.0); }\n"
    "float2 erf2(float2 x) { float2 s = sign(x), a = abs(x); x = 1.0 + (0.278393 + (0.230389 + 0.078108 * (a * a)) * a) * a; x *= x; return s - s / (x * x); }\n"
    "float gauss(float x, float s) { return exp(-(x * x) / (2.0 * s * s)) / (2.5066283 * s); }\n"
    "float shadow_x(float x, float y, float s, float corner, float2 h) {\n"
    "  float delta = min(h.y - corner - abs(y), 0.0);\n"
    "  float curved = h.x - corner + sqrt(max(0.0, corner * corner - delta * delta));\n"
    "  float2 integral = 0.5 + 0.5 * erf2((x + float2(-curved, curved)) * (0.70710678 / s));\n"
    "  return integral.y - integral.x;\n"
    "}\n"
    // How much of a rounded box blurred by a Gaussian of deviation `s` covers `p`.
    "float shadow(float2 p, float4 box, float4 r, float s) {\n"
    "  if (s < 0.01) return cover(p, box, r);\n"
    "  float2 h = box.zw * 0.5; float2 d = p - (box.xy + h);\n"
    "  float corner = d.x > 0.0 ? (d.y > 0.0 ? r.z : r.y) : (d.y > 0.0 ? r.w : r.x);\n"
    "  corner = min(corner, min(h.x, h.y));\n"
    "  float lo = d.y - h.y, hi = d.y + h.y;\n"
    "  float start = clamp(-3.0 * s, lo, hi), end = clamp(3.0 * s, lo, hi);\n"
    "  float step = (end - start) / 4.0; float y = start + step * 0.5; float v = 0.0;\n"
    "  for (int k = 0; k < 4; k++) { v += shadow_x(d.x, d.y - y, s, corner, h) * gauss(y, s) * step; y += step; }\n"
    "  return v;\n"
    "}\n"
    "float4 gradient(const device Instance &n, float2 p) {\n"
    "  float4 box = n.shape; float2 c = box.xy + box.zw * 0.5; float t;\n"
    "  if (n.extra.x < 0.5) {\n"
    "    float a = n.extra.y; float2 dir = float2(sin(a), -cos(a));\n"
    "    float len = abs(box.z * dir.x) + abs(box.w * dir.y);\n"
    "    t = len <= 0.0 ? 0.0 : dot(p - c, dir) / len + 0.5;\n"
    "  } else {\n"
    "    float2 h = max(box.zw * 0.5, float2(0.0001)); float2 u = p - c;\n"
    "    t = n.extra.y > 0.5 ? length(u) / length(h) : length(u / (h * 1.4142135));\n"
    "  }\n"
    "  int count = int(n.extra.z);\n"
    "  float at[8] = { n.at0.x, n.at0.y, n.at0.z, n.at0.w, n.at1.x, n.at1.y, n.at1.z, n.at1.w };\n"
    "  if (t <= at[0]) return n.stops[0];\n"
    "  for (int k = 1; k < count; k++) {\n"
    "    if (t <= at[k]) {\n"
    "      float span = at[k] - at[k - 1]; float f = span <= 0.0 ? 1.0 : (t - at[k - 1]) / span;\n"
    "      float4 a0 = float4(n.stops[k - 1].rgb * n.stops[k - 1].a, n.stops[k - 1].a);\n"
    "      float4 a1 = float4(n.stops[k].rgb * n.stops[k].a, n.stops[k].a);\n"
    "      float4 m = mix(a0, a1, f); return m.a > 0.0 ? float4(m.rgb / m.a, m.a) : float4(0.0);\n"
    "    }\n"
    "  }\n"
    "  return n.stops[count - 1];\n"
    "}\n"
    "fragment float4 quad_fs(Out in [[stage_in]], const device Instance *ins [[buffer(0)]],\n"
    "                        texture2d<float> tex [[texture(0)]], sampler smp [[sampler(0)]]) {\n"
    "  const device Instance &n = ins[in.i];\n"
    "  float2 p = in.p; int kind = int(n.params.x); float4 c; float a;\n"
    "  float clip = n.clip.z > 0.0 ? cover(p, n.clip, n.clip_radii) : 1.0;\n"
    "  if (kind == 0) { c = n.color; a = cover(p, n.shape, n.radii); }\n"
    "  else if (kind == 1) {\n"
    "    float2 f = (p - n.quad.xy) / n.quad.zw; float2 uv = mix(n.uv.xy, n.uv.zw, f);\n"
    "    float4 t = tex.sample(smp, uv); c = t * n.color; a = 1.0;\n"
    "  } else if (kind == 2) {\n"
    "    float w = n.params.y; float4 inner = float4(n.shape.xy + w, n.shape.zw - 2.0 * w);\n"
    "    c = n.color; a = cover(p, n.shape, n.radii) - (inner.z > 0.0 && inner.w > 0.0 ? cover(p, inner, max(n.radii - w, 0.0)) : 0.0);\n"
    "  } else if (kind == 3) {\n"
    "    c = n.color; a = shadow(p, n.shape, n.radii, n.params.z) * (1.0 - cover(p, n.uv, n.extra));\n"
    "  } else if (kind == 4) {\n"
    "    c = n.color; a = cover(p, n.uv, n.extra) * (1.0 - shadow(p, n.shape, n.radii, n.params.z));\n"
    "  } else if (kind == 5) { c = gradient(n, p); a = cover(p, n.shape, n.radii); }\n"
    "  else if (kind == 6) {\n"
    "    float2 f = (p - n.quad.xy) / n.quad.zw; float2 uv = mix(n.uv.xy, n.uv.zw, f);\n"
    "    float4 t = tex.sample(smp, uv); return t * clip;\n"
    "  } else {\n"
    "    float2 uv = p / n.extra.zw; float4 t = tex.sample(smp, uv);\n"
    "    return t * (cover(p, n.shape, n.radii) * clip);\n"
    "  }\n"
    "  a *= c.a * clip; return float4(c.rgb * a, a);\n"
    "}\n"
    "struct Blur { float2 dir; float sigma; float pad; };\n"
    "struct FullOut { float4 pos [[position]]; float2 uv; };\n"
    "vertex FullOut full_vs(uint v [[vertex_id]]) {\n"
    "  float2 c = float2((v == 1 || v == 2 || v == 4) ? 1.0 : 0.0, (v == 2 || v == 4 || v == 5) ? 1.0 : 0.0);\n"
    "  FullOut o; o.pos = float4(c.x * 2.0 - 1.0, 1.0 - c.y * 2.0, 0.0, 1.0); o.uv = c; return o;\n"
    "}\n"
    // One direction of a Gaussian, deviation `sigma` texels, taps a texel apart.
    "fragment float4 blur_fs(FullOut in [[stage_in]], constant Blur &b [[buffer(0)]],\n"
    "                        texture2d<float> tex [[texture(0)]], sampler smp [[sampler(0)]]) {\n"
    "  float2 texel = 1.0 / float2(tex.get_width(), tex.get_height());\n"
    "  if (b.sigma < 0.01) return tex.sample(smp, in.uv);\n"
    "  int r = min(int(ceil(3.0 * b.sigma)), 48); float4 sum = float4(0.0); float total = 0.0;\n"
    "  for (int k = -r; k <= r; k++) {\n"
    "    float w = exp(-float(k * k) / (2.0 * b.sigma * b.sigma));\n"
    "    sum += tex.sample(smp, in.uv + b.dir * texel * float(k)) * w; total += w;\n"
    "  }\n"
    "  return sum / total;\n"
    "}\n";

struct Texture {
    SDL_GPUTexture *texture;
    SDL_GPUSampler *sampler;
    int width, height;
};

typedef struct LyraGpu {
    SDL_GPUDevice *device;
    SDL_Window *window;
    SDL_GPUGraphicsPipeline *quads;
    SDL_GPUGraphicsPipeline *blur;
    SDL_GPUSampler *linear, *nearest, *clamped;
    SDL_GPUTextureFormat format;
    struct Texture *textures;
    int64_t texture_count, texture_cap;
    SDL_GPUTexture *white;
    SDL_GPUBuffer *instances;
    uint32_t instance_bytes;
    SDL_GPUTransferBuffer *staging;
    uint32_t staging_bytes;
    // The scene, the layers being drawn, and the half-size textures blurring happens in.
    SDL_GPUTexture *scene;
    SDL_GPUTexture *layers[MAX_LAYERS];
    SDL_GPUTexture *half_a, *half_b;
    int width, height;
    // The last frame, read back on request.
    bool keep;
    uint32_t *kept;
    int kept_w, kept_h;
} LyraGpu;

static SDL_GPUTexture *make_target(LyraGpu *g, int w, int h) {
    SDL_GPUTextureCreateInfo info = {
        .type = SDL_GPU_TEXTURETYPE_2D,
        .format = g->format,
        .usage = SDL_GPU_TEXTUREUSAGE_SAMPLER | SDL_GPU_TEXTUREUSAGE_COLOR_TARGET,
        .width = (Uint32)(w > 0 ? w : 1),
        .height = (Uint32)(h > 0 ? h : 1),
        .layer_count_or_depth = 1,
        .num_levels = 1,
    };
    return SDL_CreateGPUTexture(g->device, &info);
}

static SDL_GPUShader *shader(LyraGpu *g, const char *entry, SDL_GPUShaderStage stage, Uint32 samplers,
                             Uint32 storage, Uint32 uniforms) {
    SDL_GPUShaderCreateInfo info = {
        .code_size = strlen(SHADERS),
        .code = (const Uint8 *)SHADERS,
        .entrypoint = entry,
        .format = SDL_GPU_SHADERFORMAT_MSL,
        .stage = stage,
        .num_samplers = samplers,
        .num_storage_buffers = storage,
        .num_uniform_buffers = uniforms,
    };
    return SDL_CreateGPUShader(g->device, &info);
}

static SDL_GPUGraphicsPipeline *pipeline(LyraGpu *g, SDL_GPUShader *vs, SDL_GPUShader *fs, bool blend) {
    SDL_GPUColorTargetDescription target = {
        .format = g->format,
        .blend_state = {
            .enable_blend = blend,
            .src_color_blendfactor = SDL_GPU_BLENDFACTOR_ONE,
            .dst_color_blendfactor = SDL_GPU_BLENDFACTOR_ONE_MINUS_SRC_ALPHA,
            .color_blend_op = SDL_GPU_BLENDOP_ADD,
            .src_alpha_blendfactor = SDL_GPU_BLENDFACTOR_ONE,
            .dst_alpha_blendfactor = SDL_GPU_BLENDFACTOR_ONE_MINUS_SRC_ALPHA,
            .alpha_blend_op = SDL_GPU_BLENDOP_ADD,
        },
    };
    SDL_GPUGraphicsPipelineCreateInfo info = {
        .vertex_shader = vs,
        .fragment_shader = fs,
        .primitive_type = SDL_GPU_PRIMITIVETYPE_TRIANGLELIST,
        .target_info = { .color_target_descriptions = &target, .num_color_targets = 1 },
    };
    return SDL_CreateGPUGraphicsPipeline(g->device, &info);
}

static SDL_GPUSampler *sampler(LyraGpu *g, SDL_GPUFilter filter) {
    SDL_GPUSamplerCreateInfo info = {
        .min_filter = filter,
        .mag_filter = filter,
        .mipmap_mode = SDL_GPU_SAMPLERMIPMAPMODE_NEAREST,
        .address_mode_u = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE,
        .address_mode_v = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE,
        .address_mode_w = SDL_GPU_SAMPLERADDRESSMODE_CLAMP_TO_EDGE,
    };
    return SDL_CreateGPUSampler(g->device, &info);
}

// Copy `bytes` into `texture`'s region through the staging buffer, in a copy pass of `cmd`.
static bool upload_region(LyraGpu *g, SDL_GPUCommandBuffer *cmd, SDL_GPUTexture *texture, int x, int y,
                          int w, int h, const uint8_t *bytes) {
    Uint32 size = (Uint32)(w * h * 4);
    SDL_GPUTransferBufferCreateInfo tinfo = { .usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD, .size = size };
    SDL_GPUTransferBuffer *tb = SDL_CreateGPUTransferBuffer(g->device, &tinfo);
    if (!tb) return false;
    void *map = SDL_MapGPUTransferBuffer(g->device, tb, false);
    memcpy(map, bytes, size);
    SDL_UnmapGPUTransferBuffer(g->device, tb);
    SDL_GPUCopyPass *copy = SDL_BeginGPUCopyPass(cmd);
    SDL_GPUTextureTransferInfo src = { .transfer_buffer = tb, .pixels_per_row = (Uint32)w };
    SDL_GPUTextureRegion dst = { .texture = texture, .x = (Uint32)x, .y = (Uint32)y, .w = (Uint32)w, .h = (Uint32)h, .d = 1 };
    SDL_UploadToGPUTexture(copy, &src, &dst, false);
    SDL_EndGPUCopyPass(copy);
    SDL_ReleaseGPUTransferBuffer(g->device, tb);
    return true;
}

// `argb` (0xAARRGGBB, `w` × `h`) as RGBA bytes.
static uint8_t *rgba_of(const uint32_t *argb, int64_t count) {
    uint8_t *out = SDL_malloc((size_t)count * 4);
    for (int64_t i = 0; i < count; i++) {
        uint32_t p = argb[i];
        out[i * 4 + 0] = (uint8_t)(p >> 16);
        out[i * 4 + 1] = (uint8_t)(p >> 8);
        out[i * 4 + 2] = (uint8_t)p;
        out[i * 4 + 3] = (uint8_t)(p >> 24);
    }
    return out;
}

// window.m's: the window's Metal layers keep a frame at their top-left while a resize waits.
void lyra_gpu_pin_contents(SDL_Window *window);

LyraGpu *lyra_gpu_create(SDL_Window *window) {
    if (getenv("LYRA_UI_RENDERER") && strcmp(getenv("LYRA_UI_RENDERER"), "sdl") == 0) return NULL;
    SDL_GPUDevice *device = SDL_CreateGPUDevice(SDL_GPU_SHADERFORMAT_MSL, false, NULL);
    if (!device) return NULL;
    if (!SDL_ClaimWindowForGPUDevice(device, window)) {
        SDL_DestroyGPUDevice(device);
        return NULL;
    }
    // The claim makes the window's Metal layer, after the platform pinned what it had: pinned
    // again, or a resize shows the last frame stretched to the window's new size until the next
    // is drawn (window.m).
    lyra_gpu_pin_contents(window);
    LyraGpu *g = SDL_calloc(1, sizeof *g);
    g->device = device;
    g->window = window;
    g->format = SDL_GetGPUSwapchainTextureFormat(device, window);
    // LYRA_UI_NOVSYNC presents as soon as a frame is drawn — for measuring what a frame costs,
    // not for showing one.
    SDL_GPUPresentMode mode = SDL_GPU_PRESENTMODE_VSYNC;
    if (getenv("LYRA_UI_NOVSYNC")) {
        if (SDL_WindowSupportsGPUPresentMode(device, window, SDL_GPU_PRESENTMODE_IMMEDIATE)) mode = SDL_GPU_PRESENTMODE_IMMEDIATE;
        else if (SDL_WindowSupportsGPUPresentMode(device, window, SDL_GPU_PRESENTMODE_MAILBOX)) mode = SDL_GPU_PRESENTMODE_MAILBOX;
    }
    SDL_SetGPUSwapchainParameters(device, window, SDL_GPU_SWAPCHAINCOMPOSITION_SDR, mode);
    SDL_GPUShader *qv = shader(g, "quad_vs", SDL_GPU_SHADERSTAGE_VERTEX, 0, 1, 1);
    SDL_GPUShader *qf = shader(g, "quad_fs", SDL_GPU_SHADERSTAGE_FRAGMENT, 1, 1, 0);
    SDL_GPUShader *bv = shader(g, "full_vs", SDL_GPU_SHADERSTAGE_VERTEX, 0, 0, 0);
    SDL_GPUShader *bf = shader(g, "blur_fs", SDL_GPU_SHADERSTAGE_FRAGMENT, 1, 0, 1);
    if (qv && qf) g->quads = pipeline(g, qv, qf, true);
    if (bv && bf) g->blur = pipeline(g, bv, bf, false);
    if (qv) SDL_ReleaseGPUShader(device, qv);
    if (qf) SDL_ReleaseGPUShader(device, qf);
    if (bv) SDL_ReleaseGPUShader(device, bv);
    if (bf) SDL_ReleaseGPUShader(device, bf);
    if (!g->quads || !g->blur) {
        SDL_Log("ui.gpu: %s", SDL_GetError());
        SDL_ReleaseWindowFromGPUDevice(device, window);
        SDL_DestroyGPUDevice(device);
        SDL_free(g);
        return NULL;
    }
    g->linear = sampler(g, SDL_GPU_FILTER_LINEAR);
    g->nearest = sampler(g, SDL_GPU_FILTER_NEAREST);
    // A white texel, for the instances that sample nothing.
    SDL_GPUTextureCreateInfo info = {
        .type = SDL_GPU_TEXTURETYPE_2D,
        .format = SDL_GPU_TEXTUREFORMAT_R8G8B8A8_UNORM,
        .usage = SDL_GPU_TEXTUREUSAGE_SAMPLER,
        .width = 1, .height = 1, .layer_count_or_depth = 1, .num_levels = 1,
    };
    g->white = SDL_CreateGPUTexture(device, &info);
    uint8_t white[4] = { 255, 255, 255, 255 };
    SDL_GPUCommandBuffer *cmd = SDL_AcquireGPUCommandBuffer(device);
    upload_region(g, cmd, g->white, 0, 0, 1, 1, white);
    SDL_SubmitGPUCommandBuffer(cmd);
    return g;
}

static void release_targets(LyraGpu *g) {
    if (g->scene) SDL_ReleaseGPUTexture(g->device, g->scene);
    if (g->half_a) SDL_ReleaseGPUTexture(g->device, g->half_a);
    if (g->half_b) SDL_ReleaseGPUTexture(g->device, g->half_b);
    for (int i = 0; i < MAX_LAYERS; i++)
        if (g->layers[i]) SDL_ReleaseGPUTexture(g->device, g->layers[i]);
    g->scene = g->half_a = g->half_b = NULL;
    memset(g->layers, 0, sizeof g->layers);
}

void lyra_gpu_destroy(LyraGpu *g) {
    if (!g) return;
    SDL_WaitForGPUIdle(g->device);
    release_targets(g);
    for (int64_t i = 0; i < g->texture_count; i++) {
        if (g->textures[i].texture) SDL_ReleaseGPUTexture(g->device, g->textures[i].texture);
    }
    SDL_free(g->textures);
    if (g->white) SDL_ReleaseGPUTexture(g->device, g->white);
    if (g->instances) SDL_ReleaseGPUBuffer(g->device, g->instances);
    if (g->staging) SDL_ReleaseGPUTransferBuffer(g->device, g->staging);
    SDL_ReleaseGPUSampler(g->device, g->linear);
    SDL_ReleaseGPUSampler(g->device, g->nearest);
    SDL_ReleaseGPUGraphicsPipeline(g->device, g->quads);
    SDL_ReleaseGPUGraphicsPipeline(g->device, g->blur);
    SDL_ReleaseWindowFromGPUDevice(g->device, g->window);
    SDL_DestroyGPUDevice(g->device);
    SDL_free(g->kept);
    SDL_free(g);
}

int64_t lyra_gpu_upload(LyraGpu *g, int64_t w, int64_t h, const uint32_t *argb, bool pixelated) {
    SDL_GPUTextureCreateInfo info = {
        .type = SDL_GPU_TEXTURETYPE_2D,
        .format = SDL_GPU_TEXTUREFORMAT_R8G8B8A8_UNORM,
        .usage = SDL_GPU_TEXTUREUSAGE_SAMPLER,
        .width = (Uint32)w, .height = (Uint32)h, .layer_count_or_depth = 1, .num_levels = 1,
    };
    SDL_GPUTexture *t = SDL_CreateGPUTexture(g->device, &info);
    if (!t) return -1;
    uint8_t *bytes = rgba_of(argb, w * h);
    SDL_GPUCommandBuffer *cmd = SDL_AcquireGPUCommandBuffer(g->device);
    upload_region(g, cmd, t, 0, 0, (int)w, (int)h, bytes);
    SDL_SubmitGPUCommandBuffer(cmd);
    SDL_free(bytes);
    struct Texture made = { t, pixelated ? g->nearest : g->linear, (int)w, (int)h };
    // A released texture's place first, so a picture changed often does not grow the table.
    for (int64_t i = 0; i < g->texture_count; i++) {
        if (!g->textures[i].texture) {
            g->textures[i] = made;
            return i;
        }
    }
    if (g->texture_count == g->texture_cap) {
        g->texture_cap = g->texture_cap ? g->texture_cap * 2 : 8;
        g->textures = SDL_realloc(g->textures, (size_t)g->texture_cap * sizeof *g->textures);
    }
    g->textures[g->texture_count] = made;
    return g->texture_count++;
}

// Texture `id` let go, its place free for the next upload; false where it is none. The GPU
// keeps it until the frames drawing from it are done (SDL's deferred release).
bool lyra_gpu_release(LyraGpu *g, int64_t id) {
    if (id < 0 || id >= g->texture_count || !g->textures[id].texture) return false;
    SDL_ReleaseGPUTexture(g->device, g->textures[id].texture);
    g->textures[id] = (struct Texture){ 0 };
    return true;
}

bool lyra_gpu_update(LyraGpu *g, int64_t id, int64_t x, int64_t y, int64_t w, int64_t h, const uint32_t *argb) {
    if (id < 0 || id >= g->texture_count || !g->textures[id].texture || w <= 0 || h <= 0) return false;
    uint8_t *bytes = rgba_of(argb, w * h);
    SDL_GPUCommandBuffer *cmd = SDL_AcquireGPUCommandBuffer(g->device);
    bool ok = upload_region(g, cmd, g->textures[id].texture, (int)x, (int)y, (int)w, (int)h, bytes);
    SDL_SubmitGPUCommandBuffer(cmd);
    SDL_free(bytes);
    return ok;
}

// A texture's size in texels, for the uv of a quad drawn from it.
bool lyra_gpu_texture_size(LyraGpu *g, int64_t id, int64_t *w, int64_t *h) {
    if (id < 0 || id >= g->texture_count || !g->textures[id].texture) return false;
    *w = g->textures[id].width;
    *h = g->textures[id].height;
    return true;
}

static SDL_GPURenderPass *begin(SDL_GPUCommandBuffer *cmd, SDL_GPUTexture *target, bool clear, SDL_FColor color) {
    SDL_GPUColorTargetInfo info = {
        .texture = target,
        .clear_color = color,
        .load_op = clear ? SDL_GPU_LOADOP_CLEAR : SDL_GPU_LOADOP_LOAD,
        .store_op = SDL_GPU_STOREOP_STORE,
    };
    return SDL_BeginGPURenderPass(cmd, &info, 1, NULL);
}

// One pass of the blur: `from` drawn into `to`, blurred along `dir` by `sigma` texels.
static void blur_pass(LyraGpu *g, SDL_GPUCommandBuffer *cmd, SDL_GPUTexture *from, SDL_GPUTexture *to,
                      float dx, float dy, float sigma) {
    SDL_GPURenderPass *pass = begin(cmd, to, true, (SDL_FColor){ 0, 0, 0, 0 });
    SDL_BindGPUGraphicsPipeline(pass, g->blur);
    SDL_GPUTextureSamplerBinding b = { .texture = from, .sampler = g->linear };
    SDL_BindGPUFragmentSamplers(pass, 0, &b, 1);
    float u[4] = { dx, dy, sigma, 0 };
    SDL_PushGPUFragmentUniformData(cmd, 0, u, sizeof u);
    SDL_DrawGPUPrimitives(pass, 6, 1, 0, 0);
    SDL_EndGPURenderPass(pass);
}

// `source` blurred by `radius` pixels (CSS's: deviation half of it) into `half_a`, at half size.
static void blurred(LyraGpu *g, SDL_GPUCommandBuffer *cmd, SDL_GPUTexture *source, float radius) {
    float sigma = radius / 2.0f / 2.0f;
    blur_pass(g, cmd, source, g->half_a, 0, 0, 0);
    blur_pass(g, cmd, g->half_a, g->half_b, 1, 0, sigma);
    blur_pass(g, cmd, g->half_b, g->half_a, 0, 1, sigma);
}

static void ensure_targets(LyraGpu *g, int w, int h) {
    if (g->scene && g->width == w && g->height == h) return;
    release_targets(g);
    g->width = w;
    g->height = h;
    g->scene = make_target(g, w, h);
    g->half_a = make_target(g, (w + 1) / 2, (h + 1) / 2);
    g->half_b = make_target(g, (w + 1) / 2, (h + 1) / 2);
}

static bool ensure_buffers(LyraGpu *g, Uint32 bytes) {
    if (bytes == 0) bytes = 16;
    if (g->instance_bytes < bytes) {
        if (g->instances) SDL_ReleaseGPUBuffer(g->device, g->instances);
        Uint32 size = bytes * 2;
        SDL_GPUBufferCreateInfo binfo = { .usage = SDL_GPU_BUFFERUSAGE_GRAPHICS_STORAGE_READ, .size = size };
        g->instances = SDL_CreateGPUBuffer(g->device, &binfo);
        g->instance_bytes = g->instances ? size : 0;
    }
    if (g->staging_bytes < bytes) {
        if (g->staging) SDL_ReleaseGPUTransferBuffer(g->device, g->staging);
        Uint32 size = bytes * 2;
        SDL_GPUTransferBufferCreateInfo tinfo = { .usage = SDL_GPU_TRANSFERBUFFERUSAGE_UPLOAD, .size = size };
        g->staging = SDL_CreateGPUTransferBuffer(g->device, &tinfo);
        g->staging_bytes = g->staging ? size : 0;
    }
    return g->instances && g->staging;
}

// Draw a frame: clear to the background, run `commands` over `instances`, show it — and, if
// a read was asked for, keep its pixels. False if no frame could be drawn (a hidden window).
bool lyra_gpu_frame(LyraGpu *g, float r, float gr, float b, float a, const float *instances,
                    int64_t instance_count, const int64_t *commands, int64_t command_count) {
    SDL_GPUCommandBuffer *cmd = SDL_AcquireGPUCommandBuffer(g->device);
    if (!cmd) return false;
    SDL_GPUTexture *swapchain = NULL;
    Uint32 sw = 0, sh = 0;
    if (!SDL_WaitAndAcquireGPUSwapchainTexture(cmd, g->window, &swapchain, &sw, &sh) || !swapchain) {
        SDL_CancelGPUCommandBuffer(cmd);
        return false;
    }
    ensure_targets(g, (int)sw, (int)sh);
    Uint32 bytes = (Uint32)(instance_count * FLOATS_PER_INSTANCE * sizeof(float));
    if (!ensure_buffers(g, bytes)) {
        SDL_CancelGPUCommandBuffer(cmd);
        return false;
    }
    if (bytes > 0) {
        void *map = SDL_MapGPUTransferBuffer(g->device, g->staging, true);
        memcpy(map, instances, bytes);
        SDL_UnmapGPUTransferBuffer(g->device, g->staging);
        SDL_GPUCopyPass *copy = SDL_BeginGPUCopyPass(cmd);
        SDL_GPUTransferBufferLocation src = { .transfer_buffer = g->staging };
        SDL_GPUBufferRegion dst = { .buffer = g->instances, .size = bytes };
        SDL_UploadToGPUBuffer(copy, &src, &dst, true);
        SDL_EndGPUCopyPass(copy);
    }
    SDL_GPUTexture *stack[MAX_LAYERS + 1];
    int depth = 0;
    stack[0] = g->scene;
    SDL_FColor clear = { r, gr, b, a };
    SDL_GPURenderPass *pass = begin(cmd, g->scene, true, clear);
    float frame[4] = { (float)sw, (float)sh, 0, 0 };
    for (int64_t k = 0; k < command_count; k++) {
        const int64_t *c = commands + k * CMD_WORDS;
        if (c[0] == CMD_DRAW || c[0] == CMD_BACKDROP) {
            SDL_GPUTexture *tex = g->white;
            SDL_GPUSampler *smp = g->linear;
            if (c[0] == CMD_BACKDROP) {
                // What is drawn so far, blurred, under the instance's shape.
                SDL_EndGPURenderPass(pass);
                blurred(g, cmd, stack[depth], (float)c[2] / 100.0f);
                pass = begin(cmd, stack[depth], false, clear);
                tex = g->half_a;
            } else if (c[1] >= 0 && c[1] < g->texture_count) {
                tex = g->textures[c[1]].texture;
                smp = g->textures[c[1]].sampler;
            }
            SDL_BindGPUGraphicsPipeline(pass, g->quads);
            SDL_BindGPUVertexStorageBuffers(pass, 0, &g->instances, 1);
            SDL_BindGPUFragmentStorageBuffers(pass, 0, &g->instances, 1);
            SDL_GPUTextureSamplerBinding binding = { .texture = tex, .sampler = smp };
            SDL_BindGPUFragmentSamplers(pass, 0, &binding, 1);
            int64_t first = c[0] == CMD_BACKDROP ? c[1] : c[2];
            int64_t count = c[0] == CMD_BACKDROP ? 1 : c[3];
            frame[2] = (float)first;
            SDL_PushGPUVertexUniformData(cmd, 0, frame, sizeof frame);
            SDL_DrawGPUPrimitives(pass, 6, (Uint32)count, 0, 0);
        } else if (c[0] == CMD_PUSH_LAYER && depth < MAX_LAYERS) {
            SDL_EndGPURenderPass(pass);
            depth++;
            if (!g->layers[depth - 1]) g->layers[depth - 1] = make_target(g, (int)sw, (int)sh);
            stack[depth] = g->layers[depth - 1];
            pass = begin(cmd, stack[depth], true, (SDL_FColor){ 0, 0, 0, 0 });
        } else if (c[0] == CMD_POP_LAYER && depth > 0) {
            // The layer, blurred, put down with the instance that follows in the commands'
            // operand: a quad sampling it (kind 6) over its rectangle.
            SDL_EndGPURenderPass(pass);
            blurred(g, cmd, stack[depth], (float)c[2] / 100.0f);
            depth--;
            pass = begin(cmd, stack[depth], false, clear);
            SDL_BindGPUGraphicsPipeline(pass, g->quads);
            SDL_BindGPUVertexStorageBuffers(pass, 0, &g->instances, 1);
            SDL_BindGPUFragmentStorageBuffers(pass, 0, &g->instances, 1);
            SDL_GPUTextureSamplerBinding binding = { .texture = g->half_a, .sampler = g->linear };
            SDL_BindGPUFragmentSamplers(pass, 0, &binding, 1);
            frame[2] = (float)c[1];
            SDL_PushGPUVertexUniformData(cmd, 0, frame, sizeof frame);
            SDL_DrawGPUPrimitives(pass, 6, 1, 0, 0);
        }
    }
    SDL_EndGPURenderPass(pass);
    SDL_GPUBlitInfo blit = {
        .source = { .texture = g->scene, .w = sw, .h = sh },
        .destination = { .texture = swapchain, .w = sw, .h = sh },
        .load_op = SDL_GPU_LOADOP_DONT_CARE,
        .filter = SDL_GPU_FILTER_NEAREST,
    };
    SDL_BlitGPUTexture(cmd, &blit);
    if (!g->keep) return SDL_SubmitGPUCommandBuffer(cmd);
    // Read the scene back.
    Uint32 size = sw * sh * 4;
    SDL_GPUTransferBufferCreateInfo tinfo = { .usage = SDL_GPU_TRANSFERBUFFERUSAGE_DOWNLOAD, .size = size };
    SDL_GPUTransferBuffer *down = SDL_CreateGPUTransferBuffer(g->device, &tinfo);
    SDL_GPUCopyPass *copy = SDL_BeginGPUCopyPass(cmd);
    SDL_GPUTextureRegion src = { .texture = g->scene, .w = sw, .h = sh, .d = 1 };
    SDL_GPUTextureTransferInfo dst = { .transfer_buffer = down, .pixels_per_row = sw };
    SDL_DownloadFromGPUTexture(copy, &src, &dst);
    SDL_EndGPUCopyPass(copy);
    SDL_GPUFence *fence = SDL_SubmitGPUCommandBufferAndAcquireFence(cmd);
    if (fence) {
        SDL_WaitForGPUFences(g->device, true, &fence, 1);
        SDL_ReleaseGPUFence(g->device, fence);
    }
    const uint8_t *px = SDL_MapGPUTransferBuffer(g->device, down, false);
    SDL_free(g->kept);
    g->kept = SDL_malloc(size);
    bool bgra = g->format == SDL_GPU_TEXTUREFORMAT_B8G8R8A8_UNORM || g->format == SDL_GPU_TEXTUREFORMAT_B8G8R8A8_UNORM_SRGB;
    for (Uint32 i = 0; i < sw * sh; i++) {
        uint8_t c0 = px[i * 4], c1 = px[i * 4 + 1], c2 = px[i * 4 + 2], c3 = px[i * 4 + 3];
        uint8_t rr = bgra ? c2 : c0, bb = bgra ? c0 : c2;
        g->kept[i] = ((uint32_t)c3 << 24) | ((uint32_t)rr << 16) | ((uint32_t)c1 << 8) | bb;
    }
    SDL_UnmapGPUTransferBuffer(g->device, down);
    SDL_ReleaseGPUTransferBuffer(g->device, down);
    g->kept_w = (int)sw;
    g->kept_h = (int)sh;
    return true;
}

// Keep the next frames' pixels for `lyra_gpu_read`, or stop.
void lyra_gpu_keep_frames(LyraGpu *g, bool keep) { g->keep = keep; }

// The last frame kept, 0xAARRGGBB, into `out` (`cap` pixels); its size, or false for none.
bool lyra_gpu_read(LyraGpu *g, uint32_t *out, int64_t cap, int64_t *w, int64_t *h) {
    if (!g->kept) return false;
    *w = g->kept_w;
    *h = g->kept_h;
    int64_t n = (int64_t)g->kept_w * g->kept_h;
    if (n > cap) n = cap;
    memcpy(out, g->kept, (size_t)n * 4);
    return true;
}
