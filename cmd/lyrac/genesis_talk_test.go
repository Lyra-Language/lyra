package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestGenesis_ATalkFollowsItsChoices plays a conversation through std.genesis.talk, scripted
// by frame: line 0 ("AB" says "HI", with a portrait) lists two choices, the second is chosen,
// and a press goes to its line ("C" says "BYE", no portrait); a last press ends the talk, and
// the backdrop turns green if the second choice was the one picked. Each glyph is a solid
// tile — a letter white, the space blue, '>' red — so each cell of the box reads as one
// character of a picture: the speaker on the first row inside the margin, the words under it
// (five cells further in for the portrait), the choices under them with '>' at the chosen. Its lines hold
// their strings: a struct holding a string links on the Genesis (the runtime's `free`). And a
// lit sprite (in front, which the VDP draws over even the window's front cells) sits under the
// box every frame: hidden by talk.mask_behind while the talk goes on, seen once it is over.
func TestGenesis_ATalkFollowsItsChoices(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "talk", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }
import std.genesis.talk.{ Talk, Line, Choice, END }
import std.genesis.text.{ Font, TextBox }

const SPACE: [8]u32 = #[0x11111111; 8]
const LETTER: [8]u32 = #[0x22222222; 8]
const MARK: [8]u32 = #[0x33333333; 8]

const LINES: [3]Line = #[
  Line { who: "AB", words: "HI", portrait: 0, first_choice: 0, choices: 2 },
  Line { who: "D", words: "OK", then: END },
  Line { who: "C", words: "BYE", then: END },
]
const CHOICES: [2]Choice = #[Choice { words: "YES", then: 1 }, Choice { words: "NO", then: 2 }]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 0, 7))
  vdp.set_color(0, 2, vdp.rgb(7, 7, 7))
  vdp.set_color(0, 3, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, SPACE)
  for k: u16 in 2..<61 { vdp.load_tiles(k, LETTER) }
  vdp.load_tiles(31, MARK)
  vdp.window_rows_below(8)
  var talk = Talk { box: TextBox { x: 2, y: 20, width: 14, height: 6, font: Font { tile: 1, count: 59 } } }
  vdp.display_on()
  var frame: u16 = 0
  loop {
    vdp.wait_vblank()
    sprites.show()
    frame += 1
    if frame == 1 { talk.start(LINES, CHOICES) }
    if frame == 50 { talk.choose(LINES, CHOICES, 1) }
    if frame == 70 || frame == 120 { talk.press(LINES, CHOICES) }
    if frame == 121 && !talk.talking() && talk.last_picked() == 1 { vdp.set_color(0, 0, vdp.rgb(0, 7, 0)) }
    talk.update(LINES, CHOICES)
    sprites.clear()
    talk.mask_behind()
    sprites.add(Sprite { x: 96, y: 192, tile: 31, front: true })
  }
}
`)
	cells := func(frames string) string {
		s := runROMWith(t, out, frames)
		var b strings.Builder
		for y := 20; y < 26; y++ {
			for x := 2; x < 16; x++ {
				i := ((y*8+4)*s.width + x*8 + 4) * 3
				switch fmt.Sprintf("%02x%02x%02x", s.pixels[i], s.pixels[i+1], s.pixels[i+2]) {
				case "ffffff":
					b.WriteByte('#')
				case "0000ff":
					b.WriteByte('.')
				case "ff0000":
					b.WriteByte('>')
				case "00ff00":
					b.WriteByte('g')
				case "000000":
					b.WriteByte(' ')
				default:
					b.WriteByte('?')
				}
			}
			b.WriteByte('\n')
		}
		return b.String()
	}
	for _, c := range []struct{ frames, want, what string }{
		{"40", "" +
			"..............\n" +
			"......##......\n" +
			"......##......\n" +
			"......>.###...\n" +
			"........##....\n" +
			"..............\n", "the first line typed under its speaker, its choices listed, the first chosen"},
		// The game's frame 50 is the screen's 59 or so: its start-up (59 tiles loaded) takes
		// ten frames.
		{"66", "" +
			"..............\n" +
			"......##......\n" +
			"......##......\n" +
			"........###...\n" +
			"......>.##....\n" +
			"..............\n", "the second choice chosen"},
		{"100", "" +
			"..............\n" +
			".#............\n" +
			".###..........\n" +
			"..............\n" +
			"..............\n" +
			"..............\n", "the second choice's line, with no portrait"},
		{"140", strings.Repeat(strings.Repeat("g", 14)+"\n", 4) + "gggggggggg>ggg\n" + strings.Repeat("g", 14) + "\n", "the talk over, the second choice picked, the sprite seen"},
	} {
		if got := cells(c.frames); got != c.want {
			t.Errorf("%s (frame %s):\n%s\nwant:\n%s", c.what, c.frames, got, c.want)
		}
	}
}

// TestGenesis_ATalkInAFramedBox draws a talk's box from a 3×3 frame — corners yellow, edges
// green, the middle the font's blue — a blank row between its lines, and the speaker's name in
// palette 1, where the font's ink is red: the frame's pieces at the box's corners and edges
// whatever its size, the name on the first row, a row under it before the words, the second
// line of words a row below the first.
func TestGenesis_ATalkInAFramedBox(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "framed", `import std.genesis.vdp
import std.genesis.talk.{ Talk, Line, Choice, END }
import std.genesis.text.{ Font, TextBox }

const SPACE: [8]u32 = #[0x11111111; 8]
const LETTER: [8]u32 = #[0x22222222; 8]
const CORNER: [8]u32 = #[0x44444444; 8]
const EDGE: [8]u32 = #[0x55555555; 8]

const LINES: [1]Line = #[Line { who: "A", words: "BB CC", then: END }]
const CHOICES: [0]Choice = #[]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 0, 7))
  vdp.set_color(0, 2, vdp.rgb(7, 7, 7))
  vdp.set_color(0, 4, vdp.rgb(7, 7, 0))
  vdp.set_color(0, 5, vdp.rgb(0, 7, 0))
  vdp.set_color(1, 2, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, SPACE)
  for k: u16 in 2..<61 { vdp.load_tiles(k, LETTER) }
  // The frame: tiles 70 to 78, three a row — the middle the font's blue.
  for k: u16 in 0..<9 {
    if k == 4 { vdp.load_tiles(70 + k, SPACE) } else if k == 0 || k == 2 || k == 6 || k == 8 { vdp.load_tiles(70 + k, CORNER) } else { vdp.load_tiles(70 + k, EDGE) }
  }
  vdp.window_rows_below(8)
  var talk = Talk {
    box: TextBox { x: 2, y: 20, width: 6, height: 8, font: Font { tile: 1, count: 59 }, spacing: 1, frame: 70, frame_stride: 3 },
    name_palette: 1,
  }
  talk.start(LINES, CHOICES)
  vdp.display_on()
  loop {
    vdp.wait_vblank()
    talk.update(LINES, CHOICES)
  }
}
`)
	s := runROMWith(t, out, "60")
	var b strings.Builder
	for y := 20; y < 28; y++ {
		for x := 2; x < 8; x++ {
			i := ((y*8+4)*s.width + x*8 + 4) * 3
			switch fmt.Sprintf("%02x%02x%02x", s.pixels[i], s.pixels[i+1], s.pixels[i+2]) {
			case "ffffff":
				b.WriteByte('#')
			case "0000ff":
				b.WriteByte('.')
			case "ffff00":
				b.WriteByte('Y')
			case "00ff00":
				b.WriteByte('G')
			case "ff0000":
				b.WriteByte('r')
			default:
				b.WriteByte('?')
			}
		}
		b.WriteByte('\n')
	}
	want := "" +
		"YGGGGY\n" +
		"Gr...G\n" +
		"G....G\n" +
		"G##..G\n" +
		"G....G\n" +
		"G##..G\n" +
		"G....G\n" +
		"YGGGGY\n"
	if got := b.String(); got != want {
		t.Errorf("the framed box:\n%s\nwant:\n%s", got, want)
	}
}

// TestGenesis_ATalksFlagsChooseItsLines plays a talk twice through std.genesis.talk's flags:
// the first time line 0 ("unless" flag 0, setting it) is said, the second it is passed over
// for line 1 (which "needs" flag 0); a choice there sets flag 3, which the game reads; a line
// whose flags let none of the rest be said ends the talk; flag 200 lands in its byte, 256 is
// none; and keep_flags puts every flag back
// as a save had them. Each step that goes as it should adds a colour to the backdrop's
// red, green and blue, so a backdrop of all three is every step.
func TestGenesis_ATalksFlagsChooseItsLines(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "flags", `import std.genesis.vdp
import std.genesis.talk
import std.genesis.talk.{ Talk, Line, Choice, END, FLAG_BYTES }
import std.genesis.text.{ Font, TextBox }

const LINE: [8]u32 = #[0x22222222; 8]

const LINES: [3]Line = #[
  Line { who: "A", words: "HI", unless: 0, sets: 0, then: END },
  Line { who: "A", words: "AGAIN", needs: 0, first_choice: 0, choices: 2 },
  Line { who: "A", words: "NEVER", needs: 5, then: END },
]
const CHOICES: [2]Choice = #[Choice { words: "YES", sets: 3 }, Choice { words: "NO", then: 2 }]

let main = () -> void => {
  vdp.init()
  for k: u16 in 1..<61 { vdp.load_tiles(k, LINE) }
  vdp.window_rows_below(8)
  var t = Talk { box: TextBox { x: 2, y: 20, width: 14, height: 6, font: Font { tile: 1, count: 59 } } }
  var r: u16 = 0
  var g: u16 = 0
  var b: u16 = 0
  // A stranger: line 0, which sets flag 0 and ends.
  t.start(LINES, CHOICES)
  if t.line == 0 && talk.flag(0) { r += 3 }
  t.press(LINES, CHOICES)
  t.press(LINES, CHOICES)
  if !t.talking() { r += 4 }
  // A friend: line 0 passed over for line 1; its first choice sets flag 3.
  t.start(LINES, CHOICES)
  if t.line == 1 { g += 3 }
  t.press(LINES, CHOICES)
  t.press(LINES, CHOICES)
  if talk.flag(3) && !t.talking() { g += 4 }
  // The second choice goes on to line 2, which needs flag 5: none of the rest is said.
  t.start(LINES, CHOICES)
  t.press(LINES, CHOICES)
  t.choose(LINES, CHOICES, 1)
  t.press(LINES, CHOICES)
  if !t.talking() && t.last_picked() == 1 { b += 3 }
  // A save's flags put back: flag 0 alone, so flags 3 and 200 are let go; flag 256 is none.
  talk.set_flag(200)
  talk.set_flag(256)
  let saved = talk.flags()
  var kept: [FLAG_BYTES]u8 = #[0; FLAG_BYTES]
  kept[0] = 1
  talk.keep_flags(kept)
  if !talk.flag(3) && !talk.flag(200) && !talk.flag(256) && saved[0] == 9 && saved[25] == 1 { b += 4 }
  vdp.set_color(0, 0, vdp.rgb(r, g, b))
  vdp.display_on()
  loop { vdp.wait_vblank() }
}
`)
	s := runROMWith(t, out, "20")
	i := (4*s.width + 4) * 3
	if got := fmt.Sprintf("%02x%02x%02x", s.pixels[i], s.pixels[i+1], s.pixels[i+2]); got != "ffffff" {
		t.Errorf("the backdrop is %s, want ffffff: red, green and blue each for its steps", got)
	}
}
