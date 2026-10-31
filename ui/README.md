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
| `ui.draw` | The display list: `DrawOp` (`Fill`, `Border`, `Glyphs`, `Picture` (a texture's texels into a rectangle, at an opacity), `PushClip`, `PopClip`, and the effects `Shadow`, `GradientFill`, `Backdrop`, `PushLayer`/`PopLayer` with `BoxShadow`, `Gradient` and its `ColorStop`s, `LayerEffect`) and its geometry — `Point`, `Size`, `Rect`, `Radii` (CSS's fitting), `Color` (`rgb`/`rgba`), `Image` (`pixelated` keeps its texels crisp when drawn larger), `PictureSource` (a picture a program names: a key, its image, a revision), `TextureId`, `GlyphQuad`. Logical units throughout. | no |
| `ui.color` | CSS Color 4's text — `parse_color` (`#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb(…)`, `oklab(…)`, `oklch(…)`, each with an alpha) and `color_text` (`#rrggbb`, `#rrggbbaa` when translucent) — and the spaces a picker chooses in: `Lab`/`oklab`/`to_oklab`, `Lch`/`oklch`/`to_oklch` (a colour outside sRGB brought in by CSS's gamut mapping, its chroma lowered; `in_gamut`), `Hsv`/`hsv`/`to_hsv`. | no |
| `ui.tess` | Rounded rectangles and borders as triangles (`Mesh` of `Vertex`), edges softened over one pixel. In **pixels**: a renderer scales first. | no |
| `ui.backend` | The two seams: `Platform` (windows, events, scale, text input, clipboard, cursor) and `Renderer` (`upload`, `update`, `draw(window, background, ops)`, `release` — a texture let go, nothing by default — and `window_closed`, what it made for a window let go, nothing by default), and **several windows** (below): `WindowId`, `MAIN_WINDOW` (the one a platform opens with), every call about one window naming it (`window_size(window)`, `scale(window)`, `set_text_input(window, …)`, `set_title`, `window_position`, `set_window_position`, `change_window`, …), `next_events` answering each input with its window (`[](WindowId, InputEvent)`), `open_window(NewWindow { title, size, place })` → `Maybe<WindowId>`, `close_window(window)`, `to_screen`/`from_screen` (a window's point and the screens' in the system's coordinates; `None` where windows are not placed), `on_live_resize((WindowId) -> void)`; `CloseRequested` is a window's close and `QuitRequested` the program's quit, `WindowPlaced(at)` a window moved; and `InputEvent`, `Key`, `Modifiers`, `PointerButton`, `Cursor`; `AccessTree`/`AccessNode`/`AccessRole`/`AccessRequest` and `ACCESS_*`, for `Platform.update_access` and the `AccessAction` input; and `NativeFields` (a phone's own text fields placed by identity, edits back as `FieldEdited`), declared and not yet implemented. `Key` has `Insert` and `FunctionKey(n)` (F1–F24) beside the web's names; `InputEvent` has `TextComposing(text, caret, length)` (an input method's composition) and `WindowFocused`/`WindowUnfocused`; `Platform.held()` is the modifiers held now, which a pointer event carries none of. `Platform.wake_for(fds)` wakes its wait when a descriptor has data (`ui.sdl3`: `bindings.wake`'s thread). **A see-through window**: `Platform.set_see_through(window, on)` — on macOS at any time, the window and its Metal layers made non-opaque by `bindings.gpu`'s shim, SDL's content view's black layer kept clear and the title bar given an opaque backing in the window's background colour, since SDL's GPU API refuses a window made `SDL_WINDOW_TRANSPARENT`; elsewhere only a window opened so (`open`'s `see_through`) — and `set_backdrop_blur(window, radius)`, what is behind it blurred (macOS: by Core Animation's backdrop layer — `NSVisualEffectView`'s — on a view under the window's content, so it is cut to the window's rounded corners as the content is; the window server's own blur, CoreGraphics' private `CGSSetWindowBackgroundBlurRadius`, blurred a region with other corners and drew a rim round it past the window's, and is only the fallback; both looked up at run time. A window with the backdrop can be captured only with screen-recording permission, as it shows other windows' pixels); `Command.WindowSeeThrough` and `WindowBlur` ask them. `Platform.window_position(window)` is where the window is on its screens (`None` where the system does not say) and `set_window_position(window, at)` moves it there only if a display shows that place. | no |
| `ui.gpu` | The `Renderer` on the GPU (`bindings.gpu`: SDL3's GPU API, Metal shaders; its clear colour premultiplied, as all it draws is) — **one device for every window**, the first claimed as it opens and each other the first time it is drawn (`gpu.claim`, a swapchain and offscreen targets of its own; `gpu.frame_in`), the textures the device's — and `open` (`see_through` for a window showing what is behind it where a frame is translucent) → `GpuWindow { platform, renderer: AnyRenderer }`, the GPU's renderer where there is one and `ui.sdl3`'s where not (or with `LYRA_UI_RENDERER=sdl`); `close`, `on_gpu`, `keep_frames`/`read_frame` (a frame read back), `prepare` (a frame's cost). | SDL3 (GPU) |
| `ui.sdl3` | Both traits over `bindings.sdl3`: `open` → `SdlWindow { platform, renderer }` (and its pieces, `first_window`, `renderer_for`, `platform_for`, `close_platform`, which `ui.gpu` opens with), `close`, and `capture`/`capture_in(renderer, window, …)` (a frame read back, for tests). `SdlPlatform` routes SDL's events by the window they are about (`bindings.sdl3`'s `poll_window_event`): a window beside the main one is numbered by SDL's own id for it, opened hidden, placed and shown. `SdlRenderer` draws another window through an SDL renderer of its own, holding a copy of each texture made from what the texture holds (`images`, kept for it). Draws through `SDL_RenderGeometry`, a call a run of ops on one texture (`geometry_calls` counts them); on macOS tells VoiceOver through `bindings.accesskit`. | SDL3, AccessKit |
| `ui.text` | `Fonts` (a family's faces by weight and style, `add(family, path, index, weight, italic)` — a face as data, `FontFile`, for `ui.app`'s `LoadFonts`; `add_system_font` tries `SYSTEM_FONTS`; `fallbacks`, `add_fallback`, `add_system_fallbacks` from `SYSTEM_FALLBACKS`; `hinting`), `FontSpec` (family, size, `line_height`, `weight`, `italic`), `metrics` → `FontMetrics` (ascent, descent, advance, underline and strikeout), `cells` (a row of `Cell`s, unshaped, as a one-line `TextBlock`), `lay_out` → `TextBlock` (lines of placed glyphs, cached by text, font and width), and `GlyphAtlas`, which renders glyphs into pages — a texture each, `ATLAS_SIZE` texels a side — through `Renderer.update` and answers a `Glyphs` op a page; `next_frame`, which `paint` calls, and `glyph_atlas_sized` for small pages. **One atlas serves every window** (a glyph is kept by its pixel size, so a window on a display of another scale renders its own): `window` is the window being painted, and a picture is let go only when the last frame of every window that showed it no longer does (`window_closed` for a window gone). | HarfBuzz, FreeType |
| `ui.style` | `Style` (all `Maybe`; `radius` every corner's, `corners` each one's — CSS's `border-*-radius` — where set), `Insets`, `Rule`/`Sheet`, `Theme` (`Palette`, `Spacing`, radius, font, `elements` sheet; `light_theme`, `dark_theme`), its layout properties (sizes, flex's and grid's — `FlexDirection`, `LineWrap`, `Justify`, `ItemAlign`, `Basis`, tracks built by `px`, `fr`, `auto_track`, `min_content`, `max_content`, `minmax`, `repeat_tracks`, `GridSpan`, `GridFlow`), its effects (`box_shadow`, `background_gradient`, `filter_blur`, `backdrop_blur`), and the cascade: `Cascade` settles an element's tag, classes, part and inline style into `Computed`. `scroll_follow` (lui's `scroll-follow`): a scroller at its end stays there as its content grows. | no |
| `ui.layout` | `Node<e>` (kind, tag, class, part, style, children, key, label; `e` the program's event type, `NoEvent` for none) built by `row`, `column`, `flex`, `grid`, `expanded`, `stack`, `positioned`, `padding`, `align`, `sized`, `scroll`, `text`, `image`, `themed`, `component`, `painted` (an element the program draws, `.takes_input(RawInput)` for one taking input raw: a `Painter` asked for `PaintOp`s — `PaintDraw`, `PaintText`, `PaintCells` (a row of a grid of characters), `PaintWave` (a curly underline), `PaintPicture` (a program's picture by its `PictureSource`) — for a `PaintArea`), `picture(key, image, revision, size)` (a picture of the program's own: **the host keeps a texture a key**, in the `GlyphAtlas` `paint` is handed — uploaded the first frame a key is drawn, written again only when its revision moves, in place where its size holds, released the frame no view names it — Vega's design §2, its painters' `PaintPicture` carrying the source itself rather than naming another element's key), `picture_fill(source, fit, box, corners, opacity, scale)` (a painter's ops for a picture filling a box: `FitTile` at its own pixels, `FitStretch`, `FitCover` — CSS's `cover` — inside rounded corners; a tiny picture's tiles double until there are at most `MOST_TILES`) and the controls `button`, `checkbox`, `slider`, `select` (`SelectSpec.separators`: a rule before each option listed), `text_input`, `number_field`, `radio_group`, `disclosure`, `progress`, `separator`, `splitter`, `table`, a `dock` of tab groups (`DockTree`; `ui/layout/dock.lyra`) (below), a `popup` (content floating from an anchor while the program says it is open, placed within the window after the layout: `place_floats`), a `modal` (a dialog centred over a scrim, the only thing taking input while open), `.tooltip`/`.tooltip_node` (shown by the pointer once it rests), menus drawn — `menu_bar` and `menu_button` over menus as data (`Menu`, `MenuEntry`, `MenuItem`, `Chord`; `ui/layout/menu.lyra`) — a `tab_bar` (`TabsSpec`, `TabSpec`; `ui/layout/tabs.lyra`), a `swatch` opening a colour picker (`color_picker`: a saturation-and-value square, hue and opacity strips — no opacity strip, the colour opaque, without `alpha` — and a field for the colour's text, which keeps what is typed while focused — `FieldSpec.draft`; `ui/layout/swatch.lyra`), the window's own buttons (`window_buttons`; `ui/layout/window.lyra`) and `.window_drag()` for a title bar the program draws, and `.with_class`/`.with_style`/`.with_part`/`.keyed`/`.labelled`/`.aligned`; `layout`/`layout_with(node, Constraints, fonts, Interaction) -> Laid<e>` (each with its identity, role, label, focus ring and a field's caret) and `paint(laid, origin, scale, fonts, atlas, renderer) -> []DrawOp` (`fonts` `mut`: a painter's text is laid out as it is painted). Flex and grid are in `ui/layout/flex.lyra` and `grid.lyra` (one module, a directory). | no (text through `ui.text`) |
| `ui.motion` | Transitions' values: `progress` (CSS's timing functions), `blended`/`differs` (each property that animates), `mix_color` (premultiplied), and an element's `Motion` — its target and what is `Moving` towards it — moved on by `advance`; `with_layout`/`layout_key` for the sizes a layout lays out while they move. | no |
| `ui.interact` | `Ui`: the framework's state by identity (focus, hover, press, scroll offsets, carets, selections, undo), transitions (`motions`, shown at `now`; `animating`), `frame` (lay out and drop what left the tree), `handle(laid, input, platform) -> []e`, `place_text_input` (in `Ui.window`, the window it is the state of); a tab carried in from another window's dock, `aim_away_dock`, `clear_away_dock`, `land_away_dock`, and `dock_away`/`dock_torn` for the dock it left; `hit`, `control_at`, `find`, `focus_order`; `Editing`, a field's editing operations. | no |
| `ui.app` | `App { view, update: (s, e) -> (s, []Command<e>), subscriptions: (s) -> []Subscription<e>, background }` (`sandbox(view, update, bg)` for a plain update); `Command` (`WriteFile`, `ReadFile`, `After`, `CopyText`, `PasteText`, `OpenUrl`, `SetTitle`, `QuitApp`) — `SetTitle(string)` the main window's title, as the system shows it — `Subscription` (`EveryFrame`, `Every`, `OnResize`, `OnAppearance`, `OnFilesDropped` — a drop's paths together, on a Mac a file opened from Finder too; `ui.sdl3` gathers SDL's drop events between its begin and complete into one `FilesDropped`); `run`, and `Host` (`host_of`, `step`, `main_laid`, `redraw(window, …)`) for a loop of `main`'s own — which `lyrac run --live` needs. `App.windows` (`[Windows { of, view, on_close, on_moved }]`) the windows beside the main one, as the state has them (below): `WindowSpec` gains `key` and `place`, and `on_moved` hears a `WindowPlacement { place, size }`; the host keeps each as a `HostWindow` (`Host.windows`, the main window first). `App.shortcuts` (`[Shortcuts { of }]`, an array of at most one) answers the `Shortcut`s matched before any element hears a key (`chord_matches`: `primary` the shortcut modifier); `App.main_close` (`[MainClose { of }]`, an array of at most one) is the event raised when the user asks to close the main window — its close button, or the system's quit — instead of the program ending at once: the window stays open, and the program asks its own way (unsaved work) and ends itself with `Command.QuitApp`, or simply goes on; None ends the program at once; `App.menus` (`[Menus { of }]`) the program's menus — on a Mac in the system's own bar (`Platform.set_menus` with `NativeMenu`s, each item a tag the host keeps the event of; a choice is `MenuChosen(tag)`; a key the bar holds left to it, a shifted punctuation key or digit given to AppKit as the character it types — ⌘⇧[ as ⌘{ — which is how AppKit matches it; installed after the first pass's drawing, so the window and its painters' sizes come first), elsewhere installing nothing, their shortcuts taken as `App.shortcuts`' are; `Subscription.OnFocus` the window's focus; `Command.FocusKeyed(key)` the focus to a keyed element once it is laid out; `Command.EditField(FieldEdit)` an edit of the focused text field as its key would make it (`FieldSelectAll`, `FieldCopy`, `FieldCut`, `FieldPaste`, `FieldUndo`; `Ui.edit_focused`) — for Edit's items, whose keys the Mac's bar takes before a field hears them; `Command.LoadFonts(files, done)` faces added to the window's fonts at the next pass, before any font is measured (a family the user chose), `done` told whether every one loaded. `Subscription.Readable(fd, e)` is the event each pass that descriptor has data, the wait waking for it; `Command.ScrollTo(key, y)` scrolls a keyed scroller once it is laid out. `Command.WindowMinimize`, `WindowMaximize`, `WindowRestore` ask the system, `Subscription.OnWindowState` hears what it did (`Platform.window_state`, `change_window`, `WindowStateChanged`), `Command.ChooseFile(FileDialog { filters, start, save }, make)` shows the system's open or save dialog, its answer — a path or `None` — making the event later (`Platform.choose_file`, `FileAnswered` under the host's tag; `bindings.dialog` queues SDL's answer and wakes the wait; a script answers with `choose:PATH`), and `App.title_bar: DrawnTitleBar` is a title bar of the program's (below). `App.interface_scale` (`[InterfaceScale { of }]`, an array of at most one) is the interface's size as the state says it: every logical unit that many times larger in every window, on top of the display's scale — the host gives it the platform when it changes (`Platform.set_interface_scale`) and lays every window out again; windows then report sizes and input in the larger units, and both renderers draw by it (`ui.backend`'s `interface_factor_now`, which the platform sets, so a renderer paired with the scripted platform agrees too). | no |
| `ui.access` | `access_tree(laid, focus) -> AccessTree`: the laid-out tree as assistive technology hears it, a node for each paragraph, image, scroll and control (and labelled group), ids its identities hashed; `access_id`, `access_node`. | no |
| `ui.script` | `ScriptPlatform` (a `Platform` with no window and a clock of its own, playing one step a pass), `parse_script` (inputs: `"Tab 'Ada' Shift+Tab Primary+v click:10,20 double:10,20 scroll:1"`) and `parse_steps` (with `wait:N`, `resize:W,H`, `dark`, `light`). Also `compose:'text'`, `focus`/`blur`, `Insert` and `F1`–`F24`, and a pointer input with modifiers (`Shift+click:X,Y`, `Primary+scroll:-1`, a `Holding(Modifiers)` step each side), and `drop:PATH` (`drop:A|B` two in one drop). Several windows: `window:N` the inputs after it in window `N` (`0` the main window, the ones the program opens numbered from `1`, kept as `ScriptWindow`s), `close` and `quit`, and `place:X,Y` a window moved; its main window is at `0,0`, and a window opened without a place `40,40` from the last. | no |

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
  enclosing component's scoped sheet (made from the theme in force where the component is
  `component_of`; reaching a component inside it only at that component's top-level
  elements, beneath its own sheet, as Vue's scoped styles do — `Cascade.roots`), part rules (`Card::label`, and the
  more specific `Card.big::label`, from the theme, the global sheets and the sheet of the
  component around the part's), and inline style. Within a layer the more specific (tag 1, class 10, both 11),
  then the later. `color`, `font` and `opacity` are inherited, opacity multiplied.
- **Text is shaped once a paragraph and broken afterwards**, at a simple reading of UAX #14's
  opportunities (spaces, a hyphen inside a word, ideographs; a newline is a paragraph), with
  trailing spaces hanging. **A paragraph is shaped a run at a time**, each run the characters
  one face has: a spec's `family` may list several (`"Menlo, system"`, CSS's `font-family`),
  then `Fonts.fallbacks` (`add_fallback`; `add_system_fallbacks` for a CJK font and a symbols
  font from `SYSTEM_FALLBACKS`), each character drawn from the first whose font has it — the
  first family's missing glyph where none has — a combining mark, and a space its run's face
  has, staying in the run before it. Each glyph carries its face (`LineGlyph.face`); the block's
  `face` spaces its lines. Every rune, not only the BMP's. Adding a face forgets what was laid
  out. Left to right: no bidi yet (todo.md). **Colour glyphs** — an emoji font's bitmaps
  (`sbix`, `CBDT`) or layers (`COLR`) — are rendered in their colours into the same atlas pages
  and drawn as a `Picture` each, untinted and faded by the text's alpha; an emoji font is the
  last of `SYSTEM_FALLBACKS`, so a symbol with a text form keeps it, and **U+FE0F after a
  character** moves it to a colour face that has it. **A character that is an emoji by
  default** (Unicode's Emoji_Presentation — 😀, ⌚, a flag's letters; `emoji_by_default`) goes
  to a colour face first, in text and in cells alike, ahead of a text face that has it too:
  Linux's DejaVu Sans has a 😀 of its own, and drew it grey until 10/30.
  A glyph is rasterized at the size times the scale and drawn from a whole pixel.
- **A painted element is drawn by the program** (`painted(paint, on_size)`, `ui/TERMINAL.md`
  §1): laid out as a box — its style's size, else all the room it is given, so a tall one in a
  scroller needs a `height` and a full-window one a window that does not scroll
  (`window_overflow: OverflowVisible`) — and asked at **paint**, every redraw, for its ops in
  its own coordinates: its size, the part every clip around it leaves (`PaintArea.visible`,
  what a scroller shows, all it need draw) and the scale. Its ops are moved to its place and
  clipped to its box; `PaintText` is shaped as a `text` element's, placed by its first
  baseline. **`PaintCells` is a terminal's line, not shaped** (`Fonts.cells`): each `Cell`'s
  character found by the character maps of the spec's faces and fallbacks (a bold or italic
  cell's as that spec's), its glyph centred on its cell's columns — so a monospaced font's sits
  at its cell's left, a wide or a fallback's in the middle — a cell with combining marks shaped
  alone as a cluster, a space drawing nothing; one op a colour. **`PaintWave`** is the one curve:
  fills a device pixel wide, each spanning the curve over its column and the thickness, a wave
  every eight amplitudes — the display list has no paths. **The layout cache keeps its box, never its paint**: a reused element carries the
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
- **An element taking input raw hears it first** (`.takes_input(RawInput { on_key, on_text,
  on_pointer, caret })` on a painter): focusable — a click, Tab or `FocusKeyed` — and while
  focused handed every key (`KeyInput`, pressed or let go; Tab, the arrows and Space too) and
  the text they type or an input method composes (`TypedText`, `ComposedText`) before the
  framework does anything; the pointer (`PointerInput`, in its own coordinates, with the
  modifiers held: `PointerPress` with its clicks, `PointerRelease`, `PointerMove`,
  `PointerWheel`) while it is over it — and, after a press it takes, wherever it goes until it
  is let go. A function answering `None` hands the input back, and the framework goes on as it
  would have: a key the element declines is a key, a wheel it declines scrolls what is around
  it. `caret` is where the input method's window goes. It draws no focus ring: it draws its own
  focus. **The app's shortcuts come before it** (`App.shortcuts`): a key a shortcut matches is
  the program's and no element hears it.
- **A scroller can follow its end** (`scroll_follow`, lui's `scroll-follow: true`): at its end
  when its content grows, it is kept there; scrolled off it, it stays where it was however the
  content grows; scrolled back to its end — by the wheel, the bar, the keys or `ScrollTo` — it
  follows again (`WidgetState.following`, kept by `scroll_by`, which every scroll goes through,
  and in the layout cache's key). A new one starts at its end. **`ScrollTo(key, y)` and
  `FocusKeyed(key)` find an element by its key among its siblings**, so not the tree's root.
- **A file descriptor wakes the loop** (`Readable(fd, e)`): each pass the host dispatches the
  event for every subscribed descriptor that has data (`std.io.readable`, a `poll` with no
  timeout) — level-triggered, so it comes again until the data is read — and before it waits
  hands the platform the descriptors (`Platform.wake_for`), whose wait wakes when one has data:
  `ui.sdl3` through `bindings.wake`'s thread. An idle terminal costs nothing; a writing one is
  read the pass its output arrives.
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
  chosen option raises nothing. **An option may have a font family of its own**
  (`option_families`, lui's `option_families={…}`; `Style.font_family`, CSS's `font-family`,
  the family alone, its size kept), in the list and on the select when chosen — a font list
  showing each font in itself. Which select is open is the framework's
  (`Interaction.open`, one at a time). **The list floats** (`Laid.floats`): laid out under
  the select taking none of its size, painted after the whole tree outside every clip, and
  hit before anything else (`hit` looks for floating elements first) — so it covers what is
  below it, inside a scroller too. Heard as a combo box (its value the chosen option) with
  a list box of options while open. **Placed as a popup's content is** (below): no taller
  than the window, scrolling, opened with the chosen option in view and the option under the
  keys kept there (`Ui.reveal_wanted`, the frame after), and above the select at the window's
  foot.
- **A popup** (`popup(anchor, open, content, on_dismiss, side)`, lui's `<Popup>`) is its
  anchor in its place and, while the program says it is `open`, its content floating from it
  (`Laid.floats`, `PopsUp`) in a scroller of the framework's (`Popup::content`): as wide as
  it needs (laid out once with no width to learn it, then at it — CSS's shrink-to-fit, so a
  menu's stretched rows span the menu, not the window) and no larger than the window less a
  margin. **Placement is a pass after the layout** (`place_floats`, from `layout_cached`):
  a floating element with an `Anchor` goes below its parent's box (`FloatBelow`) or beside it
  (`FloatBeside`, a submenu's), flipped above or to the left where the window has no room,
  moved in from an edge it would pass, and over the anchor where neither side fits; placed
  from the anchor every time, so a layout given back by the cache places alike, walking only
  what holds a float (`Laid.floating`). **Open is the program's** — a press outside the anchor
  and the content (and what floats from it), Escape (the innermost of nested popups; an open
  select's list first) and the window losing the focus raise `on_dismiss`, a press going on to
  what it fell on; a press on the anchor raises none, so its button can close it (`Ui.handle`).
  Opened while the keyboard had the focus on its anchor, the focus moves to the content's first
  control; closed with the focus inside, back to the anchor's (`Ui.popups`, the frame after).
- **A tooltip** (`.tooltip(text)` on any node, `.tooltip_node(node)` for one with structure;
  `Node.tip`; lui's `tooltip` and `tooltip-node`) shows once the pointer has rested on the node for `TIP_DELAY_MS` (500): the
  framework's (`Interaction.tip`, `tip_at`), laid out as a float of the node (`lay_tip`,
  `Tooltip`, its width up to its style's `max-width`) placed below the pointer's rest
  (`FloatPointer`), flipped above it at the window's foot. The rest is timed from the first
  layout after the move (`Ui.tip_rested`, by `Ui.now`), and `Ui.tip_due(now)` tells the host
  how long to wait and when to draw. A move within the node keeps it; onto another node hides
  it and starts that one's rest; a press, a key, the wheel or the window losing the focus hides
  it and spends it until the pointer leaves the node (`tip_spent`). **It takes no input** —
  `hit`, `covered` and `focus_order` pass over it (`is_tip`) — and it is heard as the node's
  description (`Laid.description`, `AccessNode.description`), not as a node.
- **A modal** (`modal(under, open, content, on_dismiss)`, `Modal`, lui's `<Modal>`) is `under` in its place and,
  while the program says it is `open`, a scrim floating over the whole window (`ModalScrim`,
  placed at the window's corner, `FloatWindow`) dimming everything, `content` centred in it
  in a scroller of the framework's (`Dialog`, no larger than the window less the scrim's
  padding). **While it is open nothing outside its dialog takes input**: a press or the wheel
  elsewhere goes nowhere (`Ui.handle`, by the dialog's areas — `Opened.areas`, what floats from
  it included), and Tab and Shift-Tab move only among its controls, wrapping (`move_focus`). It
  takes the focus when it opens, on its first control, closing the menus and any select's list
  outside it, and gives the focus back to what had it when it closes (`OpenPopup.returns`).
  Escape raises `on_dismiss` (a popup or list open inside it first); a press outside it and the
  window losing the focus raise nothing. The scrim and the dialog are tags of their own, not
  parts, so the program's sheets reach `under` as they would without the modal.
- **The smaller controls** (`ui/layout/forms.lyra`, `table.lyra`; Vega's design §5) are made of
  the elements every control is, with three of the framework's own beneath them:
  - **`Drags(DragSpec)`**, an element dragged along an axis to change a number — its value at
    the press moved by `per_unit` for each unit the pointer goes, held to `min`..`max`
    (`Ui.drag_from`, `drag_value`). It is pressed where no control is over it (`drag_at`), takes
    no focus, shows the resize arrows (`ResizeAcross`, `ResizeDown`), and drops the focused
    field's draft so the value it makes shows.
  - **`Radio(chosen, e)` in a `OneOf`**: a group with one stop for Tab — its chosen radio, else
    its first (`focus_walk`, `group_stop`) — whose arrow keys choose the one before or after,
    the focus going with the choice and kept in view (`radio_key`). Heard as radio buttons in a
    radio group; `Node.with_role` makes them a table's rows instead.
  - **`FieldSpec.on_step`**: Up and Down (ten with Shift) raise it, the draft let go.

  Built on them: **`number_field(NumberSpec, on_change)`** (`NumberField`: `::label` dragged to
  scrub, a step every `SCRUB_UNITS`; `::field` keeping its draft, each edit that reads as a
  number raised, held to the range, and stepped by the arrows; `::minus`, `::plus`), on a whole
  number of steps, `decimals` places shown (`number_text`); **`radio_group(options, selected,
  on_change, axis)`** of **`radio`s** (`Radio::circle`, `::chosen`, `::dot`); **`disclosure(title,
  open, content, on_toggle)`** (a `Disclosure` button, `.open`, its `::arrow` right or down);
  **`progress(fraction)`** (`Progress`, `::fill`; heard as a progress indicator with its
  percentage); **`separator(label)`** (`Separator::line`, `::label`); **`splitter(first, second,
  at, on_drag, axis, least)`** (`SplitEdge`, `SPLIT_EDGE` thick — named so beside the prelude's
  `split`); and **`table(columns, rows, selected, on_select, on_resize)`** (`Table`: a header —
  `::header`, `::heading` — that stays while the rows scroll under it in a `TableBody`, each
  column's edge, `ColumnEdge` with its `::rule`, laid over the header's end on a stack so it is
  the header's height; rows `TableRow`, `.odd` every other, `:checked` the selected one, their
  cells at their columns' widths). **An event the view makes eagerly is a `pure` parameter, not a
  spec's field** (`on_change`, `on_select`, `on_resize`): only a parameter's `pure` is checked
  where it is given, so only it may be called while the view is built. lui writes each
  (`<NumberField>`, `<RadioGroup>`, `<Radio>`, `<Disclosure>`, `<Progress>`, `<Separator>`,
  `<Splitter>`, `<Table>` of `<TableColumn>`s and `<TableRow>`s; `bindings/README.md`).
- **Docking** (`ui/layout/dock.lyra`; Vega's design §7): **`dock(tree, panel, on_change,
  on_close)`** (lui's `<Dock>`, `on_tear` its `.tears_off`) shows a `DockTree` — **the program's data**, a group of tabs (`DockTabs`:
  `DockGroup { keys, selected }`) or a split of two (`DockSplit`: `DockSplitSpec { axis, at,
  parts }`, `dock_tabs`/`dock_split` building them) — each group (`DockGroup`) a `tab_bar` of its
  panels over the shown one's content (`panel(key)` a `DockPanel { title, content, closable }`),
  each split (`DockSplit`) its parts at their shares and a `SplitEdge` between them, a `Drags`
  whose value is a **share** (`DragSpec.share`: a unit of travel worth one over the room the
  parent shares out, `Ui.drag_scale`). Selecting a tab, moving it along its strip, dragging an
  edge and docking a tab raise `on_change` with the tree that leaves; a tab's × raises
  `on_close` with its key. **A tab dragged off its strip** (`DOCK_TEAR` past it, or beside its
  group) **docks** (`Interaction.dock_drop`, a `DockDrop`, aimed by `dock_aim`): near the dock's
  edge (`DOCK_EDGE`) a split of the whole, the new pane `DOCK_EDGE_SHARE` of it; over a group's
  strip or middle among its tabs, nearer a side a split of that group. The dock (`Docked`) lays
  where it would land over its tree (`DockPreview`, keyed into the layout cache by
  `dock_drop_key`); letting go there raises the tree rearranged (`dock_dropped`), letting go
  over nothing or Escape leaves it. The same edits are the program's to make: `dock_with(tree,
  key, near, side)` (beside another panel, or along the dock's edge with `near` `""`),
  `dock_without`, `dock_select`, `dock_reordered`, `dock_resized`, `dock_keys`. An emptied group
  goes, and a split left one part is that part. A group's leaf is a `DockLeaf(path, keys)`, its
  path a split's parts `0` and `1` from the root.
- **Several windows** (Vega's design §8): **`App.windows: [Windows { of, view, on_close,
  on_moved }]`** — the windows beside the main one are **the program's data**, as a dock's tree
  is: `of(state)` answers them, each a `WindowSpec` with its `key` and `place`, and the host
  opens one the state gains (`Platform.open_window`, at its place and size, held to its limits),
  closes one it loses (`Renderer.window_closed`, `GlyphAtlas.window_closed`,
  `Platform.close_window`) and makes a change to one's title, size or place to it. `view(state,
  key)` is what it shows. The system's close of one raises `on_close(key)` — the program takes it
  out of `of`, or keeps it — and the user's move or resize `on_moved(key, WindowPlacement)`. The
  the main window's close (or `QuitRequested`) ends the program — or, with `App.main_close`,
  raises its event and leaves the window open until the program ends itself
  (`Command.QuitApp`). **A `Ui` a window**
  (`HostWindow`: its framework state, layout cache, tree, size drawn at): focus, hover and
  scroll are a window's, input goes to the window it happened in, and a window laid out before
  an event `update` had since is laid out again before it takes input (`Host.version`); every
  window is drawn again after an event, and only the window an input happened in after an
  input that raised none. Menus and shortcuts are every window's; `OnResize`, `OnFocus` and
  `OnWindowState` are the main window's; an edit of the focused field and a file dialog go to
  the window last focused; a drawn title bar (`App.title_bar`) is the main window's.
  **A dock's tab crosses windows**: dragged out of its window over another's dock, it aims there
  as at its own (`DockDrop.foreign`, the preview in that window alone), and let go there that
  dock raises its tree with the panel and its own dock its tree without (the program may close
  a window left empty). Let go outside every window, a dock that **`.tears_off(on_tear)`**
  raises a `DockTear { key, place, size }` — the window's top-left `TEAR_GRAB` up and left of the
  pointer, the size of the group torn from — and the program opens a window holding a dock of
  that panel; a dock's only panel does not tear off. Wayland places no window: there a torn
  window opens where the compositor puts it, and a tab is not carried between windows (no
  `to_screen`). AccessKit is told of the main window alone.
- **Menus are data** (`Menu { label, entries }`, `MenuEntry` an item, a `Submenu` or a
  `MenuSeparator`, `MenuItem { label, on_select, shortcut: Maybe<Chord>, enabled, checked }`),
  drawn as a bar of titles (`menu_bar`: `MenuBar`, each `MenuTitle`) or a face opening them
  (`menu_button`: `MenuButton`) — each an opener (`Opens`) whose list (`menu_list`: `Menu` of
  `MenuItem`s, `::check`, `::shortcut`, a submenu's `::arrow`, `MenuSeparator`) floats as a
  popup's content does, a submenu's beside its item. **Which are open is the framework's**
  (`Interaction.menus`, from the first opened to the deepest submenu; `:open` on their
  openers), and the item under the keys is the focus. A press on an opener opens or closes its
  menu; while one is open the pointer moves it across a bar's titles and into a submenu, and a
  press outside closes them; choosing an item closes them all, the focus back on the first's
  opener. The keys: Enter, Space or Down on an opener open it on its first item; Up and Down
  walk the items that can be chosen, wrapping; Right opens a submenu or the bar's next menu,
  Left closes the submenu or opens the previous; Escape closes the deepest; Tab closes them all
  and moves on from the first's opener. **A shortcut is spelled at layout** (`Spelled`,
  `chord_text`: `⇧⌘T` where `Viewport.apple` — the platform's `apple()` — else
  `Ctrl+Shift+T`), so the list made from the entries is the same everywhere and a layout from
  the cache refreshes through it (`laid_children`, which a popup's holder needs too) — and
  **the app takes every drawn menu's shortcuts** (`menu_shortcuts`, after `App.shortcuts`). **A program drawing menus adds `add_system_fallbacks()`**: Helvetica, the Mac's
  system font here, has no `⌘ ⇧ ⌥ ⌃`, nor a checked item's `✓`, and without the symbols
  fallback they are blank.
- **A title bar the program draws** (`App.title_bar: DrawnTitleBar`, `ui/WINDOW.md` step 5):
  the host takes the system's frame away once (`Platform.set_decorated`) and, after each
  redraw, tells the platform what of the window moves it — what the view marks
  `.window_drag()` — and inside that what keeps a press, its controls and the window's buttons
  (`set_window_areas`, with the margin its edges resize from); `ui.sdl3` hands them to
  `bindings.hittest`, since SDL asks through a C function. **`window_buttons()`** is minimize,
  maximize or restore, and close (`WindowButton`, each `WindowControl`), glyphs of lines and
  boxes chosen at layout by `Viewport.maximized` (in the cache's key); a press on one is the
  framework's (`Ui.take_window_buttons`), close ending the run as the system's close does. **A
  Mac keeps the system's title bar**: `ui.sdl3` ignores the frame and the areas there, and
  `window_buttons` lays out as nothing (`Viewport.apple`). A script plays the system's changes
  with `maximize`, `minimize` and `restore`, and its platform records what it was told.
- **A tab bar** (`tab_bar(TabsSpec { tabs, selected, on_select, on_close, on_move, after })`,
  `ui/WINDOW.md` step 4): a strip (`TabBar`, `TabStrip`) of tabs (`Tab`, `:checked` the
  selected; `Tabbed`), each its label clipped to its width and, closable, a × (`TabClose`), then
  `after`. **The order is the program's**: a drag past half of a movable neighbour raises
  `on_move(from, to)`, and a tab is keyed by its `TabSpec.key`, so the press stays on it as it
  moves. A press selects (on the ×, closes, the focus left where it was); a middle click closes;
  the focused tab's Enter, Space, Left, Right, Home and End select. **A tab carries the spec's
  functions, not events** (`TabSpot`): a builder is pure, and may call none, so `ui.interact`
  calls them with its place. **Widths** (`lay_tab_strip`): as wide as it needs up to
  `TAB_MOST`, the strip's `gap` apart; where the strip is narrower, the gap narrowed first, to
  `TAB_GAP_LEAST` (2) at the least, then the tabs shrunk together in proportion to what each has
  above `TAB_LEAST`, none below it; past that as many as fit show, the selected among them, beside a
  more-tabs button (`TabsMore`, the select's arrow, `more_tabs`) whose menu lists the rest, made
  at layout and made again from the fresh tab nodes when the strip comes from the cache
  (`strip_children`, through `refreshed`, which is no longer pure). Heard as a tab list of tabs.
- **Menus in the Mac's own bar** (`App.menus`, `ui/WINDOW.md` step 3): each pass the host
  builds the state's menus and hands the platform `NativeMenu`s — the events kept by the host,
  every item a tag in order, since a platform is not generic in them — and `ui.sdl3`'s
  `install_menus` puts them in the bar through `bindings.menubar`: the first as the application
  menu named for its title, macOS's Window menu after the last, **installed again only when
  their shape changes** (`menu_shape`: titles, entries, tags, key equivalents), else greyed and
  checked in place. A chosen item's tag is drained after SDL's events (`MenuChosen`), the
  binding having woken the wait. **A key the bar holds is the bar's**: AppKit chooses the
  item and SDL hears the key too, so the host gives it to nothing. A script plays a bar where
  its `native_menus` says, and chooses with `menu:N`.
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
  drawn into a layer and blurred) and `backdrop_blur` (what is behind it blurred — on a
  see-through window the desktop too: `ui.gpu` hands each `Backdrop` op, cut to its clip, to
  `bindings.gpu.set_backdrop_regions`, a backdrop layer a region under the window's content,
  each at its own radius, those of one radius sharing a capture). Painted in
  CSS's order: a filter's layer, outer shadows, backdrop, colour, gradient, inset shadows,
  border, content. A blur radius is CSS's: the Gaussian's deviation is half of it.
- **Two renderers**: `ui.gpu` draws every op exactly on the GPU — a shape's coverage its rounded
  box's signed distance (corners, borders, rounded clips), a shadow the analytic Gaussian of
  its box, a gradient per pixel with up to eight stops (more resampled), and blur as two
  passes at half size; one instanced draw a frame unless a second texture is sampled.
  **`ui.sdl3` approximates**: a shadow's falloff is a straight ramp and an inset one a soft
  band; a linear gradient is cut at each stop's line into bands of vertex colours, exact, and a
  radial one is vertex colours on the outline and its middle; a clip is a whole-pixel rectangle, so `PushClip`'s radii are ignored; blur is left
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
- `examples/ui/pixels.lyra` — a pixel editor (`ui/VEGA.md` §11's step 1): its canvas a
  `PaintPicture` of the program's pixels, written again only when a stroke moves the revision,
  drawn crisp at a zoom under a grid; strokes joined between moves, by a pure update that copies
  the pixels only when a stroke changes one. `--size N` for a larger picture, `--time` for a
  pass's cost (`--script "…" --png FILE` headless).
- `examples/ui/cells.lyra` — a terminal's screen by a painter: Menlo's (or DejaVu Sans Mono's)
  four faces a character a cell, coloured runs over backgrounds, CJK across two cells and marks
  from the fallbacks, underlines straight and curly and a strikeout at the font's own metrics
  (`--script "" --png FILE` headless).
- `examples/ui/keys.lyra` — an element taking input raw showing what it hears: every key, the
  text typed and a composition at its caret, the pointer, the window's focus, and a shortcut
  (Primary+K) taken first (`--script "…"` headless).
- `examples/ui/follow.lyra` — a command's output as it comes, on a pseudo-terminal, read through
  `Readable` and shown in a scroller following its end (`--headless` runs it under SDL's dummy
  driver until the command ends).
- `examples/ui/dialogs.lyra` — tooltips and a modal (`ui/VEGA.md` §11's step 2): a toolbar
  whose buttons have tooltips (Open's with its shortcut), and Close asking in a dialog over the
  dimmed window — a field, Don't Save, Cancel and Save — while the title has changed
  (`--script "…" [--png FILE]` headless, printing what happened and the tooltip showing). Its
  view is the markup of `dialogs_view.lui` (§9: `tooltip`, `tooltip-node`, `<Modal>`).
- `examples/ui/panels.lyra` — the smaller controls (`ui/VEGA.md` §11's step 3): a sprite's
  properties — number fields, radio buttons, a disclosure, separators, a progress bar — beside a
  table of sprites, the splitter between them dragged; a file dropped on the window joins the
  table (`--script "…" [--png FILE]` headless, printing the state it leaves). Its view is the
  markup of `panels_view.lui` (§9: `<NumberField>`, `<RadioGroup>`, `<Disclosure>`,
  `<Separator>`, `<Progress>`, `<Splitter>`, `<Table>` of `<TableRow>`s from a `<for>`).
- `examples/ui/dock.lyra` — docking (`ui/VEGA.md` §11's step 4): a studio's editors and
  panels in tab groups, split and rearranged by dragging tabs, the edges between them dragged,
  closed panels opened again from a toolbar (`--script "…" [--png FILE]` headless, printing the
  arrangement).
- `examples/ui/windows.lyra` — several windows (`ui/VEGA.md` §11's step 5): the dock example's
  studio, its tabs torn off into windows of their own and docked back between them, a window's
  close putting its panels back (`--script "…"` headless, `window:N` aiming the inputs at a
  window, printing the windows, their arrangements and which shows a drop's preview). Its
  views and its torn windows' specs are the markup of `windows_view.lui` (§9: `<Dock
  on_tear>`, `<window name="torn_window">`).
- `examples/ui/popups.lyra` — popups: File's list with a submenu beside it, a select of sixty
  whose list scrolls, and More at the window's foot whose popup flips above it and moves in
  from the right edge (`--script "…" [--png FILE]` headless, printing what happened).
- `examples/ui/menus.lyra` — menus drawn: a bar (File, Edit with Undo greyed, View with a
  checked item and a Zoom submenu) and ☰, the shortcuts shown and taken; on a Mac the menus
  in the system's own bar instead (`App.menus`); File ▸ Open… the system's open dialog
  (`ChooseFile`)
  (`--script "…" [--png FILE]` headless, printing what was chosen).
- `examples/ui/tabs.lyra` — a tab bar: Home (neither closable nor movable) and documents
  selected, closed, dragged into a new order and added with `+`, the strip shrinking and then
  holding the rest in its more-tabs menu (`--script "…" [--png FILE]` headless, printing the
  tabs).
- `examples/ui/titlebar.lyra` — a title bar of the program's: the window without the
  system's frame, moved from the bar, its own buttons and one of the program's asking the
  same by command, what the window went through heard (`--script "…" [--png FILE]` headless).
- `examples/ui/colors.lyra` — swatches: a theme's three colours, one translucent, each opening
  the picker and shown as `ui.color` writes it and in OKLCH (`--script "…" [--png FILE]`
  headless, printing the colours).
- `examples/ui/notebook_view.lui` — the window's elements in markup, as Termina's window has
  them: a title bar the program draws holding a menu bar (File's Go To a submenu filled by a
  `<for>`) and the window's buttons, the notes as tabs with `+` and More after them, and a grid
  of each note's name and swatch, two cells a `<for>`; run by `examples/ui/notebook.lyra`
  (`--script "…" [--png FILE]` headless, printing the notes).
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
  `TestRun_UiSelectSeparators` a select's rules, the options keeping their places and the keys
  walking over them; `TestRun_UiSelectListScrollsAndFlips` a long list scrolling with its option in view, and
  one at the foot opening above; `TestRun_UiPopup` a popup's placement, dismissal, nesting and
  focus; `TestRun_UiTooltip` a tooltip's rest, placement, hiding and spending on the clock;
  `TestRun_UiDialogsExample` a tooltip and a modal's focus, Tab, Escape and blocked presses,
  from the example; `TestRun_UiPanelsExample` every smaller control, a drop, and the table's
  and the splitter's edges, from the example; `TestRun_UiDockExample` docking into a group, beside
  one and along the dock's edge, the preview, a split's edge, closing, Escape, reopening and
  moving a tab, from the example; `TestRun_UiWindowsExample` tearing a tab off, aiming and
  landing one in another window and back, the preview there and not here, Escape, a window's
  last panel leaving it, a lone tab staying, a window moved, resized and closed, from the
  example; `TestRun_UiGpuSecondWindow` a second real window drawn by the GPU's renderer and by
  SDL's, and let go — the three examples on views generated from markup, which
  `TestLui_PanelControlExamplesAreCurrentAndCheck` holds current; `TestLui_UiTargetPanelControls`
  the markup of every panel control, `tooltip`, `tooltip-node` and a `<window name>`, and their
  refusals; `TestRun_UiPopupsExample` the example from the keyboard; `TestLui_UiTargetPopup`
  the markup's `<Popup>`; `TestRun_UiMenus` menus by pointer and keys, events fresh from the
  cache (a popup's too), shortcuts spelled and gathered; `TestRun_UiMenusExample` the example,
  its shortcuts taken by the app; `TestRun_UiAppMenus` `App.menus` through the host, natively
  and not; `TestRun_UiTabs` a tab bar's widths, the more-tabs menu, presses, drags, keys and roles;
  `TestRun_UiTabsExample` the example; `TestRun_UiDrawnTitleBar` a drawn title bar through the
  host; `TestRun_HitTestAnswers` `bindings.hittest`'s answers; `TestRun_UiTitleBarExample` the
  example; `TestRun_UiChooseFile` `ChooseFile` through the host; `TestRun_DialogQueue`
  `bindings.dialog`'s queue; `TestRun_UiColor` `ui.color`'s text, Oklab and OKLCH, gamut
  mapping and HSV; `TestRun_UiColorPicker` a swatch's picker by pointer, keys and its field,
  and a hue held through white; `TestRun_UiColorsExample` the example;
  `TestLui_UiTargetColourSyntax` the markup's colours; `TestLui_UiTargetWindowElements` the
  markup's menus, tabs, swatch, window buttons, `window-drag` and a `<for>` of several, and
  their refusals; `TestRun_UiNotebookExample` the example by shortcut, menu, submenu, tab and
  picker, and `TestLui_NotebookExampleIsCurrentAndChecks` its view; `TestRun_UiMenusInstalledInTheMacsBar` (`menubar_test.go`) the bar AppKit was given,
  synced and reinstalled, and a choice waking SDL's wait;
  `TestRun_UiDisabledIsLaidOutAtOnce` a control disabled with nothing else changed;
  `TestLui_UiTargetReadOnlyField` the markup's `readonly`.
  `TestRun_UiPaintedElement` a painter's area, its ops moved and clipped, its text, the newest
  function on a cached layout, and `on_size` heard before each frame; `TestRun_UiPaintedExample`
  the example's board, a pixel at a time; `TestRun_UiAtlasPages` the atlas's pages opened,
  emptied (never under the frame that drew from them) and grown, on pages of one glyph each;
  `TestRun_UiFontFaces` CSS's face matching, faces made for what a family lacks, hinting as
  part of a glyph, metrics through `MeasureFont`, and a variable font's weights where there is
  one; `TestRun_UiFontFallback` runs by face checked against HarfBuzz glyph by glyph, a mark with
  its base, and a CJK fallback where the machine has one; `TestRun_UiCells` a row of cells' glyphs
  centred on their columns, a marked cell's cluster, a bold cell's made face, and a wave's
  columns; `TestRun_UiCellsExample` the example's blocks and backgrounds a pixel at a time;
  `TestRun_UiRawInput` raw input through a host and a script — keys, Tab and a declined one,
  text and composition, a shortcut first, a modified press, a drag held past the element's
  edge, a declined wheel, the window's focus, `FocusKeyed`, and the input method's window;
  `TestRun_UiReadableAndScrollFollow` a pipe read as it fills and a scroller following, left and
  rejoining its end; `TestRun_UiWakeWatch` bindings.wake's thread waking SDL's wait once a
  watch; `TestRun_UiFollowExample` the example headless, its forty lines arriving through the
  wake; `TestLui_UiTargetScrollFollow` the markup's `scroll-follow`.
  `TestRun_UiFlex` and `TestRun_UiGrid` hold flex and grid to hand-worked CSS results, child
  rect by child rect; `TestLui_UiTargetFlexAndGrid` the markup's layout properties.
