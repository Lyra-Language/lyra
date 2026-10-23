# What a terminal needs of `ui` — design

**Status: proposed 10/22; steps 1–3 built 10/22** (`painted`, `PaintDraw`, `PaintText`,
`on_size`, `PaintArea.visible`, `examples/ui/painted.lyra`; the atlas's pages; faces, made
faces, metrics and hinting — `Fonts.hinting` light unless the program says, as `ui.text` asks
no platform which it is on; `MeasureFont` answered once, not again as fonts change) — the ops `Paint`-prefixed, as
constructors match program-wide by name and `bindings.imgui.ui` has a `Text`, and `Painter`
without `input` until step 6. Three additions to the framework, each useful
beyond Termina: an element the program paints itself (§1), font faces and fallback (§2),
and an element that takes the keyboard and pointer raw (§3). §4 is the smaller work around
them (the frame's cadence, scrolling that follows the end, focus), §5 the order to build it
in, §6 the questions left for whoever builds it.

**The forcing function is Termina** (`../termina`), a terminal drawn today through Dear
ImGui's draw list. Moving it to `ui` means moving the whole window at once — ImGui and `ui`
each own the window, its loop and its renderer — so these three come first, and the
remaining pieces (a tab bar, popup menus, a colour picker, a file dialog, a title bar the
program draws) after them, outside this note.

## What Termina does today

- **Draws cell by cell**: per visible line, a filled rectangle a run of one background, a
  `add_text` a run of one style (twice, a device pixel apart, for a bold the family lacks),
  thin lines for underlines and strike, quadratic curves for a curly underline; a wide
  rune alone, centred on its two cells; a cell with combining marks alone. Rounded cards
  under the blocks, a cursor, a translucent selection. 40–130 draw calls a frame for a
  shell's plain text, up to thousands for a full-screen program in colour.
- **Measures a cell** as 100 `M`s over 100 (ImGui rounds a measured width up to a pixel);
  its height the size times the spacing setting, rounded; underline and strike at fixed
  fractions of the size, not the font's own metrics.
- **Four faces** — regular, bold, italic, bold italic, a missing one stood in for — and
  fallback fonts (a CJK font, a symbols font) merged into each, BMP only, no colour.
  Hinted off macOS.
- **Polls keys** (`is_key_pressed`, ImGui's repeat) and reads typed text; encodes them for
  the program (Ctrl+letter, Alt as Meta off a Mac, the named keys by DECCKM). Takes its own
  chords first (⌘T, Ctrl+Shift+T…). Reads the mouse for selection (one, two, three clicks),
  reports it to a program that asked (`?1000`/`?1002`/`?1003`, SGR or X10), turns the wheel
  into arrows on the alternate screen (`?1007`).
- **Draws every frame**, reading every tab's PTY first: no idling.

`ui` has none of the first four (`ui/README.md`; the gaps are named in each section below).

## 1. An element the program paints

### The shape

A new kind, `Painted(Painter<e>)`, built by `painted(spec)`. Layout gives it a size like any
element (its style's `width`/`height`, else it fills its constraints, as `expanded` does);
**paint asks the program what to draw in that size**, every time the window is painted.

```lyra
/// An element whose insides the program draws: `paint` answers the ops for the size it was
/// laid out at, in its own coordinates (0, 0 its top left).
pub struct Painter<e> {
  paint: (PaintArea) -> []PaintOp,
  /// The event for a new size, after the layout that gave it — a terminal's columns and
  /// rows. `None` hears nothing.
  on_size: Maybe<(Size) -> e> = None,
  /// What it is to the keyboard and pointer (§3). `Inert` takes neither.
  input: Input<e> = Inert,
}

pub struct PaintArea {
  size: Size,
  /// The part of it inside every clip around it — what a scroller shows. Lines outside it
  /// need not be painted.
  visible: Rect,
  /// Device pixels a logical unit, for snapping a line to a whole pixel.
  scale: f32,
}
```

**Why a function, not ops built in `view`**: the size is not known until layout, and a
terminal's ops depend on it (where the box sits, which lines are in view). **Why it
survives the layout cache**: the cache keeps the *layout*, not the paint — `paint` runs over
the whole laid-out tree every redraw anyway. `same_kind` compares only `Painted`'s data
(none, today), and `refreshed` swaps the new node's kind in on a hit as it does for a
button's event, so the newest closure — over the newest state — is the one painted. A
painted element's layout is a box; reusing it costs nothing.

### Its ops

`DrawOp` is in logical units over textures, and a glyph's quad needs the atlas and the
scale, which the program does not have. So a painter answers a vocabulary one step up,
which `paint_into` turns into `DrawOp`s offset to the element's place:

```lyra
pub data PaintOp =
  /// A display list op, as is: Fill, Border, Shadow, GradientFill, PushClip/PopClip…
  PaintDraw(DrawOp)
  /// Text shaped and laid out as a `text` element's is, its first baseline at the point.
  | PaintText(Point, string, FontSpec, Color)
  /// Runes on a grid, unshaped: each `Cell` at `origin.x + col × advance` on the row whose
  /// baseline is `origin.y`, in the face its style picks (§2) — a terminal's line.
  | PaintCells(Point, f32, []Cell, FontSpec, Color)
  /// A wavy line across a width — a curly underline — at an amplitude, a thickness thick.
  | PaintWave(Point, f32, f32, f32, Color)

pub struct Cell {
  col: i64,
  /// The base rune and any combining marks after it.
  text: string,
  /// 2 for a wide rune, drawn centred on its two cells.
  width: i64 = 1,
  bold: bool = false,
  italic: bool = false,
}
```

- **`PaintCells` is not shaped.** A terminal places every rune on its cell, so a glyph is found
  by the face's character map (`FT_Get_Char_Index`, or HarfBuzz's nominal glyph) and put at
  its column: no HarfBuzz run, no ligatures, no measuring. A cell whose `text` holds marks
  is shaped alone (a cluster, cached by `lay_out`'s cache), so a mark sits where its font
  puts it. Its glyphs join a `Glyphs` op like a paragraph's; colour is the op's, so a
  painter emits one `PaintCells` a run of one colour, as Termina batches today.
- **`PaintWave` is the one shape the display list lacks.** It has no paths (`ui.draw`), and a
  curly underline is the only curve Termina draws. `paint_into` cuts it into thin quads
  along the curve in pixels (`ui.tess`), so both renderers draw it. A general path op is
  later work, not this.
- **The element clips to its box** (a `PushClip` around its ops), as a canvas does.
- Lines, bars, underlines, the cursor and selection are `PaintDraw(Fill(…))`; cards are
  `PaintDraw(Fill)` with `Radii` (top corners only, for a card's input: `Radii` already allows
  it).

### Measuring a cell

A terminal needs its cell before it can ask for columns, and the metrics it needs are the
font's: `Fonts.metrics(FontSpec) -> FontMetrics { ascent, descent, line_gap, advance,
underline_y, underline_thickness, strike_y }`, read by FreeType from the face (`advance` the
`M`'s, exact; the rest from `post` and `OS/2`). The program does not hold `Fonts` — the host
does — so it asks through a command, which answers again whenever fonts change:

```lyra
  /// The font's metrics, for a grid's cell.
  | MeasureFont(FontSpec, (FontMetrics) -> e)
```

Termina's columns are then `floor(size.w / advance)` from `on_size`, its line height its
setting times `ascent + descent + line_gap`, rounded, and its underline the font's. With
hinting on (§2) an advance is a whole pixel, so a cell is.

### Rejected

- **A node a cell.** 12,000 nodes for a 200×60 screen, each laid out, cascaded and hit-tested,
  to draw what is a rectangle and a glyph.
- **A program-owned texture** (`image` over `Renderer.update`): Termina would rasterize its
  own text, which is `ui.text`'s job, and re-upload a screen a frame.
- **Ops in `view`** with the size from last frame's `on_size`: a frame late on every resize,
  and the box and lines in view decided before layout says where the scroller is.

## 2. Font faces and fallback

`ui.text` has one face a paragraph, chosen by family alone, and nothing for a rune it lacks
(`todo.md`: "one font, left to right").

### Faces

```lyra
pub struct FontSpec {
  family: string = "",
  size: f32 = 14.0,
  line_height: f32 = 0.0,
  weight: i64 = 400,         // CSS's: 400 regular, 700 bold
  italic: bool = false,
}
```

- **`Fonts.add(family, path, index, weight, italic)`** adds one face of a family; a family is
  its faces. `face_for(spec)` picks as CSS's font matching does, simplified: the italic
  faces first if asked (else the upright), then the nearest weight (heavier first above
  400, lighter first below).
- **A variable font is every weight**: a face with a `wght` axis serves a weight by setting
  it (FreeType's `FT_Set_Var_Design_Coordinates`, HarfBuzz's `hb_font_set_variations`).
  SF Mono is one variable font; Termina fakes its bold today and would draw the real one.
- **What is missing is synthesized**, as CSS's `font-synthesis`: a bold the family lacks is
  the regular glyph emboldened by FreeType (`FT_Outline_Embolden`, by the size over 24 —
  better than Termina's drawn-twice), an italic the regular sheared 12° (`FT_Set_Transform`).
  The atlas keys a glyph by face, glyph, pixel size and what was synthesized.
- **`TextBlock.face` moves to each glyph** (`LineGlyph.face`), since a fallback (below)
  puts several faces in one paragraph. `Glyphs` stays one op a colour: every face's glyphs
  are in the same atlas.

### Fallback

- **A family list**: `family` may name several, comma-separated, as CSS's `font-family`;
  after them, **`Fonts.fallbacks`**, the system's, tried in order — added by
  `add_system_fallbacks` from a list per platform as `SYSTEM_FONTS` is (Termina's:
  Arial Unicode, Hiragino, Noto Sans CJK; Apple Symbols, DejaVu Sans).
- **Itemizing**: a paragraph is split into runs, each a stretch of clusters (a base rune and
  its marks, kept together) whose first rune the same face has — the first face of the
  list, then the fallbacks, that maps it — and each run is shaped with its own face. Runs
  meet at clusters, so a mark never shapes apart from its base. `PaintCells` asks the same
  question a cell at a time.
- **Every rune**, not the BMP's: Lyra's runes are 32 bits and FreeType's maps take them —
  past ImGui's 16-bit `ImWchar`.
- **Not in this step**: script itemizing (a run is one face, not one script), bidi, and
  colour emoji (FreeType's BGRA bitmaps need a second, colour atlas and a `Picture` a
  glyph); each stays in `todo.md`. A colour glyph is drawn as its outline's coverage where
  there is one, else left out, as today.

### Hinting

- **`Fonts.hinting`**: `HintNone`, `HintLight` (FreeType's light target, vertical only —
  what every glyph gets today) or `HintFull`. The default is the platform's: none on macOS,
  light elsewhere. **A hinted advance is whole pixels**, so `metrics` answers it rounded at
  the scale — Termina's cells on Linux.

### The atlas

One 1024² page that never empties (`todo.md`) does not hold a terminal: four faces, CJK,
two sizes in a session and a settings preview. **Pages**: a glyph that does not fit starts
a new page (a texture each), and `glyphs` answers an op a page. **Eviction**: past a page
limit the least recently drawn page is cleared, as the laid-paragraph cache's generations
are. This is a prerequisite, not an extra.

## 3. An element that takes input raw

Today a program's events come only from controls (`Pressable`, `Editable`…), and
`Ui.handle` keeps Tab, the arrows, Page Up/Down, Home, End and Space for itself. A terminal
needs every key, the text the keys type, the input method's composition, and the pointer
in its own coordinates.

### The shape

```lyra
pub data Input<e> =
  Inert
  /// Focusable, and given every key, typed text and pointer event while focused (keys) or
  /// under the pointer (the pointer) — before the framework does anything with them. An
  /// answer of `None` hands the input back to the framework, which goes on as it would.
  | Raw(RawInput<e>)

pub struct RawInput<e> {
  on_key: (KeyInput) -> Maybe<e>,
  on_text: (TextInput) -> Maybe<e>,
  on_pointer: (PointerInput) -> Maybe<e>,
  /// Where typed text goes — the cursor's cell — for the input method's window, in the
  /// element's coordinates; `None` asks for no text.
  caret: Maybe<Rect> = None,
}
```

- **`Maybe<e>` lets the program decline**: a terminal declines nothing it understands, but a
  canvas in a form might take the arrows and decline Tab. The function is built in `view`
  from the state, so what a key means may depend on it (DECCKM's arrows).
- **Order** in `Ui.handle`: the app's shortcuts (below), then a raw element's handler — the
  focused one for keys and text, the one under the pointer (or holding it) for the pointer
  — then the framework's own handling, as today. Tab reaches a raw element first.
- **A raw element is focusable**: a click focuses it (`focusable` gains `Painted` with
  `Raw`), Tab reaches it, and `Focus(id)` (§4) gives it the focus from `update` — a new tab,
  Settings closed. Its focus ring is off unless its style asks (`:focus-visible`).

### Keys

```lyra
pub struct KeyInput {
  key: Key,
  mods: Modifiers,
  pressed: bool,       // false for a release
  repeat: bool,        // the system's auto-repeat
  code: u32,           // the physical key (SDL's scancode), for a layout-independent chord
}
```

- **`Key` gains the keys a terminal sends**: `F(i64)` for F1–F24, `Insert`, `ContextMenu`, and
  the keypad's (`Numpad(rune)`: digits, `+ - * / . =`, and `NumpadEnter`, which is `Enter`
  today). The rest stay `Unnamed`.
- **`ui.sdl3` reads each event's own modifiers and repeat byte** (today: `SDL_GetModState`
  when the batch is translated — stale for an earlier event in it — and the repeat ignored).
- **The app's shortcuts come first**: `App.shortcuts: (s) -> []Shortcut<e>` — a chord
  (`Key`, `Modifiers`, `primary` meaning ⌘ or Ctrl) and its event, matched before any
  element hears the key. Termina's ⌘T, Ctrl+Shift+T and Alt+1–9 are its shortcuts; the
  menus' accelerators will be. On a Mac whose menu bar is native, AppKit takes its key
  equivalents first and SDL hears them too, so a chord the native menu holds is left out of
  `shortcuts` (Termina's `tab_command` does this today).

### Text and the input method

- **`TextInput = Typed(string) | Composing(string, i64, i64)`**: the committed text, as
  `TextTyped` today; and the input method's composition in progress with its caret and
  selection, from `SDL_EVENT_TEXT_EDITING` (unbound today), so a terminal draws it at the
  cursor. A text field gets composition the same way — today it never shows one.
- **Text input follows the focus**: `place_text_input` asks for text where the focused
  field's caret is, *or* where a raw element's `caret` says, and stops it otherwise.

### Pointer

```lyra
pub struct PointerInput {
  at: Point,           // in the element's coordinates
  kind: PointerKind,   // Down(button, clicks) | Moved | Up(button) | Wheel(across, down)
  mods: Modifiers,
}
```

- **A press captures**: after a `Down` on a raw element, its moves and the `Up` go to it
  wherever the pointer is, until the last button is let go — a selection dragged past the
  window's edge. Every button is reported (today only the primary is).
- **The wheel is the element's if it says so**: an answer of `Some` takes it, `None` leaves
  it to the scroller around it. A program that asked for the mouse, or the alternate screen
  turning it into arrows, answers `Some`; the shell's scrollback answers `None` and scrolls.
- **The pointer's shape** is a style property, CSS's `cursor`, so a painter's command-line
  box can be an I-beam inside a grid that is an arrow.
- **Assistive technology**: a raw element is heard as AccessKit's `Terminal` role when it
  says so (`Node.label`, and a role the builder takes); its text as a value is later work.

## 4. Around them

- **Window focus**: `FocusGained`/`FocusLost` `InputEvent`s from SDL's window events
  (dropped today), and `Subscription.OnFocus((bool) -> e)` — a terminal's `?1004`, a hollow
  cursor while unfocused. Termina's `?1004` today follows ImGui's window focus, which never
  changes when the app loses the system's.
- **`Command.Focus(string)`**: the focus to an element by identity — a new tab's terminal.
- **The frame's cadence.** `ui` draws only when something changed; a terminal changes when
  its programs write. **`Subscription.Readable(i32, e)`**: the event when the descriptor has
  data to read. `ui.sdl3` runs one watcher thread (a C shim, as `bindings.pty` has) that
  `poll`s the descriptors the subscriptions name and pushes an SDL user event, which wakes
  the host's wait. An idle terminal then draws nothing, where Termina today draws at the
  refresh rate. Until it is built, `Every(16, Poll)` is the stand-in.
- **A scroller that follows its end**: CSS has no `stick-to-bottom`, but chat and terminal UIs
  all need it. **`Style.scroll_follow: bool`** (lui `scroll-follow`): a scroller at its
  end when its content grows stays at its end; moved off it, it stays where it was. And
  **`Command.ScrollTo(string, f32)`** for the program's own jumps (a submitted command).
  Termina's `at_bottom`/`set_scroll_y` becomes the one property.
- **Painting what is visible**: `PaintArea.visible` is the scroller's window onto a tall
  painted element, so a terminal paints only the lines in view, as Termina's
  `line_at_y(scroll_y)` does today.

## 5. Order

Each step lands with its tests in `cmd/lyrac/ui_test.go` and an example; Termina moves at
the end.

1. **`Painted` and `PaintOp`** with `PaintDraw` and `PaintText`, `on_size`,
   `PaintArea.visible`; an example drawing a checkerboard and text that follows the window's
   size. **Built 10/22.**
2. **Atlas pages and eviction.** Needed by everything after. **Built 10/22**: four pages
   of 1024² (`ATLAS_PAGES`), the least recently drawn emptied, never under the frame drawing
   from it.
3. **Faces**: weight and italic, variable fonts, synthesized bold and oblique,
   `FontMetrics`/`MeasureFont`, hinting. **Built 10/22.**
4. **Fallback**: family lists, `Fonts.fallbacks`, itemized runs, every rune.
5. **`PaintCells` and `PaintWave`**: an example drawing a coloured grid of mixed-width text against
   pixels read back (`read_frame`).
6. **Raw input**: `RawInput`, `KeyInput` and the wider `Key`, `TextInput` with
   composition, `PointerInput` with capture, `App.shortcuts`, `OnFocus`, `Focus`.
7. **`Readable`** and **`scroll_follow`**/`ScrollTo`.
8. **Termina**: the grid, the box and the cards as one painter over a following scroller;
   then the tab bar, menus and Settings (the next note).

## 6. Open questions

1. **Does `paint` stay out of the layout cache entirely?** A painter's ops are rebuilt every
   redraw. A terminal redraws on every write anyway; a static canvas would want them kept
   until its state changes — a `version: i64` in `Painter`, compared by `same_kind` and
   keyed into a paint cache, would do it. Not needed for Termina; say so if it is wanted.
2. **`PaintCells` in the painter's colour runs, or a colour a cell?** A colour a cell is simpler for
   the program and one `Glyphs` op a colour for the renderer either way (`paint_into` can
   group). Proposed as runs, matching how Termina batches; worth measuring.
3. **Is the terminal's scrollback a `ui` scroller at all?** The alternative is a painter fixed to
   the window that scrolls itself, drawing its own scrollbar — fewer framework additions, a
   scrollbar unlike every other. Proposed: the scroller.
4. **Shortcuts as data, or menus?** `App.shortcuts` is a flat chord table; when the drawn menu
   bar comes, its items' accelerators could produce it. Proposed: the table now, the menus
   built on it.
