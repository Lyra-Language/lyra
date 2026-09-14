package main

import (
	"os"
	"testing"
)

// TestMain makes the package's tests independent of the shell that runs them.
//
// Resolution reads two variables from the environment: `LYRA_STD`, which puts the standard
// library — and with it the prelude — in reach, and `LYRA_NO_PRELUDE`. A test that needs the
// library sets `LYRA_STD` itself (`t.Setenv("LYRA_STD", stdRootDir(t))`), so an ambient value
// only ever *changes* the rest: a test asserting on what a prelude-free program offers passed
// in CI and failed on a machine where `LYRA_STD` had been exported, which read as a flaky
// race for a day (09/13). Cleared here, a local run answers what CI answers.
func TestMain(m *testing.M) {
	for _, name := range []string{"LYRA_STD", "LYRA_NO_PRELUDE"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
