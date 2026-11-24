package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// rasterGame is a game on a 320×224 screen, its art grown `scale` times: plane B all green
// tiles, two 16×16 sprites standing across line 100 — the first drawn from the sheet's area in
// palette 0, the second from one in palette 1, both green — and two raster changes of palette
// 0's green to blue: a screen's at line 100 and, kept to the world, one at line 150 with the
// camera 40 down, so at screen line 110 and to red.
func rasterGame(scale int) string {
	return fmt.Sprintf(`import std.game.screen
import std.game.planes
import std.game.sprites
import std.game.sprites.{ Sprite }
import std.game.raster
import std.game.raster.{ Raster, SheetArea }

const RUN: [2]Raster = #[
  Raster { line: 100, palette: 0, from: 0x00FF00, to: 0x0000FF },
  Raster { line: 150, palette: 0, from: 0x00FF00, to: 0xFF0000, world: true },
]
const AREAS: [2]SheetArea = #[
  SheetArea { x: 0, y: 0, width: 16, height: 16, palette: 0 },
  SheetArea { x: 16, y: 0, width: 16, height: 16, palette: 1 },
]

let main = () -> u8 => {
  if !screen.open("raster", 320, 224) { return 2 }
  screen.set_art_scale(%d)
  var tiles: [128]u32 = #[0xFF00FF00; 128]
  for x in 0..<64 { tiles[x] = 0 }
  screen.load_tiles(2, tiles)
  var sheet: [512]u32 = #[0xFF00FF00; 512]
  screen.load_sprites(32, sheet)
  raster.use_sprite_areas(AREAS)
  var back: [2048]u32 = #[1; 2048]
  planes.write_plane(planes.PLANE_B, back, 64)
  planes.set_scroll(planes.PLANE_B, 0.0, 0.0)
  raster.start(RUN, 0, 2)
  while screen.running() {
    raster.scroll_world(40.0)
    sprites.clear()
    sprites.add(Sprite { x: 40.0, y: 92.0, width: 16.0, height: 16.0, sheet_x: 0.0, sheet_y: 0.0 })
    sprites.add(Sprite { x: 100.0, y: 92.0, width: 16.0, height: 16.0, sheet_x: 16.0, sheet_y: 0.0 })
    screen.show()
  }
  screen.close()
  0
}
`, scale)
}

// TestRun_GameRasterChangesAColourBelowALine plays rasterGame: the plane green above line
// 100, blue from it, red from the world's line 150 (screen line 110) — the later change of a
// colour the one seen — at both edges and the middle; the palette 0 sprite changed with it
// from line 100, the palette 1 one green throughout. And the same with the art twice its
// size, the lines twice as far down the window's pixels.
func TestRun_GameRasterChangesAColourBelowALine(t *testing.T) {
	t.Parallel()
	green, blue, red := [3]byte{0, 255, 0}, [3]byte{0, 0, 255}, [3]byte{255, 0, 0}
	for _, scale := range []int{1, 2} {
		src := filepath.Join(t.TempDir(), "main.lyra")
		if err := os.WriteFile(src, []byte(rasterGame(scale)), 0o644); err != nil {
			t.Fatal(err)
		}
		frame := playGame(t, src, "--frames", "2")
		want := func(y int) [3]byte {
			switch {
			case y < 100:
				return green
			case y < 110:
				return blue
			default:
				return red
			}
		}
		// The tile's top half is clear (the backdrop) and its foot green: look at the foot.
		for y := 0; y < 224/scale; y++ {
			if y%8 < 4 {
				continue
			}
			for _, x := range []int{0, 160 / scale, 319 / scale} {
				if got := frame.at(x*scale, y*scale); got != want(y) {
					t.Errorf("scale %d: the plane at %d, %d is %v, want %v", scale, x, y, got, want(y))
				}
			}
		}
		for y := 92; y < 108; y++ {
			if got := frame.at(44*scale, y*scale); got != want(y) {
				t.Errorf("scale %d: the palette 0 sprite at line %d is %v, want %v", scale, y, got, want(y))
			}
			if got := frame.at(104*scale, y*scale); got != green {
				t.Errorf("scale %d: the palette 1 sprite at line %d is %v, want green", scale, y, got)
			}
		}
	}
}

// cycleGame stripes plane B and a sprite in palette 0's red, green and blue, a pixel each, and
// turns them as a colour cycle a step every 2 updates (`palette.step_cycles`).
const cycleGame = `import std.game.screen
import std.game.planes
import std.game.sprites
import std.game.sprites.{ Sprite }
import std.game.raster
import std.game.raster.{ SheetArea }
import std.game.palette
import std.game.palette.{ Cycle }

const CYCLES: [1]Cycle = #[Cycle { palette: 0, first: 0, count: 3, frames: 2 }]
const CYCLE_COLORS: [3]u32 = #[0xFF0000, 0x00FF00, 0x0000FF]
const AREAS: [1]SheetArea = #[SheetArea { x: 0, y: 0, width: 16, height: 16, palette: 0 }]

let main = () -> u8 => {
  if !screen.open("cycle", 320, 224) { return 2 }
  var tiles: [128]u32 = #[0; 128]
  for y in 0..<8 {
    for x in 0..<8 { tiles[y * 16 + 8 + x] = [0xFFFF0000, 0xFF00FF00, 0xFF0000FF][x % 3] }
  }
  screen.load_tiles(2, tiles)
  var sheet: [256]u32 = #[0; 256]
  for i in 0..<256 { sheet[i] = [0xFFFF0000, 0xFF00FF00, 0xFF0000FF][(i % 16) % 3] }
  screen.load_sprites(16, sheet)
  raster.use_sprite_areas(AREAS)
  var back: [2048]u32 = #[1; 2048]
  planes.write_plane(planes.PLANE_B, back, 64)
  while screen.running() {
    for _ in 0..<screen.updates_due() { palette.step_cycles(CYCLES, CYCLE_COLORS) }
    sprites.clear()
    sprites.add(Sprite { x: 160.0, y: 100.0, width: 16.0, height: 16.0, sheet_x: 0.0, sheet_y: 0.0 })
    screen.show()
  }
  screen.close()
  0
}
`

// TestRun_GameColourCyclesTurnTheArt plays cycleGame on frames 2, 4 and 6: the plane's stripes
// and the sprite's each one of the three turns of red, green and blue a cycle stepping on
// makes — a place showing the colour before it — and the three frames three different ones.
func TestRun_GameColourCyclesTurnTheArt(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(cycleGame), 0o644); err != nil {
		t.Fatal(err)
	}
	run := [][3]byte{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}}
	seen := map[int]bool{}
	for _, frames := range []string{"2", "4", "6"} {
		frame := playGame(t, src, "--frames", frames)
		turn := -1
		for s := 0; s < 3; s++ {
			plane, sprite := true, true
			for k := 0; k < 3; k++ {
				// Turned on by s, place k shows the colour s before it.
				want := run[(k-s+3)%3]
				if frame.at(k, 0) != want {
					plane = false
				}
				if frame.at(160+k, 100) != want {
					sprite = false
				}
			}
			if plane && sprite {
				turn = s
			}
		}
		if turn < 0 {
			t.Fatalf("frame %s: the plane %v %v %v and the sprite %v %v %v are not one turn of red, green and blue", frames,
				frame.at(0, 0), frame.at(1, 0), frame.at(2, 0), frame.at(160, 100), frame.at(161, 100), frame.at(162, 100))
		}
		seen[turn] = true
	}
	if len(seen) != 3 {
		t.Errorf("frames 2, 4 and 6 showed turns %v, want three different", seen)
	}
}
