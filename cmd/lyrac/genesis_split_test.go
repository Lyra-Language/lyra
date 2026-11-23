package main

import (
	"fmt"
	"slices"
	"testing"
)

// splitProgram draws a green line across plane B at every eighth map line, and splits the
// plane at screen line 112 with std.genesis.raster: above it the plane scrolled to `top`, below
// it to `top + 1` — so the lines above are at multiples of 8 and those below one line sooner.
// With `moving`, `top` steps on each frame in the game's loop, while the picture is drawn, as
// a game's camera does. `bands` turns the line scroll on (plane A's lines its own), whose
// vertical blank sends the scroll another way.
func splitProgram(moving, bands bool) string {
	step := ""
	if moving {
		step = "vdp.wait_vblank()\n    top = (top + 1) & 7"
	}
	return fmt.Sprintf(`import std.genesis.vdp
import std.genesis.sprites
import std.genesis.raster
import std.genesis.raster.{ Raster }

@interrupt(vblank)
let each_frame = () -> void => sprites.show()

const LINE: [8]u32 = #[0x11111111, 0, 0, 0, 0, 0, 0, 0]
const SPLIT: [1]Raster = #[Raster { line: 112, color: raster.SPLIT_B, word: 0 }]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, LINE)
  for y: u16 in 0..<32 {
    for x: u16 in 0..<64 { vdp.write_cell(vdp.PLANE_B, x, y, 1) }
  }
  vdp.use_bands(%v, false)
  raster.start(SPLIT, 0, 1, 111)
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  var top: i16 = 0
  loop {
    %s
    raster.scroll_split(vdp.PLANE_B, top + 1)
    vdp.set_scroll(vdp.PLANE_B, 0, top)
  }
}
`, bands, step)
}

// TestGenesis_SplitScrollsAPlaneApartBelowALine checks a still split's every green line, at
// both ends of the screen: from line 112 exactly — a split a line early would show line 111
// green, one a line late line 112.
func TestGenesis_SplitScrollsAPlaneApartBelowALine(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	var want []int
	for y := 0; y < 112; y += 8 {
		want = append(want, y)
	}
	for y := 119; y < 224; y += 8 {
		want = append(want, y)
	}
	for _, bands := range []bool{false, true} {
		out := genesisProgram(t, fmt.Sprintf("split_%v", bands), splitProgram(false, bands))
		for _, frames := range []string{"30", "31"} {
			s := runROM(t, out, frames, "")
			for _, x := range []int{0, 160, s.width - 1} {
				if got := greenRows(s, x); !slices.Equal(got, want) {
					t.Errorf("bands %v, frame %s, x %d: lines at %v, want %v", bands, frames, x, got, want)
				}
			}
		}
	}
}

// TestGenesis_SplitMovesWithTheTopOnTheSameFrame moves the plane a line each frame, split and
// all, from the game's loop while the picture is drawn: on every frame the part below must be
// one line on from the part above — set mid-frame and written at once, the split moved a
// frame before the top, and the two came apart by a line each time; and a vertical blank
// coming between the loop's two calls sent the top's new scroll with the split's old one.
func TestGenesis_SplitMovesWithTheTopOnTheSameFrame(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	for _, bands := range []bool{false, true} {
		out := genesisProgram(t, fmt.Sprintf("split_moving_%v", bands), splitProgram(true, bands))
		seen := map[int]bool{}
		for _, frames := range []string{"40", "41", "42", "43", "57", "58"} {
			s := runROM(t, out, frames, "")
			got := greenRows(s, 160)
			if len(got) == 0 {
				t.Fatalf("bands %v, frame %s: no green line", bands, frames)
			}
			phase := got[0] % 8
			seen[phase] = true
			var want []int
			for y := phase; y < 112; y += 8 {
				want = append(want, y)
			}
			for y := 112 + (phase+7-112%8)%8; y < 224; y += 8 {
				want = append(want, y)
			}
			if !slices.Equal(got, want) {
				t.Errorf("bands %v, frame %s: lines at %v, want %v (the part below one line on)", bands, frames, got, want)
			}
		}
		if len(seen) < 3 {
			t.Errorf("bands %v: the plane did not move (phases %v)", bands, seen)
		}
	}
}
