package checker

import (
	"sort"
	"strings"
	"testing"
)

// The other half of the builtinEffects invariant, from inside the package where the table
// is visible.
//
// pkg/driver's TestBuiltinEffects_EveryKeyNamesACallableBuiltin proves each name here is a
// builtin a program can call — it needs the whole front end to do that, and this package
// cannot import the driver. What it cannot do is notice a *new* key, since the table is
// unexported and its list over there is maintained by hand.
//
// So this test pins the key set. Adding an entry fails here with the name in the message,
// which is the prompt to add it to the driver test too. That pairing is the point: one test
// says "these are all the keys" and the other says "every one of them is real", and a
// phantom has to get past both.
//
// Nine phantoms are what motivated it (see effects.go). The worst named `Arena.new` and
// `Arena.alloc` — a spelling the language refuses outright, lyra-E035 — and they kept an
// arena purity test green over a program with three compile errors.
func TestBuiltinEffects_KeySetIsPinned(t *testing.T) {
	want := []string{
		"panic",
		"print",
		"println",
		"random_seed",
		"read_key",
		"read_line",
		"set_raw_mode",
		"terminal_size",
		"wait_for_key_ms",
		"wall_clock_nanos",
	}
	got := make([]string, 0, len(builtinEffects))
	for k := range builtinEffects {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("builtinEffects keys changed.\n got: %v\nwant: %v\n\n"+
			"Adding a key? Confirm it names a builtin a program can actually call, add it to "+
			"pkg/driver's TestBuiltinEffects_EveryKeyNamesACallableBuiltin with the call shape "+
			"it takes, then update this list. Removing one is the same edit in reverse.", got, want)
	}
}
