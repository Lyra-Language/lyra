package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// Console input (`read_line`, input.go) and the prelude's `parse_i64`, which are
// the two halves of getting a number from a user.
//
// These need a harness the rest of the package does not: `read_line` needs
// **stdin**, and `parse_i64` lives in the real `std/prelude.lyra` rather than in
// the test source, so it needs the resolver and the actual shipped standard
// library instead of driver.Analyze's single unit.

// repoStdRoot returns the directory *containing* `std/` in this working copy — the
// root shape modules.StdRoot describes — located from this test file's own path so
// it does not depend on the working directory or on ./build.sh having been run.
func repoStdRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	// .../lyra/pkg/backend/llvm/llvm_input_test.go → .../lyra
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "std", "prelude")); err != nil {
		t.Fatalf("std/prelude/ not found under %s: %v", root, err)
	}
	return root
}

// buildAndRunWithPrelude compiles src against the **real** prelude and runs it with
// stdin fed from input, returning stdout.
//
// Deliberately the shipped `std/prelude.lyra` rather than a copy pasted into the
// test: `parse_i64` is written in Lyra, so a copy would test a second
// implementation that can drift from the one users get — and the whole reason
// parsing lives in the prelude instead of the compiler is that it is ordinary,
// readable, replaceable Lyra. Testing the real file is what keeps that honest.
func buildAndRunWithPrelude(t *testing.T, src, input string) string {
	t.Helper()
	cmd := exec.Command(preludeBinary(t, src))
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("running the binary failed: %v", err)
		}
	}
	return string(out)
}

// preludeBinary compiles src against the real prelude and returns the executable's path.
//
// Split out of buildAndRunWithPrelude so a test that needs the *trap* rather than the
// output can have it: a panic writes to stderr and exits 101, and the stdout-only runner
// above sees an empty string either way — which would make a trap test pass on a program
// that printed nothing for any other reason.
func preludeBinary(t *testing.T, src string) string {
	t.Helper()
	clang := lookClang(t)

	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := []string{dir, repoStdRoot(t)}
	units, diags := modules.Resolve(entry, roots, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Errors())
	}
	ep, epDiags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	ir, err := New().Emit(res, ep)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	return compileCached(t, clang, string(ir))
}

// read_line's contract, one case per thing that can come off a stream. The EOF
// case is the one that justifies the `Maybe` return type: with a bare `string` it
// would be indistinguishable from the blank-line case directly above it, and the
// natural read loop would never terminate.
func TestExec_ReadLine(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  match read_line() {
    Some line => println("[${line}]"),
    None => println("EOF"),
  }
}
`
	cases := []struct {
		name, input, want string
	}{
		{"a line", "hello world\n", "[hello world]\n"},
		{"no trailing newline", "hello", "[hello]\n"},
		{"blank line", "\n", "[]\n"},
		{"eof", "", "EOF\n"},
		{"crlf strips the cr", "abc\r\n", "[abc]\n"},
		{"only the first line is consumed", "one\ntwo\n", "[one]\n"},
		// Past readLineInitialCap (128), so the box is realloc'd at least twice.
		{"long line grows the buffer", strings.Repeat("x", 300) + "\n", "[" + strings.Repeat("x", 300) + "]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunWithPrelude(t, src, c.input); got != c.want {
				t.Errorf("input %q: got %q, want %q", c.input, got, c.want)
			}
		})
	}
}

// A read loop must terminate at EOF rather than spinning, and must not leak or
// corrupt a line across iterations — each line's string is a separate heap box
// whose last use is inside the loop body.
func TestExec_ReadLineLoop(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var n = 0;
  var done = false;
  for done == false {
    let line = read_line();
    if line.is_none() {
      done = true;
    } else {
      n = n + 1;
      println("${n}:${line.unwrap_or("")}");
    }
  }
  println("total ${n}");
}
`
	got := buildAndRunWithPrelude(t, src, "a\nbb\nccc\n")
	const want = "1:a\n2:bb\n3:ccc\ntotal 3\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// parse_i64's contract. The range cases are the point of the negative
// accumulator: both i64 extremes must parse, and the values one past each must be
// rejected rather than trapping — parsing is exactly where a program meets input
// it did not choose, so an out-of-range number has to be a `None` the caller can
// handle, not an abort.
func TestExec_ParseI64(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show = (s: string) -> void => {
  match s.parse_i64() {
    Some n => println("${n}"),
    None => println("None"),
  }
}
let main = () -> void => {
  var done = false;
  for done == false {
    let line = read_line();
    if line.is_none() { done = true; } else { show(line.unwrap_or("")); }
  }
}
`
	cases := []struct{ in, want string }{
		{"0", "0"},
		{"42", "42"},
		{"-42", "-42"},
		{"+7", "7"},
		{"007", "7"},
		{"9223372036854775807", "9223372036854775807"},   // i64 max
		{"-9223372036854775808", "-9223372036854775808"}, // i64 min
		{"9223372036854775808", "None"},                  // max + 1
		{"-9223372036854775809", "None"},                 // min - 1
		{"99999999999999999999999", "None"},              // far out of range
		{"", "None"},                                     // a blank line is not a number
		{"-", "None"},
		{"+", "None"},
		{"abc", "None"},
		{"1abc", "None"}, // trailing garbage is not ignored
		{"12 ", "None"},  // nor is trailing space
		{" 12", "None"},  // nor leading
		{"1-2", "None"},  // a sign only counts at the start
		{"1.5", "None"},  // this is parse_i64, not parse_f64
		{"０", "None"},    // a non-ASCII digit is not a digit
	}
	var in, want strings.Builder
	for _, c := range cases {
		in.WriteString(c.in + "\n")
		want.WriteString(c.want + "\n")
	}
	if got := buildAndRunWithPrelude(t, src, in.String()); got != want.String() {
		t.Errorf("got:\n%s\nwant:\n%s", got, want.String())
	}
}

// The two together, which is the thing the feature exists for: read a line, parse
// it, react. Also covers a `read_line` result flowing through `parse_i64` as a
// borrowed argument — the string is owned by the caller and must survive the call
// and then be released exactly once.
func TestExec_ReadLineThenParse(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let secret = 37;
  var guesses = 0;
  var done = false;
  for done == false {
    let line = read_line();
    if line.is_none() {
      println("gave up after ${guesses}");
      done = true;
    } else {
      let parsed = line.unwrap_or("").parse_i64();
      if parsed.is_none() {
        println("not a number");
      } else {
        let g = parsed.unwrap_or(0);
        guesses = guesses + 1;
        if g < secret { println("low"); }
        else if g > secret { println("high"); }
        else { println("correct in ${guesses}"); done = true; }
      }
    }
  }
}
`
	got := buildAndRunWithPrelude(t, src, "50\n25\nabc\n37\n")
	const want = "high\nlow\nnot a number\ncorrect in 3\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// read_line's result is an owned value: a call whose result is *discarded* must
// still be released. This is the case that the ownership pass's owning-builtin
// entry exists for — without it the result is treated as borrowed and the
// allocation is never freed.
func TestExec_ReadLineDiscardedResultIsReleased(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  read_line();
  read_line();
  println("done");
}
`
	if got := buildAndRunWithPrelude(t, src, "a\nb\n"); got != "done\n" {
		t.Errorf("got %q, want %q", got, "done\n")
	}
}

// read_line under AddressSanitizer, with LeakSanitizer where the platform has it.
//
// This is the check the new allocation path most needs, and the reason it is
// separate from the behavioural tests above: `read_line` is the only place in the
// backend that allocates a box the caller never sees allocated — the malloc and
// the realloc growth both happen inside the shim, so the caller's release comes
// from the ownership pass's owning-builtin rule rather than from an allocating
// expression it can see. The path-sensitive conservation check (which is how this
// package usually catches a skipped release) cannot help here: it tracks
// `lyra_rc_alloc` calls *within* the function being analyzed, and this box arrives
// as a return value wrapped in a union.
//
// **LeakSanitizer is Linux-only**; on macOS the ASan runtime rejects
// `detect_leaks=1` outright. So the leak half runs under the workspace's
// `./asan.sh`, and on macOS this still runs as a use-after-free check, which is
// the fault this code actually had during development (the string was released
// before the `match` read it).
func TestExec_ReadLineUnderASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)

	// Reads several lines, so the box is allocated and released once per iteration,
	// and one line is long enough to force the realloc growth path.
	const src = `
module main
let main = () -> void => {
  var n = 0;
  var done = false;
  for done == false {
    let line = read_line();
    if line.is_none() {
      done = true;
    } else {
      let parsed = line.unwrap_or("").parse_i64();
      if parsed.is_some() { n = n + parsed.unwrap_or(0); }
      println("${line.unwrap_or("")}");
    }
  }
  println("sum ${n}");
}
`
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Errors())
	}
	ep, epDiags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	irText, err := New().Emit(res, ep)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	bin := compileCached(t, clang, instrumentForASan(string(irText)), "-fsanitize=address")
	leaks := "0"
	if runtime.GOOS == "linux" {
		leaks = "1"
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("1\n2\n" + strings.Repeat("7", 200) + "\nnot a number\n39\n")
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks="+leaks)

	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("asan run failed (leaks=%s): %v\n%s", leaks, err, out)
	}
	if !strings.Contains(string(out), "sum 42") {
		t.Errorf("expected the program to finish with `sum 42`, got:\n%s", out)
	}
}

// checkWithPrelude analyzes src against the real prelude and returns its error
// diagnostics as strings, without emitting or running anything.
//
// The counterpart to buildAndRunWithPrelude for properties that are decided at
// compile time — the `det`/`pure` effect bounds — where the interesting outcome is
// a diagnostic rather than a program's output, and where a passing case must
// produce *no* diagnostic, which running a binary cannot demonstrate.
func checkWithPrelude(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	var out []string
	for _, d := range driver.AnalyzeUnits(units).Errors() {
		out = append(out, d.Message)
	}
	return out
}

// checkWithPreludeDiagnostics is checkWithPrelude including *warnings*. Separate rather
// than a widening of it, because most callers assert "no errors" and would then have to
// filter out every advisory diagnostic the compiler ever gains.
func checkWithPreludeDiagnostics(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	var out []string
	for _, d := range driver.AnalyzeUnits(units).Diagnostics {
		out = append(out, d.Message)
	}
	return out
}
