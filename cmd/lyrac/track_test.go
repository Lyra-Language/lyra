package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// std.game.track played headless: each compressed fixture (testdata/audio: 440 Hz left,
// 660 Hz right, half a second at 44.1 kHz) as the music, under nothing — heard in the WAV
// at its pitches on its own sides, resampled to 48 kHz; then, looping from its start, still
// sounding past its end, and once over, silent after.
func TestRun_GameTrackIsHeardInStereo(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "build", "lib", "liblyra-audio.a")); err != nil {
		t.Skip("no build/lib/liblyra-audio.a: run ./build.sh")
	}
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	for _, name := range []string{"tone.mp3", "tone.ogg", "tone.flac"} {
		for _, loops := range []bool{false, true} {
			fixture := filepath.Join(root, "cmd", "lyrac", "testdata", "audio", name)
			loop := "-1"
			if loops {
				loop = "0"
			}
			src := filepath.Join(dir, name+loop+".lyra")
			program := `import std.game.sound
import std.game.track
import std.game.track.{ Track }
import std.chips.effects.{ EffectStep }
import std.chips.ym2612.{ FmPatch }

const STEPS: [1]EffectStep = #[EffectStep { period: 0, volume: 0 }]
const NO_PATCHES: [0]FmPatch = #[]

let main = () -> u8 => {
  Track { file: "` + fixture + `", loop_from: ` + loop + ` }.play()
  for _ in 0..<60 {
    track.update()
    sound.update(STEPS, NO_PATCHES)
  }
  0
}
`
			if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
				t.Fatal(err)
			}
			wav := filepath.Join(dir, name+loop+".wav")
			if _, stderr, code := captureRun(t, "run", src, "--", "--frames", "60", "--wav", wav); code != 0 {
				t.Fatalf("%s: run exited %d\nstderr: %s", name, code, stderr)
			}
			data, err := os.ReadFile(wav)
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint16(data[22:]) != 2 {
				t.Fatalf("%s: not stereo", name)
			}
			left, right := stereo(data)
			// 0.1 s to 0.4 s: inside the tone either way.
			from, to := 4800, 19200
			l, r := risesPerSecond(left, from, to), risesPerSecond(right, from, to)
			if l < 430 || l > 450 || r < 645 || r > 675 {
				t.Errorf("%s: %v Hz on the left, %v on the right (want 440 and 660)", name, l, r)
			}
			// 0.7 s to 0.9 s: past its end.
			early, late := peak(left, from, to), peak(left, 33600, 43200)
			if loops && late < early*0.8 {
				t.Errorf("%s looping: silent past its end (peak %v)", name, late)
			}
			if !loops && late > early*0.02 {
				t.Errorf("%s once: still heard past its end (peak %v)", name, late)
			}
		}
	}
}

// stereo is a 16-bit stereo WAV's two channels, -1 to 1.
func stereo(data []byte) ([]float64, []float64) {
	var left, right []float64
	for i := 44; i+3 < len(data); i += 4 {
		left = append(left, float64(int16(binary.LittleEndian.Uint16(data[i:])))/32768)
		right = append(right, float64(int16(binary.LittleEndian.Uint16(data[i+2:])))/32768)
	}
	return left, right
}

// risesPerSecond is how often samples rise through zero between from and to, at 48 kHz.
func risesPerSecond(samples []float64, from, to int) float64 {
	n := 0
	for i := from; i < to && i < len(samples); i++ {
		if samples[i-1] < 0 && samples[i] >= 0 {
			n++
		}
	}
	return float64(n) * 48000 / float64(to-from)
}

func peak(samples []float64, from, to int) float64 {
	p := 0.0
	for i := from; i < to && i < len(samples); i++ {
		if v := samples[i]; v > p {
			p = v
		} else if -v > p {
			p = -v
		}
	}
	return p
}
