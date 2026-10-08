# Binding modules (`bindings/`)

A binding module is a per-library Lyra module that owns its `extern`s and exports Lyra over
them (Rust's `*-sys` pattern). It needs nothing new from the language. Compiler-side FFI
notes are in [`pkg/backend/FFI.md`](../pkg/backend/FFI.md); language rules are in
[`LANGUAGE.md`](../LANGUAGE.md).

## Conventions for every binding

- **Externs are private; the module exports Lyra.** There is no `pub extern`, so the
  wrapper is where `unsafe` stops, NULL becomes a `Maybe`, and untagged C unions become
  `data` types. An example using a binding should contain no `unsafe` and no `extern`.
- **`@link("lib", pkg: "package")` goes on the `module` header**, once. `pkg:` names the
  pkg-config package, which `lyrac` asks where the library lives — so a Homebrew install
  links with no `LIBRARY_PATH`. Give every binding for a packaged library one.
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
- **An in-place C edit takes `self: mut`**, in the type's `impl` block (the modifier binds
  to the type, after the colon, so it is `self: mut T` outside one). raylib's `ImageResize`
  frees the old buffer, so a form returning a new value would leave the caller holding a
  freed one.
- **A type's methods go in its `impl` block**, `impl Image { … }`; the imgui generator
  writes each handle's wrappers into one.
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
| `events.lyra` | `SDL_Event` is a Lyra `union` exposed as the `data` type `Event`; `poll_event` reads the tag, then the one member it licenses. `MouseWheel(x, y)` has SDL's "flipped" (natural-scrolling) direction undone. `GamepadAdded`/`GamepadRemoved(id)` — SDL sends `Added` for pads already plugged in at startup, so no enumeration is bound. |
| `keys.lyra` | **Generated** by `gen_keys.py` from the installed SDL headers — every `SCANCODE_*` (position, 247), `KEY_*` (key code, 256) and `MOD_*` (modifier mask, 18), plus `SCANCODES`/`KEY_CODES` arrays. Do not edit; rerun on an SDL upgrade and review the diff. Checked by properties and by asking SDL to name every code (`editor.lyra --check`). |
| `keyboard.lyra` | `key_held(SCANCODE_*)`: held state by **position**, not label. Updated as events are pumped — read it after draining the queue. Bounds-checked against SDL's reported length. `modifiers()` (a `MOD_*` mask; `MOD_GUI` is Command), `key_name`/`scancode_name` (SDL's human names). |
| `mouse.lyra` | `mouse_state()` (window position + button mask) and `button_held(state, BUTTON_*)`; `show_cursor`, `system_cursor(CURSOR_*)` → `Cursor` (`@must_release(destroy_cursor)`), `set_cursor`. **Positions are window pixels** — convert with `render.window_to_render` on a logical screen. |
| `gamepad.lyra` | `Gamepad` is `@must_release(close_gamepad)`. `BUTTON_*` are positions (`SOUTH` = Xbox A / ✕ / Nintendo B; the shoulders `LEFT_SHOULDER`, `RIGHT_SHOULDER`); `AXIS_LEFT_*` runs -32768..32767, negative up. Needs `INIT_GAMEPAD`. |
| `video.lyra` | `create_window(title, w, h, flags)` with `WINDOW_*` flags; `set_fullscreen` (borderless desktop); `set_window_size`, `set_window_title`. `system_theme()` → `ThemeLight`/`ThemeDark`/`ThemeUnknown`, the system's appearance (unknown before SDL's video starts). |
| `render.lyra` | `Color` (`SDL_Color`) and `rgb`; points, lines, outlined/filled `Rect`s; `debug_text`, SDL's 8×8 ASCII font (`DEBUG_TEXT_SIZE`). `set_logical_presentation(…, PRESENT_INTEGER_SCALE)` is the pixel-art screen: everything, text included, scales by a whole number. `set_vsync` paces the loop. `save_screenshot` (ReadPixels → BMP) must run **before** `present` and saves at window resolution. `window_to_render(renderer, x, y)` maps a window (mouse) position onto the logical screen, undoing scale and letterbox. |
| `effect.lyra` | **Custom fragment shaders** on SDL's GPU renderer (3.4): `create_named_renderer(window, "gpu")` first, then `create_msl_effect(renderer, source, uniform_buffers)` → `Maybe<Effect>` (`None` on any other renderer, or a device without Metal); `set_effect_uniforms`, `use_effect(renderer, Some/None)` around the draws, `destroy_effect` before the renderer. The shader is **Metal Shading Language, entry `main0`**, and receives what SDL's own vertex shader hands on: colour at `[[user(locn0)]]`, texture coordinate at `[[user(locn1)]]`, the drawn texture and sampler at index 0, uniforms at `[[buffer(0)]]` — found from SDL's `tri_texture.vert` and `testgpurender_effects`, since the header does not say. Sheliak's display looks. |
| `texture.lyra` | `Texture` (handle + size) is `@must_release(destroy_texture)`. **Set `set_default_scale_mode(…, SCALE_NEAREST)` before loading** — a texture takes the mode in force when made, and the default blurs pixel art. `draw_texture(src, dst)`, `draw_texture_flipped` (`FLIP_*`). `set_texture_color`/`set_texture_alpha` are the **texture's state**, not the draw's: reset them after a tinted draw. `adopt_texture` is `unsafe` (it keeps a pointer) and is how other bindings hand a texture over. `create_streaming_texture(renderer, w, h)` + `update_texture(texture, []u32)` is a screen a program draws itself (texels `0x00RRGGBB`, exactly `w × h` of them or nothing changes) — Sheliak's emulator window. |
| `audio.lyra` | **Push model only**: `open_audio(rate, channels)` → a stream on the default device (`None` with no device — run silently), `put_audio([]f32)`, `queued_frames` to top the queue up to a target each frame. No callback: that would be Lyra running on SDL's audio thread. `AudioStream` is `@must_release(close_audio)`. Needs `init(INIT_AUDIO)`, which may be called after the first `init` — so a failure means "no sound", not "no SDL". `SDL_AUDIO_DRIVER=dummy` exercises it with no speakers. |
| `process.lyra` | `create_process([path, args…])` → `Maybe<Process>` (a bare name is looked up on the `PATH`; output inherited), `run_process([path, args…])` → `Maybe<(exit code, output)>`, run to its end with standard error and output captured together (it blocks for the whole run); `create_process_to_file(args, path)` starts one writing both to a file, for a caller that polls `wait_process` rather than waiting (Vega builds games with it; a file, not a pipe, so a long output never stalls), `wait_process(p, block)` → the exit code, or `None` while running when not blocking; `kill_process(p, force)`; `destroy_process` forgets it — **a program still running keeps running**, and one never waited for is a zombie until this one exits. Vega launches Sheliak with it. **SDL reads the argument array at `SDL_CreateProcessWithProperties`, not when the property is set**, and Lyra frees an array after its last use, so `run_process` uses it again after the create — without that, a segfault. |
| `filesystem.lyra` | `pref_path(org, app)` → `Maybe<string>`: the folder for a program's own files (settings), made if missing, ending in the separator — `~/Library/Application Support/<org>/<app>/` on macOS, the XDG data folder on Linux, `%APPDATA%` on Windows. Vega keeps its settings there. |
| `timer.lyra` | `delay`, `ticks`, and `ticks_ns` — a fixed timestep needs nanoseconds: whole milliseconds drift a 16.67 ms step by a whole step every few seconds. |

**Examples**: `examples/SDL3/basic.lyra` (a bouncing square, bindings only), and in
`examples/SDL3/nes/`:
`screen.lyra` (a 256×240 logical screen: palette, lines, points, text); `sprites.lyra` (a
PNG sheet: flips, tints, fades); `input.lyra` (an on-screen NES controller; `--check` tests
the pad logic headlessly); `sound.lyra` (a four-channel loop, coin and explosion effects
that borrow a channel, meters and an oscilloscope; `--check` tests the APU, `--wav <file>`
renders the song); `tilemap.lyra` (a four-screen level scrolled by a camera, walk/run/jump
with tile collision one axis at a time, coins taken from the map, pits; `--check` tests the
timestep, the physics and that the first pit forgives ordinary timing); and **`game.lyra`,
SLIME TRAIL** — the ladder's goal: title, one seven-screen level, patrolling slimes (green
slow, red fast; they turn at walls and ledges and wake as they come on screen), stomps,
three lives, a flagpole, music and effects. `--check` plays it, including an **autopilot
that must clear the level without losing a life**; `--autoplay` lets it play in the window.
`tilemap` and `sound` are built on the same modules as the game; `--level <file>` plays a
saved level. **`editor.lyra`** edits one — mouse painting and erasing, wheel and key
brushes, keyboard and middle-drag scrolling, Ctrl/Cmd+Z and +S, its own tile cursor over
the level, and on macOS a native menu bar (`bindings/menubar/`) — and drove the keyboard
and mouse bindings; `--check` tests the editing and the
generated key table. Sibling modules:
- `console.lyra` — `open_screen`/`close_screen` (init video+gamepad, window, 256×240
  integer-scaled renderer, vsync, nearest sampling), `end_frame` (the `--shot` logic, then
  present; answers `Continue`/`Stop(code)`), `wants_quit`, and the **fixed 60 Hz timestep**:
  `start_clock`, `updates_due(screen, clock)` (whole 1/60 s steps real time has paid for,
  capped at `MAX_STEPS` so a stall forgives rather than catches up; always 1 under `--shot`),
  and its pure arithmetic `accrue`. Pieces, not a loop: captures
  are by value, so a loop-owning callback could not update the example's `var`s.
- `pad.lyra` — the NES pad: `Buttons` (8 bools), `advance(previous, now) -> Pad` (`held` and
  one-frame `pressed`, pure), `read_buttons(Maybe<Gamepad>)` merging keyboard and pad,
  opposite directions cancelled, and `track_gamepad(current, event)` — the first pad added is
  opened, closed when it is the one removed. Mapping table in its module doc.
- `level.lyra` — ASCII levels (`s`/`r` slime spawns, `|`/`F` goal), `Body` with its own box,
  `steer` (the explorer's controls), `move_x`/`move_y` (collision one axis at a time),
  coins, `at_goal`, camera, and drawing the level and explorer. **Sprites are drawn at whole
  pixels** (`pixel`, inside `draw_cell`): a cell at a half pixel samples its neighbour in the
  sheet — a slime at 0.5 px/step showed the brick's black mortar down one edge.
- `music.lyra` — the song, `music_tick`, and effects as note lists (`Effect`) that borrow the
  second pulse channel (or, via `play_noise`, the noise) for their length; melody and bass
  are never interrupted.
- `palette.lyra` — the NES 2C02 palette as `nes(index)`.
- `course.lyra` — SLIME TRAIL's built-in course. A level file is its fifteen rows as text
  (`level.read_level`/`level_text`, validated by `level_problem`).
- `apu.lyra` — the 2A03's pulse ×2, triangle (32 steps, freezes when silenced) and noise
  (15-bit LFSR, long/short mode, NTSC period table), nesdev's linear mixer and a ~28 Hz
  high-pass. Two clocks: `render` at 44.1 kHz, `tick` per 735 samples (1/60 s) for
  envelopes and music — so tempo follows the audio clock, not the display's refresh.
`examples/SDL3/nes/assets/sprites.png` is committed and made by `assets/generate.py` beside
it (standard library only; reads the palette from `../palette.lyra`; 16×16 cells, ≤3 colours each). Cells 9–12 are
background tiles (ground, dirt, cloud, bush), 13–14 the goal pole, **appended** so earlier
indices never move. Examples find the sheet through `sheet.lyra`'s `load_sheet`, which
tries its path from the workspace, the repository, `examples/`, `examples/SDL3/` and `nes/`
(`lyrac run` builds into a temp dir, so only the shell's directory is known) and prints
every path it tried when none works. Every graphical example takes **`--shot <file.bmp>`** (and optionally `--at <frame>`):
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

## `bindings/menubar/`

A native menu bar — macOS's, for a window SDL owns. **The first binding with a C half**:
choosing an item calls a method on a target object, and Lyra cannot hand C a function, so
`menubar.m` holds the target, which queues the item's tag. Build with `menu(title)`,
`item(title, key, mods, tag)` (`COMMAND`/`SHIFT`/`OPTION`/`CONTROL`), `separator()`,
`window_menu()`, then `install(app_name)`, which adds the application menu (Hide, Quit —
Quit arrives as SDL's quit event) and answers whether there is a native bar. Items built
after `app_menu()` go into that application menu instead, above Hide (About, Settings…);
`install` starts the next build without them. `describe()` is the installed bar as text, a
line an item with its key — what a check or a person debugging reads, since System Events
needs Accessibility permission to list another process's menus. Each frame,
drain `poll_menu() -> Maybe<i32>` after `poll_event`; `set_checked`/`set_enabled` by tag.
`header(title)` heads a section; `slider(min, max, value, tag)` is an `NSSlider` in the menu,
queueing `tag` (once per drag) and read with `slider_value(tag)`.
`choose_file(title, extensions)` is a **modal Open panel** answering `Maybe<string>` (`None`
off macOS) — modal, so it answers on the main thread, with none of the queueing SDL's own
asynchronous dialogs need (`bindings/imgui`'s host has those) — and `alert(message,
detail)` a modal alert. `set_title(tag, title)` retitles an item (or a grid button).
**A grid window** is a settings window built the same way: `grid_window(title, columns,
close_tag)`, then `grid_row(label)` and `grid_cell(title, tag)` for its buttons and
`grid_note(text)` beneath; `show_grid()` shows it, laid out on first show. A button's press
and the window's close arrive through `poll_menu` like an item's; `set_title`,
`set_checked` (pressed in, accent-tinted) and `set_enabled` reach its buttons too.
`describe()` lists it after the bar (`window <title>`, a line a row).
- **A key pressed in the grid window never reaches SDL** — the window is AppKit's. So
  `capture_keys(true)` has a local event monitor take the window's keys (not ⌘-chords, so
  ⌘W and ⌘Q still work; modifiers as they are pressed) and queue them as **SDL scancodes**
  for `poll_key()`, translated from Mac virtual key codes by position with a table in
  `menubar.m`. That is how a program asks "which key?" from a settings window.
- **Built by `build.sh`** into `build/lib/liblyra-menubar.a`: `menubar.m` on macOS,
  `menubar_stub.c` elsewhere (every call a no-op, `install` false), so a program using it
  links everywhere. `lyrac` searches `<root>/lib`; `go run ./cmd/lyrac` needs
  `LIBRARY_PATH=build/lib`.
- **No framework flag**: `-fmodules` makes `@import AppKit` record `-framework AppKit` and
  `-lobjc` in the object file. Keep it — `@link` cannot name a framework.
- **A key equivalent arrives twice**: SDL reports ⌘S as a key press, then AppKit hands it
  to the menu. A program with a native menu leaves ⌘ keys to it.
- **A menu titled "Edit" gets AppKit's additions** — Writing Tools, AutoFill, Start
  Dictation, Emoji & Symbols — found by its title. The shim appends U+2060 (word joiner,
  invisible) to that title so the menu holds only what the program put there; the user
  defaults meant to turn the items off removed Dictation alone.
- Tags are ≥ 0 (`poll_menu`'s C side answers -1 for none) and unique (items are found by
  tag). `install` replaces SDL's default bar, so call it after `init`.

## `bindings/imgui/`

Dear ImGui (docking branch) with multi-viewports: a window dragged out of the main one
becomes an OS window. `examples/imgui/pixels.lyra` is a pixel editor built on it.
- **The host** (`imgui.lyra` over `host.cpp`): `create_host(title, w, h, flags)`
  (`HOST_DOCKING | HOST_VIEWPORTS`), then `for begin_frame(host) { …; end_frame(host, r, g, b) }`,
  then `destroy_host`. It owns the window, the SDL_GPU device and the frame loop, so Lyra
  binds no SDL_GPU. SDL_GPU because SDL_Renderer's ImGui backend has no multi-viewport support.
- **`host_macos.m` lets a torn-off window reach a monitor above.** AppKit's
  `constrainFrameRect:toScreen:` pulled every borderless window straddling two displays back
  below the menu bar, and ImGui moves a dragged panel a few pixels a frame, so it never got
  past. The host adds the method to SDL's window class: a borderless frame stands while
  its title bar's middle is in some screen's visible area, and otherwise gets AppKit's rule,
  so a panel may straddle onto a monitor above but never park its title under the menu bar.
- **The API is generated** into `generated.lyra` and `generated.cpp` (committed, never
  edited) by `gen/gen.lyra` from dear_bindings' `dcimgui.json`; the generator's header has
  the command and the type mapping. Rerun it after bumping the pin;
  `cmd/lyrac/imgui_example_test.go` fails when the committed files are not what it makes.
- **Fields are methods**: `generated.cpp` is a C accessor per field of `ImGuiIO`,
  `ImGuiStyle` and `ImGuiViewport` (`FIELD_STRUCTS`), so `get_io().delta_time()`,
  `get_style().set_alpha(0.9)`, `get_style().set_colors(COL_TEXT, v)`. An array's index is
  checked in C (out of range reads zero, writes nothing); strings and handles are read-only.
  A field whose setter is also a real method (`ImGuiIO::SetAppAcceptingEvents`) keeps the method.
- **Naming**: ImGui's names in snake case, one wrapper per function family — the full `…Ex`
  form under the short name, C++'s defaults as Lyra's (`button(label, size = Vec2 { … })`).
  A struct's functions are methods on its handle (`draw_list.add_line(p1, p2, col)`); one
  named like a free function takes its receiver as a suffix (`push_clip_rect_draw_list`).
  Enum values are `pub const`s (`WINDOW_FLAGS_NO_TITLE_BAR`, `MOUSE_BUTTON_LEFT`).
- **In-out pointers are `mut` parameters**: `checkbox(label, open) -> bool` flips the
  caller's `open` and answers whether it was clicked, as ImGui's `bool *` does; `float[3]` is
  `mut [3]f32`. A nullable `bool *p_open` makes two wrappers: `begin(name)` and
  `begin_closable(name, open)`. A `const char *` defaulting to NULL is `Maybe<string> = None`.
- **Hand-written in `imgui.lyra`** (over `host.cpp`) — what needs C++ on Lyra's behalf:
  - `input_text(label, value: mut string)`, `…_with_hint`, `…_multiline`: the host copies
    the string into a buffer ImGui grows through its resize callback, and the wrapper copies
    it back only when it changed. Any length.
  - **Textures**: `create_texture(host, w, h)` → `Texture` (`@must_release(destroy_texture)`),
    `update_texture(t, rgba)` (w × h × 4 bytes, uploaded at once), and `image`,
    `image_button`, `draw_list.add_image`, each with `filter: Filter = Nearest` — the
    host brackets the draw with ImGui's sampler callbacks, since pixel art must not be
    smoothed. Do not destroy a texture the frame being built still draws.
  - **Interface scale**: `set_ui_scale(host, s)` draws everything — text through
    `FontScaleMain` (1.92's fonts are sharp at any size), padding and spacing through
    `ScaleAllSizes` — at `s` times the host's own style, the display's density already in
    it; `ui_scale(host)` answers it. **From the base style each time**: the host keeps a
    copy, since `ScaleAllSizes` multiplies and rounds, and scaling the scaled style drifts.
    Clamped to 0.5–3. Call it between frames; a window sized in pixels keeps its size
    (fit it to its contents, or size it from `get_font_size()`).
  - `text(s)` (ImGui's `Text` is printf-style, so the variadics are bound through their
    `…Unformatted` forms and interpolation does the formatting) and `col32(r, g, b, a)`.
  - **A headless host for interface tests**: `create_headless_host(w, h, flags)` runs real
    ImGui frames with no window, no GPU and no SDL video, a fixed 1/60 s each, and input
    only from `get_io().add_mouse_pos_event`/`add_mouse_button_event`/`add_key_event`/
    `add_input_characters_utf8`. It never reads or writes `imgui.ini`, turns off macOS's
    Cmd/Ctrl swap (an injected `MOD_CTRL` chord is the same everywhere), ignores
    `HOST_VIEWPORTS`, answers `None` for textures, and **records file dialogs instead of
    showing them** — `take_dialog_request`, then `answer_dialog` — and `request_quit` stands
    in for the close button. Why headless rather than scripting a real window: the SDL
    backend adds the OS pointer's position after anything injected, so a real mouse wins.
    The texture handling is `imgui_impl_null.cpp`'s: every request marked done. A test
    finds widgets by rectangles it records itself (`get_item_rect_min/max`); Vega's
    `probe.lyra` is the pattern.
  - **`cancel_quit(host)`**: `begin_frame` answering false reports a quit (the window
    closed, Cmd+Q) and it *stays* requested; this takes it back, so a program with unsaved
    work asks first and keeps looping. SIGTERM arrives as the same quit.
  - **File dialogs**: `show_open_dialog`/`show_save_dialog(host, tag, filters,
    default_location)` return at once; the answer (`Chosen(tag, path)`, `Cancelled(tag)`,
    `DialogFailed(tag, why)`) comes from `poll_dialog(host)`, called each frame. SDL may run
    the callback on another thread and frees the file list when it returns, so `host.cpp`
    copies the answer onto a queue under an `SDL_Mutex` — `bindings/menubar`'s pattern, and
    no C++ runtime. `FileFilter { name, extensions }` takes `"vega;json"` or `"*"`. A dialog
    still open when the host is destroyed would answer into freed memory.
  - **A title bar the program draws**: with `HOST_CUSTOM_TITLE_BAR` the main window is
    borderless, and the main menu bar is begun with `begin_title_bar(host, after_button)`
    and ended, after the menus, with `end_title_bar(host, title, after_button)`: the title
    centred, and the window's buttons (glyphs drawn with lines: the default font has none)
    where the platform puts them — at the left end on macOS, close, minimize, maximize,
    before the menus; at the right end elsewhere, minimize, maximize/restore, close.
    Without the flag the pair is `begin_main_menu_bar`/`end_main_menu_bar`. Close is
    `request_quit`, so a program's "unsaved changes?" path is the same. **The window still
    moves and resizes as one with a frame**: SDL's hit test answers *draggable* for the
    bar's empty stretch, as the last frame drew it (`drag_area`, between the menus and the
    buttons, so a press never has to be told from a click by hover state), and *resize*
    within 5 points of an edge — except on macOS, where SDL's Cocoa hit test knows only
    dragging and AppKit resizes a borderless resizable window at its edges itself. A
    double-click on the bar does what the person's setting says (`host_macos.m` reads
    `AppleActionOnDoubleClick`; Windows' caption does it natively; X11 and Wayland do
    nothing yet). `set_window_title` keeps the system's title (Dock, taskbar) in step. A
    headless host draws the buttons, so a test can click them.
  - **Traffic lights**: `set_traffic_lights(host, on)` draws the title bar's buttons as
    macOS's red, yellow and green circles (grey while the window is not in use, their
    glyphs under the pointer) — Vega's macOS look turns it on.
  - **A status bar**: `begin_status_bar(name)` … `end_status_bar()` (only when it answered
    true) is a one-line menu bar along the viewport's bottom, over ImGui's internal
    `BeginViewportSideBar`. Call it before `dock_space_over_viewport`, which then docks in
    what is left.
  - **A font from a file**: `font_file(host, path, size, offset_y)` → `Maybe<Font>`, for
    `push_font_float` — `AddFontFromFileTTF`, which the generator leaves out for its
    glyph-range array. Loaded once and **kept by the host**: a font belongs to the context
    that loaded it, and a program-wide cache handed the next host a freed font (Vega's
    checks, a host each, 10/05). `None` when the file cannot be read (ImGui itself asserts).
    1.92's fonts are dynamic, so the size drawn is chosen at the push; `offset_y` raises
    (negative) or lowers every glyph, in pixels at `size`. Vega's macOS look loads SF with it.
- **Not bound** — `void *`, callbacks, `va_list`, `ImTextureRef` by value — listed with the
  reason at the end of `generated.lyra`.
- **Built by `bindings/imgui/build.sh`** (run from `./build.sh`) into
  `build/lib/liblyra-imgui.a` from a **downloaded** ImGui and dear_bindings, pinned by tag
  and SHA-256 and cached in `build/deps/`. Bump both tags together.
- **No C++ runtime**: `-fno-exceptions -fno-rtti -fno-threadsafe-statics` leaves only
  `__cxa_atexit` (libc's), so `@link("lyra-imgui")` + `@link("SDL3")` is the whole link line.

## `bindings/sdl3_image.lyra`

SDL3_image, `@link("SDL3_image")` — its own module so a program that loads no image does
not link it. `load_texture(renderer, path) -> Maybe<Texture>` answers `bindings.sdl3`'s
`Texture` via `adopt_texture`. `brew install sdl3_image` (Debian `libsdl3-image-dev`);
found through `pkg: "sdl3-image"`.

## `bindings/jpeg.lyra`

libjpeg-turbo, because Homebrew's raylib is built without JPEG support. `decode_jpeg(bytes)
-> Maybe<Jpeg>` answers RGBA pixels in a Lyra-owned array (baseline and progressive). An
`Image` pointed at that buffer must **never** be `unload_image`d. Link `-lturbojpeg` (`brew
install jpeg-turbo`, Debian `libturbojpeg0-dev`), found through `pkg: "libturbojpeg"`.

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
