package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
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
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
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
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
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
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
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
