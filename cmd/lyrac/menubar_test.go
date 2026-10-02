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
	if runtime.GOOS != "darwin" {
		t.Skip("the native menu bar is macOS's")
	}
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "build", "lib", "liblyra-menubar.a")); err != nil {
		t.Skip("no build/lib/liblyra-menubar.a — run ./build.sh")
	}
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", root)
	// Built in-process, lyrac is not the install's, so it does not search build/lib itself.
	t.Setenv("LIBRARY_PATH", filepath.Join(root, "build", "lib"))
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
	out, err := exec.Command(bin).Output()
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
