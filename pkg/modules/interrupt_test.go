package modules_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// `@interrupt(vblank)` / `@interrupt(hblank)` marks the function a console runs on that
// interrupt (09/30). What it may be is checked (lyra-E087), with a console target, since a
// handler on the host is itself a mistake.
func TestInterrupt_Rules(t *testing.T) {
	for _, c := range []struct{ name, config, src, want string }{
		{"on the host", "", "@interrupt(vblank)\nlet tick = () -> void => {}\nlet main = () -> void => {}\n",
			"this program is built for the host"},
		{"with a parameter", genesis, "@interrupt(vblank)\nlet tick = (n: i16) -> void => {}\nlet main = () -> void => {}\n",
			"takes nothing and returns nothing"},
		{"returning a value", genesis, "@interrupt(hblank)\nlet tick = () -> i16 => 1\nlet main = () -> void => {}\n",
			"takes nothing and returns nothing"},
		{"two of a kind", genesis, "@interrupt(vblank)\nlet a = () -> void => {}\n@interrupt(vblank)\nlet b = () -> void => {}\nlet main = () -> void => {}\n",
			"a second `@interrupt(vblank)` handler; \"a\" is already the one"},
		{"an unknown interrupt", genesis, "@interrupt(timer)\nlet a = () -> void => {}\nlet main = () -> void => {}\n",
			"`@interrupt` takes the interrupt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeGame(t, c.config, c.src)
			found := false
			for _, d := range res.Errors() {
				if strings.Contains(d.Message, c.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected an error containing %q; got %v", c.want, res.Diagnostics)
			}
		})
	}
	// One of each kind, well formed, on the Genesis: nothing to report.
	res := analyzeGame(t, genesis, "@interrupt(vblank)\nlet a = () -> void => {}\n@interrupt(hblank)\nlet b = () -> void => {}\nlet main = () -> void => {}\n")
	for _, d := range res.Diagnostics {
		if d.Code == diagnostic.CodeInterruptHandler {
			t.Errorf("a well-formed handler was refused: %v", d)
		}
	}
}
