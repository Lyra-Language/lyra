package main

import (
	"fmt"
	"slices"
	"testing"
)

// TestGenesis_BandsScrollAPlanesLinesApart repeats a 16-cell map across plane B — a green
// stripe down its first pixel column, so one every 128 pixels — and scrolls its lines as
// bands with std.genesis.camera's scroll_bands, the camera at x 40: lines 0–31 fixed to
// the screen, 32–63 at twice the camera, 64–127 ramping from fixed to the camera's speed,
// 128–159 drifting ten pixels a frame to the right (one frame: ticks 1), 160–191 rippling
// a quarter turn on (16 pixels at most: 15), and 192 on in no band, at the plane's own speed (the
// camera's). Each line's stripes must be where its scroll puts them — the VDP's line scroll
// on, the table written a line a word.
func TestGenesis_BandsScrollAPlanesLinesApart(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "bands", `import std.genesis.vdp
import std.genesis.camera.{ Camera, Band }

const STRIPE: [8]u32 = #[
  0x10000000, 0x10000000, 0x10000000, 0x10000000,
  0x10000000, 0x10000000, 0x10000000, 0x10000000,
]

const BANDS: [6]Band = #[
  Band { top: 0, height: 32, speed: 0, ramp_to: 0 },
  Band { top: 32, height: 32, speed: 512, ramp_to: 512 },
  Band { top: 64, height: 64, speed: 0, ramp_to: 256 },
  Band { top: 128, height: 32, drift: 2560 },
  Band { top: 160, height: 32, wave: 16, wave_speed: 16384 },
  Band { top: 600, height: 8 },
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, STRIPE)
  var map: [448]u16 = #[0; 448]
  for row: i64 in 0..<28 { map[row * 16] = 1 }
  let view = Camera { x: 40, world_width: 1024, world_height: 224 }
  view.show_repeating(vdp.PLANE_B, map, 16)
  vdp.use_bands(false, true)
  view.scroll_bands(vdp.PLANE_B, 0, 256, BANDS, 1)
  vdp.display_on()
  loop {}
}
`)
	screen := runROM(t, out, "30", "")
	for _, c := range []struct {
		line int
		want []int
		what string
	}{
		{10, []int{0, 128, 256}, "fixed to the screen"},
		{40, []int{48, 176, 304}, "at twice the camera (x 80)"},
		{64, []int{0, 128, 256}, "the ramp's first line, fixed"},
		{127, []int{89, 217}, "the ramp's last line, x 39 (40 × 63/63 in 256ths, rounded down)"},
		{140, []int{98, 226}, "drifted ten pixels right, showing the map from x 30"},
		{170, []int{73, 201}, "rippled 15 on, showing the map from x 55"},
		{200, []int{88, 216}, "in no band, with the camera (x 40)"},
	} {
		if got := greenColumns(screen, c.line); !slices.Equal(got, c.want) {
			t.Errorf("line %d (%s): stripes at %v, want %v", c.line, c.what, got, c.want)
		}
	}
}

// greenColumns is where on line y the screen is pure green.
func greenColumns(s screen, y int) []int {
	var xs []int
	for x := 0; x < s.width; x++ {
		k := (y*s.width + x) * 3
		if fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2]) == "00ff00" {
			xs = append(xs, x)
		}
	}
	return xs
}
