package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// std.genesis.art's choose_variant: the pre-rendered angle and size to draw, and the flips —
// of 8 angles only the first 4 drawn, a half turn more the same flipped both ways, a mirrored
// one turned the other way and flipped across; the nearest angle and size.
func TestRun_GenesisChoosesAVariant(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.art.{ choose_variant }

const SIZES: [2]i16 = #[50, 200]
const NONE: [0]i16 = #[]

let say = (angle: i16, size: i16, mirrored: bool, turns: u16) -> void => {
  let (turn, s, fx, fy) = choose_variant(angle, size, mirrored, turns, SIZES)
  println("${angle} ${size} ${mirrored} ${turns}: ${turn} ${s} ${fx} ${fy}")
}

let main = () -> u8 => {
  say(0, 100, false, 8)
  say(45, 100, false, 8)
  say(50, 100, false, 8)
  say(180, 100, false, 8)
  say(225, 100, false, 8)
  say(-45, 100, false, 8)
  say(45, 100, true, 8)
  say(315, 100, true, 8)
  say(90, 60, false, 3)
  say(240, 180, false, 3)
  say(30, 100, false, 0)
  let (_, s, _, _) = choose_variant(0, 400, false, 0, NONE)
  println("none ${s}")
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := strings.Join([]string{
		"0 100 false 8: 0 0 false false",
		"45 100 false 8: 1 0 false false",
		"50 100 false 8: 1 0 false false",
		"180 100 false 8: 0 0 true true",
		"225 100 false 8: 1 0 true true",
		"-45 100 false 8: 3 0 true true",
		"45 100 true 8: 3 0 false true",
		"315 100 true 8: 1 0 true false",
		"90 60 false 3: 1 1 false false",
		"240 180 false 3: 2 2 false false",
		"30 100 false 0: 0 0 false false",
		"none 0",
	}, "\n") + "\n"
	if stdout != want {
		t.Errorf("got\n%swant\n%s", stdout, want)
	}
}
