package llvm

import (
	"strings"
	"testing"
)

// `join` — `split`'s inverse (`std/prelude/strings.lyra`, 08/14).
//
// Generic over the element rather than taking `[]string`, so a list of anything printable
// joins without being converted first. A `[]string` pays nothing for that, since
// `impl Show for string` returns the value rather than interpolating it.
func TestExec_Join(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, expr, want string }{
		{"with a separator", `parts.join(", ")`, "a, b, c"},
		{"with none", `parts.join()`, "abc"},
		// `sep` goes *between* parts, never around them — so one element uses none.
		{"a single element", `one.join(", ")`, "solo"},
		{"an empty array", `none.join(", ")`, ""},
		// The Show bound is what makes this work on something that is not a string.
		{"a list of integers", `nums.join(" - ")`, "1 - 2 - 3"},
		{"roundtrips with split", `"a::b::c".split("::").join("::")`, "a::b::c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let parts: []string = ["a", "b", "c"];
  let one: []string = ["solo"];
  let none: []string = [];
  let nums: []i64 = [1, 2, 3];
  println("[" ++ ` + c.expr + ` ++ "]");
}
`
			want := "[" + c.want + "]"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
				t.Errorf("%s = %q; want %q", c.expr, got, want)
			}
		})
	}
}

// The frame-buffer shape it was added for: rows accumulated with `push`, then joined once.
func TestExec_JoinBuildsAFrame(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var rows: []string = []
  for r in 0..<3 { rows.push("row${r}") }
  println(rows.join("\n"));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "row0\nrow1\nrow2" {
		t.Errorf("got %q; want three rows", got)
	}
}

// A literal receiver reaching a `[]t` combinator, against the **real** prelude — which is
// what a reader actually meets: `join` is declared once and `map` is overloaded on three
// receivers, and both must work, since a literal is built in the shape its receiver asks
// for. This asserted the *refusal* and its hint until 08/28.
//
// Checked here rather than in the typechecker's own tests because those run without a
// prelude — `join` and `map` are not declared there, so an earlier draft of this passed
// vacuously against a bare "member access on non-struct type".
func TestExec_ArrayLiteralReceiverReachesAPreludeCombinator(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ src, want string }{
		"join, declared once": {
			`let main = () -> void => { println(["a", "b"].join("-")); }`,
			"a-b",
		},
		"map, overloaded on three receivers": {
			`let main = () -> void => { println("${[1, 2].map((x: i64) -> i64 => x * 10).len()}"); }`,
			"2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := checkWithPrelude(t, tc.src+"\n"); len(errs) != 0 {
				t.Fatalf("expected a clean program; got: %s", strings.Join(errs, "\n"))
			}
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, tc.src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// The hint still exists and still names the edit — for a fixed-array **binding**, where
// the value already exists as a stack `[N]T` and reaching a `[]t` combinator would widen
// it. That is the case the rule is actually about.
func TestCheck_ArrayBindingReceiverNamesTheEditAgainstThePrelude(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  let parts = ["a", "b"]
  println(parts.join(""))
}`
	errs := strings.Join(checkWithPrelude(t, src+"\n"), "\n")
	if !strings.Contains(errs, "join takes a dynamic array") {
		t.Errorf("want the array hint; got: %s", errs)
	}
	if !strings.Contains(errs, "annotate the value as") {
		t.Errorf("the hint should name the annotation; got: %s", errs)
	}
}

// The repeat form reaching a prelude combinator, end to end. It needed both halves: the
// typechecker's literal allowance (which covered both array forms from the start) and a
// grammar change making `array_repeat_init` a postfix head, without which this was a
// parse error while `["x", "x", "x"].join("-")` compiled.
func TestExec_ArrayRepeatReceiverReachesAPreludeCombinator(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => { println(["x"; 3].join("-")); }`
	if errs := checkWithPrelude(t, src+"\n"); len(errs) != 0 {
		t.Fatalf("expected a clean program; got: %s", strings.Join(errs, "\n"))
	}
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "x-x-x" {
		t.Errorf("got %q; want %q", got, "x-x-x")
	}
}
