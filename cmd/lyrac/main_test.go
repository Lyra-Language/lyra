package main

import (
	"os"
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
	os.Exit(m.Run())
}
