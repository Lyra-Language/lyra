package main

import (
	"testing"
)

// TestGenesis_StreamedSpritesSendTheirFramesByDMA keeps a sprite's two one-tile frames — red,
// then green — in ROM only: nothing loads them, so a sprite shows only what std.genesis.sprites
// sent by DMA. ONE has one slot and shows frame 0 for 120 frames of its loop, then frame 1;
// TWO has two and shows both at once; a second frame of ONE wanted in the same list finds its
// one slot drawn and is not drawn at all. The backdrop turns magenta if `vdp.dma_run` does not stop a copy at
// a 128 KB boundary of its source.
func TestGenesis_StreamedSpritesSendTheirFramesByDMA(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "stream", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite, Stream }

const ART: [16]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
]

const ONE: Stream = Stream { tile: 5, size: 1, slots: 1, first_slot: 0 }
const TWO: Stream = Stream { tile: 6, size: 1, slots: 2, first_slot: 1 }

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(7, 0, 0))
  vdp.set_color(0, 2, vdp.rgb(0, 7, 0))
  if vdp.dma_run(0x1FFF8, 16) != 4 || vdp.dma_run(0x20000, 16) != 16 {
    vdp.set_color(0, 0, vdp.rgb(7, 0, 7))
  }
  vdp.display_on()
  var n: u16 = 0
  loop {
    sprites.clear()
    let a = sprites.streamed(ONE, ART, if n < 120 { 0 } else { 1 }, 1)
    if a != sprites.NO_TILE { sprites.add(Sprite { x: 16, y: 16, tile: a }) }
    let b = sprites.streamed(TWO, ART, 0, 1)
    if b != sprites.NO_TILE { sprites.add(Sprite { x: 40, y: 16, tile: b }) }
    let c = sprites.streamed(TWO, ART, 1, 1)
    if c != sprites.NO_TILE { sprites.add(Sprite { x: 64, y: 16, tile: c }) }
    let d = sprites.streamed(ONE, ART, if n < 120 { 1 } else { 0 }, 1)
    if d != sprites.NO_TILE { sprites.add(Sprite { x: 88, y: 16, tile: d }) }
    vdp.wait_vblank()
    sprites.show()
    if n < 1000 { n += 1 }
  }
}
`)
	red, green, black := [3]byte{255, 0, 0}, [3]byte{0, 255, 0}, [3]byte{0, 0, 0}
	at := func(s screen, x, y int) [3]byte {
		k := (y*s.width + x) * 3
		return [3]byte{s.pixels[k], s.pixels[k+1], s.pixels[k+2]}
	}
	for _, c := range []struct {
		frames string
		want   [4][3]byte
		what   string
	}{
		{"100", [4][3]byte{red, red, green, black}, "frame 0 sent to ONE's slot; both frames to TWO's two; no slot left for ONE's second"},
		{"300", [4][3]byte{green, red, green, black}, "frame 1 sent to ONE's slot once it is shown"},
	} {
		s := runROM(t, out, c.frames, "")
		if s.dominant() != "000000" {
			t.Fatalf("after %s frames the backdrop is %s: dma_run does not stop at a 128 KB boundary", c.frames, s.dominant())
		}
		for k, x := range []int{20, 44, 68, 92} {
			if got := at(s, x, 20); got != c.want[k] {
				t.Errorf("after %s frames (%s): the sprite at x %d is %v, want %v", c.frames, c.what, x-4, got, c.want[k])
			}
		}
	}
}
