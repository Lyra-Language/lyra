package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenesis_SixButtonPadsAndPadTwo reads both pads every frame and paints the backdrop by
// what they hold: X, Y and Z on pad 1 its red, green and blue, Mode a dimmer red, and pad 2's
// A and Z a dimmer green and blue. std.genesis.pad drives TH through a 6-button pad's four
// rises, so each must be seen — and nothing past the eight buttons from a 3-button pad, which
// never answers the signature.
func TestGenesis_SixButtonPadsAndPadTwo(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "pads", `import std.genesis.vdp
import std.genesis.pad

let main = () -> void => {
  vdp.init()
  pad.init()
  vdp.display_on()
  loop {
    vdp.wait_vblank()
    let one = pad.read(1)
    let two = pad.read(2)
    var r: u16 = 0
    var g: u16 = 0
    var b: u16 = 0
    if pad.held(one, pad.X) { r = 7 }
    if pad.held(one, pad.Y) { g = 7 }
    if pad.held(one, pad.Z) { b = 7 }
    if pad.held(one, pad.MODE) { r = 3 }
    if pad.held(two, pad.A) { g = 3 }
    if pad.held(two, pad.Z) { b = 3 }
    if pad.held(one, pad.A) && pad.held(one, pad.START) { b = 5 }
    vdp.set_color(0, 0, vdp.rgb(r, g, b))
  }
}
`)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--hold", "x"}, "ff0000"},
		{[]string{"--hold", "y"}, "00ff00"},
		{[]string{"--hold", "z"}, "0000ff"},
		{[]string{"--hold", "mode"}, "770000"},
		{[]string{"--hold", "x,z"}, "ff00ff"},
		{[]string{"--hold", "a,start"}, "0000ae"},
		{[]string{"--hold2", "a"}, "007700"},
		{[]string{"--hold2", "z"}, "000077"},
		{[]string{"--pads", "3", "--hold", "x,y,z,mode"}, "000000"},
		{[]string{"--pads", "3", "--hold", "a,start"}, "0000ae"},
	} {
		if got := runROMWith(t, out, "30", c.args...).dominant(); got != c.want {
			t.Errorf("%s: the backdrop is %s, want %s", strings.Join(c.args, " "), got, c.want)
		}
	}
}

// TestGenesis_ASaveOutlivesTheConsole runs a game twice on one save file (Sheliak's
// `--save`, the .srm beside the ROM): the first run finds no save, stores one and turns the
// screen red; the second loads it and turns it green. `save_ram` in lyra.toml must reach the
// header, and the file must hold what std.genesis.save writes: its mark, length and sum, then
// the bytes.
func TestGenesis_ASaveOutlivesTheConsole(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgramWith(t, "kept", "save_ram = \"8192\"\n", `import std.genesis.vdp
import std.genesis.save

let main = () -> void => {
  vdp.init()
  var kept: [4]u8 = #[0, 0, 0, 0]
  if save.load(kept) && kept[0] == 7 && kept[3] == 42 {
    vdp.set_color(0, 0, vdp.rgb(0, 7, 0))
  } else {
    kept = #[7, 1, 2, 42]
    save.store(kept)
    vdp.set_color(0, 0, vdp.rgb(7, 0, 0))
  }
  vdp.display_on()
  loop {}
}
`)
	rom, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(rom[0x1B0:0x1B2]) != "RA" {
		t.Fatalf("the header declares no save RAM: % x", rom[0x1B0:0x1BC])
	}
	if got := runROMWith(t, out, "30", "--save").dominant(); got != "ff0000" {
		t.Errorf("the first run is %s, want red: no save yet, one stored", got)
	}
	srm, err := os.ReadFile(strings.TrimSuffix(out, ".bin") + ".srm")
	if err != nil {
		t.Fatalf("Sheliak kept no save file: %v", err)
	}
	if want := []byte{'L', 'S', 0, 4, 0, 52, 7, 1, 2, 42}; len(srm) != 8192 || !bytes.HasPrefix(srm, want) {
		t.Errorf("the save file is %d bytes from % x, want 8192 from % x", len(srm), srm[:min(len(srm), 10)], want)
	}
	if got := runROMWith(t, out, "30", "--save").dominant(); got != "00ff00" {
		t.Errorf("the second run is %s, want green: the save found", got)
	}
}

// TestGenesis_ATextBoxTypesAndWraps types "HELLO THERE FRIEND!" into a box of six cells by
// three inside its margin, in the window over the bottom eight rows, a letter every two
// frames: each glyph a solid white tile, the space solid blue. Part way, the letters shown
// must be the start of the line; at the end, HELLO and THERE on rows of their own (the word
// that would pass the edge wraps), FRIEND filling the third and the "!" cut — no fourth row.
// And text.write puts "ab" as capitals at the bottom row.
func TestGenesis_ATextBoxTypesAndWraps(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "talk", `import std.genesis.vdp
import std.genesis.text
import std.genesis.text.{ Font, TextBox }

const SPACE: [8]u32 = #[0x11111111; 8]
const LETTER: [8]u32 = #[0x22222222; 8]
const WORDS: string = "HELLO THERE FRIEND!"

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 0, 7))
  vdp.set_color(0, 2, vdp.rgb(7, 7, 7))
  vdp.load_tiles(1, SPACE)
  for k: u16 in 2..<61 { vdp.load_tiles(k, LETTER) }
  let font = Font { tile: 1, count: 59 }
  vdp.window_rows_below(8)
  text.write(vdp.WINDOW, 30, 27, font, "ab")
  var talk = TextBox { x: 2, y: 20, width: 8, height: 5, font: font }
  talk.open()
  vdp.display_on()
  loop {
    vdp.wait_vblank()
    talk.update(WORDS)
  }
}
`)
	cells := func(frames string) string {
		s := runROMWith(t, out, frames)
		var b strings.Builder
		for y := 20; y < 25; y++ {
			for x := 2; x < 10; x++ {
				i := ((y*8+4)*s.width + x*8 + 4) * 3
				switch fmt.Sprintf("%02x%02x%02x", s.pixels[i], s.pixels[i+1], s.pixels[i+2]) {
				case "ffffff":
					b.WriteByte('#')
				case "0000ff":
					b.WriteByte('.')
				default:
					b.WriteByte('?')
				}
			}
			b.WriteByte('\n')
		}
		for _, x := range []int{30, 31} {
			i := ((27*8+4)*s.width + x*8 + 4) * 3
			if s.pixels[i] != 0xFF || s.pixels[i+2] != 0xFF || s.pixels[i+1] != 0xFF {
				b.WriteString("(a written letter is missing)")
			}
		}
		return b.String()
	}
	whole := "........\n.#####..\n.#####..\n.######.\n........\n"
	if got := cells("200"); got != whole {
		t.Errorf("the box typed out:\n%s\nwant:\n%s", got, whole)
	}
	part := cells("30")
	typed := strings.Count(part, "#")
	if typed == 0 || typed >= strings.Count(whole, "#") {
		t.Fatalf("part way, %d letters are shown:\n%s", typed, part)
	}
	// The letters shown part way are the first of the whole, in reading order.
	var prefix strings.Builder
	n := 0
	for _, c := range whole {
		if c == '#' {
			if n < typed {
				prefix.WriteByte('#')
			} else {
				prefix.WriteByte('.')
			}
			n++
		} else {
			prefix.WriteRune(c)
		}
	}
	if part != prefix.String() {
		t.Errorf("part way the box shows:\n%s\nwant the first %d letters:\n%s", part, typed, prefix.String())
	}
}

// genesisProgramWith is genesisProgram with more lines for its lyra.toml.
func genesisProgramWith(t *testing.T, name, toml, source string) string {
	t.Helper()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"+toml), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, name+".lyra")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	return out
}

// runROMWith is runROM with Sheliak's arguments as given.
func runROMWith(t *testing.T, romPath, frames string, extra ...string) screen {
	t.Helper()
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Log("SHELIAK is not set; the ROM was built but not run")
		t.SkipNow()
	}
	ppm := fmt.Sprintf("%s.%s.%s.ppm", romPath, frames, strings.NewReplacer(" ", "", ",", "", "-", "").Replace(strings.Join(extra, "")))
	args := append([]string{romPath, "--frames", frames, "--ppm", ppm}, extra...)
	cmd := command(t, emulator, args...)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ppm)
	if err != nil {
		t.Fatal(err)
	}
	var s screen
	var magic string
	var maxval int
	if n, err := fmt.Sscan(string(data), &magic, &s.width, &s.height, &maxval); err != nil || n != 4 || magic != "P6" {
		t.Fatalf("not a PPM: %v", err)
	}
	s.pixels = data[len(data)-s.width*s.height*3:]
	return s
}
