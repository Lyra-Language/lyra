package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestRun_GameFadesTheScreen is TestGenesis_PaletteCyclesAndFades's fades on std.game: a red
// screen faded to white over four updates, and faded to white and back, then shown — white,
// then red again — the colour drawn over the frame (`screen.set_fade`), as the Genesis fades
// its colours — and the same a step an update (`start_fade_to`, `start_fade_back`, `step_fade`),
// as a cutscene fades while its game goes on, halfway after half its steps.
func TestRun_GameFadesTheScreen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fades string
		want  [3]byte
		what  string
	}{
		{"palette.fade_to(0xFFFFFF, 4)", [3]byte{255, 255, 255}, "faded to white"},
		{"palette.fade_to(0xFFFFFF, 4)\n  palette.fade_back(4)", [3]byte{255, 0, 0}, "faded to white and back"},
		{"palette.start_fade_to(0xFFFFFF, 4)\n  if stepped() != 4 { return 3 }", [3]byte{255, 255, 255}, "faded to white a step an update"},
		{"palette.start_fade_to(0xFFFFFF, 4)\n  if stepped() != 4 { return 3 }\n  palette.start_fade_back(4)\n  if stepped() != 4 { return 4 }", [3]byte{255, 0, 0}, "faded to white and back a step an update"},
	} {
		src := filepath.Join(t.TempDir(), "main.lyra")
		if err := os.WriteFile(src, []byte(fmt.Sprintf(`import std.game.screen
import std.game.palette
import std.game.sprites

let main = () -> u8 => {
  if !screen.open("fade", 64, 64) { return 2 }
  screen.set_backdrop(255, 0, 0)
  %s
  while screen.running() {
    sprites.clear()
    screen.show()
  }
  screen.close()
  0
}

/// The fade begun stepped an update at a time, as a cutscene steps it: the steps it took,
/// halfway faded after two of four, and over at once after.
let stepped = () -> i64 => {
  var steps = 0
  let from = screen.faded()
  loop {
    steps += 1
    let over = palette.step_fade()
    if steps == 2 {
      let half = screen.faded() - from
      if half.abs() < 0.45 || half.abs() > 0.55 { return -1 }
    }
    if over { break }
  }
  if !palette.step_fade() { return -2 }
  steps
}
`, c.fades)), 0o644); err != nil {
			t.Fatal(err)
		}
		frame := playGame(t, src, "--frames", "20")
		if got := frame.at(10, 10); got != c.want {
			t.Errorf("%s: the screen is %v, want %v", c.what, got, c.want)
		}
	}
}
