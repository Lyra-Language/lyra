package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGenesis_SheliakMeasuresAFrame holds Sheliak's `--stats` (what Vega's Profiler panel
// shows) to a game whose cost is known. Each frame it waits for the vertical blank — by
// the status register, or with B held by a count its handler bumps, as Vega's games do —
// then DMAs 64 words and does a little work; with A held, more work than a frame has; with
// C held, 22 sprites on one line, two past what the VDP draws. The waiting must be told
// from the working either way, an overrun seen as a frame worked whole, and the DMA's bytes
// and the crowded line counted.
func TestGenesis_SheliakMeasuresAFrame(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "costs", `import std.genesis.vdp
import std.genesis.pad
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }

var ticks: u16 = 0
var sink: u16 = 0

@interrupt(vblank)
let each_frame = () -> void => {
  ticks = ticks.wrapping_add(1)
}

let main = () -> void => {
  vdp.init()
  pad.init()
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {
    let held = pad.read(1)
    if pad.held(held, pad.B) {
      let seen = unsafe { (&ticks).read_volatile() }
      while unsafe { (&ticks).read_volatile() } == seen {}
    } else {
      vdp.wait_vblank()
    }
    vdp.dma_to_vram(0x1000, 0xA000, 64)
    sprites.clear()
    let crowd: i16 = if pad.held(held, pad.C) { 22 } else { 2 }
    for k: i16 in 0..<crowd { sprites.add(Sprite { x: k * 12, y: 100, tile: 1 }) }
    sprites.show()
    let turns: u16 = if pad.held(held, pad.A) { 20000 } else { 200 }
    for k: u16 in 0..<turns { unsafe { (&mut sink).write_volatile(k) } }
  }
}
`)
	// The symbol map beside it: every function by address — main, the runtime's, and the local
	// ones LLVM's internalize made (a panic's) — each inside the ROM.
	sym, err := os.ReadFile(out + ".sym")
	if err != nil {
		t.Fatalf("lyrac wrote no symbol map: %v", err)
	}
	rom, _ := os.ReadFile(out)
	var last uint64
	found := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(sym)), "\n") {
		fields := strings.Fields(line)
		addr, err := strconv.ParseUint(fields[0], 16, 32)
		if err != nil || len(fields) != 3 || addr < last || addr >= uint64(len(rom)) {
			t.Fatalf("not a symbol map line, in order, inside the ROM: %q", line)
		}
		last = addr
		found[fields[2]] = true
	}
	if !found["main"] || !found["_start"] || !found["lyra_panic_overflow"] {
		t.Errorf("the symbol map lacks main, the runtime's _start or a local function:\n%s", sym)
	}
	for _, c := range []struct {
		hold                 string
		least, most          float64 // the frames' worked share, on average
		sprites, crowdedLine int
	}{
		{"", 0.005, 0.25, 2, 0},
		{"b", 0.005, 0.25, 2, 0},
		// A game frame some six of the VDP's: each worked whole, but the one it waits in.
		{"a", 0.9, 1.0, 2, 0},
		{"c", 0.005, 0.25, 22, 8},
	} {
		frames, functions := statsOf(t, out, c.hold)
		var worked float64
		dmas := 0
		for _, f := range frames {
			worked += float64(f.worked) / float64(f.clocks)
			if f.dma == 128 {
				dmas++
			} else if f.dma != 0 {
				t.Errorf("--hold %q: a frame's DMA is %d bytes, want 128 or none", c.hold, f.dma)
			}
			if f.most != c.sprites || f.crowded != c.crowdedLine {
				t.Errorf("--hold %q: a frame's most sprites a line %d and crowded lines %d, want %d and %d", c.hold, f.most, f.crowded, c.sprites, c.crowdedLine)
				break
			}
		}
		worked /= float64(len(frames))
		if worked < c.least || worked > c.most {
			t.Errorf("--hold %q: the frames worked %.3f of their clocks on average, want %.3f to %.3f", c.hold, worked, c.least, c.most)
		}
		// By function (lyrac writes the ROM's symbol map beside it): with A held the work is
		// main's own loop, most of it.
		if len(functions) == 0 {
			t.Errorf("--hold %q: no function's work in the stats", c.hold)
		} else if c.hold == "a" && !strings.HasSuffix(functions[0], " main") {
			t.Errorf("--hold %q: the busiest function is %q, want main", c.hold, functions[0])
		}
		// A frame a pass of the loop, each DMAing; an overrun DMAs in only the frames it waits in.
		if wantAll := c.hold != "a"; (wantAll && dmas != len(frames)) || (!wantAll && (dmas == 0 || dmas > len(frames)/3)) {
			t.Errorf("--hold %q: %d of %d frames DMA'd", c.hold, dmas, len(frames))
		}
	}
}

// frameCost is a line of Sheliak's `--stats` file.
type frameCost struct {
	clocks, worked, dma, most, crowded int
}

// statsOf runs a ROM 120 frames holding `hold` on pad 1 and reads `--stats`' file: the
// last 60 frames, past the start-up, and its functions' lines, the busiest first.
func statsOf(t *testing.T, rom, hold string) (frames []frameCost, functions []string) {
	t.Helper()
	stats := filepath.Join(t.TempDir(), "stats.txt")
	args := []string{"--stats", stats}
	if hold != "" {
		args = append(args, "--hold", hold)
	}
	runROMWith(t, rom, "120", args...)
	data, err := os.ReadFile(stats)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(all) < 4 || all[0] != "sheliak stats 2" || all[len(all)-1] != "end 120" {
		t.Fatalf("not a whole stats file:\n%s", data)
	}
	// The frames' lines, the functions' set apart.
	var lines []string
	for _, line := range all {
		if strings.HasPrefix(line, "function ") {
			functions = append(functions, line)
		} else {
			lines = append(lines, line)
		}
	}
	limits := strings.Fields(lines[1])
	clocks, _ := strconv.Atoi(limits[1])
	for _, line := range lines[len(lines)-61 : len(lines)-1] {
		var n [4]int
		for k, field := range strings.Fields(line) {
			n[k], _ = strconv.Atoi(field)
		}
		frames = append(frames, frameCost{clocks, n[0], n[1], n[2], n[3]})
	}
	return frames, functions
}
