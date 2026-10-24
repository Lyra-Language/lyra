# What a terminal's window needs of `ui` — design

**Status: proposed 10/23; steps 1–6 built 10/23** (a file dialog — `ChooseFile` through `Platform.choose_file`, `bindings.dialog` queueing SDL's answer and waking the wait, a script's `choose:`; the window — `WindowMinimize`/`WindowMaximize`/`WindowRestore`, `OnWindowState`, `DrawnTitleBar` with `bindings.hittest`, `window_buttons` drawn as lines and boxes chosen at layout, the areas told after each redraw; the double click on a drag area still open; a tab bar — `tab_bar`, a tab carrying the spec's functions since a builder may call none, the × no control of its own, `overflow-x: hidden` scrolled by the wheel and the selection, the focus kept by a × press; menus native — `App.menus` through `Platform.set_menus` as tagged `NativeMenu`s, reinstalled only on a change of shape, `MenuChosen`, the bar's keys left to it, `menubar.m` waking SDL's wait through `dlsym`; menus drawn — `menu_bar`, `menu_button`, `Spelled` shortcuts spelled by `Viewport.apple`, the app taking every drawn menu's shortcuts, Tab from an open menu moving on from its title; popups — `popup`, `place_floats`, the select's list on them; the content as wide as it needs, laid out twice, as CSS's shrink-to-fit; over the anchor where neither side has room; `examples/ui/popups.lyra`). The second half of `TERMINAL.md`'s step 8: what Termina's window
still takes from Dear ImGui, now that its tabs run on `ui` (`termina/build/termina-ui`, beside
the ImGui app). §1 is popups, which the rest stand on; §2 menus, §3 a tab bar, §4 the
window's own controls and title bar, §5 a file dialog, §6 colour, §7 the smaller pieces and
lui. §8 is the order to build it in, and §9 the questions left for whoever builds it.

**The forcing function is Termina** again, and the end of the work is the ImGui app's: when
termina-ui does all of the below, `Termina.app` is built from it, and `termina/src/gui/`,
`bindings.imgui` as Termina's dependency and `uitest` (replaced by scripted checks) go. Each
addition is meant for any `ui` program — Vega's editors are the next to want menus, tabs and
colour — not shaped to Termina.

## What Termina's window does today

The ImGui front end (`termina/src/termina.lyra`, `gui/`, `menus/`, the two `.lui` files):

- **A tab bar** (`gui/tabs.lyra`, ImGui's): a tab a session, labelled with its title,
  dragged into a new order (which ImGui keeps to itself, so `tab_bar` reads it back each
  frame), closed by its ×; a Settings tab out of the order at the bar's right end
  (`TAB_ITEM_FLAGS_TRAILING`), and `+` there off macOS.
- **Menus, one tree** (`menus_view.lui`) shown three ways: installed in the Mac's own bar
  through `bindings.menubar` (once, greyed each frame), drawn as ImGui's main menu bar off
  macOS, and on Linux as a GNOME header bar's ☰ popup with submenus. Items have shortcuts
  (shown; the keys act where they are read), checks (the Window menu's shown tab) and
  greying (Edit's, from `Session.can`).
- **A title bar of the program's on Linux** (`HOST_CUSTOM_TITLE_BAR`, ImGui's host in C++):
  a borderless window whose hit test makes the bar's empty stretch move it and its edges
  resize it, the window's buttons (minimize, maximize or restore, close) drawn as lines, and
  `+` and ☰ beside them the bar's full height.
- **The Settings tab** (`settings_view.lui`): combos (the font family — a long list — and
  the theme, saved themes after a separator), two sliders with formatted values, a text
  field, buttons (one disabled), a checkbox, twenty-nine colour swatches each opening a
  picker, groups side by side wrapping as the width allows, and **Other Font File…**, the
  system's open dialog (`show_open_dialog`, SDL's, asynchronous).

`ui` has buttons, checkboxes, sliders, a select whose list floats over the page
(`Laid.floats`), text fields, flex and grid (wrapping included), and `App.shortcuts`. It has
no popup but the select's, no menus, tabs, colour, window controls or dialogs.

## 1. Popups

**One floating layer, generalized from the select's.** A select's list is laid out under it,
taking none of its size, painted after the whole tree outside every clip and hit before
anything else. Menus, a menu button's popup and the colour picker are the same thing, so the
list becomes a case of it rather than three copies:

```
pub let popup<e> = pure (anchor: Node<e>, open: bool, content: Node<e>, on_dismiss: e) -> Node<e>
```

- **Open is the program's, not the framework's.** A select's open list is the framework's
  (`Interaction.open`) because it holds nothing the program cares about; a menu's opening is
  what the program's menus say, and a colour picker stays open while the program changes the
  colour under it. `on_dismiss` is raised by Escape, a press outside every open popup, or the
  window losing focus. The select keeps its framework state and is built on the same layer.
- **Placed to fit**: below the anchor, flipped above when it would leave the window's foot,
  moved in from the right edge, and at most the window's height, scrolling inside past it
  (this closes the select's open todo: its list neither scrolls nor flips). A submenu's
  popup goes to the anchor's right, or its left where the right has no room.
- **Several open, nested**: popups opened from inside a popup stack, each above the one it
  came from; a press inside any of them dismisses none.
- **Focus moves into a popup when it opens from the keyboard**, and back to its anchor when
  it is dismissed. Heard as a menu or dialog by role (§2, §6).

## 2. Menus

**A menu is data, not a node**, because one tree is shown three ways and one of them is not
drawn by `ui` at all:

```
pub data MenuEntry<e> = MenuItemEntry(MenuItem<e>) | Submenu(string, []MenuEntry<e>) | MenuSeparator
pub struct MenuItem<e> {
  label: string,
  on_select: e,
  shortcut: Maybe<Shortcut<e>>,   // shown, and taken by the app (below)
  enabled: bool = true,
  checked: bool = false,
}
pub struct Menu<e> { label: string, entries: []MenuEntry<e> }
```

- **Drawn**: `menu_bar(menus)` — the labels in a row; a press opens a menu's popup, and while
  one is open the pointer over another label moves to it, as every menu bar does; the arrows
  walk items and across menus, Enter chooses, Escape closes. `menu_button(face, entries)` —
  any node (a ☰) opening a popup of the entries. Both are §1's popups, with the menu's
  open path the framework's (`Interaction`, by identity), since no program wants it. An
  item shows its shortcut at its right in the platform's spelling (`⌘⇧T` on a Mac,
  `Ctrl+Shift+T` elsewhere: `shortcut_text`), a check, and greys when disabled.
- **Native on a Mac**: `App.menus: (s) -> []Menu<e>` (an array of at most one function, as
  `shortcuts` is). Where `bindings.menubar` installs, the host installs the tree — the first
  menu as the application menu, macOS's own Window menu after — and each pass sets the items'
  enabled and checked from the tree as it is now (`set_enabled`, `set_checked`). It
  **installs again when the tree's shape changes** (labels, entries, their order), which
  Termina's never does on a Mac but a program's may. A chosen item's tag is drained by
  `ui.sdl3` after SDL's events (`poll_menu`) and raised as the event it was installed with.
  **`menubar.m` must push an SDL event when it queues a choice**, or a loop asleep in SDL's
  wait does not hear a click until the next input.
- **Shortcuts come from the menus**: an item's `shortcut` is in `App.shortcuts`' table
  without being written twice, matched before any element hears the key (`TERMINAL.md`'s
  open question 4, answered). Where the Mac's bar holds it, AppKit's key equivalent fires the
  item and SDL hears the key as well, so the host leaves a natively installed chord to the
  bar.
- **Off a Mac** `App.menus` installs nothing; the program draws `menu_bar` or a
  `menu_button` where it wants them. This replaces both Termina's and Vega's walks of an
  ImGui tree into `bindings.menubar` (todo.md, "install a tree in the Mac's bar").

## 3. A tab bar

```
pub let tab_bar<e> = pure (spec: TabsSpec<e>) -> Node<e>
pub struct TabsSpec<e> {
  tabs: []TabSpec,
  selected: i64,
  on_select: (i64) -> e,
  on_close: (i64) -> e,
  on_move: (i64, i64) -> e,   // from, to
  after: []Node<e> = [],      // after the tabs: `+`, a menu button
}
pub struct TabSpec { label: string, key: string, closable: bool = true, movable: bool = true }
```

- **The order is the program's.** A drag raises `on_move(from, to)` as the tab passes half its
  neighbour, and the program reorders its own list, so the strip, `⌘1–9` and the menus can
  never disagree — ImGui keeps its order and Termina reads it back each frame (`TabBar.order`),
  which this retires. A tab that is not `movable` — Termina's Settings tab, kept last —
  neither moves nor is passed by one that does.
- **A tab is keyed** (`TabSpec.key`), so a title changing every prompt neither moves its
  framework state nor its transition.
- **Closing**: the tab's × (the `Tab::close` part, drawn as two lines — the display list has
  no paths and the font may have no ×), shown on the selected tab and the one under the
  pointer, and a middle click. `on_close`; the program decides, and a tab is gone when it
  leaves `tabs`.
- **Width**: each tab as wide as its label, up to a most; past the strip's width they shrink
  together to a least, and the strip then scrolls across (CSS's overflow on it, its bar hidden,
  the wheel and the selected tab scrolled into view).
- **Heard** as a tab list of tabs (AccessKit's roles), the selected one selected; the arrows
  move along the strip when it has the focus. `Ctrl+Tab` and its kin stay the program's
  shortcuts.
- Styled as everything else: `Tab`, `Tab:checked` for the selected, `:hover`, the
  `TabBar` itself.

## 4. The window's controls and its title bar

**Commands and state** first, useful to any program: `MinimizeWindow`, `MaximizeWindow`,
`RestoreWindow` (`Command`s, SDL's three calls) and `Subscription.OnWindowState((WindowState)
-> e)`, `WindowState { maximized, fullscreen, minimized }`, from SDL's window events. A
script steps them (`maximize`, `restore`).

**A title bar the program draws**, where the system's is not wanted: `App.title_bar`
`SystemTitleBar` (the default) or `DrawnTitleBar`.

- **Drawn, the window is borderless** and SDL's hit test decides what a press on it does. The
  program says where: `.window_drag()` on a node makes its area move the window (a bar's empty
  stretch — and its children that take presses keep them, as the hit test asks the laid tree
  for the topmost element, not the rectangle); a borderless window not maximized resizes from
  a margin round its edges and corners. **SDL's hit test is a C callback**, so `ui.sdl3`
  gives the laid areas to a small C shim that answers it from them (as `bindings.wake` is
  C for its thread): Lyra cannot hand C a function.
- **`window_buttons()`**: minimize, maximize or restore (from `OnWindowState`, kept by the
  host) and close, drawn as lines the bar's height, in the platform's order and place —
  right off a Mac. Close raises what the system's close does (`CloseRequested`), so a
  program asking before quitting is asked the same way.
- **On a Mac the title bar stays the system's** for now: macOS's own buttons, its full-screen
  and its Window menu are what a Mac user expects, and the ImGui app draws its own only off
  macOS. A drawn bar there (with the traffic lights where macOS puts them) is a later
  choice, not this note's.
- **The title** the system shows — the Dock, the taskbar, the Window menu — is
  `Platform.set_title` already, through `App.window`.

## 5. A file dialog

`Command.ChooseFile(FileDialog { filters: [](string, string), start: Maybe<string>, save:
bool }, (Maybe<string>) -> e)` — SDL's open or save dialog, asynchronous: its answer, or
`None` when it is cancelled, arrives as the event. **SDL answers from a callback, possibly on
another thread**, so the shim queues the answer and pushes an SDL event, and `ui.sdl3` drains
it as it drains the menu bar's. The ImGui host's C (`show_open_dialog`) is the model; this is
its SDL half alone, needing no ImGui. A script answers a dialog with a step of its own
(`choose:PATH`, `choose:` for a cancel), so a test never opens one.

## 6. Colour

```
pub let swatch<e> = pure (color: Color, open: bool, on_open: e, on_change: (Color) -> e, on_dismiss: e) -> Node<e>
```

- **A swatch** is the colour on a square (a checkerboard under it when translucent — a
  terminal's selection is), opening on a press **a picker** in a §1 popup: a saturation and
  value square (two gradients over the hue: white to it across, transparent to black down,
  both of which the display list draws, the fallback renderer included), a hue strip, an
  alpha strip, and a text field for the colour as CSS writes it. Dragging raises `on_change`
  with each colour, as ImGui's picker does, so the program shows it live and keeps it when it
  is let go.
- **The colour's text is CSS Color 4's** — `#rgb`, `#rrggbb`, `#rrggbbaa`, `rgb(…)`, and
  `oklab(L a b [/ alpha])` and `oklch(L C H [/ alpha])`, the lightness as `N%` or 0–1, the hue
  in degrees (`deg` allowed) — read by one parser in `ui.color` (`parse_color`), which lui's
  style sheets use too (`color: oklch(70% 0.12 250)`). Oklab is converted to sRGB as CSS does
  (Björn Ottosson's matrices, then the sRGB transfer function); a colour outside sRGB's gamut
  is brought in by CSS's gamut mapping, its chroma lowered in OKLCH at the same lightness and
  hue until it is in, rather than clipped channel by channel, which shifts the hue. The field
  shows a colour as it was typed while it is edited, and as `#rrggbb` (`#rrggbbaa` when
  translucent) once it is set; `Color` stays 8-bit sRGB, so a colour read from `oklch(…)` is
  stored as the nearest sRGB one.
- **The field keeps what is typed** (`FieldSpec.draft`): while it is focused it shows the text
  as typed, a colour raised whenever the text reads as one, and the colour's own text once the
  focus leaves — a half-typed `oklch(70%` is not replaced by the colour it is not yet.
- **Open is the program's** (§1), so a program can keep one picker open while it shows the
  colour elsewhere; most will keep "which swatch is open" in their state as one key.
- The picker holds its hue while the colour is grey (saturation or value 0), where the colour
  itself no longer says it — the framework's state, by identity.
- Heard as a button labelled with its colour (AccessKit's colour well is a later want, todo.md);
  its strips are sliders and its field an ordinary one.

## 7. The smaller pieces, and lui

- **A select's separators** (`SelectSpec.separators: []i64`, a rule before each index listed)
  for Termina's saved themes after its own; HTML's `<optgroup>` labels are a later want.
- **A slider's value shown** — Termina's `14 pt`, `1.20` — is text the program puts beside
  it, not a `SliderSpec.format`: a generic struct's lambda or `None` default is not settled
  yet (todo.md), and a row of a slider and its text says the same.
- **A disabled button** is `Node.disabled_if` already. `SeparatorText`, `Form` and `Field`
  are compositions — a rule with a label, a grid of two columns — written in Termina's
  markup, not framework.
- **lui's `ui` target** gains `<MenuBar>`, `<MenuButton>`, `<Menu>`, `<MenuItem>`,
  `<Separator>` (as data — a `<Menu>`'s children are `MenuEntry`s, not nodes), `<TabBar>` with
  `<Tab>`s, `<Swatch>`, `<Popup>`, `.window_drag` as an attribute (`window-drag`), and
  **`Fragment`**: a `<for>` or `<if>` holding several elements (todo.md), which Termina's
  settings use throughout.

## 8. Order

Each step lands with its tests in `cmd/lyrac/ui_test.go` (scripted, at `-O0`) and an example;
Termina moves at the end.

1. **Popups**, with the select rebuilt on them: its list scrolls and flips, as the test of the
   placement. Example: `examples/ui/popups.lyra`. **Built 10/23.**
2. **Menus drawn**: `menu_bar`, `menu_button`, shortcuts from the menus, their text. **Built
   10/23**; their lui elements wait for step 8.
3. **Menus native**: `App.menus` installed through `bindings.menubar` on a Mac, synced,
   reinstalled on a change of shape; `menubar.m` pushing its event. Checked by a test that
   installs and reads back, as Termina's `menutest` did. **Built 10/23.**
4. **The tab bar**: order by `on_move`, keys, closing, shrinking and scrolling. **Built
   10/23**; its lui element waits for step 8.
5. **The window**: the commands, `OnWindowState`, `DrawnTitleBar` with its hit-test shim and
   `window_buttons` (Linux; a Mac keeps the system's). **Built 10/23**; SDL's calling the shim
   for a real borderless window is untested here (the dummy driver refuses a hit test).
6. **`ChooseFile`** and its shim; a script's `choose:`. **Built 10/23**; a real dialog is not
   shown by any test (a dialog needs a user), only the queue it answers through.
7. **Colour**: `swatch` and the picker, `ui.color` (CSS Color 4's text, Oklab, OKLCH, HSV),
   lui's colours through it. **Built 10/23**; its lui element waits for step 8.
8. **lui's elements** for all of it, and `Fragment`. **Built 10/23**: a `<for>` of several is
   an array each pass, flattened (`flatten`, new in the prelude); example
   `examples/ui/notebook_view.lui`.
9. **Termina**: termina-ui gains the tab bar, its menus (native on a Mac, a GNOME header bar
   with ☰ on Linux, a menu bar elsewhere), the Settings tab as `ui`-target markup, and
   Other Font File…; then `Termina.app` and the Linux releases are built from it, and the
   ImGui front end and `uitest` go. **The tab bar, the menus and the header bar built 10/23**
   (`termina/src/face/window.lyra`; the header bar off every Mac, there being no third
   platform), **and the Settings tab** (`termina/src/settings_page.lui`; a family loaded
   while the app runs by `LoadFonts`, the theme list's rules `SelectSpec.separators`); and
   **the move, 10/23**: `Termina.app` and the Linux releases are built from it (FreeType and
   HarfBuzz built static for them, `termina/tools/textlibs.sh`), and the ImGui front end,
   uitest and menutest are gone, uitest's checks moved to vttest and scripted ones.

## 9. Open questions

1. **Should a drawn menu bar's open path be the program's, as a popup's is?** Proposed: the
   framework's — nothing a program does depends on which menu is open, and making it the
   program's puts an event and a field in every program with menus.
2. **Tabs past the strip's width: shrink, scroll, or a list of the rest?** **Answered 10/23:
   shrink to a least, then a "more tabs" menu** — as many tabs as fit, the selected among
   them, and an arrow whose menu lists the rest (`more_tabs`). Scrolling hid the tabs past
   the edge with nothing to say they were there.
3. **A drawn title bar's double click.** It maximizes on every desktop, but SDL's hit test
   starts a move at the press, so the second click never reaches the program — the ImGui
   app's open item. Either the shim answers the press `SDL_HITTEST_NORMAL` when it is a
   second one in time, or the program starts the move itself; worth trying on GNOME and KDE
   before choosing.
4. **Is Termina's Window menu drawn-only for good?** It lists the tabs off a Mac, which on a
   Mac would make the installed tree change shape at every tab opened. Proposed: as today,
   the Mac's Window menu is macOS's and lists the window, not its tabs.
5. **Colour as `ui.draw.Color` or a packed integer?** Proposed: `Color` (what the framework
   draws with); Termina converts its ABGR at the edge, as it already does to paint.
6. **An OKLCH mode for the picker?** Built and taken out (10/23): lightness, chroma and hue
   strips a toggle from HSV's square did not work well as an interface — OKLCH's chroma has no
   fixed top, so its strip's end is arbitrary, and the strips' faded out-of-gamut parts left
   little to pick from. OKLCH is the field's: `oklch(…)` typed is taken, whichever square is
   shown.
