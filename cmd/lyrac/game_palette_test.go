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
// its colours.
func TestRun_GameFadesTheScreen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		fades string
		want  [3]byte
		what  string
	}{
		{"palette.fade_to(0xFFFFFF, 4)", [3]byte{255, 255, 255}, "faded to white"},
		{"palette.fade_to(0xFFFFFF, 4)\n  palette.fade_back(4)", [3]byte{255, 0, 0}, "faded to white and back"},
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
`, c.fades)), 0o644); err != nil {
			t.Fatal(err)
		}
		frame := playGame(t, src, "--frames", "20")
		if got := frame.at(10, 10); got != c.want {
			t.Errorf("%s: the screen is %v, want %v", c.what, got, c.want)
		}
	}
}
