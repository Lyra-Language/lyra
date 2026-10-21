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
| `ui.draw` | The display list: `DrawOp` (`Fill`, `Border`, `Glyphs`, `Picture`, `PushClip`, `PopClip`) and its geometry — `Point`, `Size`, `Rect`, `Radii` (CSS's fitting), `Color` (`rgb`/`rgba`), `Image` (`pixelated` keeps its texels crisp when drawn larger), `TextureId`, `GlyphQuad`. Logical units throughout. | no |
| `ui.tess` | Rounded rectangles and borders as triangles (`Mesh` of `Vertex`), edges softened over one pixel. In **pixels**: a renderer scales first. | no |
| `ui.backend` | The two seams: `Platform` (window, events, scale, text input, clipboard, cursor) and `Renderer` (`upload`, `update`, `draw(background, ops)`), and `InputEvent`, `Key`, `Modifiers`, `PointerButton`, `Cursor`; and `NativeFields` (a phone's own text fields placed by identity, edits back as `FieldEdited`), declared and not yet implemented. | no |
| `ui.sdl3` | Both traits over `bindings.sdl3`: `open` → `SdlWindow { platform, renderer }`, `close`, and `capture` (a frame read back, for tests). Draws through `SDL_RenderGeometry`. | SDL3 |
| `ui.text` | `Fonts` (font files by family; `add_system_font` tries `SYSTEM_FONTS`), `FontSpec` (family, size, `line_height`), `lay_out` → `TextBlock` (lines of placed glyphs, cached by text, font and width), and `GlyphAtlas`, which renders glyphs into one texture through `Renderer.update` and answers a `Glyphs` op. | HarfBuzz, FreeType |
| `ui.style` | `Style` (all `Maybe`), `Insets`, `Rule`/`Sheet`, `Theme` (`Palette`, `Spacing`, radius, font, `elements` sheet; `light_theme`, `dark_theme`), and the cascade: `Cascade` settles an element's tag, classes, part and inline style into `Computed`. | no |
| `ui.layout` | `Node<e>` (kind, tag, class, part, style, children, key, label; `e` the program's event type, `NoEvent` for none) built by `row`, `column`, `expanded`, `stack`, `positioned`, `padding`, `align`, `sized`, `scroll`, `text`, `image`, `themed`, `component` and the controls `button`, `checkbox`, `slider`, `text_input`, and `.with_class`/`.with_style`/`.with_part`/`.keyed`/`.labelled`/`.aligned`; `layout`/`layout_with(node, Constraints, fonts, Interaction) -> Laid<e>` (each with its identity, role, label, focus ring and a field's caret) and `paint(laid, origin, scale, fonts, atlas, renderer) -> []DrawOp`. | no (text through `ui.text`) |
| `ui.interact` | `Ui`: the framework's state by identity (focus, hover, press, scroll offsets, carets, selections, undo), `frame` (lay out and drop what left the tree), `handle(laid, input, platform) -> []e`, `place_text_input`; `hit`, `control_at`, `find`, `focus_order`; `Editing`, a field's editing operations. | no |
| `ui.app` | `App { view, update: (s, e) -> (s, []Command<e>), subscriptions: (s) -> []Subscription<e>, background }` (`sandbox(view, update, bg)` for a plain update); `Command` (`WriteFile`, `ReadFile`, `After`, `CopyText`, `PasteText`, `OpenUrl`, `QuitApp`), `Subscription` (`EveryFrame`, `Every`, `OnResize`, `OnAppearance`); `run`, and `Host` (`host_of`, `step`) for a loop of `main`'s own — which `lyrac run --live` needs. | no |
| `ui.script` | `ScriptPlatform` (a `Platform` with no window and a clock of its own, playing one step a pass), `parse_script` (inputs: `"Tab 'Ada' Shift+Tab Primary+v click:10,20 scroll:1"`) and `parse_steps` (with `wait:N`, `resize:W,H`, `dark`, `light`). | no |

**Nothing above `ui.backend` calls a platform's library**; `ui.text` calls the two text
libraries, which every platform has. A new backend — a GPU renderer, the web, a phone's own
layer — implements the two traits; the display list is the whole contract.

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
- **A texture is smooth unless its `Image` is `pixelated`** (CSS's `image-rendering`), which a
  renderer reads at `upload`: pixel art sets it, glyphs and photos do not. `ui.sdl3` samples a
  pixelated texture with SDL 3.4's pixel-art mode, or nearest on an older SDL.
- **Layout is Flutter's protocol**: constraints down (a least and greatest width and
  height, `UNBOUNDED` a large number, not an infinity), sizes up, the parent places. A row
  or column with a limit along its axis fills it (Flutter's `mainAxisSize.max`) and shares
  what its fixed children leave among its `expanded` ones; a wrapper hands one child its own
  constraints and lays several out as a column. A row's child may overflow its width.
- **The cascade is five layers**, each beating the ones below whatever their specificity:
  the theme's `elements`, the global sheets (`themed`'s, outermost first, one layer), the
  enclosing component's scoped sheet (never reaching a component inside it; made from the
  theme in force where the component is `component_of`), part rules (`Card::label`, and the
  more specific `Card.big::label`, from the theme, the global sheets and the sheet of the
  component around the part's), and inline style. Within a layer the more specific (tag 1, class 10, both 11),
  then the later. `color`, `font` and `opacity` are inherited, opacity multiplied.
- **Text is shaped once a paragraph and broken afterwards**, at a simple reading of UAX #14's
  opportunities (spaces, a hyphen inside a word, ideographs; a newline is a paragraph), with
  trailing spaces hanging. One font, left to right: no fallback and no bidi yet (todo.md).
  A glyph is rasterized at the size times the scale and drawn from a whole pixel.
- **Events are data.** A control names the event its use stands for, of the program's type
  `e`; `ui.interact` answers the events an input raised and the program's pure `update` makes
  the next state. A control's value is the program's: the next frame shows what it kept.
  Phase 4's commands and subscriptions grow `App`, not the view.
- **Identity is a node's path from the root**, an index a step or a `.keyed` key in its place;
  `Ui` holds focus, hover, press, scroll offsets and a field's caret, selection and undo by
  it, and drops what belongs to an identity no longer in the tree.
- **A control's state is a class** — `hover`, `pressed`, `focus` — added for the cascade
  (`Button.hover`); pseudo-selectors (`:hover`) are deferred (todo.md). A focused control gets
  a ring in the theme's `focus` colour. A control's insides are parts (`Checkbox::box`,
  `Slider::thumb`, `TextInput::placeholder`); `control_rules` gives a theme its defaults.
- **The keyboard**: Tab/Shift-Tab through the controls in the tree's order, wrapping; Space
  and Enter press; the arrows, Page Up/Down, Home and End move a slider; a field edits by rune
  (not yet grapheme cluster), by word with Alt (Control off a Mac), to the ends with Command or
  Home/End, selects with Shift, and takes the primary modifier's A, C, X, V and Z.
- **The pointer's shape follows it**: an I-beam over a text field (and while a selection is
  dragged in one), the arrow elsewhere, set through `Platform.set_cursor` as it moves —
  `ui.sdl3` makes SDL's system cursor and changes it only when the shape does.
- **Commands and subscriptions**: `update` is pure and answers the next state and the
  `Command`s to carry out, each naming the event its result comes back as; the host performs
  them after `update` and delivers the results on the next pass (not through the platform:
  `InputEvent` carries no program event). `subscriptions(state)` is asked again after every
  update, so a subscription holds exactly while the state says; the loop waits for input only
  while no `EveryFrame` is on, and no longer than the next timer or `Every`.
- **`Node.map(f)`** lifts a component's own events into its parent's type (Elm's `Html.map`).
- **Live reload**: a program whose loop is `main`'s (`while host.step(…) {}`) takes `lyrac run
  --live`'s swaps — `view` and `update` are function values in `App`, so they reach the newest
  code — keeping the window, the framework's state and its own; under `--live` a pass waits
  at most 100 ms, so an idle window still reaches the swap. A change to the state's shape
  restarts it (lyrac says so). With `lui --watch` beside it, an edit to markup lands in the
  running window.
- **The platform's clock and look**: `now_ms`, `appearance` (and `AppearanceChanged`), and
  `open_url` are the platform's; `ui.sdl3` reads SDL's ticks and system theme.
- **Every element has a role and a label** (`Role`, AccessKit's), a control's label its
  visible text unless `.labelled`, for phase 5's bridge.
- **What SDL's 2D renderer cannot do**: a clip is a whole-pixel rectangle, so `PushClip`'s
  radii are ignored there; and its software renderer (headless, or no GPU) truncates each
  vertex to a whole pixel, so edges come out half a pixel soft.

## Examples and tests

- `examples/ui/shapes.lyra` — every op, in a window; `--frames 1 --png FILE` draws one frame
  to a PNG instead (headless with `SDL_VIDEODRIVER=dummy SDL_RENDER_DRIVER=software`).
- `examples/ui/settings.lyra` — phase 2's gate, a static settings screen: text, rows and
  columns, a stack, components styled through their parts, a dark-themed card. `--boxes`
  prints where each element with a class was laid out.
- `examples/ui/components_view.lui` — phase 4's gate: the `bindings.imgui.ui` example's
  interface as markup for the `ui` target (a global sheet of `$tokens`, components with parts,
  a command, a timer, the system's look followed), run by `examples/ui/components.lyra`, whose
  loop is `main`'s (`lyrac run --live`); `--script` plays steps headless.
- `examples/ui/form.lyra` — phase 3's gate, a form that works from the keyboard: two
  fields, a checkbox, a slider and Submit, run by `ui.app`. `--script "…"` plays input
  instead of waiting for a user and prints the state it left; `--png FILE` the last frame.
- `cmd/lyrac/ui_test.go` — `TestRun_UiDrawsADisplayList` reads the shapes frame a pixel at a
  time; `TestRun_UiGeometry` checks the pure geometry and mesh sizes; `TestRun_UiLayout` and
  `TestRun_UiCascade` the layout and the cascade's order, with no font; `TestRun_UiLaysOutAScreen`
  the settings frame against its boxes; `TestFreetypeOffsets` the binding's offsets;
  `TestRun_UiInteraction` hit-testing, identity, the keyboard, a field's editing and roles,
  scripted with no window; `TestRun_UiFormFromTheKeyboard` the form played by its script;
  `TestRun_UiCommandsAndSubscriptions` timers, subscriptions on and off, a file round trip,
  the clipboard, frames, `map` and quitting; `TestRun_UiComponentsPort` the markup example from
  the keyboard; `TestLive_SwapsAUiViewAndRestartsOnItsState` (`live_test.go`) a view swapped
  into a running host, and a restart on the state's shape. None
  needs a display; the ones that draw need SDL3 and a font, and all need HarfBuzz and FreeType.
