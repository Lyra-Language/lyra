package llvm

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	out, err := command(bin, dir, empty,
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
	out, err := command(bin, dir, empty, filepath.Join(dir, "no-such-dir")).Output()
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
	out, err := command(bin, file, sub, toFile, toDir, broken,
		filepath.Join(dir, "no-such-path")).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// real file, real directory, link to each, a broken link, and a path that is nothing.
	if want := "false\nfalse\ntrue\ntrue\ntrue\nfalse\n"; string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// `std.io.exists` over `access(path, F_OK)`: anything at the path, a file or a directory,
// through a link to either — and not a link to nothing, which `is_symlink` still calls a link.
func TestExec_Exists(t *testing.T) {
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
	if err := os.Symlink(filepath.Join(dir, "gone"), broken); err != nil {
		t.Fatal(err)
	}

	src := `module main
import std.io.{ exists }
let main = () -> void => {
  let args = program_args()
  for i in 1..<args.len() { println(exists(args[i])) }
}`
	bin := preludeBinary(t, src)
	out, err := command(bin, file, sub, toFile, toDir, broken,
		filepath.Join(dir, "no-such-path"), filepath.Join(file, "under-a-file")).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// real file, real directory, a link to each, a broken link, a path that is nothing, and a
	// name below a file (which no directory holds).
	if want := "true\ntrue\ntrue\ntrue\nfalse\nfalse\nfalse\n"; string(out) != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// `std.io.walk_dir` against Go's `filepath.WalkDir` over one tree: nested directories,
// empty ones, dotfiles, names that sort differently by byte than by eye, and four kinds of
// link — to a file, to a directory, to nothing, and to an ancestor (which a walk that
// followed links would never leave). The expected list is WalkDir's under walk_dir's
// documented rules, so the two can disagree only where the Lyra walk is wrong:
//
//   - a symlink to a directory is neither walked nor listed; any other symlink is listed;
//   - a directory beneath the root that cannot be read is listed as though it were a file
//     (skipped when the test runs as root, which can read it anyway);
//   - `keep` false skips a file and prunes a directory, and is never asked about the root.
//
// Run twice: once for its output, and once under ASan — LeakSanitizer on Linux, where
// `./asan.sh` runs it — with the same answers baked into the program.
func TestExec_WalkDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkdir := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel string) {
		mkdir(filepath.Dir(rel))
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, rel string) {
		if err := os.Symlink(target, filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"a.txt", "Z.txt", "é.txt", ".hidden", ".git/config", ".git/refs/main",
		"b/c.lyra", "b/d/e.lyra", "b/d/f/g.lyra", "b-c/h.lyra", "b.lyra",
	} {
		write(f)
	}
	mkdir("b/empty")
	mkdir("empty_top")
	link(filepath.Join(root, "a.txt"), "link_file")
	link(filepath.Join(root, "b"), "link_dir")
	link(filepath.Join(root, "gone"), "broken")
	link(root, "b/d/up")
	if os.Getuid() != 0 {
		mkdir("locked/inside")
		locked := filepath.Join(root, "locked")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
	}
	empty := t.TempDir()
	file := filepath.Join(root, "a.txt")
	missing := filepath.Join(root, "no-such-dir")
	for _, p := range []string{root, empty} {
		if strings.ContainsAny(p, "\"\\$") {
			t.Skipf("temp path %q needs escaping in a Lyra string", p)
		}
	}

	walk := func(start string, keep func(string) bool) string {
		var files []string
		err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == start {
				return nil
			}
			if !keep(p) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if info, err := os.Stat(p); err == nil && info.IsDir() {
					return nil
				}
				files = append(files, p)
				return nil
			}
			if d.IsDir() {
				if _, err := os.ReadDir(p); err != nil {
					files = append(files, p)
					return fs.SkipDir
				}
				return nil
			}
			files = append(files, p)
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir %s: %v", start, err)
		}
		out := fmt.Sprintf("%d files\n", len(files))
		for _, f := range files {
			out += f + "\n"
		}
		return out
	}
	everything := func(string) bool { return true }
	undotted := func(p string) bool { return !strings.HasPrefix(filepath.Base(p), ".") }
	want := walk(root, everything) + walk(root, undotted) + "0 files\n" + "none\n" + "none\n"

	// The walks, written once: `%s` is how the program names its five paths.
	body := `let show = (found: Maybe<[]string>) -> string => {
  let Some(files) = found else { return "none\n" }
  var out = "${files.len()} files\n"
  for f in files { out = out ++ f ++ "\n" }
  out
}

let walks = (root: string, empty: string, file: string, missing: string) -> string =>
  show(walk_dir(root, (_) => true)) ++
    show(walk_dir(root, (p) => !p.base().starts_with("."))) ++
    show(walk_dir(empty, (_) => true)) ++
    show(walk_dir(file, (_) => true)) ++
    show(walk_dir(missing, (_) => true))
`
	src := `module main
import std.io.{ walk_dir }
import std.path
let main = () -> void => {
  let args = program_args()
  print(walks(args[1], args[2], args[3], args[4]))
}
` + body
	out, err := command(preludeBinary(t, src), root, empty, file, missing).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(out) != want {
		t.Errorf("walk_dir:\n%s\nfilepath.WalkDir:\n%s", out, want)
	}

	asanSrc := fmt.Sprintf(`module main
import std.io.{ walk_dir }
import std.path
let main = () -> u8 => {
  let got = walks("%s", "%s", "%s", "%s")
  if got == "%s" { 0 } else { 1 }
}
`, root, empty, file, missing, strings.ReplaceAll(want, "\n", `\n`)) + body
	if code := buildAndRunASanWithPrelude(t, asanSrc); code != 0 {
		t.Errorf("under ASan: exit %d, want 0", code)
	}
}
