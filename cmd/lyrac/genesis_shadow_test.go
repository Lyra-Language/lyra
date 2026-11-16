package main

import (
	"fmt"
	"testing"
)

// TestGenesis_ShadowAndHighlight turns on `vdp.shadow_highlight` over a row of grey cells of
// plane B and checks each of its rules in Sheliak (whose shadow and highlight are held to
// BlastEm's): a low-priority cell is shadowed and a high one not; palette 3's colour 14, a
// highlight operator, lifts a shadowed cell to normal and a normal one (under a high sprite) to
// the highlight; its colour 15 drops one into the shadow; a low sprite takes the shadow under
// it and a high one does not. The operators are not drawn as colours: palette 3's 14 and 15
// are red, and no red shows.
func TestGenesis_ShadowAndHighlight(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "shadow", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }

const TILES: [24]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0xEEEEEEEE, 0xEEEEEEEE, 0xEEEEEEEE, 0xEEEEEEEE,
  0xEEEEEEEE, 0xEEEEEEEE, 0xEEEEEEEE, 0xEEEEEEEE,
  0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF,
  0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(4, 4, 4))
  vdp.set_color(3, 14, vdp.rgb(7, 0, 0))
  vdp.set_color(3, 15, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, TILES)
  // A row of grey cells, every fourth from column 2; those at 6, 14 and 18 high priority.
  for k: u16 in 0..<7 {
    let column = 2 + k * 4
    let high: u16 = if column == 6 || column == 14 || column == 18 { 0x8000 } else { 0 }
    vdp.write_cell(vdp.PLANE_B, column, 2, high | 1)
  }
  vdp.shadow_highlight(true)
  vdp.display_on()
  loop {
    sprites.clear()
    sprites.add(Sprite { x: 80, y: 16, tile: 2, palette: 3 })
    sprites.add(Sprite { x: 112, y: 16, tile: 2, palette: 3, front: true })
    sprites.add(Sprite { x: 144, y: 16, tile: 3, palette: 3, front: true })
    sprites.add(Sprite { x: 176, y: 16, tile: 1 })
    sprites.add(Sprite { x: 208, y: 16, tile: 1, front: true })
    vdp.wait_vblank()
    sprites.show()
  }
}
`)
	s := runROM(t, out, "120", "")
	at := func(column int) string {
		k := (20*s.width + column*8 + 4) * 3
		return fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
	}
	shadow, normal, highlight := at(2), at(6), at(14)
	if !(shadow < normal && normal < highlight) {
		t.Fatalf("shadow %s, normal %s, highlight %s: want three greys, darkest to brightest", shadow, normal, highlight)
	}
	for _, c := range []struct {
		column int
		want   string
		what   string
	}{
		{2, shadow, "a low-priority cell, shadowed"},
		{6, normal, "a high-priority cell, normal"},
		{10, normal, "a highlight over a shadowed cell, back to normal"},
		{14, highlight, "a highlight from a high sprite over a high cell, highlighted"},
		{18, shadow, "a shadow from a high sprite over a high cell, shadowed"},
		{22, shadow, "a low sprite over a shadowed cell, shadowed"},
		{26, normal, "a high sprite over a shadowed cell, normal"},
	} {
		if got := at(c.column); got != c.want {
			t.Errorf("column %d (%s) is %s, want %s", c.column, c.what, got, c.want)
		}
	}
}
