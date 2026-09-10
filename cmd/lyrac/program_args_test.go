package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `lyrac run prog.lyra -- a b` hands `a b` to the program, reachable there through
// `program_args()`.
//
// **Why a `--` where `go run` needs none.** `go run` requires its own flags before the
// file, so everything after the file is unambiguously the program's. This parser
// deliberately accepts flags on *either* side of the source path — its own comment says a
// build command is as often edited by appending a flag as by inserting one — so the same
// rule here would silently reclaim `lyrac run prog.lyra --cc clang`, which today means the
// compiler's C compiler, as two arguments for the program. Adding a feature must not
// change what an existing command line means.
func TestRun_PassesArgumentsAfterSeparator(t *testing.T) {
	requireCC(t)
	// program_args() is prelude Lyra over two builtins, so this needs the real std root.
	t.Setenv("LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "args.lyra")
	if err := os.WriteFile(src, []byte(`
let main = () -> void => {
  var out = ""
  for _ in program_args() { out = out ++ "|" }
  println("${program_args().len()}${out}")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		args []string
		want string // count, then one | per argument
	}{
		// argv[0] is the program's own name, as in C, so the baseline is 1.
		{"no separator", []string{"run", src}, "1|"},
		{"bare separator is no arguments", []string{"run", src, "--"}, "1|"},
		{"two arguments", []string{"run", src, "--", "a", "b"}, "3|||"},
		// The case the separator exists for: text after `--` that would otherwise be
		// read as lyrac's own flags, including one that consumes a value.
		{"flag-shaped arguments", []string{"run", src, "--", "-O0", "--cc"}, "3|||"},
		// And a real lyrac flag before the separator still reaches lyrac.
		{"lyrac flag before, program args after", []string{"run", src, "-O0", "--", "-O0"}, "2||"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, code := captureRun(t, c.args...)
			if code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != c.want {
				t.Errorf("program saw %q; want %q", got, c.want)
			}
		})
	}
}

// `build` compiles and runs nothing, so arguments for a program it will not execute are
// refused rather than accepted and dropped — the same call this parser already makes for
// `-o` under `run`.
func TestBuild_RefusesProgramArguments(t *testing.T) {
	src := fixture("hello.lyra")
	_, stderr, code := captureRun(t, "build", src, "--", "foo")
	if code == 0 {
		t.Fatal("build should refuse program arguments")
	}
	if !strings.Contains(stderr, "runs nothing") {
		t.Errorf("stderr should say build runs nothing; got: %s", stderr)
	}
}
