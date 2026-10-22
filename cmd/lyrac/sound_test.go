package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// std.chips.effects sends the SN76489 the bytes its registers take, frame by frame: a
// tone effect's period (a latch byte with the low 4 bits, a data byte with the upper 6) and
// its volume as an attenuation; a noise effect's setting once, its pitching channel (tone 2)
// silenced, then that channel's period and the noise's attenuation; and each channel
// silenced after its effect's last step. The bytes are worked out here from the register
// layout, not copied from the program.
func TestRun_PsgEffectsSendTheChipItsBytes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "effects.lyra")
	program := `import std.chips.effects.{ Effect, EffectStep, new_player, MAX_BYTES }

const STEPS: [3]EffectStep = #[
  EffectStep { period: 254, volume: 15 },
  EffectStep { period: 0x123, volume: 10 },
  EffectStep { period: 64, volume: 5 },
]

let main = () -> u8 => {
  var player = new_player()
  player.start(Effect { first: 0, count: 2 })
  player.start(Effect { first: 2, count: 1, noise: true, white: false })
  var out: [8]u8 = #[0; MAX_BYTES]
  for _ in 0..<4 {
    let n = player.frame(STEPS, out)
    var line = "frame"
    for i in 0..<n { line = line ++ " ${out[i]}" }
    println(line)
  }
  println("playing ${player.playing()}")
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("run exited %d\nstderr: %s", code, stderr)
	}
	latch := func(channel, low int) int { return 0x80 | channel<<5 | low }
	attenuation := func(channel, a int) int { return 0x90 | channel<<5 | a }
	frame := func(bytes ...int) string {
		words := []string{"frame"}
		for _, b := range bytes {
			words = append(words, strconv.Itoa(b))
		}
		return strings.Join(words, " ")
	}
	want := strings.Join([]string{
		// Tone: 254 on channel 0 at full volume. Noise: periodic at tone 2's rate, tone 2
		// silenced, its period 64 and the noise at volume 5 (attenuation 10).
		frame(latch(0, 254&15), 254>>4, attenuation(0, 0), 0xE3, attenuation(2, 15), latch(2, 64&15), 64>>4, attenuation(3, 10)),
		// Tone's second step, 0x123 at volume 10; the noise's last step was its only one.
		frame(latch(0, 0x123&15), 0x123>>4, attenuation(0, 5), attenuation(3, 15)),
		frame(attenuation(0, 15)),
		frame(),
		"playing false",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("got\n%swant\n%s", stdout, want)
	}
}

// std.game.sound played headless with --frames and --wav: an effect of thirty steps at
// period 254 is heard at the PSG's pitch for it (the clock over 32 × 254, 440.4 Hz) for half
// a second, then is silent — written as a 16-bit mono WAV at 48 kHz.
func TestRun_GameSoundIsHeardAtThePsgsPitch(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	src := filepath.Join(dir, "beep.lyra")
	program := `import std.game.sound
import std.chips.effects.{ Effect, EffectStep }
import std.chips.ym2612.{ FmPatch }

const STEPS: [30]EffectStep = #[EffectStep { period: 254, volume: 15 }; 30]
const NO_PATCHES: [0]FmPatch = #[]

let main = () -> u8 => {
  Effect { first: 0, count: 30 }.play()
  for _ in 0..<40 { sound.update(STEPS, NO_PATCHES) }
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(dir, "beep.wav")
	if _, stderr, code := captureRun(t, "run", src, "--", "--frames", "40", "--wav", wav); code != 0 {
		t.Fatalf("run exited %d\nstderr: %s", code, stderr)
	}
	data, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("not a WAV: %d bytes", len(data))
	}
	channels := binary.LittleEndian.Uint16(data[22:])
	rate := int(binary.LittleEndian.Uint32(data[24:]))
	bits := binary.LittleEndian.Uint16(data[34:])
	if channels != 1 || rate != 48000 || bits != 16 {
		t.Fatalf("a WAV of %d channels at %d Hz, %d bits", channels, rate, bits)
	}
	var samples []float64
	for i := 44; i+1 < len(data); i += 2 {
		samples = append(samples, float64(int16(binary.LittleEndian.Uint16(data[i:])))/32768)
	}
	frame := rate / 60
	if len(samples) < 40*frame-2 {
		t.Fatalf("%d samples for 40 frames", len(samples))
	}
	// Rising zero crossings from frame 3 to frame 30: the pitch.
	first, last, rises := -1, -1, 0
	for i := 3 * frame; i < 30*frame; i++ {
		if samples[i-1] < 0 && samples[i] >= 0 {
			if first < 0 {
				first = i
			}
			last = i
			rises++
		}
	}
	want := 3579545.0 / (32 * 254)
	if rises < 2 {
		t.Fatalf("nothing heard")
	}
	if got := float64(rises-1) * float64(rate) / float64(last-first); math.Abs(got-want) > 0.5 {
		t.Errorf("heard %.2f Hz, want %.2f", got, want)
	}
	// Its last step silences it; the DC the square wave rested at settles within frames.
	quiet := 0.0
	for _, v := range samples[38*frame:] {
		quiet = math.Max(quiet, math.Abs(v))
	}
	if quiet > 0.01 {
		t.Errorf("after the effect, still %.3f", quiet)
	}
}

// std.game.sound plays an FM effect on its YM2612: a patch of one sine (algorithm 7,
// operator 4 alone at full level, multiple 1), at A4's block and F-number, is heard at
// the pitch they make — the chip's clock over 144, times the F-number, times 2 to the
// block, over 2^21 — by the spectrum's peak (the chip's ladder and its DC fool counting
// zero crossings); and after its last step it is keyed off, its fastest release silent
// within frames.
func TestRun_GameSoundPlaysAnFmPatchAtItsNote(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	src := filepath.Join(dir, "sine.lyra")
	program := `import std.game.sound
import std.chips.effects.{ Effect, EffectStep, fm_period }
import std.chips.ym2612.{ FmPatch, fm_note }

const PATCHES: [1]FmPatch = #[FmPatch {
  ops: #[
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
  ],
  feedback_algorithm: 0x07,
  sensitivity: 0,
}]

let main = () -> u8 => {
  let (block, fnum) = fm_note(7670453, 440.0)
  println("${block} ${fnum}")
  var steps: [30]EffectStep = #[EffectStep { period: 0, volume: 15 }; 30]
  for k in 0..<30 { steps[k] = EffectStep { period: fm_period(block, fnum), volume: 15 } }
  Effect { first: 0, count: 30, fm: true }.play()
  for _ in 0..<45 { sound.update(steps, PATCHES) }
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(dir, "sine.wav")
	stdout, stderr, code := captureRun(t, "run", src, "--", "--frames", "45", "--wav", wav)
	if code != 0 {
		t.Fatalf("run exited %d\nstderr: %s", code, stderr)
	}
	var block, fnum int
	if _, err := fmt.Sscanf(stdout, "%d %d", &block, &fnum); err != nil || block != 4 || fnum != 1083 {
		t.Fatalf("A4 is block %d, F-number %d (%v); want 4, 1083", block, fnum, err)
	}
	data, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	var samples []float64
	for i := 44; i+1 < len(data); i += 2 {
		samples = append(samples, float64(int16(binary.LittleEndian.Uint16(data[i:])))/32768)
	}
	frame := 48000 / 60
	window := samples[3*frame : 27*frame]
	power := func(f float64) float64 {
		var re, im float64
		for i, v := range window {
			re += v * math.Cos(2*math.Pi*f*float64(i)/48000)
			im += v * math.Sin(2*math.Pi*f*float64(i)/48000)
		}
		return re*re + im*im
	}
	best := 0.0
	for f := 430.0; f < 450; f += 0.1 {
		if best == 0 || power(f) > power(best) {
			best = f
		}
	}
	want := 7670453.0 / 144 * float64(fnum) * math.Pow(2, float64(block)) / (1 << 21)
	if math.Abs(best-want) > 0.3 {
		t.Errorf("heard %.2f Hz, want %.2f", best, want)
	}
	// Operators 1–3 are silenced by their levels (127) at three times the note: a level
	// write the chip dropped — a write made before the last one's slot came round — sounds
	// there.
	if third := power(3 * want); third > power(want)/1000 {
		t.Errorf("the silenced operators are heard at %.0f Hz: %.3g against %.3g at the note", 3*want, third, power(want))
	}
	quiet := 0.0
	for _, v := range samples[34*frame:] {
		quiet = math.Max(quiet, math.Abs(v))
	}
	if quiet > 0.02 {
		t.Errorf("after the key off, still %.3f", quiet)
	}
}

// songTables is a song for std.chips.music, as Vega's assets.lyra writes one: FM 1 plays
// A4 on a sine patch, PSG tone 1 an E5 it lets go at row 4, and FM 6 a looped square wave
// sample (eight periods of 32 bytes at 8 kHz: 250 Hz at C4) at C5, 500 Hz, let go at row 4 too; the other
// channels share an empty pattern. `sound` is the runtime's module, `std.game.sound` or `std.genesis.sound`.
const songTables = `import std.chips.effects.{ EffectStep }
import std.chips.ym2612.{ FmPatch }
import std.chips.music.{ Tune, Cell, Envelope, EnvelopeStep, MusicData, Sample }

const STEPS: [1]EffectStep = #[EffectStep { period: 0, volume: 0 }]

const SINE: FmPatch = FmPatch {
  ops: #[
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x03, 0x7F, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
  ],
  feedback_algorithm: 0x07,
  sensitivity: 0,
}

const PATCHES: [2]FmPatch = #[SINE, SINE]

const SAMPLE_BYTES: [256]u8 = #[
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
  0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0, 0xE0,
  0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
]

const SAMPLES: [2]Sample = #[Sample {}, Sample { first: 0, length: 256, rate: 8000, loop_at: 0 }]

const CELLS: [32]Cell = #[
  Cell { note: 69, instrument: 0 }, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {},
  Cell { note: 76 }, Cell {}, Cell {}, Cell {}, Cell { note: 0xFE }, Cell {}, Cell {}, Cell {},
  Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {},
  Cell { note: 72, instrument: 1 }, Cell {}, Cell {}, Cell {}, Cell { note: 0xFE }, Cell {}, Cell {}, Cell {},
]

const NO_ENVELOPES: [1]Envelope = #[Envelope { first: 0, count: 0 }]
const NO_STEPS: [1]EnvelopeStep = #[EnvelopeStep { volume: 15, pitch: 0 }]

/// The song's tables, read through std.chips.music's trait.
data Tables = Tables

impl MusicData for Tables {
  cell = pure (self, index) => CELLS[index]
  pattern = pure (self, order, channel) => if channel == 0 { 0 } else if channel == 6 { 8 } else if channel == 5 { 24 } else { 16 }
  patch = pure (self, index) => PATCHES[index]
  patches = pure (self) => 2
  envelope = pure (self, index) => NO_ENVELOPES[index]
  fm_envelope = pure (self, index) => NO_ENVELOPES[0]
  envelopes = pure (self) => 0
  envelope_step = pure (self, index) => NO_STEPS[index]
  sample = pure (self, index) => SAMPLES[index]
}

const TUNE: Tune = Tune { first_order: 0, orders: 1, rows: 8, speed: 6 }
`

// A song plays on both chips through std.game.sound — FM 1's A4 at the YM2612's pitch for
// it and PSG 1's E5 at the PSG's, the PSG let go at its row — and the same program builds
// for the 68000 through std.genesis.sound, played in Sheliak where it is.
func TestRun_GameMusicPlaysBothChips(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	src := filepath.Join(dir, "song.lyra")
	program := "import std.game.sound\n" + songTables + `
let main = () -> u8 => {
  TUNE.play()
  let tables = Tables
  for _ in 0..<60 { sound.update_music(STEPS, PATCHES, tables, SAMPLE_BYTES) }
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(dir, "song.wav")
	if _, stderr, code := captureRun(t, "run", src, "--", "--frames", "60", "--wav", wav); code != 0 {
		t.Fatalf("run exited %d\nstderr: %s", code, stderr)
	}
	samples := wavSamples(t, wav)
	checkSong(t, "modern", samples, 48000)

	// The 68000's build of the same.
	genesisToolchainOrSkip(t)
	gdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gdir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gsrc := filepath.Join(gdir, "song.lyra")
	gprogram := "import std.genesis.vdp\nimport std.genesis.sound\n" + songTables + `
let tables = Tables

@interrupt(vblank)
let on_vblank = () -> void => sound.update_music(STEPS, PATCHES, tables, SAMPLE_BYTES)

let main = () -> void => {
  vdp.init()
  sound.init()
  TUNE.play()
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {}
}
`
	if err := os.WriteFile(gsrc, []byte(gprogram), 0o644); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(gdir, "song.bin")
	if _, stderr, code := captureRun(t, "build", "-o", rom, gsrc); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Log("SHELIAK is not set; the ROM was built but not heard")
		return
	}
	gwav := filepath.Join(gdir, "song.wav")
	cmd := command(t, emulator, rom, "--frames", "90", "--wav", gwav)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	gsamples, rate := wavSamplesRate(t, gwav)
	// The ROM's start-up before the song: from where it is first heard.
	onset := 0
	for i, v := range gsamples {
		if math.Abs(v) > 0.05 {
			onset = i
			break
		}
	}
	checkSong(t, "Genesis", gsamples[onset:], rate)
}

// checkSong: from its first frame, `samples` sound A4 on the YM2612, E5 on the PSG and the
// DAC's 500 Hz together, and after row 4 (24 frames) the E5 and the DAC's are gone and the
// A4 rings on. The DAC's within 1%: the Genesis's Z80 plays it at its own fixed rate,
// stopped a little each frame while the 68000 queues the frame's writes (0.5% flat in
// Sheliak).
func checkSong(t *testing.T, runtime string, samples []float64, rate int) {
	t.Helper()
	frame := rate / 60
	fm := 7670453.0 / 144 * 1083 * 16 / (1 << 21)
	psg := 3579545.0 / (32 * 170)
	dac := 500.0
	power := func(from, to int, f float64) float64 {
		var re, im float64
		window := samples[from*frame : to*frame]
		for i, v := range window {
			re += v * math.Cos(2*math.Pi*f*float64(i)/float64(rate))
			im += v * math.Sin(2*math.Pi*f*float64(i)/float64(rate))
		}
		return (re*re + im*im) / float64(len(window)*len(window))
	}
	if len(samples) < 46*frame {
		t.Fatalf("%s: only %d samples", runtime, len(samples))
	}
	near := func(f float64) float64 {
		best := 0.0
		for g := f * 0.99; g <= f*1.01; g += 0.5 {
			best = math.Max(best, power(2, 22, g))
		}
		return best
	}
	for _, f := range []float64{fm, psg, dac} {
		at, off := power(2, 22, f), power(2, 22, f*1.03)
		if f == dac {
			at = near(f)
		}
		if at < 100*off {
			t.Errorf(runtime+": %.1f Hz is not heard in the song's first rows: %.3g there, %.3g beside it", f, at, off)
		}
	}
	if late, early := power(28, 46, psg), power(2, 22, psg); late > early/1000 {
		t.Errorf(runtime+": the PSG's E5 sounds on after its note off: %.3g, against %.3g", late, early)
	}
	if late, early := power(28, 46, dac), power(2, 22, dac); late > early/1000 {
		t.Errorf(runtime+": the DAC's sample sounds on after its note off: %.3g, against %.3g", late, early)
	}
	if late, early := power(28, 46, fm), power(2, 22, fm); late < early/10 {
		t.Errorf(runtime+": the FM A4 stops with the PSG's: %.3g, against %.3g", late, early)
	}
}

func wavSamples(t *testing.T, path string) []float64 {
	t.Helper()
	s, _ := wavSamplesRate(t, path)
	return s
}

// wavSamplesRate is a 16-bit WAV's first channel, -1 to 1, and its rate.
func wavSamplesRate(t *testing.T, path string) ([]float64, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	channels := int(binary.LittleEndian.Uint16(data[22:]))
	rate := int(binary.LittleEndian.Uint32(data[24:]))
	var samples []float64
	for i := 44; i+1 < len(data); i += 2 * channels {
		samples = append(samples, float64(int16(binary.LittleEndian.Uint16(data[i:])))/32768)
	}
	return samples, rate
}

// extendedTables is a song with FM 3 extended: four sine operators, every one a carrier
// (algorithm 7), operator 1 (FM 3 itself) playing A4 and operator 2 (channel 10) E5.
const extendedTables = `import std.chips.effects.{ EffectStep }
import std.chips.ym2612.{ FmPatch }
import std.chips.music.{ Tune, Cell, Envelope, EnvelopeStep, MusicData, Sample }

const STEPS: [1]EffectStep = #[EffectStep { period: 0, volume: 0 }]

const PATCHES: [1]FmPatch = #[FmPatch {
  ops: #[
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
    0x01, 0x00, 0x1F, 0x00, 0x00, 0x0F, 0x00,
  ],
  feedback_algorithm: 0x07,
  sensitivity: 0,
}]

const SAMPLE_BYTES: [0]u8 = #[]

const CELLS: [24]Cell = #[
  Cell { note: 69, instrument: 0 }, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {},
  Cell { note: 76, instrument: 0 }, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {},
  Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {}, Cell {},
]

const NO_ENVELOPES: [1]Envelope = #[Envelope { first: 0, count: 0 }]
const NO_STEPS: [1]EnvelopeStep = #[EnvelopeStep { volume: 15, pitch: 0 }]

data Tables = Tables

impl MusicData for Tables {
  cell = pure (self, index) => CELLS[index]
  pattern = pure (self, order, channel) => if channel == 2 { 0 } else if channel == 10 { 8 } else { 16 }
  patch = pure (self, index) => PATCHES[index]
  patches = pure (self) => 1
  envelope = pure (self, index) => NO_ENVELOPES[index]
  fm_envelope = pure (self, index) => NO_ENVELOPES[0]
  envelopes = pure (self) => 0
  envelope_step = pure (self, index) => NO_STEPS[index]
  sample = pure (self, index) => Sample {}
}

const TUNE: Tune = Tune { first_order: 0, orders: 1, rows: 8, speed: 6, extended: true }
`

// FM 3 extended: two of its operators each sound their own note — A4 and E5 together, on
// one channel — through std.game.sound, and through std.genesis.sound in Sheliak.
func TestRun_GameMusicExtendedChannel3(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	check := func(runtime string, samples []float64, rate int) {
		t.Helper()
		frame := rate / 60
		power := func(f float64) float64 {
			var re, im float64
			window := samples[2*frame : 30*frame]
			for i, v := range window {
				re += v * math.Cos(2*math.Pi*f*float64(i)/float64(rate))
				im += v * math.Sin(2*math.Pi*f*float64(i)/float64(rate))
			}
			return re*re + im*im
		}
		a4 := 7670453.0 / 144 * 1083 * 16 / (1 << 21)
		e5 := 7670453.0 / 144 * 811 * 32 / (1 << 21)
		// Each a real share of what is heard — silence has nothing beside either — and well
		// clear of its neighbourhood.
		window := samples[2*frame : 30*frame]
		energy := 0.0
		for _, v := range window {
			energy += v * v
		}
		for _, f := range []float64{a4, e5} {
			if at, off := power(f), power(f*1.03); at < 100*off || at < 0.05*float64(len(window))*energy {
				t.Errorf("%s: FM 3's %.1f Hz is not heard: %.3g there, %.3g beside it, %.3g in all", runtime, f, at, off, energy)
			}
		}
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "song.lyra")
	program := "import std.game.sound\n" + extendedTables + `
let main = () -> u8 => {
  TUNE.play()
  let tables = Tables
  for _ in 0..<40 { sound.update_music(STEPS, PATCHES, tables, SAMPLE_BYTES) }
  0
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(dir, "song.wav")
	if _, stderr, code := captureRun(t, "run", src, "--", "--frames", "40", "--wav", wav); code != 0 {
		t.Fatalf("run exited %d\nstderr: %s", code, stderr)
	}
	check("modern", wavSamples(t, wav), 48000)

	genesisToolchainOrSkip(t)
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Log("SHELIAK is not set; the Genesis's extended FM 3 not heard")
		return
	}
	gdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gdir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gsrc := filepath.Join(gdir, "song.lyra")
	gprogram := "import std.genesis.vdp\nimport std.genesis.sound\n" + extendedTables + `
let tables = Tables

@interrupt(vblank)
let on_vblank = () -> void => sound.update_music(STEPS, PATCHES, tables, SAMPLE_BYTES)

let main = () -> void => {
  vdp.init()
  sound.init()
  TUNE.play()
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {}
}
`
	if err := os.WriteFile(gsrc, []byte(gprogram), 0o644); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(gdir, "song.bin")
	if _, stderr, code := captureRun(t, "build", "-o", rom, gsrc); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	gwav := filepath.Join(gdir, "song.wav")
	cmd := command(t, emulator, rom, "--frames", "60", "--wav", gwav)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	gsamples, rate := wavSamplesRate(t, gwav)
	onset := 0
	for i, v := range gsamples {
		if math.Abs(v) > 0.05 {
			onset = i
			break
		}
	}
	check("Genesis", gsamples[onset:], rate)
}
