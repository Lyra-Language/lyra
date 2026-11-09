package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
