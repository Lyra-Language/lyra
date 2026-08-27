package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// **The real-library round trip, run rather than only compiled.** `examples/zlib.lyra`
// compresses, uncompresses and compares — through zlib, a library nobody wrote for Lyra,
// with the version string read back through `std.ffi`.
//
// This is the half of the FFI's testing story the vendored fixture deliberately cannot
// cover. A fixture proves the ABI is self-consistent across a boundary whose *both sides*
// this project wrote; only a real library proves Lyra matches a convention it had to obey
// rather than choose. `@link("z")` on the declarations is what puts `-lz` on the link line,
// so the build needs no flag — which is itself part of what this exercises.
//
// It goes through the **CLI**, not the backend harness, for the same reason: the harness
// hardcodes `-lm`, so a test written there would be testing a link line no user gets.
func TestExample_ZlibRoundTrips(t *testing.T) {
	if !zlibAvailable(t) {
		t.Skip("zlib is not available in this toolchain (install zlib1g-dev); " +
			"CI and asan.Dockerfile both provide it, so this must not be skipped there")
	}
	// The CLI is invoked **in-process**, so the standard library is resolved relative to
	// the *test* binary rather than to `build/lyrac`. `LYRA_STD` is the documented override
	// for exactly this — "a build can point at a working copy" — and the repo root is the
	// directory *containing* `std/`, not `std/` itself.
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	example := filepath.Join(root, "examples", "zlib.lyra")
	bin := filepath.Join(t.TempDir(), "zlib")
	if _, stderr, code := captureRun(t, "build", "-o", bin, example); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running the example failed: %v\n%s", err, out)
	}
	got := string(out)
	// The compressed size is zlib's business and may move between versions, so the
	// assertions are the ones the program itself makes: the round trip is byte-identical,
	// and the version string came back through the pointer.
	for _, want := range []string{"zlib ", "original:   96 bytes", "restored:   96 bytes", "identical"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// zlibAvailable reports whether the toolchain can link `-lz`, probed the way
// `asanAvailable` probes its runtime: by compiling a C program, since the question is about
// the *linker* and nothing else can answer it.
func zlibAvailable(t *testing.T) bool {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		return false
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.c")
	const probe = "#include <zlib.h>\nint main(void){ return (int)(compressBound(1) > 0) - 1; }\n"
	if err := os.WriteFile(src, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return exec.Command(clang, src, "-lz", "-o", filepath.Join(dir, "probe")).Run() == nil
}

// repoRoot is the directory containing `std/`, located from this file rather than from the
// working directory so the test survives being run from anywhere.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	// .../lyra/cmd/lyrac/zlib_example_test.go → .../lyra
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "std", "prelude")); err != nil {
		t.Fatalf("std/prelude/ not found under %s: %v", root, err)
	}
	return root
}
