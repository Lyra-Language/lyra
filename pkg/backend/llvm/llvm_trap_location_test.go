package llvm

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A trap says where it was. An index out of bounds printed `lyra: array index out of
// bounds` and nothing else, and Vega's crash deleting the only entity (10/14) took a
// scripted reproduction to place. Now every trap ends " at file:line:col", naming the
// innermost expression being lowered, and the index trap names the index and length.

// trapOutput builds src against the real prelude, runs it, and returns what it wrote
// to stderr and where its source was written. The program must trap.
func trapOutput(t *testing.T, src string) (stderr, file string) {
	t.Helper()
	out, err := command(preludeBinary(t, src)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the program must trap; it exited cleanly with %q", out)
	}
	if ee.ExitCode() != trapExitCode {
		t.Errorf("exited %d; want %d (a trap)", ee.ExitCode(), trapExitCode)
	}
	return string(out), filepath.Join(sourceDir(t, src), "app.lyra")
}

// Every way to index an array: a fixed one and a dynamic one, read and assigned,
// counted from the end, and a program argument. Each names its index, the length it
// missed, and the indexing expression — including a negative index and the most
// negative one, which the digits are taken from as an unsigned magnitude.
func TestExec_AnIndexTrapSaysTheIndexTheLengthAndWhere(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, want string
	}{
		{"fixed read", `  let xs: [3]i64 = #[1, 2, 3]
  println("${xs[n + 2]}")`, "(index 7, length 3) at %s:4:14"},
		{"dynamic read", `  let xs: []i64 = [1, 2, 3, 4]
  println("${xs[n]}")`, "(index 5, length 4) at %s:4:14"},
		{"fixed assignment", `  var xs: [3]i64 = #[1, 2, 3]
  xs[0 - n] = 9`, "(index -5, length 3) at %s:4:3"},
		{"dynamic assignment", `  var xs: []i64 = [1, 2]
  xs[n * 2] = 9`, "(index 10, length 2) at %s:4:3"},
		{"fixed from the end", `  let xs: [3]i64 = #[1, 2, 3]
  println("${xs.from_end(n)}")`, "(index -2, length 3) at %s:4:14"},
		{"dynamic from the end", `  let xs: []i64 = [1, 2, 3]
  println("${xs.from_end(n - 5)}")`, "(index 3, length 3) at %s:4:14"},
		{"the most negative index", `  let xs: []i64 = [1]
  println("${xs[n - 5 - 9223372036854775807 - 1]}")`,
			"(index -9223372036854775808, length 1) at %s:4:14"},
		{"a program argument", `  println(program_arg(n))`, "(index 5, length 1) at %s:3:11"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// n is 5, from a parameter, so no range fact removes the check.
			src := "let at = (n: i64) -> void => {\n" + c.body + "\n}\n" +
				"let main = () -> void => at(5)\n"
			src = "module main\n" + src
			stderr, file := trapOutput(t, src)
			want := "lyra: array index out of bounds " + strings.Replace(c.want, "%s", file, 1) + "\n"
			if stderr != want {
				t.Errorf("stderr = %q\nwant     %q", stderr, want)
			}
		})
	}
}

// The other traps say where too: the expression that trapped, not the statement or
// the function it is in, and a user's own `panic`.
func TestExec_EveryTrapSaysWhere(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, want string
	}{
		{"overflow", `  let big = 9223372036854775807
  let v = 1 + (big - n + 6) * 1
  println("${v}")`, "lyra: arithmetic overflow at %s:4:16"},
		{"divide by zero", `  println("${100 / (n - 5)}")`, "lyra: divide by zero at %s:3:14"},
		{"match", `  let v = match n {
    1 => 10,
    2 => 20,
  }
  println("${v}")`, "lyra: match not exhaustive at %s:3:11"},
		{"panic", `  if n > 0 { panic("n is ${n}") }`, "lyra: panic: n is 5 at %s:3:14"},
		{"string index", `  let s = "abc"
  println("${s[n]}")`, "lyra: string index out of bounds at %s:4:14"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "module main\nlet at = (n: i64) -> void => {\n" + c.body + "\n}\n" +
				"let main = () -> void => at(5)\n"
			stderr, file := trapOutput(t, src)
			if want := strings.Replace(c.want, "%s", file, 1) + "\n"; stderr != want {
				t.Errorf("stderr = %q\nwant     %q", stderr, want)
			}
		})
	}
}

// Each kind of trap is still one function however many sites call it: a site passes
// its place, and the message is written once.
func TestEmit_TrapSitesShareOneFunctionAndPassTheirPlace(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `let at = (xs: [3]i64, i: i64, j: i64) -> i64 => xs[i] + xs[j]
let main = () -> u8 => u8(at(#[1, 2, 3], 0, 1))
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if n := strings.Count(ir, "define void @lyra_panic_index_out_of_bounds("); n != 1 {
		t.Errorf("want one index trap function, got %d", n)
	}
	if n := strings.Count(ir, "call void @lyra_panic_index_out_of_bounds("); n != 2 {
		t.Errorf("want two calls to the index trap, got %d", n)
	}
	for _, place := range []string{" at 1:49", " at 1:57"} {
		if !strings.Contains(ir, place+`\00"`) {
			t.Errorf("the IR names no place ending %q", place)
		}
	}
}
