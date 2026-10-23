package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// captureRun runs lyrac with args as its own process — this test binary, which TestMain
// makes lyrac (asLyrac) — in t's environment (setenv), returning everything it wrote to
// stdout and stderr and its exit code. A process of its own, rather than run(args) here,
// so tests run in parallel: each has its own stdout, stderr and environment.
func captureRun(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := asLyrac(t, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("running lyrac %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return outBuf.String(), errBuf.String(), code
}

// captureRunUnoptimized is captureRun for a `build` or `run` (args[0]) whose program is tested
// for what it does, not how fast: at -O0, as `--live` builds, since clang's optimizer is
// nearly all of building a program that holds `ui` — examples/ui/overflow.lyra took 33.5s at
// -O2 and 3.2s at -O0, and CI's `ui` tests ran 90s each at -O2 on its four cores, contending.
// The backend's suite holds the code to every -O level; these tests are about `ui`.
func captureRunUnoptimized(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return captureRun(t, append([]string{args[0], "-O0"}, args[1:]...)...)
}

// asLyrac is the command running lyrac with args as its own process, in t's environment: this
// test binary, which TestMain makes lyrac when LYRAC_TEST_AS_LYRAC is set. Under -cover it
// writes its coverage where the test binary's own goes, so the run counts.
func asLyrac(t *testing.T, args ...string) *exec.Cmd {
	cmd := command(t, os.Args[0], args...)
	cmd.Env = append(cmd.Env, "LYRAC_TEST_AS_LYRAC=1")
	if dir := coverDir(); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	return cmd
}

// command is exec.Command in t's environment (setenv): for a program or tool a test starts.
func command(t *testing.T, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = environ(t)
	return cmd
}

// commandContext is exec.CommandContext in t's environment.
func commandContext(t *testing.T, ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = environ(t)
	return cmd
}

// coverDir is where the test binary writes its coverage counters under -cover (go test merges
// every file there into the profile), or "".
func coverDir() string {
	if f := flag.Lookup("test.gocoverdir"); f != nil {
		return f.Value.String()
	}
	return ""
}

// Each test's environment for what it starts, by test name: set with setenv rather than
// t.Setenv, which changes this process's environment and so forbids t.Parallel.
var (
	testEnvMu sync.Mutex
	testEnvs  = map[string]map[string]string{}
)

// setenv sets key to value for the processes t and its subtests start (captureRun, asLyrac,
// command), until t ends.
func setenv(t *testing.T, key, value string) {
	t.Helper()
	testEnvMu.Lock()
	defer testEnvMu.Unlock()
	name := t.Name()
	if testEnvs[name] == nil {
		testEnvs[name] = map[string]string{}
		t.Cleanup(func() {
			testEnvMu.Lock()
			defer testEnvMu.Unlock()
			delete(testEnvs, name)
		})
	}
	testEnvs[name][key] = value
}

// environ is this process's environment with what setenv set for t and every test around it,
// the innermost last (a later entry wins).
func environ(t *testing.T) []string {
	env := os.Environ()
	testEnvMu.Lock()
	defer testEnvMu.Unlock()
	parts := strings.Split(t.Name(), "/")
	for i := range parts {
		set := testEnvs[strings.Join(parts[:i+1], "/")]
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+set[k])
		}
	}
	return env
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
	if !strings.Contains(string(ir), "define i32 @main(i32 %argc, i8** %argv)") {
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
