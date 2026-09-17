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

// `std.io.is_symlink` over `readlink` into a one-byte buffer: the link itself, not what it
// points at, which is the question `read_dir` cannot answer because `opendir` follows a
// link. A walker needs it to not read a tree twice through its own symlinks.
func TestExec_IsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	toFile := filepath.Join(dir, "to_file")
	toDir := filepath.Join(dir, "to_dir")
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(file, toFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, toDir); err != nil {
		t.Fatal(err)
	}
	// A link to nothing is still a link — the test that follows it would say otherwise.
	if err := os.Symlink(filepath.Join(dir, "gone"), broken); err != nil {
		t.Fatal(err)
	}

	src := `module main
import std.io.{ is_symlink }
let main = () -> void => {
  let args = program_args()
  for i in 1..<args.len() { println(is_symlink(args[i])) }
}`
	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, file, sub, toFile, toDir, broken,
		filepath.Join(dir, "no-such-path")).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// real file, real directory, link to each, a broken link, and a path that is nothing.
	if want := "false\nfalse\ntrue\ntrue\ntrue\nfalse\n"; string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}
