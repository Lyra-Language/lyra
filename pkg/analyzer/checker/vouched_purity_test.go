package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// `@vouched unsafe pure` asserts the bound rather than having it checked, as `unsafe extern
// pure` does for C: the body writes a global — memory it fills on first use, which no caller can
// tell from memory filled before — and is not reported, and a `pure` caller costs only what
// the bound says. Vega's maps unpack into a scene's RAM through such a function, read by
// generated `pure` collision tests and the games' own `pure` helpers.
func TestPurity_UnsafePureIsTakenAtItsWord(t *testing.T) {
	src := `
var area: [4]u16 = #[0, 0, 0, 0]
var filled: bool = false

@vouched
let cells = unsafe pure () -> ^[4]u16 => {
  if !filled {
    area[0] = 7
    filled = true
  }
  unsafe { pointer_at((&area).address()) }
}

let first = pure () -> u16 => unsafe { cells()^ }[0]
`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Without `@vouched` the same body is checked, and refused — `unsafe pure` too, which the
// standard library writes of functions it means checked (`std.ffi`'s `data`).
func TestPurity_UnsafePureWithoutVouchedIsChecked(t *testing.T) {
	src := `
var area: [4]u16 = #[0, 0, 0, 0]

let fill = unsafe pure () -> u16 => {
  area[0] = 7
  area[0]
}
`
	errs := checkPurity(t, src)
	if len(errs) == 0 {
		t.Fatal("an unsafe pure function writing a global was accepted without `@vouched`")
	}
}

// `unsafe` alone vouches for nothing: an `unsafe` function with no bound is inferred from
// its body like any other, and a `pure` caller is charged what it does.
func TestPurity_UnsafeWithoutABoundIsInferred(t *testing.T) {
	src := `
var area: [4]u16 = #[0, 0, 0, 0]

let fill = unsafe () -> void => { area[0] = 7 }

let caller = pure () -> u16 => {
  unsafe { fill() }
  0
}
`
	errs := checkPurity(t, src)
	if len(errs) == 0 {
		t.Fatal("a pure function calling an unsafe one that writes a global was accepted")
	}
}

// `@vouched` vouches only for an `unsafe` function's declared bound: on one not `unsafe`, or
// with no bound, it is refused where it is written.
func TestCollect_VouchedNeedsUnsafeAndABound(t *testing.T) {
	for _, src := range []string{
		"@vouched\nlet f = pure () -> i64 => 1\n",
		"@vouched\nlet f = unsafe () -> i64 => 1\n",
	} {
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}
		_, _, _, errs := collector.NewCollector([]byte(src)).Collect(tree.RootNode())
		if len(errs) == 0 {
			t.Errorf("accepted: %q", src)
		}
	}
}
