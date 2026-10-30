# What Vega needs of `ui` — design

**Status: proposed 10/26; steps 1–3 (§2–§6: pictures, tooltips, modals, the smaller controls,
dropped files) built 10/26, steps 4–6 (§7, docking in one window, §8, several windows, and §9,
lui's elements) 10/27, the rest to come.** Vega, the game studio (the private `vega`
repo), runs on Dear ImGui through `bindings.imgui`. This is the move onto `ui`, as Termina
made its own (`TERMINAL.md`, `WINDOW.md`): §1 is what Vega's interface does today, §2–§9 what
`ui` must gain for it, §10 Vega's new front end, §11 the order and §12 the questions left.

**Decided 10/26** (the user's choices):

- **Docking and torn-off windows are built in `ui` first**, so Vega gives up nothing it has —
  editors docked, split and rearranged, and torn off onto another monitor. Every `ui` program
  gets them.
- **A new front end beside the old one.** Vega's model, commands and `Document` stay as they
  are; a second front end is written over them on `ui`, editor by editor, with its own
  scripted checks, while releases keep shipping the ImGui one. When it does everything the
  ImGui one does, `Vega.app` is built from it and the ImGui front end, `mac_look` and
  `ui_checks.lyra` go — as Termina's `gui/` and `uitest` went.

**The forcing function is Vega**, and each addition is meant for any `ui` program, not
shaped to Vega. The end of the work is `bindings.imgui`'s last user: after Vega, nothing in
the workspace needs it.

## 1. What Vega's interface does today

About 23,000 lines across 26 files import `bindings.imgui` (381 distinct names), 9,000 of them
the checks. By what they need:

- **One window of docked editors** (`HOST_DOCKING`, `dock_space_over_viewport`): any number of
  sprite, tile-set, map, entity, scene, sound, instrument and song editors, and the Palettes,
  Collision layers and Problems panels, as tabs in a dock space — split, dragged between tab
  groups, and **torn off into windows of their own** on another monitor (`HOST_VIEWPORTS`).
  The arrangement persists in `layout.ini` beside the settings.
- **The window's furniture**: a drawn title bar (`HOST_CUSTOM_TITLE_BAR`, traffic lights on a
  Mac), menus installed in the Mac's bar or drawn (`menus.lyra`, already a tree of
  `bindings.imgui.ui` nodes), an editors' bar of pixel-art icons under it, and a status bar.
- **Canvases**: the sprite and tile-set editor's pixels, the map editor's cells and the scene
  editor's planes are **textures Vega writes** (`create_texture`, `update_texture`) and draws
  scaled, nearest-sampled, under overlays drawn with ImGui's draw list (selection, pieces,
  collision shapes, the camera's zone, the grid). Input on them is the pointer's — strokes,
  drags, the wheel and Cmd+wheel — and keys held (Space to pan, modifiers).
- **Graphs and grids drawn by hand**: the sound editor's bars, the instrument editor's
  algorithm and envelopes, the song editor's pattern grid (only rows in view drawn, a frozen
  header), the colour grid, the tool icons.
- **Panels of forms** (`form.lyra`): labels right-aligned, fields — integer and float
  fields with ± buttons, sliders, checkboxes, radio buttons, combos, text fields, colour
  editors — and tables (resizable columns, a frozen header, alternating rows), collapsing
  sections, separators with text, a progress bar, tooltips on hover, and side panels whose
  edge is dragged (`side_panel.lyra`).
- **Dialogs**: modal popups (the import dialogs, the unsaved-changes question, a typed path),
  the system's file dialogs, and **a file dropped on the window or opened from Finder**
  (`take_dropped_file`).
- **Checks** (`ui_checks.lyra`): the real frame loop on a headless host, clicked and typed at
  through widgets named by `probe(…)`.

## 2. Pictures the program changes

The prerequisite of every canvas. `ui.draw` has `Picture(TextureId, …)` and the renderer
`upload`/`update`, but an app's `update` is pure and never holds the renderer, so a program
on `ui.app` has no way to put its own pixels on the screen.

**Proposed: a picture is a value the view names, kept by the host.**

```
pub let picture<e> = pure (key: string, image: Image, revision: i64, size: Size) -> Node<e>
```

- The host keeps a texture per key, uploading the first time and **writing it again only
  when the revision moves** (Vega's `doc.revision`, a map's fingerprint) — `update` in place
  where the size is the same, a new upload where not. A key no longer in any view drops its
  texture at the next frame (renderers gain `release`).
- `pixelated` on the `Image` keeps the texels crisp at any zoom — the canvases' case.
- **A painter draws a picture by its key** (`PaintPicture(key, texels, at)`), so a canvas can
  draw its texture scaled and panned under its own overlays; the key resolves to the texture
  at paint, where the host has it.
- A dirty rectangle (`revision` plus `changed: Maybe<Rect>`) is the later optimisation for a
  large map painted a cell at a time; measured first — and not needed (§12, 4).

**Built 10/26** as proposed, with two changes: the host keeps the textures in the
`GlyphAtlas` that `paint` is handed (no state of its own to thread), and a painter's
`PaintPicture` carries its `PictureSource` itself rather than naming another element's key, so
a canvas needs no hidden `picture` element beside it. `picture_fill` tiles, stretches or covers
a box with one. `examples/ui/pixels.lyra` is a pixel editor on it.

## 3. Tooltips

A hover held still (CSS has none; macOS's is about half a second) shows a small floating box
near the pointer, gone at the pointer's next move or press. **`.tooltip(text)` on any node**,
and `.tooltip_node(node)` for one with structure — the editors' bar's `New sprite editor
(⌘1)`. On the popup layer (`Laid.floats`), placed below the pointer and moved in from the
window's edges; never takes input; heard as the element's accessible description.

**Built 10/26** (`ui/README.md`): as above, but a move within the node keeps a tooltip showing —
as a browser's `title` does — and only leaving the node hides it; a press, a key or the wheel
hides it until the pointer leaves. The rest is timed on the host's clock (`Ui.tip_due`, which
the loop waits for), the box is `Tooltip` (up to 320 wide), and the description reaches
AccessKit through a new `description` on `AccessNode` and the shim.

## 4. Modal dialogs

**`modal(open, content, on_dismiss)`**: while open, the window dimmed under it and nothing
else taking input; the focus moved into it and held there by Tab; Escape raises
`on_dismiss`. Centred, at most the window less a margin, scrolling inside past it. Built on
the popup layer. The unsaved-changes question, the import dialogs and the typed-path prompt
are its users.

**Built 10/26** as **`modal(under, open, content, on_dismiss)`** — wrapping what it is over, as
`popup` wraps its anchor, so it needs no place of its own in a row or column. A press outside
the dialog and the window losing the focus raise nothing (unlike a popup's); the focus goes
back to what had it when the dialog closes. The scrim (`ModalScrim`, its padding the margin)
and the dialog (`Dialog`) are tags, not parts, so the program's sheets reach `under` as before.
Example `examples/ui/dialogs.lyra`.

## 5. The smaller controls

Each with its lui element, parts for the cascade, and keys:

- **A number field** (`number_field`): an integer or float, typed, stepped by ± buttons and
  the arrow keys, dragged across its label to scrub, held to a range — ImGui's `input_int`,
  `input_float` and `slider_int` in one.
- **Radio buttons** (`radio_group`): one of several, arrows moving the choice.
- **A disclosure** (`disclosure(title, open, content, on_toggle)`): a collapsing section.
- **A progress bar**, and **a separator with text**.
- **A splitter** (`split(first, second, at, on_drag)`): two panes and the edge between them
  dragged, the position the program's — a side panel's edge, and the dock's (§7) splits.
- **A table**: a header row that stays while the body scrolls, columns resizable by their
  header's edges (widths the program's), alternating rows, a row selectable. Grid lays it
  out; the header and the column edges are what is new.

**Built 10/26** (`ui/layout/forms.lyra`, `table.lyra`, `ui/README.md`), all but the lui
elements, which are step 6's. Three new pieces of the framework carry them: an element dragged
to change a number (`Drags` — the splitter's edge, a column's, a number field's label), a group
with one Tab stop whose arrow keys move the choice (`Radio` in a `OneOf` — the radios, and the
table's rows), and a field's Up and Down (`FieldSpec.on_step`). Changed on the way: the splitter
is `splitter`, beside the prelude's `split`; a number field's `on_change` is a parameter
(`number_field(spec, on_change)`), since only a parameter's `pure` lets the view call it; the
table is laid out by rows of fixed-width cells rather than grid, its header's edges on a stack
over the header. Example `examples/ui/panels.lyra`.

## 6. Files dropped on the window

`InputEvent.FilesDropped([]string)` and **`Subscription.OnFilesDropped(([]string) -> e)`** —
on a Mac also a file opened from Finder, which SDL hands over as a drop
(`bindings.imgui.take_dropped_file` today). `ui.sdl3` gathers `SDL_EVENT_DROP_FILE` between
`DROP_BEGIN` and `DROP_COMPLETE` into one event; a script drops with `drop:PATH`.

**Built 10/26** as described (`drop:A|B` drops two at once).

## 7. Docking

**The arrangement is the program's data**, as a tab bar's order is: a `DockTree` the program
keeps (and saves, in Vega's settings, in place of `layout.ini`), changed only by the events
the dock raises.

```
pub data DockTree = DockTabs(DockTabsSpec) | DockSplit(Axis, f32, DockTree, DockTree)
pub let dock<e> = pure (tree: DockTree, panel: (string) -> DockPanel<e>, on_change: (DockTree) -> e) -> Node<e>
```

- **A leaf is a tab group** (`tab_bar`, §3 of `WINDOW.md`): panels by key, the selected one
  shown. A split is two subtrees and the fraction between them, its edge a splitter (§5).
- **Dragging a tab** out of its strip shows where it would land — the middle of a group (its
  tabs), a side of it (a new split), the dock's outer edges — as ImGui's dock targets do; the
  drop raises `on_change` with the tree rearranged, an emptied group and a split of one
  removed. **Outside the window, it tears off** (§8).
- Closing a tab is the program's, through its panel's `on_close`; a panel the program stops
  listing leaves the tree at the next change.
- Pure functions build and edit trees (`dock_with(tree, key, near, side)`, `dock_without`),
  so a program opens a new editor next to the last focused one, as Vega's View menu does.

**Built 10/27** (`ui/layout/dock.lyra`, `ui/README.md`). Changed on the way: a split holds its
parts in an array (`DockSplit(DockSplitSpec { axis, at, parts })`), since a type holding itself
directly has no size and an array boxes it; closing is the dock's `on_close(key)`, not each
panel's, and a panel the program stops listing stays until it takes it out (`dock_without`);
`panel` is `pure (string) -> DockPanel<e>` (`{ title, content, closable }`). A split's edge is
the splitter's `Drags`, its value a share of the room (`DragSpec.share`). The drop targets are
zones, not ImGui's buttons: within `DOCK_EDGE` of the dock's edge a split of the whole, else a
group's strip or middle (among its tabs) or the side the pointer is nearer, previewed as
`DockPreview`. Example `examples/ui/dock.lyra`.

## 8. Several windows

**`App.windows: (s) -> []WindowSpec`** — the main window and others, each with a key, a
title, a place and a size, and its view (`view_window: (s, string) -> Node<e>`). A torn-off
dock tab is a window whose view is a dock of its own, its tree in the program's state; the
window's close is the program's (`on_close`), and docking it back is a drag into the main
window's dock.

- **One platform, several windows.** The `Platform` trait gains a window id on the calls
  that name one (size, scale, cursor, title, position, areas) and on the input events, and
  `open_window`/`close_window`; `ui.sdl3` routes SDL's events by their `windowID`. One
  renderer device, a swapchain a window (`ui.gpu`); the glyph atlas is shared — a window on a
  display of another scale rasterises at its own.
- **The host keeps a `Ui` a window** (focus, hover, scroll by identity are a window's), lays
  out and draws only the windows that changed, and menus stay the app's (the Mac's bar is
  one; elsewhere each window that wants them draws them).
- A torn-off window's place is the program's, saved with the dock tree: a monitor that has
  gone is ignored as `set_window_position` already does.
- **Wayland cannot place windows**, so a tear-off lands where the compositor puts it there.

**Built 10/27** (`ui/app.lyra`, `ui/backend.lyra`, `ui/README.md`). Changed on the way: the
windows are beside the main one, not including it — `App.windows: [Windows { of, view,
on_close, on_moved }]` (an array of at most one, as `App.menus` is), `of` answering a
`WindowSpec` with a `key` and a `place`, `on_moved` hearing a `WindowPlacement` the program
keeps; the main window stays `App.view`'s. Every `Platform` call about one window takes a
`WindowId` (`MAIN_WINDOW` the one it opened with), the cursor and the clipboard staying the
platform's; `next_events` answers `(WindowId, InputEvent)` pairs, `CloseRequested` is a window's
close and `QuitRequested` the program's, and `to_screen`/`from_screen` carry a point between
windows. The renderer is told the window (`draw(window, …)`, `window_closed`) and takes a new
one on at its first frame: the GPU claims it on its one device; SDL's 2D renderer, which cannot
share a texture between windows, makes it a renderer of its own and copies every texture into
it. The atlas is shared as proposed, a picture let go only when no window's last frame showed
it. A tab is carried between windows by the host, aimed at the dock under the pointer as a
`foreign` `DockDrop`; it tears off only where the dock says so (`.tears_off(on_tear)`, a
`DockTear` — what Vega's View menu needs no more than a dock does), and never a dock's only
panel. Not done: AccessKit hears the main window alone, and `OnResize`/`OnFocus`/
`OnWindowState` are the main window's. Example `examples/ui/windows.lyra`.

## 9. lui

`<Picture>`, `.tooltip`, `<Modal>`, `<NumberField>`, `<Radio>`, `<Disclosure>`,
`<Progress>`, `<Splitter>`, `<Table>`, `<Dock>`, and `<window>` for a second window; and
`<script target="ui">` for Vega's two views, `settings_view.lui` and `import_view.lui`, which
are `bindings.imgui.ui` markup today.

**Built 10/27** (`tools/lui/lui.lyra`, `bindings/README.md`): every element above, each the
`ui.layout` call it names. Changed on the way: a picture's key is `name` (`key` stays every
element's identity); a number field's text is its label, as a button's is; radio buttons are
`<RadioGroup>` of `<Option>`s, as a select's are, beside a lone `<Radio>`; `.tooltip` is two
common attributes, `tooltip="…"` and `tooltip-node={…}`; a table's columns are `<TableColumn
width>`s and its rows `<TableRow>`s of cells, `<for>` and `<if>` among them; `<Separator>` is
the rule, its text in it; a second window is `<window name="…" props={{ … }} key place>`, a
function of its props, any number to a file. **Vega's two views move with its front end**, not
here (§10): they import its ImGui state, and the ui ones will import `Studio`'s — Settings with
the shell, the import dialog with the import dialogs, last. Proof that the elements work is
three examples whose views became markup — `panels`, `dialogs`, `windows` — their scripted
runs unchanged and the first two's frames byte-identical. On the way: an import used only in a
struct field's default warned unused; a closure calling its enclosing function's `pure`
parameter was refused as impure; and a closure inside a spread (`[...[…]]`, which lui writes for
an `<if>` in a `<for>`) took no retains — a use-after-free (COMPLETED.md 10/27).

## 10. Vega's new front end

Written in `vega/src/face/` beside the ImGui front end, over the same `Document`, commands
and model — the reason the command layer exists: **every edit is already an `Edit`**, so a
view raises events that become `doc.run(edit)` in `update`, and nothing in the model changes.

- **State and events**: `Studio` (the document, the open editors by key, the dock tree, the
  settings, what is playing, builds) and `StudioEvent`, an editor's events mapped in
  (`Node.map`). **Undo gestures** (`run_gesture`, ended when nothing is held) become a press
  and a release event around a stroke.
- **The frame's other work**: audio previews, a song playing, builds polled and a game run —
  today `run_frame`'s — become subscriptions (`EveryFrame` while something plays, `Readable`
  on a build's output) and commands.
- **Checks**: `face_checks.lyra` checks the new front end headless. The **event-level**
  checks run and pass — build the studio, raise a panel's events (a swatch's press, the
  picker's Done and Cancel, a palette's rename; a layer added, deleted and renamed), and
  check what each does to the state. **Render-level** checks drive the window's own loop
  (`running`/`step_running` over `ui.script`'s platform, SDL's dummy video and software
  renderer) and find a widget by its `.keyed` key where the loop laid it out, as
  `ui_checks` finds one by probe: a drag across the sprite editor's canvas paints it. (They
  were thought blocked on `shared Running` not seeing the loop end; it does — a script's
  end asks to close, which unsaved changes hold open, so the passes are bounded.)
- **The window does not scroll** (`App.window_overflow: OverflowVisible`): under `ui.app`'s
  default, `OverflowAuto`, the body got the height it asked for and the dock its content's.
- **The look**: a `ui` theme for each of macOS's light and dark looks, in place of
  `mac_look`; and the Latin-1 rule goes, `ui` drawing any text its fonts have.
- **Order**: the shell (window, menus, editors' bar, status bar, Settings, file dialogs and
  drops, the dock), then Palettes, Problems and Collision layers, then the sprite and
  tile-set editor (the first canvas), map, entity, scene, sound, instrument and song editors,
  then the import dialogs. `vega --ui` opens the new front end while it grows. Settings and the
  import dialog stay markup, written anew for `ui` (`<script target="ui">`, §9) beside their
  ImGui files until the switch.

## 11. Order

Each step lands with tests in `cmd/lyrac/ui_test.go` (scripted, at `-O0`) and an example.

1. **Pictures** (§2), example `examples/ui/pixels.lyra`: a small pixel editor, its canvas a
   picture repainted per stroke and zoomed. **Done 10/26.**
2. **Tooltips and modals** (§3, §4), example `examples/ui/dialogs.lyra`. **Done 10/26.**
3. **The smaller controls** (§5) and **dropped files** (§6), example `examples/ui/panels.lyra`.
   **Done 10/26.**
4. **Docking** in one window (§7), example `examples/ui/dock.lyra`. **Done 10/27.**
5. **Several windows** (§8), and tearing a dock tab off and back, example
   `examples/ui/windows.lyra`. **Done 10/27.**
6. **lui's elements** (§9), examples `examples/ui/panels_view.lui`, `dialogs_view.lui` and
   `windows_view.lui`. **Done 10/27.**
7. **Vega's front end** (§10): the shell (window, menus, editors' bar, status bar,
  Settings, file dialogs, dock) **10/29**; Problems panel **10/29**; **Palettes panel and
  its colour picker 10/29** — a swatch's press opens a picker: RGB sliders and a live
  preview for the free-colour consoles, the console's fixed colours for the fixed ones —
  and the **Collision layers panel 10/29**, every layer its name typed over and a button
  deleting it, then Add up to the console's mask; each change one undoable edit, checked
  at the event level in `face_checks.lyra`. **The sprite and tile-set editor's first
  slice 10/29** (`face/picture_editor.lyra`): the canvas a picture fingerprinted by its
  pixels (a gesture's amends count no revision), pencil, eraser, fill, line, box and picker,
  the brushes, the picture and frame, zoom (View's too) and the grids — a stroke one gesture.
  Its selections, shapes, flips, frame strip, animations, preview and resizing to come.
  **The map editor 10/29** (`face/tile_map_editor.lyra`): stamp, erase, fill and pick (a
  drag a stamp of cells, as is a drag across the tile picker's tiles), the canvas a fixed
  viewport the view pans and zooms about the pointer, the grid, plane and screen, the tiles'
  shapes, the map's properties and Edit tiles; a size typed is a draft made by Resize, since
  a number field raises each edit and resizing to 6 on the way to 64 would drop cells; and
  the map's own collision shapes (the Shapes tool, a drag a box or a move, and the panel's
  name, kind, layers, one-way edge, place and size). The sprite and tile-set editor's
  **collision shapes, keys and frame operations 10/29** — one shapes section for both
  editors (`face/shapes.lyra`) — and **selections and the clipboard 10/29**, the canvas
  taking Cmd+A, C, X, V and D itself (an Edit menu item's shortcut would be taken from
  every text field too: a window's shortcuts run before any element hears a key) — and
  **the frame strip 10/29**, a thumbnail a frame in a row that scrolls across — and **the
  animations section 10/29**, each step over two lines in the narrow panel. **The entity
  editor 10/29** (`face/entity_editor.lyra`): the list, the name, the sprite and the
  animation it starts with, the colliders — a shape and the layers it is on and meets as
  checkboxes, in place of ImGui's combos of them — and the movement in pixels a frame, over
  a preview playing the start (`EveryFrame` only while an entity editor's tab is shown),
  each collider outlined in its row's colour. **The scene editor 10/29**
  (`face/scenes.lyra`): the scenes and their properties (name, maps behind and in front,
  the camera's zone), a canvas of the maps — each a picture of its cells, the front over the
  back — the placements as the game first shows them and the camera's starting screen and
  zone; Select drags a placement or the zone (its corner resizes it), Place puts an entity
  down, by whole tiles while Snap is on, a drag one gesture; and the side panel's entities,
  placements and the chosen one's fields. **The sound editor 10/29** (`face/sounds.lyra`):
  the sounds and their voice, note, length, loop and release; the volume, pitch and fine
  graphs a bar a frame, painted, a drag across one a gesture, scrolled sideways together
  past the panel, the line under them saying what the step under the pointer plays; each
  graph's wave in a disclosure, its dots over the bars; Play heard as the game will hear
  it, a line following it on the same `EveryFrame` as the entity preview. **The instrument
  editor 10/29** (`face/instruments.lyra`): the algorithm drawn beside the channel's values,
  the operators a CSS grid of sliders (`minmax(0, 1fr)` columns, so a narrow panel squeezes
  them), each envelope drawn; with no drag-end event in `ui`, a run of edits to one field
  is one undo step by amending the last (`doc.amend`, while `doc.revision` is the one it
  left); a sample's wave; Import through the system's dialog; and the piano — pointer, the
  Z and Q rows once it has the focus, or a phrase — heard live through the studio's one
  `fm_preview` channel every frame while a note sounds and through its release. **The song
  editor 10/29** (`face/songs.lyra`), a tracker: the pattern grid one painted element, its
  cells on a fixed pitch (`PaintCells`, in a `mono` family Vega loads, though the pitch holds
  in any font), scrolled a row at a time — rows in view only are drawn — and sideways with
  the channels; the order list painted beside it; Z/Q-row notes, hex digits, Shift-selected
  blocks with Copy, Cut, Paste and Transpose, each one undo step; Play and a typed note's
  audition through `song_preview` on the same `EveryFrame`. **The sprite editor's preview
  and onion skin 10/29**: the preview in the side panel plays the animation chosen, or the
  frames in turn, on the entity preview's `EveryFrame` (Enter on the canvas plays and
  stops); the onion skin is the frame before as a second picture under the canvas's, at a
  third of its opacity. **The sprite editor's properties and true colour 10/30**: the name,
  the size — typed in pixels as a draft that Resize makes, as the map's is, for a sprite a
  console draws at any size, else chosen in whole tiles — the palette, and Make true colour
  or Make indexed (`fit.made_*`, one edit); a true-colour picture's left brush made with
  R, G, B and A sliders. The import dialogs to come. `vega --ui` opens the new front end.
8. **The switch**: `Vega.app` and the Linux releases built from the new front end; the
   ImGui front end, `mac_look` and `ui_checks.lyra` removed, and `bindings.imgui` with no
   user left in the workspace.

## 12. Open questions

1. **Is the dock tree the program's or the framework's?** Proposed: the program's (§7), as a
   tab bar's order is — Vega opens editors beside others and saves the arrangement, and a
   framework-held tree would need commands for both. The cost: every docking program holds a
   tree and an event.
2. **One `Ui` a window, or one with identities prefixed by the window?** Proposed: one a
   window — focus is a window's, and a window closing drops its state whole.
3. **Does a pure `update` copy too much of Vega's state?** **No (measured 10/26).** A
   pure update cannot write the pixels the last state holds, so a stroke's step copies them
   (`[...pixels]`) — 0.13 ms for 512×512 (1 MB), 0.42 ms for 1024×1024, on an M-series Mac
   at `-O2`; the rest of the state is fields and aliased arrays, copied shallowly. A stroke
   that changes no pixel copies nothing (`pixels.lyra`'s `drawn`). A `Studio` can hold the
   project as it is.
4. **Do the canvases need dirty rectangles** (§2) on a large map from the start? **No
   (measured 10/26).** Writing a whole 512×512 texture again costs 0.15 ms through SDL's 2D
   renderer and 0.22 ms on the GPU (1024×1024: 0.44 and 0.64 ms), against a frame of 8.3 ms
   at 120 Hz. A copy and a full upload together are under 4% of that frame for a 512×512
   picture; `--size 512 --time` on the example shows no change in a pass's cost from 32×32.
   Revisit for a map past 2048 a side.
5. **The macOS look's future.** `mac_look` was an experiment in how near ImGui comes to
   AppKit; a `ui` theme can come nearer (SF from the system, the window's materials), so it is
   proposed as the default look rather than a setting.
