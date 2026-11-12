package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `bindings.audio`: each format's fixture (testdata/audio, half a second of 440 Hz on the
// left and 660 Hz on the right at 44.1 kHz, made by ffmpeg) decoded — its channels and rate,
// about its length (an MP3 pads and a Vorbis stream trims a little), each channel's pitch by
// its rising zero crossings — whole, and in pieces after a seek back to the start, which
// must give the same samples. Needs `build/lib/liblyra-audio.a` (`./build.sh`).
func TestRun_AudioDecodesEachFormat(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if !archiveRunsHere(filepath.Join(root, "build", "lib", "liblyra-audio.a")) {
		t.Skip("no build/lib/liblyra-audio.a built for this platform: run ./build.sh")
	}
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	for _, name := range []string{"tone.wav", "tone.mp3", "tone.ogg", "tone.flac"} {
		data, err := os.ReadFile(filepath.Join(root, "cmd", "lyrac", "testdata", "audio", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.io.{ read_bytes }
import bindings.audio.{ open_decoder, close_decoder, decode_all }

let main = () -> u8 => {
  for name in ["tone.wav", "tone.mp3", "tone.ogg", "tone.flac"] {
    let path = "${program_args()[1]}/${name}"
    let Some(bytes) = read_bytes(path) else { return 1 }
    let Some(whole) = decode_all(bytes) else {
      println("${name}: not decoded")
      return 2
    }
    let frames = whole.samples.len() / whole.channels
    let left = rises(whole.samples, 0, 2) * whole.rate / frames
    let right = rises(whole.samples, 1, 2) * whole.rate / frames
    let Some(d) = open_decoder(bytes) else { return 3 }
    let first = d.read(1000)
    let _ = d.read(5000)
    let back = d.seek(0)
    let again = d.read(1000)
    close_decoder(d)
    println("${name} ${whole.channels} ${whole.rate} ${frames} ${left} ${right} ${back && first == again}")
  }
  if open_decoder([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]).is_some() { return 4 }
  0
}

/// How often channel ch of interleaved samples rises through zero.
let rises = pure (samples: []f32, ch: i64, channels: i64) -> i64 => {
  var n = 0
  var k = ch + channels
  while k < samples.len() {
    if samples[k - channels] < 0.0 && samples[k] >= 0.0 { n += 1 }
    k += channels
  }
  n
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src, "--", dir)
	if code != 0 {
		t.Fatalf("exit %d; stdout: %s; stderr: %s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("want a line a format, got %q", stdout)
	}
	for _, line := range lines {
		var name, same string
		var channels, rate, frames, left, right int
		if _, err := fmt.Sscan(line, &name, &channels, &rate, &frames, &left, &right, &same); err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		if channels != 2 || rate != 44100 || frames < 21000 || frames > 24000 ||
			left < 430 || left > 450 || right < 645 || right > 675 || same != "true" {
			t.Errorf("%s: %d channels at %d, %d frames, %d Hz left and %d right, the same after a seek: %s (want 2 at 44100, about 22050, 440 and 660, true)",
				name, channels, rate, frames, left, right, same)
		}
	}
}
