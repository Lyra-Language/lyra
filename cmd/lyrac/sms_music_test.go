package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// std.sms.music must say what std.chips.music says, byte for byte, frame by frame: it is that
// player for the PSG, in 16 bits. Random songs, with every effect either reads and envelopes of
// every shape, played through both on the host; then the 16-bit one on the Z80 itself, which is
// held to the bytes the host heard from it.
func TestSMS_MusicSaysWhatTheSharedPlayerSays(t *testing.T) {
	t.Parallel()
	z80ToolchainOrSkip(t)
	root := repoRoot(t)
	smscheck := smscheckOrSkip(t, root)
	z80run := filepath.Join(filepath.Dir(smscheck), "z80run")
	if _, err := os.Stat(z80run); err != nil {
		t.Skipf("no z80run at %s", z80run)
	}
	setenv(t, "LYRA_STD", root)
	for seed := int64(1); seed <= 5; seed++ {
		tables := randomSong(rand.New(rand.NewSource(seed)))

		dir := t.TempDir()
		host := filepath.Join(dir, "host.lyra")
		if err := os.WriteFile(host, []byte(tables+musicAgreement), 0o644); err != nil {
			t.Fatal(err)
		}
		said, stderr, code := captureRun(t, "run", host)
		if code != 0 {
			t.Fatalf("seed %d: host run exited %d: %s", seed, code, stderr)
		}
		lines := strings.Split(strings.TrimSpace(said), "\n")
		counts := lines[len(lines)-1]
		var frames, bytes, notes, silences, heldFrames int
		if _, err := fmt.Sscanf(counts, "frames=%d bytes=%d notes=%d silences=%d held=%d", &frames, &bytes, &notes, &silences, &heldFrames); err != nil {
			t.Fatalf("seed %d: %v, from %q", seed, err, counts)
		}
		// The comparison has to have compared something, of every kind.
		if bytes < 3000 || notes < 1000 || silences < 20 || heldFrames < 500 {
			t.Errorf("seed %d: too little was played to prove anything: %s", seed, counts)
		}

		z80dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(z80dir, "lyra.toml"), []byte("target = \"sms\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(z80dir, "song.lyra")
		if err := os.WriteFile(src, []byte(tables+musicOnTheZ80), 0o644); err != nil {
			t.Fatal(err)
		}
		rom := filepath.Join(z80dir, "song.sms")
		if _, stderr, code := captureRun(t, "build", "-o", rom, src); code != 0 {
			t.Fatalf("seed %d: build exited %d: %s", seed, code, stderr)
		}
		out, err := exec.Command(z80run, rom, "reports").CombinedOutput()
		if err != nil {
			t.Fatalf("seed %d: z80run: %v\n%s", seed, err, out)
		}
		// Its last line is z80run's own: where the Z80 stopped, which must be at the end.
		ran := strings.Split(strings.TrimSpace(string(out)), "\n")
		if end := ran[len(ran)-1]; !strings.Contains(end, "halted=1") {
			t.Fatalf("seed %d: the Z80 did not finish: %s", seed, end)
		}
		got := ran[:len(ran)-1]
		want := lines[:len(lines)-1]
		if len(got) != len(want) {
			t.Errorf("seed %d: the Z80 said %d bytes, the host %d", seed, len(got), len(want))
		}
		for i := 0; i < len(got) && i < len(want); i++ {
			if got[i] != want[i] {
				t.Errorf("seed %d: byte %d: the Z80 said %s, the host %s", seed, i, got[i], want[i])
				break
			}
		}
		t.Logf("seed %d: %s", seed, counts)
	}
}

// randomSong is the tables of a song in Lyra: patterns of cells drawn so that every effect comes
// up often, on notes across the range, and envelopes with every combination of loop and release.
func randomSong(r *rand.Rand) string {
	const patterns, rows, orders, channels = 24, 16, 8, 13
	effects := []int{0xFF, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x08, 0x0A, 0x0B, 0x0D, 0x0F,
		0x20, 0xE1, 0xE2, 0xE5, 0xEC, 0xED}
	effect := func() (int, int) {
		if r.Intn(2) == 0 {
			return 0xFF, 0
		}
		code := effects[r.Intn(len(effects))]
		param := r.Intn(256)
		switch code {
		case 0x0B: // Most jumps stay in the song; a few fall off it.
			param = r.Intn(orders + 2)
		case 0x0D:
			param = r.Intn(rows + 2)
		case 0x0F:
			param = r.Intn(9)
		case 0xEC, 0xED:
			param = r.Intn(10)
		}
		return code, param
	}
	var b strings.Builder
	b.WriteString(`import std.chips.music.{ Tune, Cell, Envelope, EnvelopeStep, MusicData, Sample, new_music as new_shared }
import std.chips.ym2612.{ FmPatch }
import std.sms.music.{ new_music as new_small }

`)
	fmt.Fprintf(&b, "const ROWS: i32 = %d\nconst CHANNELS: i32 = %d\n", rows, channels)

	fmt.Fprintf(&b, "const CELL_BYTES: [%d]u8 = #[", patterns*rows*7)
	for p := 0; p < patterns*rows; p++ {
		note := 0xFF
		switch n := r.Intn(10); {
		case n < 4:
			note = r.Intn(128)
		case n < 5:
			note = 0xFE
		}
		instrument, volume := 0xFF, 0xFF
		if r.Intn(2) == 0 {
			instrument = r.Intn(7) // Past the end now and then: no envelope.
		}
		if r.Intn(3) == 0 {
			volume = r.Intn(20)
		}
		e1, p1 := effect()
		e2, p2 := 0xFF, 0
		if r.Intn(4) == 0 {
			e2, p2 = effect()
		}
		fmt.Fprintf(&b, "%d, %d, %d, %d, %d, %d, %d, ", note, instrument, volume, e1, p1, e2, p2)
	}
	b.WriteString("]\n")

	fmt.Fprintf(&b, "const ORDER_PATTERNS: [%d]u8 = #[", orders*channels)
	for i := 0; i < orders*channels; i++ {
		fmt.Fprintf(&b, "%d, ", r.Intn(patterns))
	}
	b.WriteString("]\n")

	var steps []string
	b.WriteString("const ENVELOPES: [5]Envelope = #[\n")
	for i := 0; i < 5; i++ {
		count := 1 + r.Intn(12)
		first := len(steps)
		for s := 0; s < count; s++ {
			steps = append(steps, fmt.Sprintf("EnvelopeStep { volume: %d, pitch: %d }", r.Intn(16), r.Intn(1200)-600))
		}
		loop, release := 0xFFFF, 0xFFFF
		if r.Intn(2) == 0 {
			loop = r.Intn(count + 1)
		}
		if r.Intn(2) == 0 {
			release = r.Intn(count)
		}
		fmt.Fprintf(&b, "  Envelope { first: %d, count: %d, white: %v, loop: %d, release: %d },\n",
			first, count, r.Intn(2) == 0, loop, release)
	}
	b.WriteString("]\n")
	fmt.Fprintf(&b, "const STEPS: [%d]EnvelopeStep = #[%s]\n", len(steps), strings.Join(steps, ", "))

	b.WriteString("const TUNES: [4]Tune = #[\n")
	tunes := [][4]int{{6, 0, 60, 0}, {3, 5, 50, 2}, {1, 2, 120, 1}, {4, 0, 30, 0}}
	for i, u := range tunes {
		fmt.Fprintf(&b, "  Tune { first_order: 0, orders: %d, rows: ROWS_U, speed: %d, speed2: %d, rate: %d, loop_to: %d, extended: %v },\n",
			orders, u[0], u[1], u[2], u[3], i%2 == 1)
	}
	b.WriteString("]\n")
	return strings.ReplaceAll(b.String(), "ROWS_U", fmt.Sprint(rows)) + musicTables
}

const musicTables = `
const PATCHES: [0]FmPatch = #[]
const SAMPLES: [1]Sample = #[Sample {}]

struct Tables {}

impl MusicData for Tables {
  cell = pure (self, index) => {
    let at = index * 7
    Cell {
      note: CELL_BYTES[at],
      instrument: CELL_BYTES[at + 1],
      volume: CELL_BYTES[at + 2],
      effect: CELL_BYTES[at + 3],
      param: CELL_BYTES[at + 4],
      effect2: CELL_BYTES[at + 5],
      param2: CELL_BYTES[at + 6],
    }
  }
  pattern = pure (self, order, channel) => i32(ORDER_PATTERNS[order * CHANNELS + channel]) * ROWS
  patch = pure (self, index) => PATCHES[index]
  patches = pure (self) => 0
  envelope = pure (self, index) => ENVELOPES[index]
  fm_envelope = pure (self, index) => ENVELOPES[index]
  envelopes = pure (self) => 5
  envelope_step = pure (self, index) => STEPS[index]
  sample = pure (self, index) => SAMPLES[0]
}

/// An effect comes and goes on the PSG's channels, as the song plays: tone 1, or the noise, which
/// holds tone 3 too, or both.
let held_at = pure (frame: i16) -> u8 => {
  let phase = (frame >> 5) & 7
  if phase == 1 { 1 } else if phase == 3 { 12 } else if phase == 6 { 13 } else { 0 }
}

const FRAMES: i16 = 500
const SILENT_FRAMES: i16 = 10
`

// musicAgreement plays both players, saying the 16-bit one's bytes and stopping at any
// difference, and last how much it compared.
const musicAgreement = `
var a = new_shared()
var b = new_small()
var psg_a: [16]u8 = #[0; 16]
var fm: [768]u8 = #[0; 768]
var bytes = 0
var notes = 0
var silences = 0
var held_frames = 0

let main = () -> void => {
  var frames = 0
  for t in 0..<4 {
    a.play_song(TUNES[t])
    b.play_song(TUNES[t])
    var f: i16 = 0
    while f < FRAMES + SILENT_FRAMES {
      if f == FRAMES {
        a.stop()
        b.stop()
      }
      let held = held_at(f)
      if held != 0 { held_frames += 1 }
      let (na, _) = a.frame(Tables {}, u16(held) << 6, psg_a, fm)
      let nb = b.frame(Tables {}, held)
      var same = na == i64(nb)
      var i = 0
      while same && i < na {
        same = psg_a[i] == b.byte(i16(i))
        i += 1
      }
      if !same {
        var left = ""
        var right = ""
        for k in 0..<na { left = "${left} ${psg_a[k]}" }
        for k in 0..<nb { right = "${right} ${b.byte(k)}" }
        panic("tune ${t} frame ${f}: shared says${left}, the 16-bit says${right}")
      }
      for k in 0..<na {
        let byte = psg_a[k]
        println("${byte}")
        bytes += 1
        if (byte & 0x90) == 0x90 && (byte & 0x0F) == 15 { silences += 1 }
        if (byte & 0x90) == 0x80 { notes += 1 }
      }
      frames += 1
      f += 1
    }
  }
  println("frames=${frames} bytes=${bytes} notes=${notes} silences=${silences} held=${held_frames}")
}
`

// musicOnTheZ80 plays the 16-bit one, reporting its bytes.
const musicOnTheZ80 = `
var b = new_small()

let main = () -> void => {
  var t: i16 = 0
  while t < 4 {
    b.play_song(TUNES[t])
    var f: i16 = 0
    while f < FRAMES + SILENT_FRAMES {
      if f == FRAMES { b.stop() }
      let n = b.frame(Tables {}, held_at(f))
      var k: i16 = 0
      while k < n {
        unsafe { port_write(0x7E, b.byte(k)) }
        k += 1
      }
      f += 1
    }
    t += 1
  }
}
`
