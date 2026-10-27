# What Vega needs of `ui` — design

**Status: proposed 10/26; steps 1–3 (§2–§6: pictures, tooltips, modals, the smaller controls,
dropped files) built 10/26, the rest to come.** Vega, the game studio (the private `vega`
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

## 9. lui

`<Picture>`, `.tooltip`, `<Modal>`, `<NumberField>`, `<Radio>`, `<Disclosure>`,
`<Progress>`, `<Splitter>`, `<Table>`, `<Dock>`, and `<window>` for a second window; and
`<script target="ui">` for Vega's two views, `settings_view.lui` and `import_view.lui`, which
are `bindings.imgui.ui` markup today.

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
- **Checks**: `face_checks.lyra` drives the new front end headless with `ui.script`'s
  platform, elements found by key as `ui_checks` finds them by probe; each ImGui scenario is
  ported before its editor counts as moved. `--check` runs both until the switch.
- **The look**: a `ui` theme for each of macOS's light and dark looks, in place of
  `mac_look`; and the Latin-1 rule goes, `ui` drawing any text its fonts have.
- **Order**: the shell (window, menus, editors' bar, status bar, Settings, file dialogs and
  drops, the dock), then Palettes, Problems and Collision layers, then the sprite and
  tile-set editor (the first canvas), map, entity, scene, sound, instrument and song editors,
  then the import dialogs. `vega --ui` opens the new front end while it grows.

## 11. Order

Each step lands with tests in `cmd/lyrac/ui_test.go` (scripted, at `-O0`) and an example.

1. **Pictures** (§2), example `examples/ui/pixels.lyra`: a small pixel editor, its canvas a
   picture repainted per stroke and zoomed. **Done 10/26.**
2. **Tooltips and modals** (§3, §4), example `examples/ui/dialogs.lyra`. **Done 10/26.**
3. **The smaller controls** (§5) and **dropped files** (§6), example `examples/ui/panels.lyra`.
   **Done 10/26.**
4. **Docking** in one window (§7), example `examples/ui/dock.lyra`.
5. **Several windows** (§8), and tearing a dock tab off and back.
6. **lui's elements** (§9).
7. **Vega's front end** (§10): the shell first, then editor by editor, each with its
   checks.
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
