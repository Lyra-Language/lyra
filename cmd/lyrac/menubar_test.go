package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bindings.menubar's submenus, read back from the bar AppKit was given: a submenu's
// items indented under its title, the items after `end_submenu` back in the menu it was
// begun in, and an item in it greyed through its tag (Vega's File > Open Recent, 10/02).
// macOS only — elsewhere the binding is a stub — and skipped without the archive, SDL3,
// or a window server to make an application object.
func TestRun_MenubarSubmenusAreInstalled(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("the native menu bar is macOS's")
	}
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "build", "lib", "liblyra-menubar.a")); err != nil {
		t.Skip("no build/lib/liblyra-menubar.a — run ./build.sh")
	}
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", root)
	// Built in-process, lyrac is not the install's, so it does not search build/lib itself.
	setenv(t, "LIBRARY_PATH", filepath.Join(root, "build", "lib"))
	dir := t.TempDir()
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import bindings.sdl3.{ init, quit, INIT_VIDEO }
import bindings.menubar

let main = () -> u8 => {
  if !init(INIT_VIDEO) { return 2 }
  menubar.menu("File")
  menubar.item("Open...", "o", menubar.COMMAND, 0)
  menubar.submenu("Open Recent")
  menubar.item("hero.vega", "", 0, 1)
  menubar.separator()
  menubar.item("Clear Menu", "", 0, 2)
  menubar.end_submenu()
  menubar.item("Save", "s", menubar.COMMAND, 3)
  if !menubar.install("Test") { return 2 }
  menubar.set_enabled(2, false)
  print(menubar.describe())
  quit()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "menus")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	out, err := command(t, bin).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skip("no window server: SDL or the menu bar could not start")
	} else if err != nil {
		t.Fatalf("running: %v", err)
	}
	want := "File\n  Open...\tCmd+O\n  Open Recent >\n    hero.vega\n    ---\n    Clear Menu (off)\n  Save\tCmd+S\n"
	if !strings.HasSuffix(string(out), want) {
		t.Errorf("the bar is:\n%s\nwant it to end:\n%s", out, want)
	}
}

// ui.sdl3's install_menus, App.menus' half on a Mac (ui/WINDOW.md step 3), read back from the
// bar AppKit was given: the first menu as the application menu, its title the app's name; the
// shortcuts as key equivalents — Shift a modifier on a letter, folded into a bracket's character
// (⌘⇧[ is ⌘{), the arrows and F1 AppKit's function-key characters; a submenu; a greyed and a checked item; macOS's Window menu after the last. A
// change of greying keeps the shape and is only synced; a renamed item is a new shape, installed
// again. And a choice wakes SDL's event wait: the binding pushes an event, or a ui program
// sleeping there would not hear a click until its next input.
func TestRun_UiMenusInstalledInTheMacsBar(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("the native menu bar is macOS's")
	}
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "build", "lib", "liblyra-menubar.a")); err != nil {
		t.Skip("no build/lib/liblyra-menubar.a — run ./build.sh")
	}
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", root)
	setenv(t, "LIBRARY_PATH", filepath.Join(root, "build", "lib"))
	dir := t.TempDir()
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import bindings.sdl3.{ init, quit, INIT_VIDEO, wait_event, poll_event }
import bindings.menubar.{ describe, choose, poll_menu }
import ui.sdl3.{ install_menus }
import ui.backend.{ NativeMenu, NativeItem, NativeItemSpec, NativeSubmenu, NativeSeparator, Chord, Character, Modifiers, ArrowUp, FunctionKey }

let main = () -> u8 => {
  if !init(INIT_VIDEO) { return 2 }
  let shape = install_menus(menus("Count", true), "")
  if shape == "" { return 2 }
  print(describe())
  println("=== greyed")
  let same = install_menus(menus("Count", false), shape)
  print(describe())
  println("same shape: ${same == shape}")
  println("=== renamed")
  let renamed = install_menus(menus("Tally", false), same)
  print(describe())
  println("new shape: ${renamed != same}")
  while poll_event() != None {}
  choose(3)
  let woke = wait_event(2000) != None
  println("woken: ${woke}, chosen: ${poll_menu() ?? -1}")
  quit()
  0
}

let menus = pure (name: string, enabled: bool) -> []NativeMenu => [
  NativeMenu {
    label: "Counter",
    entries: [
      NativeItem(NativeItemSpec { label: "Settings…", tag: 0, shortcut: Some(Chord { key: Character(','), mods: Modifiers { primary: true } }) }),
      NativeItem(NativeItemSpec { label: "Previous Tab", tag: 4, shortcut: Some(Chord { key: Character('['), mods: Modifiers { primary: true, shift: true } }) }),
    ],
  },
  NativeMenu {
    label: "Edit",
    entries: [
      NativeItem(NativeItemSpec { label: name, tag: 1, shortcut: Some(Chord { key: Character('k'), mods: Modifiers { primary: true, shift: true } }), enabled: enabled }),
      NativeSeparator,
      NativeSubmenu("Move", [
        NativeItem(NativeItemSpec { label: "Up", tag: 2, shortcut: Some(Chord { key: ArrowUp, mods: Modifiers { alt: true } }) }),
        NativeItem(NativeItemSpec { label: "Help", tag: 3, checked: true, shortcut: Some(Chord { key: FunctionKey(1) }) }),
      ]),
    ],
  },
]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "menus")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	out, err := command(t, bin).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skip("no window server: SDL or the menu bar could not start")
	} else if err != nil {
		t.Fatalf("running: %v", err)
	}
	parts := strings.Split(string(out), "=== ")
	if len(parts) != 3 {
		t.Fatalf("output:\n%s", out)
	}
	first, greyed, renamed := parts[0], parts[1], parts[2]
	for _, want := range []string{
		"Counter\n  Settings…\tCmd+,\n  Previous Tab\tCmd+{\n",
		"Quit Counter\tCmd+Q",
		// Edit's title carries a word joiner (the binding's, keeping AppKit's own Edit items out).
		"\n  Count\tShift+Cmd+K\n  ---\n  Move >\n    Up\tOption+\uf700\n    Help\t\uf704 [x]\n",
		"Minimize\tCmd+M",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("the bar lacks %q:\n%s", want, first)
		}
	}
	if !strings.Contains(greyed, "  Count\tShift+Cmd+K (off)\n") || !strings.Contains(greyed, "same shape: true") {
		t.Errorf("greying Count:\n%s", greyed)
	}
	if !strings.Contains(renamed, "  Tally\tShift+Cmd+K (off)\n") || strings.Contains(renamed, "Count\t") || !strings.Contains(renamed, "new shape: true") {
		t.Errorf("renaming Count:\n%s", renamed)
	}
	if !strings.Contains(renamed, "woken: true, chosen: 3") {
		t.Errorf("a choice did not wake SDL's wait:\n%s", renamed)
	}
}
