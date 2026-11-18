package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// litSquares is which of `n` squares, the k-th with its top-left at (x0 + k*step, y), shows
// on screen s: a pixel inside it that is not black.
func litSquares(s screen, n, x0, step, y int) []bool {
	out := make([]bool, n)
	for k := range out {
		at := ((y+3)*s.width + x0 + k*step + 3) * 3
		out[k] = s.pixels[at] != 0 || s.pixels[at+1] != 0 || s.pixels[at+2] != 0
	}
	return out
}

// missing is the indices of the squares not shown.
func missing(lit []bool) []int {
	var out []int
	for k, on := range lit {
		if !on {
			out = append(out, k)
		}
	}
	return out
}

// turnsProgram is a loop drawing `n` one-tile sprites side by side on one line each frame,
// a figure each, taking turns when `turns` — more than the chip shows on a line.
func turnsProgram(console string, n int, turns bool) string {
	take := ""
	if turns {
		take = "sprites.take_turns()"
	}
	if console == "genesis" {
		return fmt.Sprintf(`import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }

const SQUARE: [8]u32 = #[0x11111111; 8]

let main = () -> void => {
  vdp.init()
  vdp.load_tiles(1, SQUARE)
  vdp.set_color(0, 1, vdp.rgb(7, 7, 7))
  vdp.display_on()
  loop {
    sprites.clear()
    %s
    for k: i16 in 0..<%d {
      sprites.figure()
      sprites.add(Sprite { x: 8 + k * 12, y: 100, tile: 1 })
    }
    vdp.wait_vblank()
    sprites.show()
  }
}
`, take, n)
	}
	return fmt.Sprintf(`import std.sms.vdp
import std.sms.sprites
import std.sms.sprites.{ Sprite }

const COLORS: [2]u8 = #[0x00, 0x3F]

const SQUARE: [32]u8 = #[
  0xFF, 0, 0, 0, 0xFF, 0, 0, 0, 0xFF, 0, 0, 0, 0xFF, 0, 0, 0,
  0xFF, 0, 0, 0, 0xFF, 0, 0, 0, 0xFF, 0, 0, 0, 0xFF, 0, 0, 0
]

let main = () -> void => {
  vdp.init()
  vdp.load_palette(16, COLORS)
  vdp.load_tiles(1, SQUARE)
  vdp.show()
  loop {
    sprites.clear()
    %s
    for k: i16 in 0..<%d {
      sprites.figure()
      sprites.add(Sprite { x: 8 + k * 12, y: 100, tile: 1 })
    }
    vdp.wait_vblank()
    sprites.show()
  }
}
`, take, n)
}

// TestGenesis_SpritesTakeTurnsOnACrowdedLine: 24 sprites on a line, where the VDP shows 20.
// Without turns the last four are missing every frame; with them, two frames running each
// show 20 and miss four, but not the same four.
func TestGenesis_SpritesTakeTurnsOnACrowdedLine(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	still := genesisProgram(t, "still", turnsProgram("genesis", 24, false))
	for _, frames := range []string{"30", "31"} {
		lost := missing(litSquares(runROM(t, still, frames, ""), 24, 8, 12, 100))
		if fmt.Sprint(lost) != "[20 21 22 23]" {
			t.Errorf("without turns, frame %s misses %v, want the last four", frames, lost)
		}
	}
	turning := genesisProgram(t, "turning", turnsProgram("genesis", 24, true))
	a := missing(litSquares(runROM(t, turning, "30", ""), 24, 8, 12, 100))
	b := missing(litSquares(runROM(t, turning, "31", ""), 24, 8, 12, 100))
	if len(a) != 4 || len(b) != 4 || fmt.Sprint(a) == fmt.Sprint(b) {
		t.Errorf("taking turns, frames 30 and 31 miss %v and %v, want four each and not the same", a, b)
	}
}

// TestSMS_SpritesTakeTurnsOnACrowdedLine: ten sprites on a line, where the Master System
// shows eight — the same as the Genesis's, on the Z80.
func TestSMS_SpritesTakeTurnsOnACrowdedLine(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	setenv(t, "LYRA_STD", root)
	build := func(name string, turns bool) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"sms\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, name+".lyra")
		if err := os.WriteFile(src, []byte(turnsProgram("sms", 10, turns)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, name+".sms")
		if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
			t.Fatalf("build exited %d: %s", code, stderr)
		}
		return out
	}
	// Clearing video memory takes the first half second.
	still := build("still", false)
	for _, frames := range []string{"60", "61"} {
		lost := missing(litSquares(smsScreen(t, smscheck, still, frames, ""), 10, 8, 12, 100))
		if fmt.Sprint(lost) != "[8 9]" {
			t.Errorf("without turns, frame %s misses %v, want the last two", frames, lost)
		}
	}
	turning := build("turning", true)
	a := missing(litSquares(smsScreen(t, smscheck, turning, "60", ""), 10, 8, 12, 100))
	b := missing(litSquares(smsScreen(t, smscheck, turning, "61", ""), 10, 8, 12, 100))
	if len(a) != 2 || len(b) != 2 || strings.EqualFold(fmt.Sprint(a), fmt.Sprint(b)) {
		t.Errorf("taking turns, frames 60 and 61 miss %v and %v, want two each and not the same", a, b)
	}
}
