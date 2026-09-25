# Binding modules (`bindings/`)

A binding module is a per-library Lyra module that owns its `extern`s and exports Lyra over
them (Rust's `*-sys` pattern). It needs nothing new from the language. Compiler-side FFI
notes are in [`pkg/backend/FFI.md`](../pkg/backend/FFI.md); language rules are in
[`LANGUAGE.md`](../LANGUAGE.md).

## Conventions for every binding

- **Externs are private; the module exports Lyra.** There is no `pub extern`, so the
  wrapper is where `unsafe` stops, NULL becomes a `Maybe`, and untagged C unions become
  `data` types. An example using a binding should contain no `unsafe` and no `extern`.
- **`@link("lib")` goes on the `module` header**, once. `@link` cannot say *where* — a
  Homebrew install needs e.g. `LIBRARY_PATH=$(pkg-config --variable=libdir sdl3)`.
- **Not `vendor/`**: that name at a Go module root is Go's own and breaks every `go` command.
- **Resolution**: `bindings.sdl3` → `<root>/bindings/sdl3/`. `build.sh` links `bindings`
  beside `std` in `build/`; `go run ./cmd/lyrac` still needs `LYRA_STD`.
- **A C function can fail by succeeding; the wrapper stops that.** Gate on the library's
  validity check (`IsSoundValid`, `IsWaveValid`, …) and answer a `Maybe` — `to_maybe`'s rule
  applied to conventions that are not NULL.
- **An out-parameter becomes a `Maybe`** (`lines_intersect` → `Maybe<Vector2>`); the `^mut`
  never leaves the wrapper.
- **An empty array does nothing rather than trapping** (`xs.data()` traps on empty); array
  predicates answer `false`.
- **Bind one spelling per capability**: the Vector2/Rectangle form, not raylib's loose-`int`
  twins (single deliberate exception: `draw_pixel` beside `draw_pixel_v`).
- **Unloading is the caller's** — Lyra has no destructors. Mark resource structs
  `@must_release(unload_x)` (`lyra-W022`). A function that takes over a resource takes it
  `own` (`model_from_mesh(own Mesh)`), so a later unload is a use-after-move error.
- **An in-place C edit takes `self: mut T`** (the modifier binds to the type, after the
  colon). raylib's `ImageResize` frees the old buffer, so a form returning a new value would
  leave the caller holding a freed one.
- **Mark an extern `pure` only when it is arithmetic on its arguments** (collision tests,
  spline getters). An extern bound is recorded, not checked; anything reading the world or
  returning a pointer into a static buffer (all of `files.lyra`) stays unmarked.
- **`rec` is a reserved word** (a function modifier) — use `rect`.
- Angles are `Degrees` (`bindings/raylib/angle.lyra`), a deliberately **unconstrained**
  newtype over f32: literals convert implicitly, a computed `f32` must say `Degrees(x)` or
  `from_radians(x)` (`lyra-E046`). A range constraint would refuse real angles like 370.
  `turns(fraction)` is the animation form. No operator impls; read out with `f32(d)`.
- **A binding that will not lower is evidence about the compiler**, not the library — a
  whole-module failure over one untouched file is what a by-name lookup in the wrong scope
  looks like (see `CLAUDE.md` rule 9).

## `bindings/sdl3/`

SDL3 (3.4); `@link("SDL3")` on the header. Grown **by example**, towards an NES-style game
(the ladder is in `todo.md`): each example binds only what it draws with.

| file | what, and gotchas |
|---|---|
| `events.lyra` | `SDL_Event` is a Lyra `union` exposed as the `data` type `Event`; `poll_event` reads the tag, then the one member it licenses. `KEY_*` are `SDLK_` codes (function keys carry bit 30). `GamepadAdded`/`GamepadRemoved(id)` — SDL sends `Added` for pads already plugged in at startup, so no enumeration is bound. |
| `keyboard.lyra` | `key_held(SCANCODE_*)`: held state by **position**, not label. Updated as events are pumped — read it after draining the queue. Bounds-checked against SDL's reported length. |
| `gamepad.lyra` | `Gamepad` is `@must_release(close_gamepad)`. `BUTTON_*` are positions (`SOUTH` = Xbox A / ✕ / Nintendo B); `AXIS_LEFT_*` runs -32768..32767, negative up. Needs `INIT_GAMEPAD`. |
| `video.lyra` | `create_window(title, w, h, flags)` with `WINDOW_*` flags; `set_fullscreen` (borderless desktop). |
| `render.lyra` | `Color` (`SDL_Color`) and `rgb`; points, lines, outlined/filled `Rect`s; `debug_text`, SDL's 8×8 ASCII font (`DEBUG_TEXT_SIZE`). `set_logical_presentation(…, PRESENT_INTEGER_SCALE)` is the pixel-art screen: everything, text included, scales by a whole number. `set_vsync` paces the loop. `save_screenshot` (ReadPixels → BMP) must run **before** `present` and saves at window resolution. |
| `texture.lyra` | `Texture` (handle + size) is `@must_release(destroy_texture)`. **Set `set_default_scale_mode(…, SCALE_NEAREST)` before loading** — a texture takes the mode in force when made, and the default blurs pixel art. `draw_texture(src, dst)`, `draw_texture_flipped` (`FLIP_*`). `set_texture_color`/`set_texture_alpha` are the **texture's state**, not the draw's: reset them after a tinted draw. `adopt_texture` is `unsafe` (it keeps a pointer) and is how other bindings hand a texture over. |
| `audio.lyra` | **Push model only**: `open_audio(rate, channels)` → a stream on the default device (`None` with no device — run silently), `put_audio([]f32)`, `queued_frames` to top the queue up to a target each frame. No callback: that would be Lyra running on SDL's audio thread. `AudioStream` is `@must_release(close_audio)`. Needs `init(INIT_AUDIO)`, which may be called after the first `init` — so a failure means "no sound", not "no SDL". `SDL_AUDIO_DRIVER=dummy` exercises it with no speakers. |
| `timer.lyra` | `delay`, `ticks`, and `ticks_ns` — a fixed timestep needs nanoseconds: whole milliseconds drift a 16.67 ms step by a whole step every few seconds. |

**Examples**: `examples/SDL3/basic.lyra` (a bouncing square, bindings only), and in
`examples/SDL3/nes/`:
`screen.lyra` (a 256×240 logical screen: palette, lines, points, text); `sprites.lyra` (a
PNG sheet: flips, tints, fades); `input.lyra` (an on-screen NES controller; `--check` tests
the pad logic headlessly); `sound.lyra` (a four-channel loop, coin and explosion effects
that borrow a channel, meters and an oscilloscope; `--check` tests the APU, `--wav <file>`
renders the song); `tilemap.lyra` (a four-screen level scrolled by a camera, walk/run/jump
with tile collision one axis at a time, coins taken from the map, pits; `--check` tests the
timestep, the physics and that the first pit forgives ordinary timing). Sibling modules:
- `console.lyra` — `open_screen`/`close_screen` (init video+gamepad, window, 256×240
  integer-scaled renderer, vsync, nearest sampling), `end_frame` (the `--shot` logic, then
  present; answers `Continue`/`Stop(code)`), `wants_quit`, and the **fixed 60 Hz timestep**:
  `start_clock`, `updates_due(screen, clock)` (whole 1/60 s steps real time has paid for,
  capped at `MAX_STEPS` so a stall forgives rather than catches up; always 1 under `--shot`),
  and its pure arithmetic `accrue`. Pieces, not a loop: captures
  are by value, so a loop-owning callback could not update the example's `var`s.
- `pad.lyra` — the NES pad: `Buttons` (8 bools), `advance(previous, now) -> Pad` (`held` and
  one-frame `pressed`, pure), `read_buttons(Maybe<Gamepad>)` merging keyboard and pad,
  opposite directions cancelled. Mapping table in its module doc.
- `palette.lyra` — the NES 2C02 palette as `nes(index)`.
- `apu.lyra` — the 2A03's pulse ×2, triangle (32 steps, freezes when silenced) and noise
  (15-bit LFSR, long/short mode, NTSC period table), nesdev's linear mixer and a ~28 Hz
  high-pass. Two clocks: `render` at 44.1 kHz, `tick` per 735 samples (1/60 s) for
  envelopes and music — so tempo follows the audio clock, not the display's refresh.
`examples/SDL3/assets/sprites.png` is committed and made by `assets/generate.py` (standard
library only; reads the palette from `nes/palette.lyra`; 16×16 cells, ≤3 colours each). Cells 9–12 are
background tiles (ground, dirt, cloud, bush), **appended** so earlier indices never move. Examples find
`assets/` from the repo root or, as `../assets/`, from beside themselves in `nes/`. Every graphical example takes **`--shot <file.bmp>`** (and optionally `--at <frame>`):
draw to frame 20 (or `--at`), save it, exit — how an example is checked without anyone
watching (run in the foreground; `sips -s format png` to view).

## `bindings/raylib/`

Needs struct-by-value (`pkg/abi`); nothing in the binding mentions registers.

| file | gotchas |
|---|---|
| `audio.lyra` | `Sound` (40 B) / `Wave` (24 B) return via `sret`. Both `@must_release`. `LoadWaveFromMemory` wants the extension **with a dot** and silently answers an all-zero `Wave` without one; `wave_from_memory` normalises it. |
| `shapes.lyra` | Named colours are `pub const` structs. `DrawRectangleGradientV`/`H` unbound (they are `…Ex` with a repeated colour). |
| `texture.lyra` | `Image`/`Texture2D` are `@must_release`. The image half is headless; a `Texture2D` needs `init_window` (without it raylib answers an invalid texture → `None`). `image_colors` copies into `[]Color` and frees raylib's buffer. `LoadImageFromMemory` has the same dot trap. `ExportImageToMemory` unbound: it answers size 0. `LoadTextureCubemap` waits on 3D. |
| `image.lyra` | Painting is `paint_*`: screen `draw_*` has no receiver, and receiver-keyed overloading needs every declaration to have one. `gen_mipmaps` is a cross-file overload (`mut Image` / `mut Texture2D`). `image_text` answers `Maybe` — with no window the default font's glyph array is NULL and raylib segfaults; `font_valid` predicts it. A convolution kernel is a `[]f32` of perfect-square length, and a kernel not summing to 1 changes alpha too. `dither` answers `bool`: only 5-6-5, 5-5-5-1, 4-4-4-4 exist, and a narrower packing leaves the image in pixel format 0. Loose-int twins and `ImageRotateCW`/`CCW` unbound. |
| `text.lyra` | `Font`, `GlyphInfo`, measuring, `draw_text_ex`/`draw_text_pro` (a `Degrees`). |
| `shapes3d.lyra` | `Camera3D` (44 B), billboards, rays. `Matrix` (16 floats) is not an HFA on aarch64 and crosses in memory. `RayCollision` → `Maybe<RayHit>`; its C `_Bool` is transcribed `u8`. **A negative distance is not a hit**: `GetRayCollisionSphere` is a line test and reports spheres behind the origin; the guard is `>= 0.0` (inside a sphere reports positive, on its surface 0.0). Names mirror 2D (`circles_overlap` → `spheres_overlap`). |
| `files.lyra` | Directory listing, dropped files, metadata, binary I/O, DEFLATE/Base64/hashes (`std.io` stays the text answer). Every `int` return becomes `bool`: 0 is success for `MakeDirectory`/`FileRename`/`FileRemove`, 1 for `FileCopy`. **`FileMove` copies and always returns -1**, so `move_file` is `copy_file` + `remove_file`. `FileTextReplace` always returns 1, so `replace_in_file` answers `find_in_file`'s question. Hashes are hex strings: raylib returns a static word array, **MD5 little-endian, SHA big-endian**. |
| `models.lyra` | **Everything touching the GPU is gated on `window_ready()`** — `GenMeshCube` with no window segfaults. A hidden window (`FLAG_WINDOW_HIDDEN`) gives a GL context for headless checks. A model owns its meshes; meshes/materials are reached by bounds-checked index, never handed out as values. `Animations` is one handle for the whole array. `draw_mesh_instanced` falls back to per-transform `draw_mesh` when the shader lacks an instance-transform attribute (raylib otherwise draws one mesh at the origin); `material_supports_instancing` says which. **`model_valid` answers false for CPU-skinned models** (bone data has no GPU buffer), so `load_model` tests "has meshes" instead. `unload_model` also frees the model's distinct textures (raylib's does not), never the shared 1x1 default; a texture set on a model is taken `own`. A glTF may carry no normals — a null `normals` pointer is the only reliable signal. `LoadMaterials` unbound: its array goes back to `MemFree(^u8)` and Lyra has no pointer reinterpretation. |
| `raymath.lyra` | Rotations, scale, `matrix_multiply` (angles as `Degrees`); `matrix_identity`/`matrix_translation` are Lyra. `matrix_multiply(first, second)` applies `first` then `second`; translation is in `m12`/`m13`/`m14`. |
| `shaders.lyra` | raylib's default shader is unlit. A failed compile is `None` (raylib substitutes its default). `bind_shader_map` gives a sampler a slot (`texture0..2` by name, the rest via `locs[SHADER_LOC_MAP_ALBEDO + map]`, hence `Shader.locs: ^mut i32`). Uniform values are unbound (`const void *`); pass per-material values as a texture. `set_backface_culling`/`set_depth_write`. `Shader` is `@must_release(unload_shader)`; `unload_model` does not free it. |

## `bindings/sdl3_image.lyra`

SDL3_image, `@link("SDL3_image")` — its own module so a program that loads no image does
not link it. `load_texture(renderer, path) -> Maybe<Texture>` answers `bindings.sdl3`'s
`Texture` via `adopt_texture`. `brew install sdl3_image` (Debian `libsdl3-image-dev`);
Homebrew puts it on SDL3's `LIBRARY_PATH`.

## `bindings/jpeg.lyra`

libjpeg-turbo, because Homebrew's raylib is built without JPEG support. `decode_jpeg(bytes)
-> Maybe<Jpeg>` answers RGBA pixels in a Lyra-owned array (baseline and progressive). An
`Image` pointed at that buffer must **never** be `unload_image`d. Link `-lturbojpeg` (`brew
install jpeg-turbo`, Debian `libturbojpeg0-dev`), on `LIBRARY_PATH` beside raylib.

## `bindings/treesitter/`

The tree-sitter runtime (`@link("tree-sitter")`; `brew install tree-sitter`, Debian
`libtree-sitter-dev`) and, in `lyra_grammar.lyra`, the Lyra grammar's own symbol
(`@link("tree-sitter-lyra")`, an archive `examples/lyrafmt/libs.sh` builds from
`tree-sitter-lyra/src`). Enough for a tree walk: `new_parser`/`parse`, `root`, `child`,
`child_count`, `kind`, `start_byte`/`end_byte`, `is_named`, `has_error`. `Parser` and `Tree`
are `@must_release`. **A `TSNode` crosses by value** — four `u32` and two pointers, spelled
as six fields — and is opaque: nothing reads them. C's `bool` results cross as `bool`. The
runtime the grammar's npm tooling vendors is 0.22 and refuses the parser
it generates (language version 15), which is why the system library is required.
`examples/lyrafmt/lyrafmt.lyra` is the use.

## Examples and galleries (`examples/raylib/`)

- **Two programs in one file**: a window, plus a `--check` mode verifying everything a
  machine can (geometry, image pixels, asset sizes, constants). `--check` runs before
  `init_window`, so queries must answer resting values with no window.
- `input.lyra` checks its 154 generated constants by **properties** (ASCII-derived key codes,
  contiguous enum groups, no duplicates), never by transcribing the table twice.
  `gamepad_name` gates on availability and non-empty (raylib returns `""`, not NULL).
- `textures.lyra` draws committed PNGs from `assets/`, generated by `assets/generate.py`;
  `--check` verifies their sizes. It finds `assets/` from the repo root or `examples/raylib/`.
- `painting.lyra` loads nothing from disk. `files.lyra` never opens a window.
- `shapes3d.lyra` stands shapes in a ring (an orbiting camera sees a row edge-on) and draws
  its 3D triangle with both windings (culling).
- `gltf_viewer.lyra` opens any model (args or drag-and-drop), lights it with its own
  metallic-roughness shader (smooth and flat variants), plays node animations and reads
  material fields raylib drops, via sibling module `gltf.lyra` over `std.json`. raylib bakes
  each node's rest transform into mesh vertices, and meshes match nodes in file order, one per
  triangle primitive; raylib's material `i + 1` is the file's `i`. Material floats reach the
  shader as a one-row float texture (`texelFetch` + `uintBitsToFloat`). `--check <path>` runs
  headlessly.
- `breakout.lyra` is the game: synthesised tones, `audio_ready()` gates audio.
- Galleries size labels from a `LABEL` constant (22px; default font ≈ 0.55em/char).
- **Look at a gallery**: copy it to `/tmp`, replace the `should_close` loop with a fixed tick
  count, screenshot with `image_from_screen` + `export_image`, read the PNG. A foreground run
  opens a window; a detached background process cannot. Also compute extents — the two find
  different things.
- There are no direct-extern examples; that proof lives in `TestExec_UnionAgainstSDL3`,
  `TestExec_ByValueAgainstRaylib` (skip when the library is absent) and
  `TestExec_FFIFixture_UnionLayoutMatchesC`. Headless raylib tests:
  `TestExec_RaylibImageBindings`, `TestExec_RaylibImageEditingAndPainting`.
