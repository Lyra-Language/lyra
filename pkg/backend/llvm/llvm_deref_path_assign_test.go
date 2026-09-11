package llvm

import (
	"os/exec"
	"testing"
)

// A deref is an assignment path element: `p.offset(i)^.field = v` stores into that
// element's field, and `p^.x += 1` reads and writes it once. The backend refused the
// path outright until 09/11 ("unsupported assignment target path element"), which is
// what a raylib binding writing a model's material through `materials.offset(i)^` met.
// A managed field goes through the interior-assignment path, so the slot takes the new
// string and releases the old one — checked under AddressSanitizer.
func TestExec_AssignThroughADerefPathASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `struct P { a: i64, name: string }
let main = () -> u8 => {
  var xs: [3]P = [P { a: 1, name: "x" }, P { a: 2, name: "y" }, P { a: 3, name: "z" }]
  unsafe {
    let p = &mut xs[0]
    p.offset(1)^.a = 20
    p.offset(2)^.name = "zed"
    p^.a += 9
  }
  if xs[0].a == 10 && xs[1].a == 20 && xs[2].name == "zed" && xs[1].name == "y" { 7 } else { 1 }
}
`
	if code := buildAndRunASan(t, clang, src); code != 7 {
		t.Errorf("ASan run: expected exit 7 (every write landed), got %d", code)
	}
}
