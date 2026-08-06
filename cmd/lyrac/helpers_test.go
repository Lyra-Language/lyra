package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureRun invokes run(args) with os.Stdout and os.Stderr redirected to
// pipes, returning everything the CLI wrote to each stream plus its exit code.
// The pipes are drained concurrently so a large amount of output can't deadlock
// on a full pipe buffer.
func captureRun(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW

	var wg sync.WaitGroup
	var outBuf, errBuf bytes.Buffer
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, errR) }()

	code = run(args)

	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	os.Stdout, os.Stderr = origOut, origErr
	_ = outR.Close()
	_ = errR.Close()

	return outBuf.String(), errBuf.String(), code
}

// fixture returns the repo-relative path to a committed testdata/*.lyra file.
// Passing this relative path to the CLI keeps the path that appears in
// diagnostics ("testdata/foo.lyra:1:1: …") stable across machines, so golden
// files stay deterministic.
func fixture(name string) string {
	return filepath.Join("testdata", name)
}

// copyFixtureToTemp copies a committed fixture into a fresh temp directory and
// returns the new path. Used by `build` tests so the emitted .ll lands in the
// temp dir instead of polluting testdata/.
func copyFixtureToTemp(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(fixture(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("write temp fixture: %v", err)
	}
	return dst
}

// requireCC skips a test that needs to link an executable when no clang is on
// PATH, matching the backend's behavioural tests: a machine without a C
// compiler can still run everything else.
func requireCC(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
}

// assertIsIR fails unless path holds the emitted LLVM IR.
func assertIsIR(t *testing.T, path string) {
	t.Helper()
	ir, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected emitted IR at %s: %v", path, err)
	}
	if !strings.Contains(string(ir), "define i32 @main()") {
		t.Errorf("%s is missing the @main definition:\n%s", path, ir)
	}
}

// checkGolden compares got against testdata/<name>. If the golden file is
// missing or empty it is created and the test fails, asking for a re-run
// (mirroring the collector golden tests). Set UPDATE_GOLDEN=1 to rewrite a
// stale golden in place.
func checkGolden(t *testing.T, got, name string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read golden %s: %v", path, err)
		}
		writeGolden(t, path, got)
		t.Fatalf("wrote initial golden file %s; re-run to verify", path)
	}
	if len(want) == 0 {
		writeGolden(t, path, got)
		t.Fatalf("golden file %s was empty; wrote current output, re-run to verify", path)
	}
	if got != string(want) {
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			writeGolden(t, path, got)
			return
		}
		t.Errorf("output mismatch for %s (set UPDATE_GOLDEN=1 to update)\n--- want ---\n%s\n--- got ---\n%s",
			name, want, got)
	}
}

func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}
