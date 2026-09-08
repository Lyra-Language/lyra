package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// `examples/primes.lyra` is the target program for lazy sequences, and this pins its
// status (09/08): it **type-checks** — `gen`, `yield`, `Seq<t>` as a source, the prelude's
// combinators — and the build is **refused by name**, because the lowering has not landed.
// When stage 1 lands, the second half of this test flips to running the program; until
// then a refusal that reads as anything but "not lowered yet" is a regression in the
// message, and a build that succeeds is a lowering nobody wrote.
func TestExample_PrimesTypeChecksAndIsRefusedByName(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	example := filepath.Join(root, "examples", "primes.lyra")
	if _, stderr, code := captureRun(t, "check", example); code != 0 {
		t.Fatalf("check exited %d\nstderr: %s", code, stderr)
	}
	_, stderr, code := captureRun(t, "build", "-o", filepath.Join(t.TempDir(), "primes"), example)
	if code == 0 {
		t.Fatal("build succeeded: lazy sequences lower now, so this test should run the program instead")
	}
	if !strings.Contains(stderr, "lazy sequences are not lowered yet") {
		t.Errorf("the refusal should say what is missing; got:\n%s", stderr)
	}
}
