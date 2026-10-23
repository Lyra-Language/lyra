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
| `ui.draw` | The display list: `DrawOp` (`Fill`, `Border`, `Glyphs`, `Picture`, `PushClip`, `PopClip`, and the effects `Shadow`, `GradientFill`, `Backdrop`, `PushLayer`/`PopLayer` with `BoxShadow`, `Gradient` and its `ColorStop`s, `LayerEffect`) and its geometry — `Point`, `Size`, `Rect`, `Radii` (CSS's fitting), `Color` (`rgb`/`rgba`), `Image` (`pixelated` keeps its texels crisp when drawn larger), `TextureId`, `GlyphQuad`. Logical units throughout. | no |
| `ui.tess` | Rounded rectangles and borders as triangles (`Mesh` of `Vertex`), edges softened over one pixel. In **pixels**: a renderer scales first. | no |
| `ui.backend` | The two seams: `Platform` (window, events, scale, text input, clipboard, cursor) and `Renderer` (`upload`, `update`, `draw(background, ops)`), and `InputEvent`, `Key`, `Modifiers`, `PointerButton`, `Cursor`; `AccessTree`/`AccessNode`/`AccessRole`/`AccessRequest` and `ACCESS_*`, for `Platform.update_access` and the `AccessAction` input; and `NativeFields` (a phone's own text fields placed by identity, edits back as `FieldEdited`), declared and not yet implemented. | no |
| `ui.gpu` | The `Renderer` on the GPU (`bindings.gpu`: SDL3's GPU API, Metal shaders), and `open` → `GpuWindow { platform, renderer: AnyRenderer }`, the GPU's renderer where there is one and `ui.sdl3`'s where not (or with `LYRA_UI_RENDERER=sdl`); `close`, `on_gpu`, `keep_frames`/`read_frame` (a frame read back), `prepare` (a frame's cost). | SDL3 (GPU) |
| `ui.sdl3` | Both traits over `bindings.sdl3`: `open` → `SdlWindow { platform, renderer }` (and its pieces, `open_window`, `renderer_for`, `platform_for`, which `ui.gpu` opens with), `close`, and `capture` (a frame read back, for tests). Draws through `SDL_RenderGeometry`, a call a run of ops on one texture (`geometry_calls` counts them); on macOS tells VoiceOver through `bindings.accesskit`. | SDL3, AccessKit |
| `ui.text` | `Fonts` (a family's faces by weight and style, `add(family, path, index, weight, italic)`; `add_system_font` tries `SYSTEM_FONTS`; `hinting`), `FontSpec` (family, size, `line_height`, `weight`, `italic`), `metrics` → `FontMetrics` (ascent, descent, advance, underline and strikeout), `lay_out` → `TextBlock` (lines of placed glyphs, cached by text, font and width), and `GlyphAtlas`, which renders glyphs into pages — a texture each, `ATLAS_SIZE` texels a side — through `Renderer.update` and answers a `Glyphs` op a page; `next_frame`, which `paint` calls, and `glyph_atlas_sized` for small pages. | HarfBuzz, FreeType |
| `ui.style` | `Style` (all `Maybe`), `Insets`, `Rule`/`Sheet`, `Theme` (`Palette`, `Spacing`, radius, font, `elements` sheet; `light_theme`, `dark_theme`), its layout properties (sizes, flex's and grid's — `FlexDirection`, `LineWrap`, `Justify`, `ItemAlign`, `Basis`, tracks built by `px`, `fr`, `auto_track`, `min_content`, `max_content`, `minmax`, `repeat_tracks`, `GridSpan`, `GridFlow`), its effects (`box_shadow`, `background_gradient`, `filter_blur`, `backdrop_blur`), and the cascade: `Cascade` settles an element's tag, classes, part and inline style into `Computed`. | no |
| `ui.layout` | `Node<e>` (kind, tag, class, part, style, children, key, label; `e` the program's event type, `NoEvent` for none) built by `row`, `column`, `flex`, `grid`, `expanded`, `stack`, `positioned`, `padding`, `align`, `sized`, `scroll`, `text`, `image`, `themed`, `component`, `painted` (an element the program draws: a `Painter` asked for `PaintOp`s — `PaintDraw`, `PaintText` — for a `PaintArea`) and the controls `button`, `checkbox`, `slider`, `select`, `text_input`, and `.with_class`/`.with_style`/`.with_part`/`.keyed`/`.labelled`/`.aligned`; `layout`/`layout_with(node, Constraints, fonts, Interaction) -> Laid<e>` (each with its identity, role, label, focus ring and a field's caret) and `paint(laid, origin, scale, fonts, atlas, renderer) -> []DrawOp` (`fonts` `mut`: a painter's text is laid out as it is painted). Flex and grid are in `ui/layout/flex.lyra` and `grid.lyra` (one module, a directory). | no (text through `ui.text`) |
| `ui.motion` | Transitions' values: `progress` (CSS's timing functions), `blended`/`differs` (each property that animates), `mix_color` (premultiplied), and an element's `Motion` — its target and what is `Moving` towards it — moved on by `advance`; `with_layout`/`layout_key` for the sizes a layout lays out while they move. | no |
| `ui.interact` | `Ui`: the framework's state by identity (focus, hover, press, scroll offsets, carets, selections, undo), transitions (`motions`, shown at `now`; `animating`), `frame` (lay out and drop what left the tree), `handle(laid, input, platform) -> []e`, `place_text_input`; `hit`, `control_at`, `find`, `focus_order`; `Editing`, a field's editing operations. | no |
| `ui.app` | `App { view, update: (s, e) -> (s, []Command<e>), subscriptions: (s) -> []Subscription<e>, background }` (`sandbox(view, update, bg)` for a plain update); `Command` (`WriteFile`, `ReadFile`, `After`, `CopyText`, `PasteText`, `OpenUrl`, `QuitApp`), `Subscription` (`EveryFrame`, `Every`, `OnResize`, `OnAppearance`); `run`, and `Host` (`host_of`, `step`) for a loop of `main`'s own — which `lyrac run --live` needs. | no |
| `ui.access` | `access_tree(laid, focus) -> AccessTree`: the laid-out tree as assistive technology hears it, a node for each paragraph, image, scroll and control (and labelled group), ids its identities hashed; `access_id`, `access_node`. | no |
| `ui.script` | `ScriptPlatform` (a `Platform` with no window and a clock of its own, playing one step a pass), `parse_script` (inputs: `"Tab 'Ada' Shift+Tab Primary+v click:10,20 double:10,20 scroll:1"`) and `parse_steps` (with `wait:N`, `resize:W,H`, `dark`, `light`). | no |

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
- **Flex and grid are CSS's** (`flex`, `grid`), inside the same protocol, configured wholly
  by style properties so a sheet can set them (`Flex.cards { flex_wrap: Wrap }`). Flex follows
  CSS Flexbox §9: an item's base size (its `basis`, else its `width`, else its max-content
  width), its automatic least size (its min-content width, none for a scroll), lines for
  `flex_wrap`, the freeze loop growing or shrinking each line with every item's least and
  greatest, then `align_content`, `align_items`/`align_self` (baseline by first lines of
  text) and `justify_content`, reversed for the `-reverse` directions; `order` sorts items,
  ties in the tree's order. Grid follows CSS Grid §8 and §12: placement by lines, then items
  locked to a row, then the rest by `grid_flow` (dense optional); column tracks sized from
  the items' min- and max-content widths, rows from their heights at those widths; fixed,
  intrinsic, maximized, `fr` and stretched `auto` tracks; items aligned in their areas. **A
  container's width is definite when bounded and its height only when fixed**, as a CSS block's
  is — so `fr` rows and a growing column need a `height`. **Every element honours `width`,
  `height` and their `min_`/`max_`** (border-box; within its parent's constraints, which
  win). Row, Column and Expanded stay as they are — Flutter's, measuring children unbounded —
  beside them. **Intrinsic widths** (`intrinsic_width`) are measured outside the layout cache,
  so the cache keeps only real layouts; an unchanged flex container or grid is given back
  whole like any element.
- **Overflow is CSS's** (`overflow_x`, `overflow_y`: `OverflowVisible`, `OverflowHidden`,
  `OverflowClip`, `OverflowScroll`, `OverflowAuto`; lui's `overflow` takes one or two). An axis
  that is not visible clips, and lays its content out unbounded down (`max_h` `UNBOUNDED`) or,
  across, at least its narrowest — the width below which it overflows, a flex row's items at
  their least, so an item's `min-width: 0` lets the row fit as it would in a browser, which
  never widens it (`narrowest_of`); the element's own size stays within its
  constraints. The content's size and the offset are the `Laid`'s `scroller`. **Offsets live in
  `Ui` by identity** (`scroll` down, `scroll_x` across) and are clamped to the content at every
  layout, so a window grown past the end pulls back; `clip` and `hidden` take no input but a
  program's own offset, `hidden` scrolls by focus. **The wheel goes to the innermost scroller
  under the pointer that can still move on that axis, then bubbles** — out to the window — and
  Shift+wheel is across (`ui.sdl3`). A thumb drags, a track click pages, and the arrows, Page
  Up/Down, Home and End scroll the scroller under the pointer or around the focus (not inside a
  slider or a field); Tab scrolls what it focuses into view in each scroller around it.
  **Scrollbars overlay the content**, 8 thick and 2 in from the edge, a thumb at least 20 long,
  styled as the elements `ScrollTrack` and `ScrollThumb` (`:hover`, `:active`), their
  defaults `scrollbar_rules` — a layer beneath the theme, applied only to a scroller's bars; `scroll` shows them always, `auto` when the content is larger. A
  hit-test only finds what is inside every clip around it. **A scroller lends its parent no
  min-content width beyond its padding**, as CSS's, and has no automatic least size in flex.
  **The window scrolls**: the host lays the root out tight to the window and gives it
  `App.window_overflow` (`OverflowAuto` unless set; lui's `<window overflow>`), which the
  root's own style beats. `scroll(child, axis)` is a wrapper with `overflow` on its axis, which
  fills that axis when bounded (Flutter's).
- **Transitions are CSS's** (`Style.transition: []Transition` — a property by its CSS name or
  `all`, a duration and delay in milliseconds, a `Timing`; lui's `transition` and longhands).
  When a property an element's transition names changes its computed value — a class, a state,
  the program's state, `@media` on a resize — it moves from **what is shown now** to the new
  value (so a change back mid-way retargets from where it is); one not named jumps, and the
  first frame an element is seen moves nothing. Opacity, colours (premultiplied, as CSS),
  radius, shadows (pair by pair, padded with transparent ones), a gradient of one kind and
  stop count, and both blurs **only change what is drawn**: `Ui.frame_within` blends them into
  the laid-out tree after layout — an opacity multiplied into everything inside, a colour
  passed to text that inherits it — and the layout cache gives the element back untouched.
  Padding, gaps, sizes and border width **change sizes**: they reach the next layout through
  `Interaction.shown`, keyed into the cache, so a moving element lays out each frame (and at
  once on the frame it starts, never showing its target first). An `auto` size, gradients of
  different shapes and shadows inset on one side only switch at the midpoint. State is by
  identity in `Ui.motions`, dropped with the element; the clock is `Platform.now_ms`, which the
  host hands `Ui.now`. **The host draws every frame while anything moves and idles once it has
  arrived**; `ScriptPlatform`'s `wait:N` steps its clock a frame at a time, so a script plays a
  transition deterministically.
- **Media rules** (`Rule.media`, CSS's `@media`): a rule may hold while the viewport —
  the window's logical size and the system's look (`Viewport`, which the host passes in
  `Interaction.viewport`) — meets any of its `Media` conditions (min/max width and height,
  orientation, colour scheme, each reversible by `not`). **A media rule stays where it is
  written**, in its layer, with its specificity and its place in its sheet's order — CSS's
  rule, chosen so a breakpoint never reorders the cascade. Sheets are filtered as a subtree's
  cascade is built (`Sheet.at`). **The layout cache keys on what the media answered**, not on
  the window's size: each `Laid` carries the queries its subtree's sheets asked
  (`Laid.media`), and an element is given back only while they answer the same — so a resize
  that crosses no breakpoint relays out only what its new constraints change.
- **A selector list** (`Button.primary, Checkbox::box, .card`) is a rule for each selector,
  in the same place, each with its own specificity.
- **The window's least and greatest size** are `App.min_size` and `App.max_size`, which the
  host hands `Platform.set_size_limits` on its first pass (`ui.sdl3`: SDL's minimum and maximum
  window size; `ScriptPlatform` holds its resize steps to them). The size it opens at is
  `main`'s, given to `gpu.open`/`sdl3.open`. **A `WindowSpec`** (`ui.app`) holds a window's
  title, opening size, limits, resizing and overflow — lui's `<window>` writes a function
  answering one, `window_spec` — and `ui.gpu.open_app(window_spec, view, update,
  subscriptions, state)` opens it: the system's font, the window on the GPU (or SDL's 2D
  renderer), the `App` (`app_in`) and its host, leaving `main` its own `while
  step_running(window) {}` (where `--live` swaps code in) and `close_app`.
  `examples/ui/7GUIs/` drive their markup so. **The function is kept** (`App.window`): under
  `--live` the host calls it each pass and makes a change — a reload's — to the open window
  (`follow_window`: `Platform.set_title`, `set_window_size`, the limits lifted, the window
  resized, and set again). **`App.resize_x` and `App.resize_y`** (both `true` unless set; lui's `resize`,
  `resize_x`, `resize_y`) hold a side that may not resize to the size the window opened at,
  within those limits: the host hands that side's least and greatest as one size
  (`Host.limit_size`), and `ui.sdl3` makes a window held so both ways not resizable at all.
- **Live resizing, every program's**: AppKit holds the main thread while a window's edge is
  dragged, so a program's loop gets nothing until the drag ends. `ui.app`'s `run` — and a
  program with its own loop, through `running`, `follow_resizes` and `step_running` — hands the
  platform a hook (`Platform.on_live_resize`) that redraws the running window, held in one
  `shared` box so the hook and the loop step the same host. `ui.sdl3` owns the one SDL event
  watch that calls it, on both renderers. **The hook draws only at a size not yet shown, and
  never from inside its own frame**: SDL asks sixty times a second for as long as AppKit says
  the resize lasts, and answering each with a whole frame froze the window. `ui.gpu` rebuilds
  its scene, layer and blur textures at the new size, and the window's Metal layer is pinned
  top-left (`bindings.gpu`'s `pin_contents`, both renderers) so AppKit never shows the last
  frame stretched while the next is drawn. `ScriptPlatform` plays a drag
  (`drag:W,H`) the same way, the hook called from inside its wait.
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
- **A painted element is drawn by the program** (`painted(paint, on_size)`, `ui/TERMINAL.md`
  §1): laid out as a box — its style's size, else all the room it is given, so a tall one in a
  scroller needs a `height` and a full-window one a window that does not scroll
  (`window_overflow: OverflowVisible`) — and asked at **paint**, every redraw, for its ops in
  its own coordinates: its size, the part every clip around it leaves (`PaintArea.visible`,
  what a scroller shows, all it need draw) and the scale. Its ops are moved to its place and
  clipped to its box; `PaintText` is shaped as a `text` element's, placed by its first
  baseline. **The layout cache keeps its box, never its paint**: a reused element carries the
  newest function, so it draws the newest state. **`on_size` is heard before the size is
  painted**: the host raises it after a layout that changed the element's size (by identity)
  and lays out again (`Host.lay_out_view`, at most `SIZE_ROUNDS` times).
- **Faces are CSS's**: a family is the faces added under its name, each a weight (400
  regular, 700 bold) and italic or not, and a `FontSpec` is drawn in the one CSS Fonts 4's
  matching picks — its italic or upright faces as asked if there are any, else the others; then
  the nearest weight in CSS's order (from 400 to 500 up to 500 first, then down, then above;
  below 400 down first; above 500 up first). **What the family lacks is made, once**: a
  variable font with a `wght` axis is its instance at the weight (HarfBuzz's variations and
  FreeType's design coordinates set together), a bold of 600 or more drawn from a lighter face
  is emboldened, an italic from an upright one slanted (FreeType's synthesis; the advance
  unchanged). `metrics(spec, scale)` reads a face's own tables (HarfBuzz's metrics, with its
  fallbacks), its advance the `M`'s, whole pixels while glyphs are hinted; `MeasureFont` asks it
  of a host. Glyphs are hinted lightly unless a program sets `Fonts.hinting` — ui.text asks no
  platform which it is on, so macOS's unhinted look is the program's to choose.
- **The glyph atlas is pages**: a glyph that fits on none opens one, and past `ATLAS_PAGES`
  the page drawn from least recently is cleared and filled again — never one the frame being
  painted has drawn from, since that frame's ops still show it (a frame needing more pages
  than the limit gets more). A slot carries its page's generation, so an emptied page's glyphs
  are rendered again when next drawn and never found stale. Renderers have no way to free a
  texture, so pages are reused, not released.
- **Events are data.** A control names the event its use stands for, of the program's type
  `e`; `ui.interact` answers the events an input raised and the program's pure `update` makes
  the next state. A control's value is the program's: the next frame shows what it kept.
  Phase 4's commands and subscriptions grow `App`, not the view.
- **Identity is a node's path from the root**, an index a step or a `.keyed` key in its place;
  `Ui` holds focus, hover, press, scroll offsets and a field's caret, selection and undo by
  it, and drops what belongs to an identity no longer in the tree.
- **A state is a pseudo-class, as CSS's**: `:hover` and `:active` (pressed) on the element and
  every one around it — the deepest under the pointer and its ancestors, as CSS's — `:focus`,
  `:focus-visible` (the focus came from Tab or assistive technology, not a click),
  `:focus-within` (it or something in it has the focus), `:checked` (a ticked checkbox), and
  `:read-only`/`:read-write` (a text field), and `:disabled`/`:enabled` (a control, by
  `Node.disabled_if`). A selector puts them on a compound
  (`Button.primary:hover`, `:hover` alone) or a part (`Slider:hover::thumb`, the component's;
  `Slider::thumb:active`, the part's), each — like a class — adding 10 to its specificity.
  **A class is only ever the program's**: the framework adds none, and a class named `hover` is
  just a class (lui refuses one, as a mistake). **A disabled control takes no input** — not
  pressed, not focused, skipped by Tab, its accessibility actions ignored — but is hovered
  like any element, and gives up the focus or press it held when it becomes disabled. A focused control gets
  a ring in the theme's `focus` colour. A control's insides are parts (`Checkbox::box`,
  `Slider::thumb`, `TextInput::placeholder`); `control_rules` gives a theme its defaults.
- **A read-only field** (`FieldSpec.read_only`, `read_only_input(value)`, lui's `readonly`) is
  HTML's `readonly`: focused, its caret moved, selected and copied, but typing, deleting,
  cutting, pasting, undo and an assistive technology's new value raise no edit, and it needs
  no `on_edit` (`read_only_input` gives it one that is never called).
- **A select** (`select(SelectSpec { options, selected, on_change })`, lui's `<Select>`) is
  HTML's `<select>`: its chosen option and an arrow (`Select::arrow`, bars narrowing to a
  point — the display list has no paths), as wide as its widest option. A click, or Space,
  Enter or Alt-Down, opens its list (`Select::list` of `Select::option`s, `:checked` the
  chosen, `:hover` the one under the pointer or the arrow keys); a click or Enter chooses,
  Escape, Tab or a click elsewhere closes; closed, the arrows choose outright. Choosing the
  chosen option raises nothing. Which select is open is the framework's
  (`Interaction.open`, one at a time). **The list floats** (`Laid.floats`): laid out under
  the select taking none of its size, painted after the whole tree outside every clip, and
  hit before anything else (`hit` looks for floating elements first) — so it covers what is
  below it, inside a scroller too. Heard as a combo box (its value the chosen option) with
  a list box of options while open. It does not yet scroll or open upwards (todo.md).
- **The keyboard**: Tab/Shift-Tab through the controls in the tree's order, wrapping; Space
  and Enter press; the arrows, Page Up/Down, Home and End move a slider; a field edits by rune
  (not yet grapheme cluster), by word with Alt (Control off a Mac), to the ends with Command or
  Home/End, selects with Shift, and takes the primary modifier's A, C, X, V and Z.
- **A field's clicks**: one puts the caret there and a drag selects by rune; a double click
  selects the word under the pointer (or the run of spaces it is on) and a drag goes on by
  whole words either way; a triple click selects the whole text. The count is the
  platform's, at the system's double-click speed (`PointerDown`'s third field, SDL's
  `clicks`); a script writes `double:X,Y` and `triple:X,Y`. A double click takes a run of
  one kind — letters and digits (`_` and non-ASCII too), punctuation, or spaces — so `34`
  alone in `12.34`. (Alt-arrow still moves by spaces only.)
- **The pointer's shape follows it**: an I-beam over a text field (and while a selection is
  dragged in one), the pointing hand over a button that can be pressed, the arrow elsewhere,
  set through `Platform.set_cursor` as it moves —
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
  visible text unless `.labelled`. `ui.access` reads the laid-out tree into an `AccessTree`
  the host hands `Platform.update_access` after each frame: wrappers and unlabelled rows and
  columns give their children to their parent, a control is one node (its insides its label),
  and a node's id is its identity hashed, so it is the same from frame to frame. What
  VoiceOver asks comes back as `AccessAction(identity, request)`, handled as the keyboard
  would be — focus, press, step — or a field's text or a slider's number set outright.
  **The tree is in logical units; AccessKit's macOS adapter takes the window's pixels**, from
  its content's top left, so `ui.sdl3` scales each node's bounds by `scale()` at the seam
  (`access_bounds`), as its renderer scales a display list. Given points, VoiceOver's
  highlight on a Retina display was half size.
- **A frame is drawn only when something may have changed**: an input, an event, the first
  pass — or every pass under `--live`. An idle window costs no CPU (`Host.frames`,
  `Host.layouts` count).
- **Layout is cached by identity** (`layout_cached`, `LayoutCache`, which `Host` keeps): an
  element whose node, constraints, cascade and state are as last frame gives back its last
  `Laid`, the theme in the key. Shaped text is cached too, at most `LAID_LIMIT` paragraphs
  a generation, two generations kept.
- **Effects, CSS's**: `box_shadow` (offset, blur, spread, colour, `inset`, several, the first on
  top), `background_gradient` (linear by angle, radial as circle or ellipse, colour stops at
  places or spread evenly, mixed premultiplied), `filter_blur` (the element and its insides
  drawn into a layer and blurred) and `backdrop_blur` (what is behind it blurred). Painted in
  CSS's order: a filter's layer, outer shadows, backdrop, colour, gradient, inset shadows,
  border, content. A blur radius is CSS's: the Gaussian's deviation is half of it.
- **Two renderers**: `ui.gpu` draws every op exactly on the GPU — a shape's coverage its rounded
  box's signed distance (corners, borders, rounded clips), a shadow the analytic Gaussian of
  its box, a gradient per pixel with up to eight stops (more resampled), and blur as two
  passes at half size; one instanced draw a frame unless a second texture is sampled.
  **`ui.sdl3` approximates**: a shadow's falloff is a straight ramp and an inset one a soft
  band; a gradient is vertex colours on the outline and its middle (a two-stop linear one
  exact); a clip is a whole-pixel rectangle, so `PushClip`'s radii are ignored; blur is left
  out, said once a run on stderr; and its software renderer (headless, or no GPU) truncates
  each vertex to a whole pixel, so edges come out half a pixel soft. Both draw the existing
  screens alike (the Layouts demo: a mean difference of 0.0015 of 255 a channel).

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
- `examples/ui/layouts_view.lui` — flex and grid in markup, responsive: a card list wrapping
  to the window's width, a toolbar standing its buttons in a column below 560, and a dashboard
  grid of one, two or four columns by `@media`, with a `<window>` least size and a dark look
  by `prefers-color-scheme`; run by `examples/ui/layouts.lyra`, which redraws during a live
  resize (`--width W --script "…" --png FILE` headless).
- `examples/ui/effects_view.lui` — shadows, gradients, blur and transitions (a card's shadow, the frost coming and going, a blurred line focusing and dots fading under the pointer): a gradient header, cards whose
  shadows lift under the pointer, a frosted panel over a busy background, a blurred line; run by
  `examples/ui/effects.lyra` on the GPU (`--png FILE` reads a GPU frame back).
- `examples/ui/overflow_view.lui` — overflow: a list scrolled down, a strip across, a canvas
  both ways with bars always, a hidden box, a scroller inside another, in a page taller than
  its window, which scrolls; run by `examples/ui/overflow.lyra` (`--script "…" --png FILE`
  headless).
- `examples/ui/painted.lyra` — elements the program paints: a chessboard filling the window,
  as many squares as fit, its count heard through `on_size`, beside a list of 200 rows in a
  scroller whose painter draws only the rows in view (`--script "…" --png FILE` headless).
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
  Phase 5's: `TestRun_UiCacheAndIdle` (what the layout cache lays out and gives back; an idle
  host draws nothing), `TestRun_UiBatchesGeometryCalls` (the draws a frame takes),
  `TestRun_UiAccessibilityTree` (a form's tree, and VoiceOver's actions driving it).
  `TestRun_UiMediaQueries` breakpoints, a media rule against specificity, `not` and a comma,
  and a host resized within a breakpoint (given back) and across one (relaid out);
  `TestRun_UiWindowSizeLimits` the window held to its least and greatest size;
  `TestRun_UiWindowResizeAxes` a side, then both, held from resizing;
  `TestRun_UiWindowFitsAShrinkingFlexItem` a row whose field asks `min-width: 0` fitting its
  window, and overflowing it without;
  `TestLui_UiTargetWindowSize` the markup's `<window width height resize resize_x resize_y>`;
  `TestRun_UiLiveResize` a drag redrawing at each new size from inside the wait.
  `TestRun_UiEffectsInTheCascade` the effects' order and a compound selector and hover through a
  child; `TestRun_UiFallbackShadowsAndGradients` the fallback's pixels; `TestRun_UiGpuReadback`
  a GPU frame read back — a shadow's falloff, gradient stops, a blurred edge (macOS with a GPU).
  `TestRun_UiOverflow` scrollers' sizes, a click through a clip, the wheel on both axes
  bubbling to the window, a thumb drag, a track page, the keys, Tab revealing, and the clamp on
  resize; `TestLui_UiTargetOverflow` the markup's `overflow` and `<window overflow>`.
  `TestRun_UiTransitions` the timing curves, premultiplied colour, retargeting, a delay, a
  named list against `all`, an opacity fading a child, no layout for paint-only properties and
  idle on arrival; `TestRun_UiTransitionsDrawOnlyWhileMoving` the host's frames;
  `TestLui_UiTargetTransitions` the markup's.
  `TestRun_UiPseudoClasses` the states; `TestRun_UiReadOnlyField` a read-only field;
  `TestRun_UiSelect` a select by pointer and keys, its list over a button, and what is heard;
  `TestRun_UiDisabledIsLaidOutAtOnce` a control disabled with nothing else changed;
  `TestLui_UiTargetReadOnlyField` the markup's `readonly`.
  `TestRun_UiPaintedElement` a painter's area, its ops moved and clipped, its text, the newest
  function on a cached layout, and `on_size` heard before each frame; `TestRun_UiPaintedExample`
  the example's board, a pixel at a time; `TestRun_UiAtlasPages` the atlas's pages opened,
  emptied (never under the frame that drew from them) and grown, on pages of one glyph each;
  `TestRun_UiFontFaces` CSS's face matching, faces made for what a family lacks, hinting as
  part of a glyph, metrics through `MeasureFont`, and a variable font's weights where there is
  one.
  `TestRun_UiFlex` and `TestRun_UiGrid` hold flex and grid to hand-worked CSS results, child
  rect by child rect; `TestLui_UiTargetFlexAndGrid` the markup's layout properties.
