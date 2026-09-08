package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/primes.lyra` is the target program for lazy sequences, run end to end: an
// infinite `naturals()` through `filter`, `map`, `take` and `take_while`, consumed by
// brackets, by `for`, and by three terminals. Every value is checked against the number
// theory rather than against the program's own output — the sum of the first hundred
// primes is 24133, eight primes lie below twenty, and 1009 is the first above a
// thousand — so a lowering that runs and answers wrong fails here too.
func TestExample_PrimesRuns(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	example := filepath.Join(root, "examples", "primes.lyra")
	bin := filepath.Join(t.TempDir(), "primes")
	if _, stderr, code := captureRun(t, "build", "-o", bin, example); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running the example failed: %v\n%s", err, out)
	}
	want := "First ten primes: 2 3 5 7 11 13 17 19 23 29\n4 9 25 49 121 \nSum of the first hundred primes: 24133\nHow many primes are below twenty: 8\nFirst prime above a thousand: 1009\nodd squares: 1 9\n"
	if got := string(out); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
	_ = strings.TrimSpace
}
