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
	t.Parallel()
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
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// The two-variable form `for i, x in xs` binds the index i (i64) alongside the
// element x.
func TestExec_ForIn_TwoVar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Uses both the index and the element: sum of i*x.
			"index times element (dynamic)",
			`let main = () -> u8 => {
  var acc: u8 = 0
  let xs: []u8 = [10, 20, 30]
  for i, x in xs {
    acc += u8(i) * x
  }
  acc
}`,
			80, // 0*10 + 1*20 + 2*30
		},
		{
			// Uses both over a fixed-size array: sum of i + x.
			"index plus element (fixed)",
			`let main = () -> u8 => {
  var acc: u8 = 0
  let xs: [3]u8 = [1, 2, 3]
  for i, x in xs {
    acc += u8(i) + x
  }
  acc
}`,
			9, // (0+1)+(1+2)+(2+3)
		},
		{
			// The index advances 0,1,2 — the last value seen is 2.
			"index reaches the last position",
			`let main = () -> u8 => {
  var last: u8 = 0
  let xs: []u8 = [5, 6, 7]
  for i, x in xs {
    last = u8(i)
  }
  last
}`,
			2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// Iterating produces each element in order — observable via println.
func TestExec_ForIn_PrintsEachElement(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// The two-variable form over a string (`for i, c in s`) is deferred — the
// index/rune pairing isn't defined. (Arrays, ranges, and single-var string all lower.)
func TestEmit_ForIn_Deferred(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  for i, c in "hello" { println(c) }
}
`
	if _, err := emitSource(t, src); err == nil {
		t.Errorf("expected a loud error for a two-variable for-in over a string (deferred):\n%s", src)
	}
}
