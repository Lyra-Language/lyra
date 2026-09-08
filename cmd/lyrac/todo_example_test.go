package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/todo.lyra` is the target program for writing files: `std.io.write_file` over
// `creat`, subcommands read out of `ProgramArgs.positional`, and a stable sort.
//
// It runs a whole session rather than one command, because the property worth pinning is
// that the numbers survive between invocations: `list` prints a number, the *next*
// process takes it, and the two agree only because `load` is what sorts. Numbering the
// display separately would pass a single-command test and fail here at the first `done`.
func TestExample_TodoSession(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	bin := filepath.Join(dir, "todo")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(root, "examples", "todo.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	list := filepath.Join(dir, "list.txt")
	todo := func(args ...string) (string, int) {
		t.Helper()
		out, err := exec.Command(bin, append([]string{"-f", list}, args...)...).CombinedOutput()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("running the example failed: %v\n%s", err, out)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}

	if out, code := todo("list"); out != "nothing to do\n" || code != 0 {
		t.Errorf("an empty list printed %q (exit %d); want \"nothing to do\"", out, code)
	}
	for _, task := range [][]string{{"add", "buy", "milk"}, {"add", "write", "the", "guide"}, {"add", "call", "the", "bank"}} {
		if out, code := todo(task...); code != 0 {
			t.Fatalf("%v exited %d: %s", task, code, out)
		}
	}
	want := "1. [ ] buy milk\n2. [ ] write the guide\n3. [ ] call the bank\n"
	if out, _ := todo("list"); out != want {
		t.Errorf("after three adds:\n%s\nwant:\n%s", out, want)
	}
	// The number `list` printed for "write the guide" is the number `done` takes, and the
	// finished task then sorts below the unfinished ones.
	if out, code := todo("done", "2"); code != 0 {
		t.Fatalf("done 2 exited %d: %s", code, out)
	}
	want = "1. [ ] buy milk\n2. [ ] call the bank\n3. [x] write the guide\n"
	if out, _ := todo("list"); out != want {
		t.Errorf("after done 2:\n%s\nwant:\n%s", out, want)
	}
	// …and removing 1 removes the task printed as 1, not whichever is first in the file.
	if out, code := todo("remove", "1"); code != 0 {
		t.Fatalf("remove 1 exited %d: %s", code, out)
	}
	want = "1. [ ] call the bank\n2. [x] write the guide\n"
	if out, _ := todo("list"); out != want {
		t.Errorf("after remove 1:\n%s\nwant:\n%s", out, want)
	}
	// The store is a text file anyone can read, and a line typed into it by hand loads.
	stored, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(stored); !strings.Contains(got, "[x] write the guide") || !strings.Contains(got, "[ ] call the bank") {
		t.Errorf("the file does not hold both tasks:\n%s", got)
	}
	if err := os.WriteFile(list, []byte("typed by hand\n[x] finished\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want = "1. [ ] typed by hand\n2. [x] finished\n"
	if out, _ := todo("list"); out != want {
		t.Errorf("a hand-edited file:\n%s\nwant:\n%s", out, want)
	}
	// Every way of naming a task that is not on the list is refused the same way.
	for _, args := range [][]string{{"done", "9"}, {"done", "zero"}, {"done"}, {"remove", "0"}} {
		if out, code := todo(args...); code != 2 {
			t.Errorf("%v exited %d; want 2 (%s)", args, code, out)
		}
	}
	if out, code := todo("frobnicate"); code != 2 || !strings.Contains(out, "usage:") {
		t.Errorf("an unknown command exited %d without usage: %s", code, out)
	}
}
