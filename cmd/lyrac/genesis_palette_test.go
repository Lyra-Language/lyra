package main

import (
	"fmt"
	"testing"
)

// TestGenesis_PaletteCyclesAndFades turns two runs of three colours — red, green, blue at 1
// to 3 — with std.genesis.palette: palette 0's forward and palette 1's backward, a step every
// five frames, stepped five times, so each has taken one step: palette 0's colour 1 (the
// backdrop) is what was its 3, blue, and palette 1's colour 1 (a cell of plane A) what was its
// 2, green. Then `fade_to` takes every colour to white, and with `fade_back` after it back to
// what was shown — and the same a step a frame (`start_fade_to`, `start_fade_back`,
// `step_fade`), as a cutscene fades while its game goes on.
func TestGenesis_PaletteCyclesAndFades(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	program := func(fades string) string {
		return fmt.Sprintf(`import std.genesis.vdp
import std.genesis.palette
import std.genesis.palette.{ Cycle }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

const CYCLES: [2]Cycle = #[
  Cycle { palette: 0, first: 1, count: 3, frames: 5 },
  Cycle { palette: 1, first: 1, count: 3, frames: 5, backward: true },
]

let main = () -> void => {
  vdp.init()
  for p: u16 in 0..<2 {
    vdp.set_color(p, 1, vdp.rgb(7, 0, 0))
    vdp.set_color(p, 2, vdp.rgb(0, 7, 0))
    vdp.set_color(p, 3, vdp.rgb(0, 0, 7))
  }
  vdp.set_backdrop(0, 1)
  vdp.load_tiles(1, SOLID)
  vdp.write_cell(vdp.PLANE_A, 2, 2, (1 << 13) | 1)
  for _: i16 in 0..<5 { palette.step_cycles(CYCLES) }
  vdp.send_colors()
  vdp.display_on()
  %s
  loop {}
}

/// The fade begun stepped a frame at a time, as a cutscene steps it: over after its four
/// steps, and over at once after that — else palette 1's cell is red, which no case wants.
let stepped = () -> void => {
  var steps = 0
  loop {
    steps += 1
    let over = palette.step_fade()
    vdp.wait_vblank()
    vdp.send_colors()
    if over { break }
  }
  if steps != 4 || !palette.step_fade() {
    vdp.set_color(1, 1, vdp.rgb(7, 0, 0))
    vdp.set_color(1, 2, vdp.rgb(7, 0, 0))
    vdp.set_color(1, 3, vdp.rgb(7, 0, 0))
  }
}
`, fades)
	}
	at := func(s screen, x, y int) string {
		k := (y*s.width + x) * 3
		return fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
	}
	for _, c := range []struct {
		fades, backdrop, cell, what string
	}{
		{"", "0000ff", "00ff00", "a step each: forward brings 3 to 1, backward 2"},
		{"palette.fade_to(vdp.rgb(7, 7, 7), 4)", "ffffff", "ffffff", "faded to white"},
		{"palette.fade_to(vdp.rgb(7, 7, 7), 4)\n  palette.fade_back(4)", "0000ff", "00ff00", "faded to white and back"},
		{"palette.start_fade_to(vdp.rgb(7, 7, 7), 4)\n  stepped()", "ffffff", "ffffff", "faded to white a step a frame"},
		{"palette.start_fade_to(vdp.rgb(7, 7, 7), 4)\n  stepped()\n  palette.start_fade_back(4)\n  stepped()", "0000ff", "00ff00", "faded to white and back a step a frame"},
	} {
		out := genesisProgram(t, "palette", program(c.fades))
		s := runROM(t, out, "120", "")
		if got := at(s, 100, 100); got != c.backdrop {
			t.Errorf("%s: the backdrop is %s, want %s", c.what, got, c.backdrop)
		}
		if got := at(s, 20, 20); got != c.cell {
			t.Errorf("%s: the cell in palette 1 is %s, want %s", c.what, got, c.cell)
		}
	}
}
