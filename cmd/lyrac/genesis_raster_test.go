package main

import (
	"fmt"
	"testing"
)

// TestGenesis_RasterEffects changes the backdrop at lines 56, 112 and 168 with
// std.genesis.raster, the VDP interrupting every 56 lines, while the game's loop writes a
// plane's cell over and over (each write holding the interrupts) — and checks every row of
// the picture, at both ends of the line, is the colour its band should be: the top blue
// again each frame, then green, red and white, each from its line exactly — on several
// frames, as where the game's loop is when the interrupt comes differs from one to the next.
func TestGenesis_RasterEffects(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "raster", rasterProgram(`sprites.show()`, `vdp.write_cell(vdp.PLANE_B, k % 40, 20, 0)
    k += 1`))
	checkRasterBands(t, out)
}

// TestGenesis_RasterEffectsLongBlank is the same bands under a vertical blank handler that
// runs on well into the picture, past the first band's line, as a game's music does on a
// row: the runtime lets the horizontal interrupt into it. Masked there, the hero's sky lost
// its first steps one frame in seven, the song's speed (11/16).
func TestGenesis_RasterEffectsLongBlank(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "raster-long", rasterProgram(`sprites.show()
  // Past line 56 before it returns: a long vertical blank.
  while vdp.line_now() < 90 || vdp.line_now() > 224 {}`, ``))
	checkRasterBands(t, out)
}

// rasterProgram is the three bands' program: its vertical blank handler's body, and what its
// loop does.
func rasterProgram(blank, loop string) string {
	return `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.raster
import std.genesis.raster.{ Raster }

const BANDS: [3]Raster = #[
  Raster { line: 56, color: 0, word: 0x00E0 },
  Raster { line: 112, color: 0, word: 0x000E },
  Raster { line: 168, color: 0, word: 0x0EEE },
]

@interrupt(vblank)
let each_frame = () -> void => {
  ` + blank + `
}

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 0, vdp.rgb(0, 0, 7))
  vdp.display_on()
  raster.start(BANDS, 0, 3, 56)
  vdp.enable_vblank_interrupt()
  var k: u16 = 0
  loop {
    ` + loop + `
  }
}
`
}

// checkRasterBands checks a three bands' ROM's every line, at both ends and the middle, on
// several frames.
func checkRasterBands(t *testing.T, out string) {
	t.Helper()
	for _, frames := range []string{"100", "121", "122", "150", "151"} {
		s := runROM(t, out, frames, "")
		at := func(x, y int) string {
			k := (y*s.width + x) * 3
			return fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
		}
		band := []string{at(160, 20), at(160, 80), at(160, 140), at(160, 200)}
		for i := 1; i < len(band); i++ {
			if band[i] == band[i-1] {
				t.Fatalf("frame %s: bands %d and %d are both %s: no change made", frames, i-1, i, band[i])
			}
		}
		for y := 0; y < s.height && y < 224; y++ {
			want := band[y/56]
			for _, x := range []int{0, 160, s.width - 1} {
				if got := at(x, y); got != want {
					t.Errorf("frame %s: line %d at x %d is %s, want %s", frames, y, x, got, want)
				}
			}
		}
	}
}

// TestGenesis_RasterEffectsFade fades a screen whose backdrop changes at line 112 to white
// with std.genesis.palette: the colour set partway down fades with the rest, so every line is
// white — and back, so the two bands are their own colours again.
func TestGenesis_RasterEffectsFade(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	program := func(fades string) string {
		return `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.palette
import std.genesis.raster
import std.genesis.raster.{ Raster }

const BELOW: [1]Raster = #[Raster { line: 112, color: 0, word: 0x000E }]

@interrupt(vblank)
let each_frame = () -> void => sprites.show()

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 0, vdp.rgb(0, 0, 7))
  vdp.display_on()
  raster.start(BELOW, 0, 1, 112)
  vdp.enable_vblank_interrupt()
  ` + fades + `
  loop {}
}
`
	}
	for _, c := range []struct{ fades, top, bottom, what string }{
		{"palette.fade_to(vdp.rgb(7, 7, 7), 4)", "ffffff", "ffffff", "faded to white"},
		{"palette.fade_to(vdp.rgb(7, 7, 7), 4)\n  palette.fade_back(4)", "0000ff", "ff0000", "faded to white and back"},
	} {
		s := runROM(t, genesisProgram(t, "raster-fade", program(c.fades)), "60", "")
		at := func(x, y int) string {
			k := (y*s.width + x) * 3
			return fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
		}
		if got := at(160, 50); got != c.top {
			t.Errorf("%s: above the change is %s, want %s", c.what, got, c.top)
		}
		if got := at(160, 170); got != c.bottom {
			t.Errorf("%s: below the change is %s, want %s", c.what, got, c.bottom)
		}
	}
}
