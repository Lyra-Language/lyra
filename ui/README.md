# `ui/` — the GUI framework

A declarative interface for Lyra programs, drawn by Lyra rather than by a widget toolkit:
`view` builds a tree of values, the framework lays it out and paints it as a **display
list**, and a renderer draws the list. The design (layers, phases, decisions) is the *Lyra
GUI — Design* document; this file is the map of what is built.

A root of its own beside `std/` and `bindings/` (`build.sh` links it into `build/`), so
`import ui.draw` resolves with no setup — not `std.ui` until its interface settles.

## Modules

| module | what | calls a library? |
|---|---|---|
| `ui.draw` | The display list: `DrawOp` (`Fill`, `Border`, `Glyphs`, `Picture`, `PushClip`, `PopClip`) and its geometry — `Point`, `Size`, `Rect`, `Radii` (CSS's fitting), `Color` (`rgb`/`rgba`), `Image`, `TextureId`, `GlyphQuad`. Logical units throughout. | no |
| `ui.tess` | Rounded rectangles and borders as triangles (`Mesh` of `Vertex`), edges softened over one pixel. In **pixels**: a renderer scales first. | no |
| `ui.backend` | The two seams: `Platform` (window, events, scale, text input, clipboard, cursor) and `Renderer` (`upload`, `update`, `draw(background, ops)`), and `InputEvent`, `Key`, `Modifiers`, `PointerButton`, `Cursor`. | no |
| `ui.sdl3` | Both traits over `bindings.sdl3`: `open` → `SdlWindow { platform, renderer }`, `close`, and `capture` (a frame read back, for tests). Draws through `SDL_RenderGeometry`. | SDL3 |

**Nothing above `ui.backend` calls a library.** A new backend — a GPU renderer, the web, a
phone's own layer — implements the two traits; the display list is the whole contract.

## Rules

- **Positions are logical units** everywhere above the backend. A platform divides by its
  `scale()` before it reports a position; a renderer multiplies by it before it draws.
  `ui.sdl3`'s unit is a point times the window's pixel density over its display scale.
- **Tessellate in pixels.** The softened edge is one pixel wide only in pixels, so a
  renderer scales a display list and then calls `ui.tess`.
- **Data constructors are matched program-wide by name**, so `ui`'s are chosen not to
  collide: `KeyPressed`/`KeyReleased` (SDL's `Event` has `KeyDown`/`KeyUp`), and keys named
  as the web's `KeyboardEvent.key` (`ArrowLeft`, not `Left`). An aliased import cannot
  separate two constructors of one name (LANGUAGE.md § Imports).
- **What SDL's 2D renderer cannot do**: a clip is a whole-pixel rectangle, so `PushClip`'s
  radii are ignored there; and its software renderer (headless, or no GPU) truncates each
  vertex to a whole pixel, so edges come out half a pixel soft.

## Examples and tests

- `examples/ui/shapes.lyra` — every op, in a window; `--frames 1 --png FILE` draws one frame
  to a PNG instead (headless with `SDL_VIDEODRIVER=dummy SDL_RENDER_DRIVER=software`).
- `cmd/lyrac/ui_test.go` — `TestRun_UiDrawsADisplayList` reads that frame a pixel at a
  time; `TestRun_UiGeometry` checks the pure geometry and mesh sizes. Neither needs a display.
