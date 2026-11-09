package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const resourcesProgram = `import std.game.screen

let main = () -> u8 => {
  if !screen.open("art", 16, 16) { return 9 }
  let sprites = screen.load_sprites_file("art/sprite.png")
  let tiles = screen.load_tiles_file("art/sprite.png", 1, 8)
  let missing = screen.load_sprites_file("art/none.png")
  screen.close()
  println("sprites ${sprites}, tiles ${tiles}, missing ${missing}")
  0
}
`

// A game's art folder reaches the program under `lyrac run` and beside a build elsewhere, and
// std.game loads a PNG from it, saying when one is not there.
func TestResources_CarriedAndLoaded(t *testing.T) {
	t.Parallel()
	setenv(t, "LYRA_STD", repoRoot(t))
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	setenv(t, "SDL_RENDER_DRIVER", "software")
	dir := t.TempDir()
	art := filepath.Join(dir, "art")
	if err := os.Mkdir(art, 0o755); err != nil {
		t.Fatal(err)
	}
	picture := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	picture.Set(1, 1, color.NRGBA{10, 20, 30, 255})
	var file bytes.Buffer
	if err := png.Encode(&file, picture); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"art/sprite.png": file.Bytes(),
		"lyra.toml":      []byte("resources = \"art\"\n"),
		"game.lyra":      []byte(resourcesProgram),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(dir, "game.lyra")
	stdout, stderr, code := captureRunUnoptimized(t, "run", source)
	if code != 0 {
		t.Skipf("no SDL3 here, or no window: %d %s", code, stderr)
	}
	if !strings.Contains(stdout, "sprites true, tiles true, missing false") || !strings.Contains(stdout, "cannot read") {
		t.Errorf("run said %q", stdout)
	}
	out := filepath.Join(t.TempDir(), "game")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", out, source); code != 0 {
		t.Fatalf("build: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "art", "sprite.png")); err != nil {
		t.Errorf("the art is not beside the build: %v", err)
	}
}

// A standalone package holds the program's libraries and art, runs from there, and names no
// folder of the machine that built it.
func TestStandalone_PackagesAGame(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("checked on the Mac; Linux's folder is tried by hand")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	setenv(t, "SDL_RENDER_DRIVER", "software")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "art"), 0o755); err != nil {
		t.Fatal(err)
	}
	picture := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	var file bytes.Buffer
	if err := png.Encode(&file, picture); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"art/sprite.png": file.Bytes(),
		"lyra.toml":      []byte("resources = \"art\"\n"),
		"game.lyra":      []byte(resourcesProgram),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "Game")
	if _, stderr, code := captureRunUnoptimized(t, "build", "--standalone", "-o", out, filepath.Join(dir, "game.lyra")); code != 0 {
		t.Skipf("no SDL3 here: %s", stderr)
	}
	program := filepath.Join(out+".app", "Contents", "MacOS", "Game")
	loads, err := command(t, "otool", "-L", program).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loads), "@executable_path/../Frameworks/") || strings.Contains(string(loads), "/opt/") {
		t.Errorf("the program loads:\n%s", loads)
	}
	if out, err := command(t, "codesign", "-v", filepath.Dir(filepath.Dir(filepath.Dir(program)))).CombinedOutput(); err != nil {
		t.Errorf("the signature: %s", out)
	}
	got, err := command(t, program).CombinedOutput()
	if err != nil || !strings.Contains(string(got), "sprites true, tiles true, missing false") {
		t.Errorf("it ran: %v\n%s", err, got)
	}
}

// Linux's package is a folder: the program, `lib/` (empty for a program of libc alone) and its
// resources, and the program runs from it.
func TestStandalone_LinuxFolder(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Linux's folder")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	for name, data := range map[string]string{
		"lyra.toml": "resources = \"art\"\n",
		"art/x.txt": "x",
		"game.lyra": "let main = () -> u8 => {\n  println(\"from the package\")\n  0\n}\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "game")
	if _, stderr, code := captureRunUnoptimized(t, "build", "--standalone", "-o", out, filepath.Join(dir, "game.lyra")); code != 0 {
		t.Fatalf("build: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "art", "x.txt")); err != nil {
		t.Error(err)
	}
	got, err := command(t, filepath.Join(out, "game")).CombinedOutput()
	if err != nil || !strings.Contains(string(got), "from the package") {
		t.Errorf("it ran: %v\n%s", err, got)
	}
	// A second export replaces the first; a folder it did not make is left alone.
	if _, stderr, code := captureRunUnoptimized(t, "build", "--standalone", "-o", out, filepath.Join(dir, "game.lyra")); code != 0 {
		t.Errorf("again: %s", stderr)
	}
	if _, stderr, code := captureRunUnoptimized(t, "build", "--standalone", "-o", dir, filepath.Join(dir, "game.lyra")); code == 0 || !strings.Contains(stderr, "not a game") {
		t.Errorf("over a project: %d %s", code, stderr)
	}
}
