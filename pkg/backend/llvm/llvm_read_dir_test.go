package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The `dir_names(path)` builtin and `std.io.read_dir` over it.
//
// **These are the real check on dirent.go's platform constants**, and deliberately not a
// test that repeats the offset in Go — that would agree with the backend by construction
// and prove nothing. A wrong `d_name` offset does not crash: it reads a few bytes further
// into the struct and answers plausible-looking garbage, or an empty name. Asking for the
// names of a directory whose contents the test just created is what catches that, and it
// catches the symbol question with it (on x86_64 macOS a bare `readdir` links against the
// legacy 32-bit-inode entry point, whose name field is 13 bytes earlier). Running under
// asan.sh's Linux container is what covers the other offset.
func TestExec_DirNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"b.lyra", "a.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	// The builtin answers what libc reported — every entry, `.` and `..` among them — so the
	// test sorts and counts rather than assuming an order the file system does not promise.
	src := `let main = () -> void => {
  let args = program_args()
  match dir_names(args[1]) {
    Some(names) => println(names.sorted().join(",")),
    None => println("none"),
  }
  match dir_names(args[2]) {
    Some(names) => println("empty=${names.sorted().join(",")}"),
    None => println("none"),
  }
  match dir_names(args[3]) {
    Some(_) => println("missing opened"),
    None => println("missing is none"),
  }
  match dir_names(args[4]) {
    Some(_) => println("a file opened"),
    None => println("a file is none"),
  }
}`
	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, dir, empty,
		filepath.Join(dir, "no-such-dir"), filepath.Join(dir, "a.txt")).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := ".,..,.hidden,a.txt,b.lyra,sub\n" +
		"empty=.,..\n" +
		"missing is none\n" +
		"a file is none\n"
	if string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// `std.io.read_dir` is the arithmetic over the builtin: `.` and `..` dropped, the rest
// sorted, dotfiles kept. An empty directory is `Some([])` and a missing one is `None` —
// the distinction `read_file` makes for the same reason, and the one an empty array
// would lose.
func TestExec_ReadDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"zeta.lyra", "alpha.txt", ".config"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()

	src := `module main
import std.io.{ read_dir }
let main = () -> void => {
  let args = program_args()
  match read_dir(args[1]) {
    Some(names) => println(names.join(",")),
    None => println("none"),
  }
  match read_dir(args[2]) {
    Some(names) => println("empty len=${names.len()}"),
    None => println("none"),
  }
  match read_dir(args[3]) {
    Some(_) => println("missing opened"),
    None => println("missing is none"),
  }
}`
	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, dir, empty, filepath.Join(dir, "no-such-dir")).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := ".config,alpha.txt,zeta.lyra\n" +
		"empty len=0\n" +
		"missing is none\n"
	if string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}
