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

// The hint against the **real** prelude, which is what a reader actually meets: `join` is
// declared once, `map` is overloaded on three receivers, and both must name the same edit.
//
// Checked here rather than in the typechecker's own tests because those run without a
// prelude — `join` and `map` are not declared there, so the hint has nothing to look up and
// the first draft of this passed vacuously against a bare "member access on non-struct
// type".
func TestCheck_ArrayLiteralReceiverNamesTheEditAgainstThePrelude(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ src, want string }{
		"join, declared once": {
			`let main = () -> void => { println(["a", "b"].join("")); }`,
			"join takes a dynamic array",
		},
		"map, overloaded on three receivers": {
			`let main = () -> void => { println([1, 2].map((x: i64) -> i64 => x).len()); }`,
			"map takes a dynamic array",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := strings.Join(checkWithPrelude(t, tc.src+"\n"), "\n")
			if !strings.Contains(errs, tc.want) {
				t.Errorf("want %q; got: %s", tc.want, errs)
			}
			if !strings.Contains(errs, "annotate the value as") {
				t.Errorf("the hint should name the annotation; got: %s", errs)
			}
		})
	}
}
