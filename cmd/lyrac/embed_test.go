package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `embed("path")` (typechecker/embed.go): a file's bytes as a `const`, the path relative to
// the source file — here a directory beside it, while lyrac runs from the repository — read
// as static data, the whole file and its ends exact.
func TestRun_EmbedReadsAFileIntoAConst(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "art"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 70000)
	sum := 0
	for i := range data {
		data[i] = byte(i * 7)
		sum += int(data[i])
	}
	if err := os.WriteFile(filepath.Join(dir, "art", "data.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`const DATA: [70000]u8 = embed("art/data.bin")

let main = () -> void => {
  var sum = 0
  for k in 0..<DATA.len() { sum += i64(DATA[k]) }
  println("${sum} ${DATA[0]} ${DATA[1]} ${DATA[69999]}")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, stderr)
	}
	want := fmt.Sprintf("%d %d %d %d", sum, data[0], data[1], data[69999])
	if strings.TrimSpace(stdout) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(stdout), want)
	}
}

// What embed refuses, each said where it is: a length the annotation does not give, a file
// that is not there, and anywhere but a whole `const`'s value.
func TestRun_EmbedRefusals(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "four.bin"), []byte{1, 2, 3, 4}, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, src, want string }{
		{"length", `const D: [5]u8 = embed("four.bin")
let main = () -> void => println("${D[0]}")
`, "cannot assign [4]u8 to [5]u8"},
		{"missing", `const D: [4]u8 = embed("none.bin")
let main = () -> void => println("${D[0]}")
`, "embed: cannot read"},
		{"a let", `let main = () -> void => {
  let d = embed("four.bin")
  println("${d.len()}")
}
`, "only a whole `const`'s value"},
		{"part of a const", `const D: [1][4]u8 = #[embed("four.bin")]
let main = () -> void => println("${D[0][0]}")
`, "only a whole `const`'s value"},
	} {
		src := filepath.Join(dir, c.name+".lyra")
		if err := os.WriteFile(src, []byte(c.src), 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, code := captureRun(t, "check", src)
		if code == 0 || !strings.Contains(stderr, c.want) {
			t.Errorf("%s: exit %d, want an error saying %q; stderr: %s", c.name, code, c.want, stderr)
		}
	}
}
