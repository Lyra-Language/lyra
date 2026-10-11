package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bindings.sdl3's create_texture and read_pixels, round trip: a texture of a red texel and
// a transparent one, drawn over blue and read back. The transparent texel must show the
// blue — create_texture's blending — and the colours come back as the 0xAARRGGBB they went
// in as. Headless: SDL's dummy video driver with the software renderer, which is what
// std.game's checks run under too (offscreen still wants OpenGL). Skipped without SDL3.
func TestRun_SDL3TextureRoundTrip(t *testing.T) {
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import bindings.sdl3.{ init, quit, INIT_VIDEO, create_window, destroy_window }
import bindings.sdl3.{ destroy_renderer, create_texture, destroy_texture, draw_texture }
import bindings.sdl3.{ Rect, rgb }

let main = () -> u8 => {
  if !init(INIT_VIDEO) { return 2 }
  let Some(window) = create_window("texture", 4, 2, 0) else { return 2 }
  let Some(renderer) = window.create_renderer() else { return 2 }
  let Some(texture) = create_texture(renderer, 2, 1, [0xFFFF0000, 0x00000000]) else {
    return 3
  }
  if create_texture(renderer, 2, 2, [0xFFFFFFFF]).is_some() { return 4 }
  renderer.set_draw_color(rgb(0, 0, 255))
  renderer.clear()
  let place = Rect { x: 0.0, y: 0.0, w: 2.0, h: 1.0 }
  draw_texture(renderer, texture, place, place)
  let Some(frame) = renderer.read_pixels() else { return 5 }
  println("${frame.width}x${frame.height} ${frame.argb[0].to_hex()} ${frame.argb[1].to_hex()}")
  destroy_texture(texture)
  destroy_renderer(renderer)
  destroy_window(window)
  quit()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "texture")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skip("SDL could not open a dummy window and software renderer here")
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "4x2 ffff0000 ff0000ff"; got != want {
		t.Errorf("read back %q, want %q", got, want)
	}
}
