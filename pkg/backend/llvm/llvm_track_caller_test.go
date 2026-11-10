package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// A trap inside a `@track_caller` function names its caller's place, so `m.unwrap()` on a
// `None` names the program's line rather than the prelude's `panic` (10/14 left it open).
// The place travels as a hidden parameter: through a chain of such functions (`unwrap` calls
// `expect`), through a generic's specializations, and into a thunk when the function is made a
// value, which names where it was made one.

func TestExec_ATrackCallerTrapNamesTheCall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, want string
	}{
		// unwrap is generic and reaches its panic through expect, another std function.
		{"Maybe.unwrap", `let at = (n: i64) -> i64 => {
  let m: Maybe<i64> = if n > 0 { None } else { Some(n) }
  m.unwrap()
}`, "lyra: panic: unwrap on a None at %s:4:3"},
		{"Maybe.expect", `let at = (n: i64) -> i64 => {
  let m: Maybe<i64> = if n > 0 { None } else { Some(n) }
  1 + m.expect("a small n")
}`, "lyra: panic: a small n at %s:4:7"},
		{"Result.unwrap", `let at = (n: i64) -> i64 => {
  let r: Result<i64, string> = if n > 0 { Err("big") } else { Ok(n) }
  r.unwrap()
}`, "lyra: panic: unwrap on an Err at %s:4:3"},
		// The program's own: a panic, an index and an overflow in the body all name the call.
		{"own panic", `@track_caller
let check = (n: i64) -> i64 => {
  if n > 0 { panic("n is ${n}") }
  n
}
let at = (n: i64) -> i64 => check(n)`, "lyra: panic: n is 5 at %s:7:29"},
		{"own index, generic", `@track_caller
let second<t> = (xs: []t) -> t => xs[1]
let at = (n: i64) -> i64 => {
  let words = ["one"]
  let w = second(words)
  second([n]) + w.len()
}`, "lyra: array index out of bounds (index 1, length 1) at %s:6:11"},
		{"own overflow", `@track_caller
let double = (n: i64) -> i64 => n * 2
let at = (n: i64) -> i64 => {
  let big = 9223372036854775807 - 10 + n
  double(big)
}`, "lyra: arithmetic overflow at %s:6:3"},
		// A tracking function passes on the place it was given; one that does not names its
		// own call.
		{"through the program's own", `@track_caller
let first = (m: Maybe<i64>) -> i64 => m.unwrap()
let at = (n: i64) -> i64 => {
  let m: Maybe<i64> = if n > 0 { None } else { Some(n) }
  first(m)
}`, "lyra: panic: unwrap on a None at %s:6:3"},
		{"stopped by an untracked one", `let first = (m: Maybe<i64>) -> i64 => m.unwrap()
let at = (n: i64) -> i64 => {
  let m: Maybe<i64> = if n > 0 { None } else { Some(n) }
  first(m)
}`, "lyra: panic: unwrap on a None at %s:2:39"},
		// A value names where it was made one; the call through it has no place to give.
		{"made a value", `@track_caller
let check = (n: i64) -> i64 => if n > 0 { panic("n is ${n}") } else { n }
let apply = (f: (i64) -> i64, n: i64) -> i64 => f(n)
let at = (n: i64) -> i64 => {
  let g = check
  apply(check, 0) + apply(g, n)
}`, "lyra: panic: n is 5 at %s:6:11"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "module main\n" + c.src + "\nlet main = () -> void => println(\"${at(5)}\")\n"
			stderr, file := trapOutput(t, src)
			if want := strings.Replace(c.want, "%s", file, 1) + "\n"; stderr != want {
				t.Errorf("stderr = %q\nwant     %q", stderr, want)
			}
		})
	}
}

// The standard library's checks of what its caller passed name the caller: a generic one, one
// bounded by a trait, an impl's method, and one reaching its check through a private function.
func TestExec_AStdTrapNamesItsCaller(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, imports, body, want string
	}{
		{"take", "", `  let xs = [1, 2, 3]
  xs.take(n - 6).len()`, "lyra: panic: take: count must not be negative at %s:5:3"},
		{"split", "", `  let parts = "a b".split(if n > 0 { "" } else { " " })
  parts.len()`, "lyra: panic: split: empty separator; to split a string into its runes, use to_runes() at %s:4:15"},
		{"clamp", "", `  n.clamp(9, n)`, "lyra: panic: clamp: lo is greater than hi at %s:4:3"},
		{"f64 min", "", `  let nan = 0.0 / f64(n - 5)
  nan.min(1.0).floor()`, "lyra: panic: min: NaN has no order at %s:5:3"},
		{"Rng.between", "", `  var rng = Rng.seeded(42)
  rng.between(n, 1)`, "lyra: panic: Rng.between: the high bound is below the low one at %s:5:3"},
		{"box_top, through its helper", "import std.tui.{box_top}", `  box_top(n - 4).len()`,
			"lyra: panic: std.tui: a box needs at least 2 columns for its borders at %s:4:3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// Line 2 is the import or blank, so the body starts on line 4 in every case.
			src := "module main\n" + c.imports + "\nlet at = (n: i64) -> i64 => {\n" + c.body + "\n}\n" +
				"let main = () -> void => println(\"${at(5)}\")\n"
			stderr, file := trapOutput(t, src)
			if want := strings.Replace(c.want, "%s", file, 1) + "\n"; stderr != want {
				t.Errorf("stderr = %q\nwant     %q", stderr, want)
			}
		})
	}
}

// A function without the mark still names its own line, std's included.
func TestExec_AnUntrackedTrapNamesItsOwnLine(t *testing.T) {
	t.Parallel()
	src := `module main
let check = (n: i64) -> i64 => {
  if n > 0 { panic("n is ${n}") }
  n
}
let main = () -> void => println("${check(5)}")
`
	stderr, file := trapOutput(t, src)
	if want := "lyra: panic: n is 5 at " + file + ":3:14\n"; stderr != want {
		t.Errorf("stderr = %q\nwant     %q", stderr, want)
	}
}

// The place is one pointer to a constant record, and a chain passes its own along rather than
// naming a place of its own.
func TestEmit_ATrackCallerFunctionTakesOnePlace(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `@track_caller
let inner = (n: i64) -> i64 => if n > 0 { panic("big") } else { n }
@track_caller
let outer = (n: i64) -> i64 => inner(n)
let main = () -> u8 => u8(outer(0) + outer(0))
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, want := range []string{
		`define i64 @lyra\.inner\(i64 %p\.n, \{ i8\*, i64 \}\* %caller\.place\)`,
		`call i64 @lyra\.inner\(i64 %\d+, \{ i8\*, i64 \}\* %caller\.place\)`,
	} {
		if !regexp.MustCompile(want).MatchString(ir) {
			t.Errorf("the IR has no %s:\n%s", want, ir)
		}
	}
	// Two calls of outer, two places; inner's call passes outer's.
	if n := strings.Count(ir, "= private constant { i8*, i64 }"); n != 2 {
		t.Errorf("want two place records, got %d", n)
	}
}
