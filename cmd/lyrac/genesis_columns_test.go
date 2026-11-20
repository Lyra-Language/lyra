package main

import (
	"fmt"
	"slices"
	"testing"
)

// TestGenesis_ColumnsRipplePlanesUpAndDown draws a green line across plane B at map line 96
// (the top row of a tile on cell row 12) and ripples its columns with std.genesis.camera's
// scroll_columns: a quarter turn a column, 8 pixels at most, so the screen's 16-pixel columns
// are moved 0, 7, 0 and -8 pixels in turn — the line at 96, 89, 96 and 104. Plane A's columns
// are turned on too but never rippled, and scrolled 80 down before column scroll starts
// (another scene's) and to 0 after: they must show its scroll now, its line (map line 40) at
// 40 in every column — a plane kept waiting for its first ripple showed where a scene before
// left it (the heroes' overlook, held at Start, 11/20). Also once with the line scroll on
// beside it (vdp.use_bands for plane A), the two modes in register 11 at once.
func TestGenesis_ColumnsRipplePlanesUpAndDown(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	for _, banded := range []bool{false, true} {
		out := genesisProgram(t, fmt.Sprintf("columns_%v", banded), fmt.Sprintf(`import std.genesis.vdp
import std.genesis.sprites
import std.genesis.camera
import std.genesis.camera.{ ColumnWave }

@interrupt(vblank)
let each_frame = () -> void => sprites.show()

const LINE: [8]u32 = #[0x11111111, 0, 0, 0, 0, 0, 0, 0]
const WAVE: ColumnWave = ColumnWave { wave: 8, wave_step: 16384 }

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, LINE)
  for x: u16 in 0..<64 {
    vdp.write_cell(vdp.PLANE_B, x, 12, 1)
    vdp.write_cell(vdp.PLANE_A, x, 5, 1)
  }
  vdp.set_scroll(vdp.PLANE_A, 0, 80)
  vdp.use_columns(true, true)
  vdp.use_bands(%v, false)
  vdp.set_scroll(vdp.PLANE_A, 0, 0)
  vdp.set_scroll(vdp.PLANE_B, 0, 0)
  camera.scroll_columns(vdp.PLANE_B, WAVE, 0)
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {}
}
`, banded))
		screen := runROM(t, out, "30", "")
		for _, c := range []struct {
			x    int
			want []int
			what string
		}{
			{8, []int{40, 96}, "column 0, not moved"},
			{24, []int{40, 89}, "column 1, moved 7 down: the map up"},
			{40, []int{40, 96}, "column 2, not moved"},
			{56, []int{40, 104}, "column 3, moved 8 up"},
			{72, []int{40, 96}, "column 4, a turn on"},
		} {
			if got := greenRows(screen, c.x); !slices.Equal(got, c.want) {
				t.Errorf("banded %v, x %d (%s): lines at %v, want %v", banded, c.x, c.what, got, c.want)
			}
		}
	}
}

// greenRows is where in column x the screen is pure green.
func greenRows(s screen, x int) []int {
	var ys []int
	for y := 0; y < s.height; y++ {
		k := (y*s.width + x) * 3
		if fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2]) == "00ff00" {
			ys = append(ys, y)
		}
	}
	return ys
}
