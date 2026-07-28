package llvm

import (
	"os/exec"
	"testing"
)

// Assigning to a *managed* location (a `string` array element or struct field)
// releases whatever the slot held and takes ownership of the new value — so the
// refcount stays balanced. These verify the value is updated and (under ASan) that
// no double-free or use-after-free occurs.
func TestExec_ManagedAssignment(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// A []string element.
			"dynamic array string element",
			`let main = () -> u8 => {
  var xs: []string = ["a", "b", "c"]
  xs[1] = "z"
  if xs[1] == "z" { 1 } else { 0 }
}`,
			1,
		},
		{
			// A struct string field.
			"struct string field",
			`struct Named { name: string, id: u8 }
let main = () -> u8 => {
  var n: Named = Named { name: "old", id: 1 }
  n.name = "new"
  if n.name == "new" { 1 } else { 0 }
}`,
			1,
		},
		{
			// Self-referential: the RHS reads the old element before it is released.
			"self-referential concat assignment",
			`let main = () -> u8 => {
  var xs: []string = ["ab", "cd"]
  xs[0] = xs[0] ++ "x"
  if xs[0] == "abx" { 1 } else { 0 }
}`,
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// The release-old + own-new path is memory-safe with *heap* strings: overwriting a
// []string element frees the old heap string and the array's drop glue frees the
// final elements — no double free (the old element is no longer in the array when
// the box dies). Verified under AddressSanitizer.
func TestExec_ManagedAssignment_ASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	for _, src := range []string{
		// []string with heap elements: overwrite element 0 (frees the old heap string).
		`let main = () -> u8 => {
  var xs: []string = ["a" ++ "1", "b" ++ "2"]
  xs[0] = "c" ++ "3"
  if xs[0] == "c3" { 0 } else { 1 }
}
`,
		// Reassign the same element twice — each old value is freed exactly once.
		`let main = () -> u8 => {
  var xs: []string = ["a" ++ "1"]
  xs[0] = "b" ++ "2"
  xs[0] = "c" ++ "3"
  0
}
`,
	} {
		if code := buildAndRunASan(t, clang, src); code != 0 {
			t.Errorf("ASan run: expected exit 0, got %d for %q", code, src)
		}
	}
}
