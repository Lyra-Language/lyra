package main

import (
	"os"
	"testing"
)

// TestMain makes the package's tests independent of the shell that runs them — the same
// reason cmd/lyra-lsp has one (09/13). The example tests set `LYRA_STD` themselves and
// expect the prelude, so an exported `LYRA_NO_PRELUDE` failed seven of them on a machine
// where CI passes them; each test states the configuration it needs instead.
func TestMain(m *testing.M) {
	for _, name := range []string{"LYRA_STD", "LYRA_NO_PRELUDE"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
