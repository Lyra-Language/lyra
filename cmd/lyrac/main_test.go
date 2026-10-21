package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain makes the package's tests independent of the shell that runs them — the same
// reason cmd/lyra-lsp has one (09/13). The example tests set `LYRA_STD` themselves and
// expect the prelude, so an exported `LYRA_NO_PRELUDE` failed seven of them on a machine
// where CI passes them; each test states the configuration it needs instead.
//
// With `LYRAC_TEST_AS_LYRAC` set, the test binary is lyrac itself, given the rest of its
// arguments: a test that signals a whole `lyrac run` process starts one this way
// (asLyrac).
func TestMain(m *testing.M) {
	if os.Getenv("LYRAC_TEST_AS_LYRAC") != "" {
		os.Exit(run(os.Args[1:]))
	}
	for _, name := range []string{"LYRA_STD", "LYRA_NO_PRELUDE"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	tmp, err := os.MkdirTemp("", "lyrac-test-lib")
	if err != nil {
		panic(err)
	}
	findAccessKit(tmp)
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// findAccessKit puts a `liblyra-accesskit.a` on `LIBRARY_PATH`, which every program over
// `ui.sdl3` links: build.sh's in build/lib when it has run, else `accesskit_stub.c` compiled
// into `tmp` — CI tests before it builds, and a test never needs the real adapter. A machine
// with no C compiler gets neither, and those programs say so when they link.
func findAccessKit(tmp string) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	dir := filepath.Join(root, "build", "lib")
	if _, err := os.Stat(filepath.Join(dir, "liblyra-accesskit.a")); err != nil {
		dir = tmp
		obj := filepath.Join(tmp, "accesskit_stub.o")
		stub := filepath.Join(root, "bindings", "accesskit", "accesskit_stub.c")
		if exec.Command("cc", "-c", "-o", obj, stub).Run() != nil ||
			exec.Command("ar", "rcs", filepath.Join(tmp, "liblyra-accesskit.a"), obj).Run() != nil {
			return
		}
	}
	path := []string{dir}
	if old := os.Getenv("LIBRARY_PATH"); old != "" {
		path = append(path, old)
	}
	os.Setenv("LIBRARY_PATH", strings.Join(path, string(os.PathListSeparator)))
}
