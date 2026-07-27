package llvm

import (
	"os/exec"
	"testing"
)

// `for x in <array>` lowers as an index-counter loop over the elements — the length
// is the compile-time size for a fixed array or the box's runtime len for a dynamic
// one, and the loop variable borrows each element. It works over fixed-size (`[N]T`,
// stack and `shared`) and dynamic (`[]T`) arrays, with break/continue.

func TestExec_ForIn(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"fixed array accumulate",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: [3]u8 = [10, 20, 12]
  for x in xs {
    sum += x
  }
  sum
}`,
			42,
		},
		{
			"dynamic array accumulate",
			`let main = () -> u8 => {
  var sum: i64 = 0
  let xs: []i64 = [4, 5, 6]
  for x in xs {
    sum += x
  }
  u8(sum)
}`,
			15,
		},
		{
			"shared fixed array accumulate",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: shared [3]u8 = [10, 20, 12]
  for x in xs {
    sum += x
  }
  sum
}`,
			42,
		},
		{
			"break out of the loop",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: [4]u8 = [1, 2, 3, 4]
  for x in xs {
    if x == 3 { break }
    sum += x
  }
  sum
}`,
			3, // 1 + 2, breaks at 3
		},
		{
			"continue skips an element",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: [4]u8 = [1, 2, 3, 4]
  for x in xs {
    if x == 2 { continue }
    sum += x
  }
  sum
}`,
			8, // 1 + 3 + 4
		},
		{
			"empty dynamic array iterates zero times",
			`let main = () -> u8 => {
  var sum: u8 = 7
  let xs: []u8 = []
  for x in xs {
    sum += x
  }
  sum
}`,
			7,
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

// Iterating produces each element in order — observable via println.
func TestExec_ForIn_PrintsEachElement(t *testing.T) {
	src := `let main = () -> void => {
  let xs: []i64 = [1, 2, 3]
  for x in xs {
    println(x)
  }
}`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (out=%q)", code, out)
	}
	if out != "1\n2\n3\n" {
		t.Errorf("expected \"1\\n2\\n3\\n\", got %q", out)
	}
}

// Iterating a `[]string` binds each element as a *borrow* (the array still owns it),
// so reading elements in the body never double-frees. Verified under AddressSanitizer.
func TestExec_ForIn_StringElementsASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `let score = (s: string) -> u8 => if s == "cd" { 1 } else { 0 }
let main = () -> u8 => {
  var n: u8 = 0
  let xs: []string = ["ab", "cd", "ef"]
  for x in xs {
    n += score(x)
  }
  n
}
`
	if code := buildAndRunASan(t, clang, src); code != 1 {
		t.Errorf("ASan run: expected exit 1 (one match), got %d", code)
	}
}

// A two-variable index form and a non-array iterable are deferred with loud errors.
func TestEmit_ForIn_Deferred(t *testing.T) {
	for _, src := range []string{
		`let main = () -> void => {
  let xs: [3]u8 = [1, 2, 3]
  for i, x in xs { println(x) }
}
`,
		`let main = () -> void => {
  for i in 0..<3 { println(i) }
}
`,
	} {
		if _, err := emitSource(t, src); err == nil {
			t.Errorf("expected a loud error for a deferred for-in form:\n%s", src)
		}
	}
}
