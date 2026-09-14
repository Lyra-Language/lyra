package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// `std.io.append_file` over `fopen(path, "a")`: it creates a missing file, adds to the end
// of an existing one (including one `write_file` just replaced), writes an interior NUL as
// an ordinary byte, and answers false for a path it cannot open.
//
// `O_APPEND` is a different number on macOS and Linux, which is why appending waited; the
// mode string is the one spelling that is the same on both, so this runs unchanged on
// either — and under asan.sh's Linux container is what shows it.
func TestExec_AppendFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	src := `import std.io.{ write_file, append_file }
let main = () -> void => {
  let args = program_args()
  println(append_file(args[1], "one\n"))
  println(append_file(args[1], "two\n"))
  println(append_file(args[2], "three\n"))
  println(write_file(args[3], "fresh\n"))
  println(append_file(args[3], "a\0b\n"))
  println(append_file(args[4], "never"))
}`
	replaced := filepath.Join(dir, "replaced.txt")
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("already here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, []byte("old contents that write_file drops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unopenable := filepath.Join(dir, "no-such-dir", "x.txt")

	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, path, existing, replaced, unopenable).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "true\ntrue\ntrue\ntrue\ntrue\nfalse\n"; string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	for file, want := range map[string]string{
		path:     "one\ntwo\n",
		existing: "already here\nthree\n",
		replaced: "fresh\na\x00b\n",
	} {
		got, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s holds %q, want %q", filepath.Base(file), got, want)
		}
	}
	if _, err := os.Stat(unopenable); err == nil {
		t.Errorf("a file appeared at the unopenable path")
	}
}
