package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `std.io.remove_file` over `unlink` (09/29, found missing by Vega's interface tests, which
// save a project to a temporary file): it deletes a file, answers false for one that is not
// there and for a directory, and removes a symbolic link without touching its target.
func TestExec_RemoveFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "gone.txt")
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	for _, p := range []string{file, target} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	src := `import std.io.{ remove_file }
let main = () -> void => {
  let args = program_args()
  println(remove_file(args[1]))
  println(remove_file(args[1]))
  println(remove_file(args[2]))
  println(remove_file(args[3]))
}`
	bin := preludeBinary(t, src)
	out, err := exec.Command(bin, file, dir, link).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := strings.Fields(string(out)), []string{"true", "false", "false", "true"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("answers %v; want %v (file, again, a directory, a link)", got, want)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file is still there")
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the link is still there")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("removing the link removed its target: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the directory went: %v", err)
	}
}
