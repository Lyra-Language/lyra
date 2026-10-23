# Binding modules (`bindings/`)

A binding module is a per-library Lyra module that owns its `extern`s and exports Lyra over
them (Rust's `*-sys` pattern). It needs nothing new from the language. Compiler-side FFI
notes are in [`pkg/backend/FFI.md`](../pkg/backend/FFI.md); language rules are in
[`LANGUAGE.md`](../LANGUAGE.md).

## Conventions for every binding

- **Externs are private; the module exports Lyra.** The wrapper is where `unsafe` stops,
  NULL becomes a `Maybe`, and untagged C unions become `data` types. An example using a
  binding should contain no `extern`, and no `unsafe` but the one below.
- **`pub extern` only for a function taking a C callback.** A callback must be a top-level
  function at the call (`lyra-E066`), which a wrapper's parameter is not, so no wrapper
  can pass one on: export the extern itself, documented with `///`, and the program calls
  it in `unsafe` with its own top-level function (`bindings.imgui`'s `set_redraw`).
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

SDL3 (3.4); `@link("SDL3", pkg: "sdl3")` on the header. Grown **by example**, towards an
NES-style game (the ladder is in `todo.md`): each example binds only what it draws with.
`version()` (`major * 1000000 + minor * 1000 + micro`) and `platform()` (`"macOS"`,
`"iOS"`, …) need no `init`; `set_hint(name, value)` (`HINT_ORIENTATIONS`) goes before it. **For iOS** (`lyrac --target ios|ios-simulator`) SDL3 is a
static library `tools/sdl3-ios.sh` builds into `<root>/lib/<platform>/`, its `sdl3.pc`
naming the frameworks it needs; the machine's is the system's (Homebrew, a distribution).

| file | what, and gotchas |
|---|---|
| `events.lyra` | `SDL_Event` is a Lyra `union` exposed as the `data` type `Event`; `poll_event` reads the tag, then the one member it licenses. `MouseDown(button, x, y, clicks)` carries SDL's count of clicks in a row (2 a double click). `MouseWheel(x, y, flipped)` has SDL's "flipped" (natural-scrolling) direction undone, and says whether it was (a scroll view follows the system's setting). `GamepadAdded`/`GamepadRemoved(id)` — SDL sends `Added` for pads already plugged in at startup, so no enumeration is bound; `GamepadButtonDown(id, button)`. **Touch**: `FingerDown`/`FingerMotion`/`FingerUp(id, x, y)`, x and y 0–1 across the window (a cancelled finger is lifted; a Mac's trackpad sends them too). **A phone's app**: `WillEnterBackground`/`DidEnterForeground`/`Terminating` — polled, they arrive when the app runs again, which is late for saving but in time to stop a game catching up. **An interface's**: `TextInput(text)` (only while `start_text_input` is on; the key's `KeyDown` arrives as well), `WindowResized(w, h)` in points, `WindowScaleChanged`. `wait_event(timeout_ms)` is `poll_event` that sleeps until something happens (negative: for good) — an interface with nothing animating waits there. `add_event_watch(filter, userdata)` (a `pub extern`: a top-level `(^u8, ^u32) -> u8` callback) sees each event as it is queued, those SDL sends while macOS holds the main thread in a live resize included; `live_resize(event)` says it is the frame such a resize asks for (`SDL_EVENT_WINDOW_EXPOSED`, `data1` 1). |
| `keys.lyra` | **Generated** by `gen_keys.py` from the installed SDL headers — every `SCANCODE_*` (position, 247), `KEY_*` (key code, 256) and `MOD_*` (modifier mask, 18), plus `SCANCODES`/`KEY_CODES` arrays. Do not edit; rerun on an SDL upgrade and review the diff. Checked by properties and by asking SDL to name every code (`editor.lyra --check`). |
| `keyboard.lyra` | `key_held(SCANCODE_*)`: held state by **position**, not label. Updated as events are pumped — read it after draining the queue. Bounds-checked against SDL's reported length. `modifiers()` (a `MOD_*` mask; `MOD_GUI` is Command), `key_name`/`scancode_name` (SDL's human names). `start_text_input(window)`/`stop_text_input(window)` turn `TextInput` events (and a phone's keyboard) on and off; `set_text_input_area(window, PixelRect, cursor)` says where the field is, for an input method's candidates. |
| `mouse.lyra` | `mouse_state()` (window position + button mask) and `button_held(state, BUTTON_*)`; `show_cursor`, `system_cursor(CURSOR_*)` → `Cursor` (`@must_release(destroy_cursor)`), `set_cursor`. **Positions are window pixels** — convert with `render.window_to_render` on a logical screen. |
| `gamepad.lyra` | `Gamepad` is `@must_release(close_gamepad)`. `BUTTON_*` are positions (`SOUTH` = Xbox A / ✕ / Nintendo B; the shoulders `LEFT_SHOULDER`, `RIGHT_SHOULDER`); `AXIS_LEFT_*` runs -32768..32767, negative up. Needs `INIT_GAMEPAD`. |
| `video.lyra` | `create_window(title, w, h, flags)` with `WINDOW_*` flags; `set_fullscreen` (borderless desktop); `set_window_size`, `set_window_title`, `set_window_minimum_size`, `set_window_maximum_size` (`0, 0` for none), `set_window_resizable`. `system_theme()` → `ThemeLight`/`ThemeDark`/`ThemeUnknown`, the system's appearance (unknown before SDL's video starts). `window_size(window)` is in **points**; `window_pixel_density` is pixels a point (2 on Retina) and `window_display_scale` that times the user's chosen scale — the pixels an interface's unit takes. |
| `render.lyra` | `set_draw_blend(BLEND_ALPHA)` for see-through rectangles; `set_scale` grows what follows (`debug_text`'s font); `output_size()` is the whole window in real pixels whatever logical screen is set (`SDL_GetCurrentRenderOutputSize` would answer the screen's share). `Color` (`SDL_Color`) and `rgb`; points, lines, outlined/filled `Rect`s; `debug_text`, SDL's 8×8 ASCII font (`DEBUG_TEXT_SIZE`). `set_logical_presentation(…, PRESENT_INTEGER_SCALE)` is the pixel-art screen: everything, text included, scales by a whole number. `set_vsync` paces the loop. `save_screenshot` (ReadPixels → BMP) must run **before** `present` and saves at window resolution. `read_pixels` is the same frame as `Pixels` (`0xAARRGGBB`, a pixel at a time — for tests); headless, it needs `SDL_VIDEODRIVER=dummy` with `SDL_RENDER_DRIVER=software` (the offscreen driver still wants OpenGL). `window_to_render(renderer, x, y)` maps a window (mouse) position onto the logical screen, undoing scale and letterbox. `draw_geometry(vertices, indices)` / `draw_textured_geometry(texture, …)` draw triangles of `Vertex` (`SDL_Vertex`: position, a 0–1 colour, a 0–1 texture coordinate) — **the software renderer truncates each vertex to a whole pixel**, which a GPU renderer does not. `set_clip(PixelRect)` / `clear_clip` (`PixelRect` is `SDL_Rect`, whole pixels). |
| `effect.lyra` | **Custom fragment shaders** on SDL's GPU renderer (3.4): `create_named_renderer(window, "gpu")` first, then `create_msl_effect(renderer, source, uniform_buffers)` → `Maybe<Effect>` (`None` on any other renderer, or a device without Metal); `set_effect_uniforms`, `use_effect(renderer, Some/None)` around the draws, `destroy_effect` before the renderer. The shader is **Metal Shading Language, entry `main0`**, and receives what SDL's own vertex shader hands on: colour at `[[user(locn0)]]`, texture coordinate at `[[user(locn1)]]`, the drawn texture and sampler at index 0, uniforms at `[[buffer(0)]]` — found from SDL's `tri_texture.vert` and `testgpurender_effects`, since the header does not say. Sheliak's display looks. |
| `texture.lyra` | `Texture` (handle + size) is `@must_release(destroy_texture)`. **Set `set_default_scale_mode(…, SCALE_NEAREST)` before loading** — a texture takes the mode in force when made, and the default blurs pixel art. `draw_texture(src, dst)`, `draw_texture_flipped` (`FLIP_*`). `set_texture_color`/`set_texture_alpha` are the **texture's state**, not the draw's: reset them after a tinted draw. `adopt_texture` is `unsafe` (it keeps a pointer) and is how other bindings hand a texture over. `create_streaming_texture(renderer, w, h)` + `update_texture(texture, []u32)` is a screen a program draws itself (texels `0x00RRGGBB`, exactly `w × h` of them or nothing changes) — Sheliak's emulator window. `create_texture(renderer, w, h, []u32)` is a **static, blended** texture from a program's own pixels (`0xAARRGGBB`, alpha 0 transparent) — sprite sheets and tile sets held as data; `set_texture_scale_mode` overrides the default for one texture. `update_texture_area(texture, PixelRect, []u32)` rewrites part of a `create_texture` texture (a glyph into an atlas). `draw_texture_rotated` turns a draw about its middle; `set_texture_blend` (`BLEND_ALPHA`, `BLEND_ADD` for light, `BLEND_MULTIPLY`, `BLEND_NONE`) is texture state like colour and alpha — set it back. |
| `audio.lyra` | **Push model only**: `open_audio(rate, channels)` → a stream on the default device (`None` with no device — run silently), `put_audio([]f32)`, `queued_frames` to top the queue up to a target each frame. No callback: that would be Lyra running on SDL's audio thread. `AudioStream` is `@must_release(close_audio)`. Needs `init(INIT_AUDIO)`, which may be called after the first `init` — so a failure means "no sound", not "no SDL". `SDL_AUDIO_DRIVER=dummy` exercises it with no speakers. |
| `process.lyra` | `create_process([path, args…])` → `Maybe<Process>` (a bare name is looked up on the `PATH`; output inherited), `run_process([path, args…])` → `Maybe<(exit code, output)>`, run to its end with standard error and output captured together (it blocks for the whole run); `create_process_to_file(args, path)` starts one writing both to a file, for a caller that polls `wait_process` rather than waiting (Vega builds games with it; a file, not a pipe, so a long output never stalls), `wait_process(p, block)` → the exit code, or `None` while running when not blocking; `kill_process(p, force)`; `destroy_process` forgets it — **a program still running keeps running**, and one never waited for is a zombie until this one exits. Vega launches Sheliak with it. **SDL reads the argument array at `SDL_CreateProcessWithProperties`, not when the property is set**, and Lyra frees an array after its last use, so `run_process` uses it again after the create — without that, a segfault. |
| `filesystem.lyra` | `pref_path(org, app)` → `Maybe<string>`: the folder for a program's own files (settings), made if missing, ending in the separator — `~/Library/Application Support/<org>/<app>/` on macOS, the XDG data folder on Linux, `%APPDATA%` on Windows. Vega keeps its settings there. `base_path()` → `Maybe<string>`: the folder the program was started from, for what it ships beside itself — `<App>.app/Contents/Resources/` in a macOS bundle, else the executable's folder; ends in the separator. Vega's release finds its lyrac and Sheliak there. |
| `clipboard.lyra` | `clipboard_text()` (`""` when it holds no text) and `set_clipboard_text(text)`. |
| `url.lyra` | `open_url(url)` — a web address, `mailto:` or `file://`, opened by the program the system chooses (`SDL_OpenURL`); whether the system took it. |
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
- **`set_layout_file(host, path)`** keeps the layout (`io.IniFilename`) somewhere of the
  program's choosing, before the first frame; ImGui's default is `imgui.ini` in the working
  directory. The host keeps its own copy of the path, since ImGui holds the pointer and
  writes the file again as the context is destroyed. A headless host keeps no layout.
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
- **On macOS `io.key_ctrl()` is ⌘ and `io.key_super()` is Ctrl**: ImGui swaps them
  (`ConfigMacOSXBehaviors`, on by default there, off on the headless host), so Ctrl reads as
  "the shortcut key". A program needing the physical keys — a terminal — swaps back when
  `io.config_mac_osx_behaviors()` (Termina's `ui/modifiers.lyra`).
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
  - **Typed text for a widget of the program's own** (a terminal, an editor):
    `request_text_input(pos, line_height)` each frame it has the keyboard, then
    `typed_text()`. **SDL3 sends characters only while text input is on**, and ImGui turns
    it on for its own `InputText` alone — this sets `PlatformImeData.WantTextInput` as
    InputText does (with the caret for an IME). Not `want_text_input`: that name is
    `IO`'s generated field getter. ImWchar is 16-bit, so a character past the BMP arrives
    as U+FFFD.
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
  - **Live resizing — `set_redraw(host, frame)`**: the host draws `frame`,
    the program's whole frame from `begin_frame` to `end_frame`, at each expose SDL hands
    an event watch during a live resize (`data1` 1), so the contents follow the edge. On
    macOS AppKit's resize loop holds the main thread inside `begin_frame`'s event pump
    until the mouse is let go, and the program's own loop draws nothing meanwhile; such a
    frame's `begin_frame` pumps nothing. **A `pub extern`**, not a wrapper: a C callback
    is a top-level function, which no wrapper's parameter is (`lyra-E066`), so the binding
    exports the extern itself and the program calls it in `unsafe`; its frames' state is
    module-level and **read after `begin_frame`**, which may have drawn frames while it
    waited. Termina is the user.
  - **`cancel_quit(host)`**: `begin_frame` answering false reports a quit (the window
    closed, Cmd+Q) and it *stays* requested; this takes it back, so a program with unsaved
    work asks first and keeps looping. SIGTERM arrives as the same quit.
  - **`take_dropped_file(host)`**: the next file the system asked the program to open —
    dropped on a window, or on macOS opened from Finder, which SDL hands over as a drop
    (the app's `CFBundleDocumentTypes` say which files Finder offers it) — collected as
    `begin_frame` pumps, so one the app was launched to open is there by its first frame.
    `drop_file(host, path)` queues one, for a test on a headless host. Vega opens its
    projects so.
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
    Without the flag the pair is `begin_main_menu_bar`/`end_main_menu_bar`. **Items of the
    program's own between the title and the buttons** — a GNOME header bar's menu button —
    split `end_title_bar` in two: `title_bar_title(host, title, trailing)` leaves `trailing`
    points before the buttons (`title_bar_button_width(host)` is a button's) and the cursor
    where they start, the drag area stopping there; the program draws its items, then
    `title_bar_end(host, after_button)` draws the buttons and ends the bar (Termina's
    `menus.draw_header_bar`). `title_bar_button(host, name, after_button)` is such an item
    the window buttons' size — the bar's full height, filled as they are when hovered — and
    answers whether it was pressed; the program draws its glyph over `get_item_rect_*`.
    The window's corners are square: SDL's GPU API refuses a transparent window (3.4's, on
    every driver, since D3D12 cannot present one), which round ones would need. Close is
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
  - **A fallback font**: `merge_font_file(host, font, path)` → `bool` merges a file into a
    font for the glyphs it lacks (CJK in a Latin face), at the font's size — 1.92's
    `MergeMode` with `DstFont` naming the font, so it need not be the last one added. The
    file is read once (`SDL_LoadFile`) and its bytes shared by every font it goes into,
    the host's to free after the context (`FontDataOwnedByAtlas` off: the atlas reads them
    while it lives); merging the same pair again is a no-op. Termina merges CJK and symbol
    fonts into each face.
  - **Hinted text**: `set_font_hinting(host, on)` draws the host's fonts from files
    (`font_file`, and what is merged into them) through FreeType (`imgui_freetype.cpp`),
    hinted, advances in whole pixels — crisp on a display of a pixel a point, where
    stb_truetype's unhinted glyphs are soft. ImGui 1.92 refuses FreeType as one font's
    `FontLoader` (its start-up needs the atlas), so it is `SetFontLoader` on the atlas,
    and **the interface's own font is pinned to stb_truetype** (ImGui's default, added
    then if it is not yet): a font may have a loader of its own if the loader has no
    start-up, which stb's has not. Off by default: a program that never asks (Vega)
    draws as before. Termina asks off macOS.
- **Not bound** — `void *`, callbacks, `va_list`, `ImTextureRef` by value — listed with the
  reason at the end of `generated.lyra`.
- **Built by `bindings/imgui/build.sh`** (run from `./build.sh`) into
  `build/lib/liblyra-imgui.a` from a **downloaded** ImGui and dear_bindings, pinned by tag
  and SHA-256 and cached in `build/deps/`. Bump both tags together. **FreeType is
  downloaded the same way** (a release tarball, its hash the same from savannah.gnu.org and
  SourceForge) and compiled into the archive with only the modules `lyra_ftmodule.h` lists
  — TrueType and OpenType, CFF and variable fonts, the smooth rasterizer, the auto-hinter —
  so no program needs a system FreeType.
- **No C++ runtime**: `-fno-exceptions -fno-rtti -fno-threadsafe-statics` leaves only
  `__cxa_atexit` (libc's), so `@link("lyra-imgui")` + `@link("SDL3")` is the whole link line.

### `bindings.imgui.ui` — an interface as a tree (`imgui/ui/ui.lyra`)

An interface as plain Lyra values — the layer the `.lui` markup (below) is generated into.
`examples/imgui/components.lyra` shows it; in Vega the menus are a tree and the Settings
window is markup (`vega/src/settings_view.lui`).

- **`Node<e>`** — `Window`, `MainMenuBar`, `MenuBar`, `Menu`, `MenuItem`, `Button`,
  `Checkbox`, `Combo` (a drop-down whose children are `Selectable`s, each its own event —
  ImGui's own shape, so a choice needs no event carrying a value), `Form` and `Field` (a
  label right-aligned in a column as wide as the widest, its controls after it), `Text`,
  `Separator`, `SeparatorText`, `Row` (children side by side; with `wrap`, onto a new
  line when the next would not fit across, as CSS's `flex-wrap` — each child taken to be
  as wide as it was last frame, kept in ImGui's state storage), `Group` (its children
  one under another as a single item, ImGui's group, a `<div>`: groups in a `Row` stand
  as columns, their tops lined up), `Space` (empty room, a
  `height` down or a `width` across: ImGui's `Dummy`), `Radio`, `InputInt`,
  `InputText` (a `hint` greyed while it is empty), `Slider` (an `f32` dragged between
  `min` and `max`, shown in `format`, clamped to them however entered) and `ColorEdit` (a
  `0xRRGGBB` color's swatch, which opens ImGui's picker; `inputs` adds its channels'
  fields; `rgb_channels`/`rgb_of` convert to and from ImGui's floats) — the inputs below —
  `Modal` (open while it is in the tree; a `closes` button closes it,
  Escape is its `on_escape`), `Slot` (where the caller draws — a canvas, a preview — in
  its `seen`), `Component` and `Fragment` (children in place, several where one is
  expected) — each with an `…Attrs` struct of
  its attributes. `e` is the program's event type. A `Window` may be `centered` (each time
  it appears); a button, checkbox, combo, selectable or menu item has an `id`, what a
  program's tests know it by.
- **`render(nodes, seen) -> []e`** draws the tree and answers the frame's events. Every
  `begin_*` is ended, and only when ImGui says it began (`end_menu` under a `begin_menu`
  that answered true; `end` after every `begin`); `seen` is called with each element just
  after it is drawn (Vega names its menus for the interface tests there).
- **Events, not callbacks**: an element names the event its use stands for (`on_select`,
  `on_click`, `on_toggle`, a window's `on_close`); a checkbox does not write its value, the
  program flips it. A view is a `pure` function of the state. **An event shows from the
  next frame**: `render` draws the whole tree, then answers the events, so state they
  change is drawn a frame later — unseen by a person, but a test clicking what a click
  moved must let that frame pass.
- **Inputs carry their value in a function**: `InputInt`, `InputText`, `Slider` and
  `ColorEdit` call `on_change: (value) -> e` with the value the field would now hold, each
  keystroke, step, drag or pick (the field never writes `value`; the program keeps or
  refuses it), and send `on_commit` once when the edit ends — change per edit, commit
  once, as an undo step wants; a slider's commit is the button coming up.
  `node_id(node)` is any element's `id` (else its label), for a `seen` that probes.
- **The tree is data more than ImGui reads**: `menu_items(nodes)` flattens the menu items
  (Vega installs the same tree in macOS's menu bar), `pressed_shortcut(nodes)` presses a
  `Shortcut` (the primary modifier, Cmd or Ctrl, and optionally Shift and Alt — or Alt
  alone, `primary: false`; a character or a named key, `PgUp` `PgDn`) while its menu is
  closed, `key_known` says whether ImGui has a shortcut's key.
- **Inline style** (`Style`: `color`, `background`, `padding`, `rounding`, `opacity`,
  `width`, `height`, `width_em` — a width in the font's size, growing with the
  interface — and `gap`, the space between a container's children: a `Form`'s and a
  `Group`'s down, a `Row`'s children and a `Field`'s controls across; a `Form`, a `Row`
  and a `Group` take a `class` and a `style` for it, and nothing else of a style applies
  to them) means each
  field in the element's own terms — `background` is a
  button's face (its hover and press shades derived), a checkbox's box, a menu item's
  highlight, a window's or menu's fill — and is pushed onto ImGui's style stack around the
  element and popped after its children, so it is **inherited** as CSS's inherited
  properties are.
- **A component is a function returning a `Node<e>`**, generic in `e` so any program's
  tree can hold it, its props its parameters. Wrapping its elements in
  `Component(sheet, children)` **scopes a style sheet** to them: a `Sheet`'s
  `StyleRule`s match by tag (`Button`), class (`.primary`; an element's `class` is
  space-separated) or both (`Button.primary`), the more specific winning, then the later;
  inline style is laid over the sheet's. **Selectors stop at the boundary** — not the tree
  around the component, nor a component inside it, which has its own sheet (Vue's
  `scoped`, CSS modules) — while what a rule pushes is inherited as inline style is.
  `Sheet.style_for(tag, classes, inline)` is the cascade, pure.
- **Not yet**: keys for repeated labels (ImGui's `push_id`; a `key` as JSX has), layout
  beyond `Row` and `Form`, tooltips, tables, child regions, sliders and colour editors.

### `.lui` markup — `tools/lui`, built into `build/lui`

Markup for the tree, in files of its own, **generated into Lyra** so lyrac checks it: `lui
view.lui` writes `view.lyra` beside it, `lui --check view.lui` fails when that is stale.
A directory stands for every `.lui` file beneath it (`lui --check src`), names beginning
with `.` and symlinked directories skipped, as lyrafmt walks.
The example's interface is `examples/imgui/components_view.lui`.

```
<component name="confirm" props={{ question: string, yes: e, no: e }}>
  <style>
    Button { rounding: 6; padding: 14 4 }
    Button.danger { background: #aa2828 }
  </style>
  <Text>{question}</Text>
  <Row>
    <Button class="danger" on_click={yes}>Yes</Button>
    <Button on_click={no}>No</Button>
  </Row>
</component>
```

- **A file is `<script>`s and `<component>`s.** A script's Lyra is copied to the top of the
  module (imports, the state and event types, `update`); a component is a `pure` function
  of its props returning `Node<event>` (`event={Event}`; default `e`, so it fits any
  tree), wrapped in its `Component`. **Props are an object**, `props={{ question: string,
  title: string = "Sure?", }}` — each `name: Type` (a Lyra type, generics and functions
  whole), an optional `= default` (Lyra), a trailing comma allowed; they become the
  function's parameters in order. The old `props="…"` string is refused, saying so.
- **A capitalized tag is an element**, its attributes its `…Attrs` fields and its text its
  label. An `on_change` given a bare name (`on_change={Width}`) is made the typed lambda
  calling it, `(value: i64) -> Ev => Width(value)`; a lambda written out must give its
  types. Attributes are `"text"` (a string; `{…}` inside interpolates, and text that is one
  `{n}` alone is the string `"${n}"`, whatever `n` is), `{Lyra}`, or bare
  (a flag, `true`); `on_close={X}` is `Some(X)`; `shortcut="Cmd+Shift+S"` (Cmd or Ctrl is
  the primary modifier either way; `Alt+1` holds Alt alone, `Ctrl+PgUp` names its key)
  and `style="rounding: 4"` are read for you; a `<Slot name="…">` is named as any
  attribute is, so a component can name its caller's (`<Slot name="colour {which}"/>`).
  An unknown tag or attribute, a missing one, and text where none goes are lui's errors, at their line.
- **A lowercase tag calls a component of the same file**, its attributes matched to the
  props by name (a prop's `= default` fills a missing one), its children passed as a
  `children: []Node<e>` prop. Another file's component, or one written in Lyra, is a
  `{name(…)}` child.
- **`{…}` is Lyra, copied through**: in text (`Reset {n} times`), in attributes, and as a
  child — `{node}`, or `{...nodes}` for an array. **Control flow** is `<if test={…}>`
  followed by an optional `<else>`, and `<for each={x in xs}>` (a comprehension; several
  children make a `Fragment`).
- **`<style>` is the component's scoped sheet**: `Tag`, `.class` and `Tag.class` selectors
  (comma lists), `color`/`background` as `#rgb`, `#rrggbb` or `#rrggbbaa`, `padding` one
  or two numbers, `rounding`, `opacity`, `width` (in pixels, or `6em`), `height`, `gap`;
  `/* … */` comments.
- **The generated module is not formatted** (`.lyrafmtignore` lists it) — one element to a
  line, each marked with the markup line it came from (`// view.lui:12`; a sheet's at its
  `<style>`, a component's `/// From view.lui, line N.`, a script's alone on the line
  above it, meaning the next line).
- **`lui map` puts lyrac's errors back at the markup**: `lyrac check app.lyra 2>&1 | lui
  map` rewrites each `view.lyra:L:C` in a module lui generated (its first line says so)
  to `view.lui:N`, the nearest marker at or above `L`, and leaves every other line as it
  was — so a whole program's build maps too. The column goes: it was the generated one.
- **`lui --watch view.lui …`** generates at once and again whenever a file's text changes
  (read every 250 ms, `std.io.sleep_ms`), saying each generation and any mistake at its
  line; a mistake leaves the last good module in place. Stop it with Ctrl-C. **A
  directory is walked again every pass** (`lui --watch src`), so a file added beneath it
  is generated as it appears.
- **Two targets** — `bindings.imgui.ui`, the default, and `ui` (`ui.layout`), named by a
  script (`<script target="ui">`) or `lui --target ui`. For `ui` a capitalized tag is a
  `ui.layout` builder (`<Row main="between" cross="center">`, `<Column>`, `<Stack>`,
  `<Expanded flex>`, `<Positioned left top …>`, `<Padding all | x y | top …>`, `<Align
  at="top-left">`, `<Scroll axis offset>`, `<SizedBox width height>`, `<Text>`, `<Button
  on_click>`, `<Checkbox checked on_toggle>`, `<Slider value min max step on_change>`,
  `<Select selected on_change>` with `<Option>`s inside (each its text) or `options={…}`
  (`on_change` takes the index, an `i64`), `<TextInput value placeholder on_edit>`, `<Image texture texels width height>`, `<Themed
  theme sheet>`, `<Flex direction wrap justify align align-content gap row-gap column-gap>`,
  `<Grid columns rows gap row-gap column-gap flow justify-items align-items auto-columns
  auto-rows>` — a `<Flex>`'s or `<Grid>`'s attributes are the style properties they name,
  written as CSS writes them), and every element takes `class`, `part`, `key` (its identity,
  `.keyed`), `label` (for assistive technology) and `style` — **a component's call too**,
  on the component's own node (a prop of that name is the prop), so `<tile class="wide"/>`
  spans its grid's columns. A `<for>` there holds one element.
- **Styling for `ui`**: a component's `<style>` is a sheet made from the theme it is drawn
  in (`component_of`); a top-level `<style global theme={AppTheme} name="app_sheet">` is a
  `pub` function of that theme (`Theme` by default) answering the sheet, for `<Themed
  theme={t} sheet={app_sheet(t)}>`. A `$path` reads the theme — `$colors.accent` is
  `t.colors.accent`, `$space.m` `t.space.m` — wherever a number or colour goes (not in an
  inline `style`, which has no theme); properties `color`, `background`, `border-color`,
  `border-width`, `radius` (or `rounding`), `gap`, `opacity`, `font-size`, `line-height`,
  `padding` (one, two — across then down — or four); and layout's, as CSS writes them:
  `width`, `height` and their `min-`/`max-`, `row-gap`, `column-gap`, `flex` (`none`, `auto`,
  or grow, shrink and basis — `flex: 1` is `1 1 0`), `flex-direction`, `flex-wrap`,
  `flex-grow`, `flex-shrink`, `flex-basis` (`auto`, a length, `N%`), `order`,
  `justify-content`, `align-content`, `align-items`, `align-self`, `justify-items`,
  `justify-self`, `grid-template-columns`/`-rows` (lengths, `Nfr`, `auto`, `min-content`,
  `max-content`, `minmax(a, b)`, `repeat(n, …)`), `grid-auto-columns`/`-rows`,
  `grid-auto-flow` (`row`, `column`, `dense`) and `grid-column`/`grid-row` (`N`, `span N`,
  `N / M`, `N / span M`). An unknown value is an error listing the ones there are; a later
  declaration replaces an earlier one, as in CSS. **`@media (…) { rules }`** makes every rule
  inside conditional (`Rule.media`): `min-width`, `max-width`, `min-height`, `max-height`
  (logical units, `px` allowed), `orientation` (`portrait`, `landscape`) and
  `prefers-color-scheme` (`light`, `dark`), joined by `and`, listed with commas (any of), and
  reversed by a leading `not`; `screen` and `all` are taken and ignored. An unknown feature or
  value, a nested `@media` and any other at-rule are lui's errors at their line; the
  `bindings.imgui.ui` target has no `@media`. **A selector list** (`Button.primary,
  Checkbox::box, .card { … }`) is a rule for each, each selector checked — `Tag`, `.class`,
  `Tag.class`, `Comp::part`, `Comp.class::part`, each with pseudo-classes (below); a
  descendant selector is an error at its own line. **States are CSS's pseudo-classes**:
  `:hover`, `:active`, `:focus`, `:focus-visible`, `:focus-within`, `:checked`,
  `:read-only`, `:read-write`, `:disabled` and `:enabled`, on a compound (`Button.primary:hover`, `:focus-within`) or a part — the
  component's before `::` (`Slider:hover::thumb`), the part's after it (`Slider::thumb:active`,
  which takes no classes). Another pseudo-class, a class after a part, and a class named for a
  state (`.hover`, `.pressed`, `.focus` — `:hover`, `:active`, `:focus` now) are errors at their
  line. Any element takes **`disabled`**, bare or `disabled={condition}`. **`<TextInput
  readonly>`** is a read-only field, needing no `on_edit`; `readonly={condition}` keeps its
  `on_edit` for while the condition does not hold. **`<window title width height min-width min-height max-width max-height resize resize_x
  resize_y overflow/>`** at the top of a ui file writes `window_spec`, a function answering a
  `ui.app.WindowSpec` with the fields given (the rest its defaults), for `ui.gpu.open_app` —
  which opens the window, builds the `App`, leaves `main` its loop, and under `--live` makes an
  edit to the description to the open window — or for an `App`'s fields
  (`window_spec().min_size`). `width` and `height` go together; `resize`, `resize_x` and `resize_y`
  are bare or `{condition}`: `resize={false}` holds the window to its size, and a side's own
  attribute overrides `resize` for that side (`resize={false} resize_x` resizes only across). **Effects, as CSS writes them**: `box-shadow` (`none`, or shadows a comma apart,
  each `[inset] x y [blur [spread]] #colour`, `px` allowed), `background` or `background-image`
  as `linear-gradient([Ndeg | to side or corner,] stops…)` (a corner taken as 45°) or
  `radial-gradient([circle | ellipse,] stops…)`, each stop `#colour [N%]`, and `filter` and
  `backdrop-filter` as `blur(N)` or `none` — each checked, an error at its line.
  **`overflow`** takes one value for both axes or two (x, then y), and `overflow-x`/`overflow-y`
  one: `visible`, `hidden`, `clip`, `scroll` or `auto`; `<window overflow="…">` sets
  `window_spec().overflow`. **Transitions, as CSS writes them**: `transition` is a comma list, each
  `[property] duration [timing-function] [delay]` in any order (the property `all` when
  omitted; `none` for none), and `transition-property`, `-duration`, `-timing-function` and
  `-delay` are its longhands, their lists paired by place and a shorter one repeated. Times
  are `Nms` or `Ns`; timing functions `ease`, `linear`, `ease-in`, `ease-out`, `ease-in-out`,
  `step-start`, `step-end`, `cubic-bezier(x1, y1, x2, y2)` (x 0–1) and `steps(n[, jump-start |
  jump-end])`; a property is one `ui.motion` animates, `all` or `background`, `radius` read as
  `border-radius`. A selector may ask for several classes (`.card.big`). Each rule is marked with its line, so
  `lui map` puts a token lyrac refuses back at it. **Parts**: `Comp::part` and
  `Comp.class::part` are checked against the parts the component's markup marks
  (`part="title"`) — or a control's (`Checkbox::box`, `Slider::thumb`,
  `TextInput::placeholder`) — and a part it lacks is lui's error, as a misspelt prop is.
- **Another file's components**: `<import src="widgets.lui"/>` at the top reads that file,
  checks calls to its components as this file's are, and imports the ones called from its
  module — the path without `.lui` (`/` as `.`), or `module="…"`. `lyra-lsp` passes the
  markup's path, so imports resolve in the editors too.
- **Tested** by `cmd/lyrac/lui_test.go`: the examples' modules (both targets) are what lui
  makes and check clean, each kind of mistake is reported at its line, and a token's or a
  script's error from lyrac is mapped back.
- **The editors show errors at the markup**: `lyra-lsp` runs `lui --stdin` on a `.lui`
  buffer, analyzes the module beside it, and publishes each diagnostic at the markup line
  its marker names (`cmd/lyra-lsp/README.md`); both extensions send `.lui` to it.
- **Highlighted in both editors**: VS Code by `lyra-vscode-ext/syntaxes/lui.tmLanguage.json`,
  Zed from the grammar in `tree-sitter-lyra/lui/` (`lyra-zed-ext/languages/lui/`) — Lyra in
  `{…}` and `<script>`, CSS in `<style>`.

## `bindings/accesskit/`

A window's accessibility tree for assistive technology — VoiceOver — through
[AccessKit](https://github.com/AccessKit/accesskit-c)'s macOS adapter, and the actions it asks
for back. `ui.sdl3` is its one caller; `ui.access` builds the tree it is handed.

- **A snapshot, not callbacks.** AccessKit asks for the tree from a callback, whenever
  VoiceOver starts listening, and a Lyra closure cannot be one. So the program hands over the
  whole tree each time it changes — `begin(root, focus)`, an `element(…)` a node, `commit()` —
  and `accesskit.m` keeps it, builds AccessKit's nodes from it when asked, and sends AccessKit
  an update when it is already listening. An action request is queued there and an
  `SDL_EVENT_USER` pushed to wake a program waiting in `wait_event`; `next_request()` takes
  them. `bindings/menubar`'s pattern.
- **Roles and actions are numbers** the shim maps (`RoleCode`, `ACTION_*`), `ui.backend`'s
  `ACCESS_*` alike. A label's text is its *value*, as AccessKit has it. Bounds are in the
  window's **pixels** (AccessKit divides by the backing scale), from the content's top left.
- **`start(window)` subclasses SDL's `NSWindow`'s content view** (the subclassing adapter,
  with the focus forwarder on the window's class) and answers whether it could — `false`
  off macOS.
- **Built by `bindings/accesskit/build.sh`**, which `./build.sh` runs, into
  `build/lib/liblyra-accesskit.a`: AccessKit's **prebuilt release, pinned** (0.23.1) and its
  zip SHA-256-checked, downloaded into `build/deps/` (not vendored), its static library merged
  with the compiled shim by `libtool -static`. `-fmodules` records the frameworks. Off macOS,
  offline or without SDL3 it builds `accesskit_stub.c` instead (every call a no-op, `start`
  false) and says why. MIT or Apache-2.0 (`NOTICE.md`); the licences are copied beside the
  archive.

## `bindings/gpu/`

`ui.gpu`'s renderer over SDL3's GPU API, in C (`gpu.c`) with its Metal shaders as source that
SDL compiles at start-up — no shader toolchain at build time. Lyra hands it a frame as
instances (`FLOATS_PER_INSTANCE`, 80 floats: quad, shape, radii, colours, kind, uv, the clip,
gradient stops) in one storage buffer, and commands (`DRAW` a run from a texture, `PUSH_LAYER`,
`POP_LAYER`, `BACKDROP`); `frame` draws them into a scene texture and blits it to the window.

- **Built by `bindings/gpu/build.sh`**, which `./build.sh` runs, into `build/lib/liblyra-gpu.a`
  on macOS with SDL3; elsewhere `gpu_stub.c`, whose `create` answers `None`, so `ui.gpu`
  falls back to SDL's 2D renderer. Linux (Vulkan, SPIR-V) and Windows wait for a shader
  toolchain (todo.md).
- **The window must not have an SDL_Renderer**: SDL_GPU claims it, which is why `ui.gpu`
  opens its own.
- `keep_frames` and `read` give the last frame back (a fence waited on), for tests.
- `LYRA_UI_RENDERER=sdl` refuses the GPU; `LYRA_UI_NOVSYNC` presents without vsync where the
  window allows it (for measuring).

## `bindings/pty/`

A program on a pseudo-terminal, for a terminal emulator (`../termina`). `spawn(args, cols,
rows, cwd, env) -> Maybe<Pty>` (`@must_release(close)`); `read()` never blocks — empty when
nothing is there, `None` once the terminal is closed; `write`/`write_bytes` loop through a full
buffer; `resize` (SIGWINCH); `exit_status()` reaps without waiting. macOS and Linux.

- **The C half is `pty.c`**, built by `build.sh` into `build/lib/liblyra-pty.a`: `TIOCSWINSZ`,
  `TIOCSCTTY`, `O_NONBLOCK` and the wait-status encoding all differ by platform, and the
  child's half of `fork` may only make system calls — so argv and the environment are built
  before it. `posix_openpt`, not `forkpty` (which needs libutil on older glibc).
- **The terminal closes before the program can be reaped**: `read()` answering `None` comes
  a moment before `exit_status()` answers — poll the second until it does.
- **SIGCHLD ignored is put back to the default** at the first `spawn`; inherited `SIG_IGN`
  makes the kernel reap children, and no exit status can ever be read.

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

## `bindings/harfbuzz.lyra` and `bindings/freetype.lyra`

`ui.text`'s shaping and rasterizing, one library each, both reading the same font file.
`open_hb_font(path, index)` / `shape(font, utf8, size) -> []ShapedGlyph` / `font_extents`:
HarfBuzz's 26.6 fixed point divided out, so a position is in the units of the size asked for,
and a glyph's `cluster` is a **byte** offset into the UTF-8 given; a variable font's axes
(`variation_axes`, `set_variations`), a metric by its OpenType tag with HarfBuzz's fallback
(`font_metric`: `METRIC_UNDERLINE_OFFSET` and the rest) and a character's advance
(`advance_of`). `open_ft_font(path, index)` / `render_glyph(font, glyph, pixels, hinting,
embolden, oblique) -> Maybe<GlyphBitmap>`: 8-bit coverage with its bearing from the pen,
hinted as `Hinting` says (`HintNone`, `HintLight` — the default — or `HintFull`), and for a
face a family lacks emboldened or slanted by FreeType's synthesis (`FT_GlyphSlot_Embolden`,
`FT_GlyphSlot_Oblique`) before it is rendered; `set_design_coordinates` for a variable
font's instance. **The two libraries each hold the font's variation**: set both, or HarfBuzz
shapes one weight while FreeType draws another. Both fonts are `@must_release`. **FreeType's glyph slot and
its bitmap are read by offset** (`FT_FaceRec.glyph` and three `FT_GlyphSlotRec` fields: no
function answers them), held to `offsetof` by `TestFreetypeOffsets`. `brew install harfbuzz
freetype` (Debian `libharfbuzz-dev libfreetype-dev`), found through `pkg: "harfbuzz"` and
`pkg: "freetype2"`.

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
