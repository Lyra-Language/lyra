package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenesis_ACameraStreamsABlockMap is TestGenesis_ACameraScrollsAMapBiggerThanAPlane on
// the same 80×40 cells written as blocks (std.genesis.blocks): forty by twenty 2×2 blocks,
// three of them — empty, a tile at its top-left, a tile at its bottom-left. Scrolled right
// then down, staged a frame ahead and sent by sprites.show, the two tiles must be where the
// view shows them; then back left and up, two others — across the plane's wrap each way.
func TestGenesis_ACameraStreamsABlockMap(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	program := func(start, step string, tiles string) string {
		return `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.blocks
import std.genesis.camera
import std.genesis.camera.{ Camera }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  // The header (2×2 blocks, 40 rows, 40 blocks a row, numbers from 16), three blocks, then
  // a block number a square.
  var map: [816]u16 = #[0; 816]
  map[0] = 1
  map[1] = 40
  map[2] = 40
  map[3] = 16
  map[8] = 1
  map[14] = 1
` + tiles + `
  let width = blocks.BLOCKS | 80
  var view = ` + start + `
  camera.show_map(view, vdp.PLANE_B, map, width)
  vdp.display_on()
  loop {
    view.last_x = view.x
    view.last_y = view.y
    ` + step + `
    vdp.wait_vblank()
    sprites.show()
    camera.scroll_map(view, vdp.PLANE_B, map, width)
  }
}
`
	}
	// Cell (70, 20) is block (35, 10)'s top-left, block 1; (62, 35) block (31, 17)'s
	// bottom-left, block 2.
	right := genesisProgram(t, "blocks_right", program(
		"Camera { world_width: 640, world_height: 320 }",
		"if view.x < 480 { view.x += 4 } else { view.y = (view.y + 2).min(96) }",
		"  map[16 + 10 * 40 + 35] = 1\n  map[16 + 17 * 40 + 31] = 2",
	))
	screen := runROM(t, right, "240", "")
	if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != 16 || y0 != 64 || x1 != 88 || y1 != 192 {
		t.Errorf("scrolled right and down, the tiles span [%d,%d)–[%d,%d), want [16,64)–[88,192)", x0, y0, x1, y1)
	}
	// Cell (10, 20) is block (5, 10)'s top-left; (30, 5) block (15, 2)'s bottom-left.
	back := genesisProgram(t, "blocks_back", program(
		"Camera { x: 480, y: 96, last_x: 480, last_y: 96, world_width: 640, world_height: 320 }",
		"if view.x > 0 { view.x -= 4 } else { view.y = (view.y - 2).max(0) }",
		"  map[16 + 10 * 40 + 5] = 1\n  map[16 + 2 * 40 + 15] = 2",
	))
	screen = runROM(t, back, "240", "")
	if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != 80 || y0 != 40 || x1 != 248 || y1 != 168 {
		t.Errorf("scrolled back, the tiles span [%d,%d)–[%d,%d), want [80,40)–[248,168)", x0, y0, x1, y1)
	}
}

// blockForm is `cells` (`width` a row) as std.genesis.blocks lays out a block map of
// `side`×`side` blocks: the header, each distinct block once, then a number a square.
func blockForm(cells []uint16, width, side int) []uint16 {
	rows := len(cells) / width
	bw, bh := (width+side-1)/side, (rows+side-1)/side
	shift := map[int]uint16{2: 1, 4: 2}[side]
	var table []uint16
	index := map[string]uint16{}
	numbers := make([]uint16, 0, bw*bh)
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			block := make([]uint16, side*side)
			for y := 0; y < side; y++ {
				for x := 0; x < side; x++ {
					if mx, my := bx*side+x, by*side+y; mx < width && my < rows {
						block[y*side+x] = cells[my*width+mx]
					}
				}
			}
			key := fmt.Sprint(block)
			n, ok := index[key]
			if !ok {
				n = uint16(len(index))
				index[key] = n
				table = append(table, block...)
			}
			numbers = append(numbers, n)
		}
	}
	out := []uint16{shift, uint16(rows), uint16(bw), uint16(4 + len(table))}
	return append(append(out, table...), numbers...)
}

// lyraWords is a const array's literal.
func lyraWords(words []uint16) string {
	parts := make([]string, len(words))
	for k, w := range words {
		parts[k] = fmt.Sprint(w)
	}
	return "#[" + strings.Join(parts, ", ") + "]"
}

// TestRun_BlockMapsReadAsTheirCells: a 21×13 map — odd both ways, so its last blocks are cut —
// of ground, walls and flipped tiles, and the same map as 2×2 and 4×4 blocks
// (std.genesis.blocks). Every cell, past the edges too, must read the same; and a collider
// swept over every place must hit the map's tiles, and an entity moved through it stop,
// exactly where it does on the cells — on the host, std.genesis's code being plain Lyra.
func TestRun_BlockMapsReadAsTheirCells(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	const width, rows = 21, 13
	cells := make([]uint16, width*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < width; x++ {
			switch {
			case y == rows-1 || x == 0:
				cells[y*width+x] = 1 // ground and a wall
			case (x*7+y*3)%11 == 0:
				cells[y*width+x] = 2 | 0x0800 // a half tile, mirrored
			case (x+y)%9 == 0:
				cells[y*width+x] = 2 | 0x1000 // flipped
			}
		}
	}
	two, four := blockForm(cells, width, 2), blockForm(cells, width, 4)
	src := filepath.Join(t.TempDir(), "main.lyra")
	program := `import std.genesis.blocks
import std.genesis.collision.{ Shape, Collider }
import std.genesis.entity.{ Animation, EntityKind }

const CELLS: [` + fmt.Sprint(len(cells)) + `]u16 = ` + lyraWords(cells) + `
const TWO: [` + fmt.Sprint(len(two)) + `]u16 = ` + lyraWords(two) + `
const FOUR: [` + fmt.Sprint(len(four)) + `]u16 = ` + lyraWords(four) + `

// Tile 1 a whole box, tile 2 its left half; tiles from 1.
const STARTS: [3]u16 = #[0, 1, 2]
const SHAPES: [2]Shape = #[Shape { x: 0, y: 0, width: 8, height: 8 }, Shape { x: 0, y: 0, width: 4, height: 8 }]
const WALLS: [2]Collider = #[
  Collider { shape: Shape { x: 0, y: 0, width: 8, height: 8 }, layers: 1, mask: 0 },
  Collider { shape: Shape { x: 0, y: 0, width: 4, height: 8 }, layers: 1, mask: 0 },
]
const NO_BANDS: [2]u16 = #[0, 0]
const NO_SHAPES: [1]Collider = #[Collider { shape: Shape { x: 0, y: 0, width: 0, height: 0 }, layers: 0, mask: 0 }]

const STILL: Animation = Animation { sprite: 0, first: 0, count: 1, total: 1, looping: false }
const NONE: Collider = Collider { shape: Shape { x: 0, y: 0, width: 0, height: 0 }, layers: 0, mask: 0 }
const BODY: Collider = Collider { shape: Shape { x: 1, y: 1, width: 6, height: 7 }, layers: 0, mask: 1 }
const WALKER: EntityKind = EntityKind {
  width: 8,
  height: 8,
  animation: STILL,
  colliders: #[BODY, NONE, NONE, NONE],
  collider_count: 1,
  max_speed: 512,
  acceleration: 64,
  friction: 64,
  gravity: 64,
}

let main = () -> u8 => {
  let w2 = blocks.BLOCKS | 21
  if blocks.rows(CELLS, 21) != 13 || blocks.rows(TWO, w2) != 13 || blocks.rows(FOUR, w2) != 13 { return 1 }
  for my: u16 in 0..<16 {
    for mx: u16 in 0..<24 {
      let want = blocks.cell_at(CELLS, 21, mx, my)
      if blocks.cell_at(TWO, w2, mx, my) != want || blocks.cell_at(FOUR, w2, mx, my) != want { return 2 }
    }
  }
  let probe = Shape { x: 0, y: 0, width: 5, height: 6 }
  for y: i16 in -4..<108 {
    for x: i16 in -4..<170 {
      let want = probe.hits_map(x, y, CELLS, 21, 1, STARTS, SHAPES)
      if probe.hits_map(x, y, TWO, w2, 1, STARTS, SHAPES) != want || probe.hits_map(x, y, FOUR, w2, 1, STARTS, SHAPES) != want { return 3 }
    }
  }
  for start: i16 in 0..<18 {
    var a = WALKER.spawn(8 * start + 8, 16)
    var b = WALKER.spawn(8 * start + 8, 16)
    var c = WALKER.spawn(8 * start + 8, 16)
    for frame: i16 in 0..<90 {
      let dx: i16 = if frame < 45 { 1 } else { -1 }
      a.push(dx, 0)
      b.push(dx, 0)
      c.push(dx, 0)
      a.move_in_with(CELLS, 21, 1, STARTS, WALLS, NO_BANDS, NO_SHAPES)
      b.move_in_with(TWO, w2, 1, STARTS, WALLS, NO_BANDS, NO_SHAPES)
      c.move_in_with(FOUR, w2, 1, STARTS, WALLS, NO_BANDS, NO_SHAPES)
      if a.x() != b.x() || a.y() != b.y() || a.x() != c.x() || a.y() != c.y() { return 4 }
    }
  }
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "run", src); code != 0 {
		t.Errorf("case %d failed; stderr: %s", code, stderr)
	}
}
