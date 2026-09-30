package llvm

import "testing"

// `std.path.relative_to`: the path from a directory to a file, which joined back onto the
// directory names the file again — down, up and across, the same path, and a pair it
// cannot relate (one absolute, one not), which comes back normalized.
func TestExec_PathRelativeTo(t *testing.T) {
	t.Parallel()
	src := `import std.path.{ relative_to, join, normalize }
let main = () -> void => {
  println("games/hero/main.lyra".relative_to("games"))
  println("art/hero.vega".relative_to("code"))
  println("/a/b/c".relative_to("/a/x/y"))
  println("a/./b".relative_to("a/b/"))
  println("a/b".relative_to("/a"))
  println("x".relative_to("."))
  println("/a/b/c".relative_to("/"))
  let base = "one/two"
  println(base.join("one/three/f".relative_to(base)).normalize())
}`
	want := "hero/main.lyra\n../art/hero.vega\n../../b/c\n.\na/b\nx\na/b/c\none/three/f\n"
	if got := buildAndRunWithPrelude(t, src, ""); got != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", got, want)
	}
}
