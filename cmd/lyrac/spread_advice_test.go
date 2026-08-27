package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// **The spread diagnostic's advice is taken here and has to compile.** Refusing a string
// operand names `to_runes()` as the spelling that splices one, and a test asserting only
// the wording passes for exactly as long as the advice is wrong — which is how
// `where Self: A` survived, recommending syntax the language does not have.
//
// It is a *CLI* test because `to_runes` is a **prelude** function: neither the typechecker's
// harness nor the backend's has a prelude, so both refuse this program with
// `string has no method "to_runes"` — a failure about the harness, not about the advice.
// The rule from CLAUDE.md, restated: a test for a diagnostic whose fix names a
// standard-library function belongs where the standard library is real.
func TestSpreadAdvice_ToRunesCompilesAndRuns(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)

	dir := t.TempDir()
	src := filepath.Join(dir, "spread.lyra")
	if err := os.WriteFile(src, []byte(`module main
let main = () -> void => {
  let s = "ab"
  let c: []rune = [...s.to_runes(), 'c']
  println("${c.len()}${c[0]}${c[2]}")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "spread")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("the advice does not compile: exited %d\nstderr: %s", code, stderr)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running failed: %v\n%s", err, out)
	}
	if got, want := string(out), "3ac\n"; got != want {
		t.Errorf("output %q; want %q", got, want)
	}
}
