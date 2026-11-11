package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// genesisHeard builds the Genesis program src (in a directory of its own, `files` written
// beside it) and plays it `frames` frames in Sheliak: its sound's first channel from where
// it starts, and the rate — or skips without the toolchain or SHELIAK.
func genesisHeard(t *testing.T, src string, files map[string][]byte, frames string) ([]float64, int) {
	t.Helper()
	genesisToolchainOrSkip(t)
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Skip("SHELIAK is not set")
	}
	dir := t.TempDir()
	files["lyra.toml"] = []byte("target = \"genesis\"\n")
	files["main.lyra"] = []byte(src)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rom := filepath.Join(dir, "game.bin")
	if _, stderr, code := captureRun(t, "build", "-o", rom, filepath.Join(dir, "main.lyra")); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	wav := filepath.Join(dir, "game.wav")
	cmd := command(t, emulator, rom, "--frames", frames, "--wav", wav)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	samples, rate := wavSamplesRate(t, wav)
	for i, v := range samples {
		if math.Abs(v) > 0.05 {
			return samples[i:], rate
		}
	}
	t.Fatal("nothing heard")
	return nil, 0
}

// tonePower is samples' power at f Hz between seconds from and to — a short window: the
// driver's real rate is a little off its nominal one, so over a second a tone's phase drifts
// from f's and the sum cancels.
func tonePower(samples []float64, rate int, from, to, f float64) float64 {
	a, b := int(from*float64(rate)), min(int(to*float64(rate)), len(samples))
	var re, im float64
	for i := a; i < b; i++ {
		x := 2 * math.Pi * f * float64(i) / float64(rate)
		re += samples[i] * math.Cos(x)
		im += samples[i] * math.Sin(x)
	}
	return math.Sqrt(re*re+im*im) / float64(max(b-a, 1))
}

// squarePCM is n DAC bytes of a square wave a period `period` bytes long.
func squarePCM(n, period int) []byte {
	out := make([]byte, n)
	for i := range out {
		if i%period < period/2 {
			out[i] = 0xE0
		} else {
			out[i] = 0x20
		}
	}
	return out
}

const genesisTrackHeader = `import std.genesis.vdp
import std.genesis.sound
import std.genesis.track
import std.genesis.track.{ Track }
import std.chips.effects.{ Effect, EffectStep }
import std.chips.ym2612.{ FmPatch }
import std.chips.music.{ Sample }

const STEPS: [1]EffectStep = #[EffectStep { period: 0, volume: 0 }]
const PATCHES: [1]FmPatch = #[FmPatch { ops: #[0; 28], feedback_algorithm: 0, sensitivity: 0 }]
`

// A track longer than the driver's 16-bit count and a 64 KB sample — 100,000 bytes of a
// 500 Hz square at 11,000 bytes a second, 9.1 s, embedded — is heard past 6 s (65,535 of
// the driver's outputs, and 65,535 bytes) to near its end, and is silent after it.
func TestRun_GenesisTrackPlaysPast64KB(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	src := genesisTrackHeader + `
const TRACK_PCM: [100000]u8 = embed("track.pcm")
const SONG: Track = Track { first: 0, length: 100000, rate: 11000 }

@interrupt(vblank)
let on_vblank = () -> void => {
  track.update()
  sound.update(STEPS, PATCHES)
}

let main = () -> void => {
  vdp.init()
  sound.init()
  sound.use_track_bytes(TRACK_PCM)
  SONG.play()
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {}
}
`
	samples, rate := genesisHeard(t, src, map[string][]byte{"track.pcm": squarePCM(100000, 22)}, "620")
	early := tonePower(samples, rate, 1, 1.2, 500)
	late := min(tonePower(samples, rate, 6.5, 6.7, 500), tonePower(samples, rate, 8.6, 8.8, 500))
	after := tonePower(samples, rate, 9.5, 9.7, 500)
	if early < 0.02 || late < early*0.5 || after > early/20 {
		t.Errorf("500 Hz at %.4f in its second second, %.4f past 6 s, %.4f after its end; want it heard to its end, then gone", early, late, after)
	}
}

// A looping music track comes back after a sampled effect has had the DAC, where it has got
// to: 500 Hz before, the effect's 1 kHz alone while it plays (18 frames from frame 60), the
// track's 500 Hz again after.
func TestRun_GenesisTrackComesBackAfterAnEffect(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	src := genesisTrackHeader + `
const TRACK_PCM: [11000]u8 = embed("track.pcm")
const SONG: Track = Track { first: 0, length: 11000, rate: 11000, loop_from: 0 }
const PCM: [2400]u8 = embed("beep.pcm")
const SOUND_SAMPLES: [1]Sample = #[Sample { first: 0, length: 2400, rate: 8000 }]
const BEEP: Effect = Effect { first: 0, count: 18, sample: 0 }

var frames: u16 = 0

@interrupt(vblank)
let on_vblank = () -> void => {
  frames += 1
  if frames == 60 { BEEP.play() }
  track.update()
  sound.update_sampled(STEPS, PATCHES, PCM, SOUND_SAMPLES)
}

let main = () -> void => {
  vdp.init()
  sound.init()
  sound.use_track_bytes(TRACK_PCM)
  SONG.play()
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  loop {}
}
`
	samples, rate := genesisHeard(t, src, map[string][]byte{
		"track.pcm": squarePCM(11000, 22),
		"beep.pcm":  squarePCM(2400, 8),
	}, "200")
	// The onset is the track's start, a frame or so after frame 0.
	before := tonePower(samples, rate, 0.5, 0.7, 500)
	during500, during1k := tonePower(samples, rate, 1.05, 1.25, 500), tonePower(samples, rate, 1.05, 1.25, 1000)
	after := tonePower(samples, rate, 1.6, 1.8, 500)
	if before < 0.02 || during1k < 0.02 || during500 > during1k/5 || after < before*0.5 {
		t.Errorf("500 Hz at %.4f before the effect; during it 1 kHz at %.4f and 500 Hz at %.4f; 500 Hz at %.4f after — want the track, the effect alone, the track again", before, during1k, during500, after)
	}
}
