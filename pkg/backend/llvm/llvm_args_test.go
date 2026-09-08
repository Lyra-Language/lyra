package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// `program_args()` is the prelude's `[]string` over the two builtins, and `std.io`'s
// `read_file` is ordinary Lyra over `open`/`read`/`close`. The binary is run with real
// arguments — a file this test writes, a path that does not exist, and an argument with
// a space and a non-ASCII rune — since nothing short of an actual argv exercises either.
func TestExec_ProgramArgsAndReadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(path, []byte("héllo\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	src := `import std.io.{ read_file }
let main = () -> void => {
  let args = program_args()
  println(args.len())
  for i in 1..<args.len() { println(args[i]) }
  match read_file(args[1]) {
    Some(t) => { println(t.len()); print(t) },
    None => { println("none") },
  }
  match read_file(args[2]) {
    Some(_) => { println("some") },
    None => { println("none") },
  }
  println(args[3].len())
}`
	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, path, missing, "two wörds").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "4\n" + path + "\n" + missing + "\ntwo wörds\n12\nhéllo\nworld\nnone\n9\n"
	if string(out) != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// With no arguments there is exactly one, the program's name, and an index past the end
// traps as an array index does.
func TestExec_ProgramArgs_NoArgsAndOutOfRange(t *testing.T) {
	t.Parallel()
	if got := buildAndRun(t, `let main = () -> u8 => u8(program_arg_count())`); got != 1 {
		t.Fatalf("program_arg_count() with no arguments = %d, want 1", got)
	}
	if got := buildAndRun(t, `let main = () -> u8 => u8(program_arg(3).len())`); got != 101 {
		t.Fatalf("program_arg(3) with no arguments exited %d, want the trap's 101", got)
	}
}
