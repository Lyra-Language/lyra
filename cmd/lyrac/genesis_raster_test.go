package main

import (
	"fmt"
	"strings"
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

// worldProgram is a raster program whose backdrop is blue, green from world line 150
// (raster.WORLD) and white from screen line 200, the interrupt every 8 lines; plane B holds a
// red line at map line 150, the world's, so the camera's place shows. Each frame its loop
// puts the camera at `y` (an expression of `frame`), in the vertical blank, beside the plane.
func worldProgram(y string) string {
	return `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.raster
import std.genesis.raster.{ Raster }

const WATER: [2]Raster = #[
  Raster { line: 150, color: raster.WORLD | 0, word: 0x00E0 },
  Raster { line: 200, color: 0, word: 0x0EEE },
]

const MARK: [8]u32 = #[0x11111111, 0, 0, 0, 0, 0, 0, 0]

@interrupt(vblank)
let each_frame = () -> void => sprites.show()

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 0, vdp.rgb(0, 0, 7))
  vdp.set_color(0, 1, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, MARK)
  // Map line 150 is row 18's third line: the tile's first line is its 0th, so row 18 put two
  // lines down is not possible — the mark is at line 144, and the change at 150 is 6 below it.
  for x: u16 in 0..<64 { vdp.write_cell(vdp.PLANE_B, x, 18, 1) }
  raster.start(WATER, 0, 2, 8)
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  var frame: i16 = 0
  loop {
    vdp.wait_vblank()
    frame = (frame + 1) % 64
    let y: i16 = ` + y + `
    raster.scroll_world(y)
    vdp.set_scroll(vdp.PLANE_B, 0, y)
  }
}
`
}

// TestGenesis_RasterKeptToTheWorld holds the camera still at places along the world: a change
// kept to the world (world line 150) shows at 150 - y on the screen, exactly, at both ends of
// the line — beside a change kept to the screen at 200, made in its place in the order — in
// the screen's first lines too, from line 3 (the frame's first interrupt at its top: from the
// step's, line 7, a change at line 4 came 4 lines low; one at line 1 or 2 still comes at 3),
// and colours the screen from the top when above it, and nothing when below it.
func TestGenesis_RasterKeptToTheWorld(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	const blue, green, white = "0000ff", "00ff00", "ffffff"
	for _, c := range []struct {
		y     string
		bands []struct {
			from  int
			color string
		}
	}{
		{"40", []struct {
			from  int
			color string
		}{{0, blue}, {110, green}, {200, white}}},
		{"0", []struct {
			from  int
			color string
		}{{0, blue}, {150, green}, {200, white}}},
		{"3", []struct {
			from  int
			color string
		}{{0, blue}, {147, green}, {200, white}}},
		{"170", []struct {
			from  int
			color string
		}{{0, green}, {200, white}}},
		{"146", []struct {
			from  int
			color string
		}{{0, blue}, {4, green}, {200, white}}},
		{"147", []struct {
			from  int
			color string
		}{{0, blue}, {3, green}, {200, white}}},
		{"-100", []struct {
			from  int
			color string
		}{{0, blue}, {200, white}}},
	} {
		out := genesisProgram(t, "raster-world", worldProgram(c.y))
		for _, frames := range []string{"60", "61"} {
			s := runROM(t, out, frames, "")
			for y := 0; y < 224; y++ {
				want := ""
				for _, b := range c.bands {
					if y >= b.from {
						want = b.color
					}
				}
				for _, x := range []int{0, 160, s.width - 1} {
					k := (y*s.width + x) * 3
					got := fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
					if got == "ff0000" {
						continue // the plane's mark
					}
					if got != want {
						t.Errorf("camera at %s, frame %s: line %d at x %d is %s, want %s", c.y, frames, y, x, got, want)
						break
					}
				}
			}
		}
	}
}

// TestGenesis_RasterKeptToTheWorldMovesWithThePlane moves the camera a line each frame from the
// game's loop: on every frame the green starts six lines below the plane's red mark (map line
// 144, the change at 150), so the change and the plane moved on the same frame.
func TestGenesis_RasterKeptToTheWorldMovesWithThePlane(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "raster-world-moving", worldProgram("frame * 2"))
	seen := map[int]bool{}
	for _, frames := range []string{"40", "41", "42", "43", "70", "71", "90"} {
		s := runROM(t, out, frames, "")
		at := func(y int) string {
			k := (y*s.width + 160) * 3
			return fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
		}
		mark, green := -1, -1
		for y := 0; y < 224; y++ {
			if mark < 0 && at(y) == "ff0000" {
				mark = y
			}
			if green < 0 && at(y) == "00ff00" {
				green = y
			}
		}
		if mark < 0 || green < 0 {
			t.Fatalf("frame %s: mark at %d, green from %d: the change or the mark is off the screen", frames, mark, green)
		}
		seen[mark] = true
		if green != mark+6 {
			t.Errorf("frame %s: green from %d, the mark at %d: want it six lines below", frames, green, mark)
		}
	}
	if len(seen) < 4 {
		t.Errorf("the camera did not move: marks at %v", seen)
	}
}

// TestGenesis_RasterConsecutiveLinesAtOneInterrupt changes the backdrop at lines 100 and 101,
// the interrupt every 8 lines, so one interrupt makes both: the second from its line exactly,
// at both ends — worked out after the first was written, it came about 100 pixels into its
// line, since a line is all there is between them (11/23).
func TestGenesis_RasterConsecutiveLinesAtOneInterrupt(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	src := strings.Replace(rasterProgram(`sprites.show()`, ``), `  Raster { line: 56, color: 0, word: 0x00E0 },
  Raster { line: 112, color: 0, word: 0x000E },`, `  Raster { line: 100, color: 0, word: 0x00E0 },
  Raster { line: 101, color: 0, word: 0x000E },`, 1)
	src = strings.Replace(src, "raster.start(BANDS, 0, 3, 56)", "raster.start(BANDS, 0, 3, 8)", 1)
	out := genesisProgram(t, "raster-consecutive", src)
	for _, frames := range []string{"60", "61"} {
		s := runROM(t, out, frames, "")
		for _, c := range []struct {
			y    int
			want string
		}{{99, "0000ff"}, {100, "00ff00"}, {101, "ff0000"}, {167, "ff0000"}, {168, "ffffff"}} {
			for _, x := range []int{0, 40, 160, s.width - 1} {
				k := (c.y*s.width + x) * 3
				if got := fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2]); got != c.want {
					t.Errorf("frame %s: line %d at x %d is %s, want %s", frames, c.y, x, got, c.want)
				}
			}
		}
	}
}

// rasterRunProgram changes palette 0's colours 1 to 6, red to blue, kept to the world on its lines
// 10 to 15 — a line apiece, the interrupt every 8, as Vega's waterline example changes its
// stone and its fish — on a plane striped in them, the camera held at `y` down the world.
func rasterRunProgram(y int) string {
	return fmt.Sprintf(`import std.genesis.vdp
import std.genesis.sprites
import std.genesis.raster
import std.genesis.raster.{ Raster }

@interrupt(vblank)
let each_frame = () -> void => sprites.show()

const STRIPES: [8]u32 = #[0x01234560; 8]

const RUN: [6]Raster = #[
  Raster { line: 10, color: raster.WORLD | 1, word: 0x0E00 },
  Raster { line: 11, color: raster.WORLD | 2, word: 0x0E20 },
  Raster { line: 12, color: raster.WORLD | 3, word: 0x0E40 },
  Raster { line: 13, color: raster.WORLD | 4, word: 0x0E60 },
  Raster { line: 14, color: raster.WORLD | 5, word: 0x0E80 },
  Raster { line: 15, color: raster.WORLD | 6, word: 0x0EA0 },
]

let main = () -> void => {
  vdp.init()
  for k: u16 in 1..<7 { vdp.set_color(0, k, vdp.rgb(7, k, 0)) }
  vdp.load_tiles(1, STRIPES)
  for y: u16 in 0..<32 {
    for x: u16 in 0..<64 { vdp.write_cell(vdp.PLANE_B, x, y, 1) }
  }
  raster.start(RUN, 0, 6, 8)
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {
    vdp.wait_vblank()
    raster.scroll_world(%d)
  }
}
`, y)
}

// TestGenesis_RasterRunOfConsecutiveLines changes six colours on six consecutive lines, at
// one interrupt, with the camera holding them on the screen, above it and below it: each
// colour's stripe red down to its line and blue from it, at both ends of the line — a third
// worked out in the interrupt took more than a line and came a line late, and one written
// after the call that makes it came pixels into its line (11/23) — and every stripe blue with
// the changes above the screen, six colours its top takes from them where four were kept.
func TestGenesis_RasterRunOfConsecutiveLines(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	for _, c := range []struct {
		y     int
		first int // the screen's line of the first change; -1 none on it
		wet   bool
	}{{0, 10, false}, {-90, 100, false}, {-200, 210, false}, {100, -1, true}, {-300, -1, false}} {
		out := genesisProgram(t, fmt.Sprintf("raster-run-%d", c.y), rasterRunProgram(c.y))
		for _, frames := range []string{"30", "31"} {
			s := runROM(t, out, frames, "")
			for k := 1; k <= 6; k++ {
				for _, x := range []int{k, 152 + k, s.width - 8 + k} {
					for y := 0; y < s.height; y++ {
						i := (y*s.width + x) * 3
						r, b := s.pixels[i], s.pixels[i+2]
						wet := c.wet || (c.first >= 0 && y >= c.first+k-1)
						if got := b > r; got != wet {
							t.Errorf("camera %d, frame %s: colour %d at line %d, x %d is %02x..%02x, want wet %v", c.y, frames, k, y, x, r, b, wet)
							break
						}
					}
				}
			}
		}
	}
}
