package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRun_GameBandsScrollAPlanesLinesApart is TestGenesis_BandsScrollAPlanesLinesApart on
// std.game: a 16-cell map repeating across plane B, a green stripe every 128 pixels, its
// lines scrolled as bands with the camera at x 40 — fixed, twice the camera, a ramp from
// fixed to the camera's speed, a drift of ten pixels an update to the right (one counted),
// a ripple a quarter turn on (16 pixels) and lines in no band at the plane's own scroll. The
// runtime works in floats, so the ramp's last line is the camera's 40 exactly and the
// ripple its whole 16, where the Genesis's 256ths round them down.
func TestRun_GameBandsScrollAPlanesLinesApart(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.screen
import std.game.planes
import std.game.planes.{ Band }
import std.game.sprites

const BANDS: [6]Band = #[
  Band { top: 0.0, height: 32.0, speed: 0.0, ramp_to: 0.0 },
  Band { top: 32.0, height: 32.0, speed: 2.0, ramp_to: 2.0 },
  Band { top: 64.0, height: 64.0, speed: 0.0, ramp_to: 1.0 },
  Band { top: 128.0, height: 32.0, drift: 10.0 },
  Band { top: 160.0, height: 32.0, wave: 16.0, wave_lines: 0.0, wave_updates: 4.0 },
  Band { top: 600.0, height: 8.0 },
]

let main = () -> u8 => {
  if !screen.open("bands", 320, 224) { return 2 }
  var tiles: [128]u32 = #[0; 128]
  for row in 0..<8 { tiles[row * 16 + 8] = 0xFF00FF00 }
  screen.load_tiles(2, tiles)
  var map: [448]u32 = #[0; 448]
  for row in 0..<28 { map[row * 16] = 1 }
  planes.write_plane(planes.PLANE_B, map, 16)
  planes.set_repeats(planes.PLANE_B, true)
  planes.set_scroll(planes.PLANE_B, 40.0, 0.0)
  planes.scroll_bands(planes.PLANE_B, 40.0, BANDS, 1)
  while screen.running() {
    sprites.clear()
    screen.show()
  }
  screen.close()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := playGame(t, src, "--frames", "1")
	for _, c := range []struct {
		line int
		want []int
		what string
	}{
		{10, []int{0, 128, 256}, "fixed to the screen"},
		{40, []int{48, 176, 304}, "at twice the camera (x 80)"},
		{64, []int{0, 128, 256}, "the ramp's first line, fixed"},
		{127, []int{88, 216}, "the ramp's last line, with the camera (x 40)"},
		{140, []int{98, 226}, "drifted ten pixels right, showing the map from x 30"},
		{170, []int{72, 200}, "rippled 16 on, showing the map from x 56"},
		{200, []int{88, 216}, "in no band, with the camera (x 40)"},
	} {
		var got []int
		for x := 0; x < frame.w; x++ {
			if frame.at(x, c.line) == [3]byte{0, 255, 0} {
				got = append(got, x)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("line %d (%s): stripes at %v, want %v", c.line, c.what, got, c.want)
		}
	}
}

// TestRun_GameCountsUpdatesNoSpriteListStarted scrolls a plane two pixels an update in a loop
// that draws no sprites — no `sprites.clear`, which counts an update — and asks the plane,
// after five, where it was the update before: 8, with it at 10. Uncounted, every update
// looked like the first's, the plane remembered where it was then (2), and a real display
// drew it sliding from there — the modern hero's overlook shook more the further it panned
// (11/13). `screen.show` counts a frame's updates no clear counted.
func TestRun_GameCountsUpdatesNoSpriteListStarted(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.screen
import std.game.planes

let main = () -> u8 => {
  if !screen.open("count", 64, 64) { return 2 }
  var x: f32 = 0.0
  while screen.running() {
    for _ in 0..<screen.updates_due() {
      x += 2.0
      planes.set_scroll(planes.PLANE_B, x, 0.0)
    }
    screen.show()
  }
  let p = planes.plane(planes.PLANE_B)
  println("was ${p.was_x} now ${p.x}")
  screen.close()
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "game")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	cmd := command(t, bin, "--frames", "5")
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if err != nil {
		t.Fatalf("playing: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "was 8.0 now 10.0") && !strings.Contains(string(out), "was 8 now 10") {
		t.Errorf("the plane remembers where it was the update before as: %s, want was 8 now 10", strings.TrimSpace(string(out)))
	}
}

// TestRun_GameColumnsRipplePlanesUpAndDown is TestGenesis_ColumnsRipplePlanesUpAndDown on
// std.game: a green line across plane B at map line 96, its 16-pixel columns rippled a
// quarter turn a column, 8 pixels at most — moved 0, 8, 0 and -8, the line at 96, 88, 96
// and 104 — and plane A's line at 40 in every column, its columns its own scroll's. The
// columns are cut inside a tile too: the screen scrolled 4 pixels across, column 1 (16–31)
// shows half of two tiles each.
func TestRun_GameColumnsRipplePlanesUpAndDown(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.screen
import std.game.planes
import std.game.camera
import std.game.planes.{ ColumnWave }
import std.game.sprites

let main = () -> u8 => {
  if !screen.open("columns", 320, 224) { return 2 }
  var tiles: [128]u32 = #[0; 128]
  for x in 8..<16 { tiles[x] = 0xFF00FF00 }
  screen.load_tiles(2, tiles)
  var back: [1792]u32 = #[0; 1792]
  var front: [1792]u32 = #[0; 1792]
  for x in 0..<64 {
    back[12 * 64 + x] = 1
    front[5 * 64 + x] = 1
  }
  planes.write_plane(planes.PLANE_B, back, 64)
  planes.write_plane(planes.PLANE_A, front, 64)
  planes.set_scroll(planes.PLANE_A, 4.0, 0.0)
  planes.set_scroll(planes.PLANE_B, 4.0, 0.0)
  camera.scroll_columns(planes.PLANE_B, ColumnWave { wave: 8.0, wave_length: 64.0 }, 0)
  while screen.running() {
    sprites.clear()
    screen.show()
  }
  screen.close()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := playGame(t, src, "--frames", "1")
	for _, c := range []struct {
		x    int
		want []int
		what string
	}{
		{8, []int{40, 96}, "column 0, not moved"},
		{17, []int{40, 88}, "column 1, moved 8 down: the map up"},
		{30, []int{40, 88}, "column 1's last pixel, the next tile's"},
		{40, []int{40, 96}, "column 2, not moved"},
		{56, []int{40, 104}, "column 3, moved 8 up"},
		{72, []int{40, 96}, "column 4, a turn on"},
	} {
		var got []int
		for y := 0; y < frame.h; y++ {
			if frame.at(c.x, y) == [3]byte{0, 255, 0} {
				got = append(got, y)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("x %d (%s): lines at %v, want %v", c.x, c.what, got, c.want)
		}
	}
}

// TestRun_GameSplitScrollsAPlaneApartBelowALine draws a green line across plane B at every
// eighth map line, the plane scrolled 0 and split at screen line 112 to 1 (std.game.planes'
// set_split, the Genesis's raster split): the lines above are at multiples of 8, those below
// one line sooner.
func TestRun_GameSplitScrollsAPlaneApartBelowALine(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.game.screen
import std.game.planes
import std.game.sprites

let main = () -> u8 => {
  if !screen.open("split", 320, 224) { return 2 }
  var tiles: [128]u32 = #[0; 128]
  for x in 8..<16 { tiles[x] = 0xFF00FF00 }
  screen.load_tiles(2, tiles)
  var back: [2048]u32 = #[0; 2048]
  for y in 0..<32 {
    for x in 0..<64 { back[y * 64 + x] = 1 }
  }
  planes.write_plane(planes.PLANE_B, back, 64)
  planes.set_scroll(planes.PLANE_B, 0.0, 0.0)
  planes.set_split(planes.PLANE_B, 112, 1.0)
  while screen.running() {
    sprites.clear()
    screen.show()
  }
  screen.close()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	frame := playGame(t, src, "--frames", "1")
	var want []int
	for y := 0; y < 112; y += 8 {
		want = append(want, y)
	}
	for y := 119; y < 224; y += 8 {
		want = append(want, y)
	}
	for _, x := range []int{0, 160, 319} {
		var got []int
		for y := 0; y < frame.h; y++ {
			if frame.at(x, y) == [3]byte{0, 255, 0} {
				got = append(got, y)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("x %d: lines at %v, want %v", x, got, want)
		}
	}
}
