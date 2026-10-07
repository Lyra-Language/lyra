package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// **A call argument written `name: value` is refused, not dropped** (lyra-E088). The grammar
// parses named arguments and a bare `_`, the language has neither, and until 10/07 the
// collector dropped both from the argument list without a word: the program below compiled
// and printed `Hello, Ada` twice, the default standing in for `"Hi"`.
//
// A CLI test because the program's whole failure was that it *ran*: `println` and string
// interpolation are the prelude's, so this is the program as a user writes it, checked by
// the command a user runs.
func TestCallArgument_NamedIsRefusedAtTheArgument(t *testing.T) {
	t.Setenv("LYRA_STD", repoRoot(t))

	src := writeCallArgumentSource(t, `let greet = pure (name: string, greeting: string = "Hello") -> string => "${greeting}, ${name}"
let main = () -> void => {
  println(greet("Ada", greeting: "Hi"))
  println(greet("Ada", nonsense: 5))
}
`)
	_, stderr, code := captureRun(t, "check", src)
	if code == 0 {
		t.Fatalf("a named argument compiled; stderr:\n%s", stderr)
	}
	for _, want := range []string{
		"app.lyra:3:24: error [lyra-E088]: named argument `greeting:`",
		"app.lyra:4:24: error [lyra-E088]: named argument `nonsense:`",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in stderr:\n%s", want, stderr)
		}
	}
	// The value stays in the argument list as the positional argument it stands in for, so
	// the mistake is one diagnostic: no arity error for a dropped argument, and nothing
	// more on line 3, whose value fits its position. (`nonsense: 5` also earns line 4's
	// type error — 5 is not a string — which is the value's own mistake.)
	if n := strings.Count(stderr, "app.lyra:3:"); n != 1 {
		t.Errorf("line 3 reported %d diagnostics; want only the named argument:\n%s", n, stderr)
	}
	if strings.Contains(stderr, "argument(s), got") {
		t.Errorf("an arity error means the argument was dropped:\n%s", stderr)
	}
}

// A bare `_` (the partial application the grammar still parses) was dropped the same way,
// and reported only as a wrong argument count. It is refused at the `_`, alone.
func TestCallArgument_WildcardIsRefusedAtTheArgument(t *testing.T) {
	t.Setenv("LYRA_STD", repoRoot(t))

	src := writeCallArgumentSource(t, `let add = pure (a: i64, b: i64) -> i64 => a + b
let main = () -> void => {
  let add_one = add(1, _)
  println(add_one(2))
}
`)
	_, stderr, code := captureRun(t, "check", src)
	if code == 0 {
		t.Fatalf("a `_` argument compiled; stderr:\n%s", stderr)
	}
	if want := "app.lyra:3:24: error [lyra-E088]: `_` is not a call argument"; !strings.Contains(stderr, want) {
		t.Errorf("missing %q in stderr:\n%s", want, stderr)
	}
	if strings.Contains(stderr, "argument(s), got") {
		t.Errorf("an arity error means the argument was dropped:\n%s", stderr)
	}
}

// **The advice is taken and has to run**: the value alone, in its parameter's place.
func TestCallArgument_PositionalAdviceRuns(t *testing.T) {
	requireCC(t)
	t.Setenv("LYRA_STD", repoRoot(t))

	src := writeCallArgumentSource(t, `let greet = pure (name: string, greeting: string = "Hello") -> string => "${greeting}, ${name}"
let main = () -> void => {
  println(greet("Ada", "Hi"))
}
`)
	bin := filepath.Join(filepath.Dir(src), "app")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("the advice does not compile: exited %d\nstderr: %s", code, stderr)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running failed: %v\n%s", err, out)
	}
	if got, want := string(out), "Hi, Ada\n"; got != want {
		t.Errorf("output %q; want %q", got, want)
	}
}

func writeCallArgumentSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.lyra")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
