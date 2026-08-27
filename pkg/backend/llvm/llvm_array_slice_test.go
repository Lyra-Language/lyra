package llvm

import (
	"strings"
	"testing"
)

// `xs.slice(start, end)` — the half-open element range, the array twin of the string
// method.
//
// **It exists for the commonest C output convention there is**: a function fills a buffer
// you sized and tells you how much it used. Without this the only spelling was a `push`
// loop — one bounds check and one capacity check per element — which is why
// `examples/zlib.lyra` hands its buffers and lengths around separately instead of
// returning a right-sized `[]u8`.

func TestExec_ArraySlice(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
let main = () -> void => {
  var xs: []i64 = [10, 20, 30, 40, 50]
  let a = xs.slice(1, 4)
  print("${a.len()} ${a[0]} ${a[2]} ")
  // end == len names the position one past the last element, so a whole-array copy is
  // spellable; start == end is the empty slice rather than a trap.
  print("${xs.slice(0, xs.len()).len()} ${xs.slice(2, 2).len()}")
}
`)
	if got := strings.TrimSpace(out); got != "3 20 40 5 0" {
		t.Errorf("slice = %q; want \"3 20 40 5 0\"", got)
	}
}

// **A `[N]T` slices too, and the result is a `[]T`** — the length is `end - start`, a
// run-time value, so no fixed size could be written down. That also makes it `push`-able,
// which is what a caller building a buffer up wants.
func TestExec_ArraySliceOfAFixedArrayIsDynamic(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
let main = () -> void => {
  let fixed = [1, 2, 3, 4]
  var g = fixed.slice(1, 3)
  g.push(99)
  print("${g.len()} ${g[0]} ${g[1]} ${g[2]}")
}
`)
	if got := strings.TrimSpace(out); got != "3 2 3 99" {
		t.Errorf("fixed-array slice = %q; want \"3 2 3 99\"", got)
	}
}

// **Every copied element is retained**, because each slot in the new box is an owner: a
// `[]string` slice holds the same pointers the parent does, so without the retains the
// parent's drop frees strings the slice still points at.
//
// The concatenations matter — a literal string is static, and only a heap-allocated one
// can be freed early. Under ASan a missing retain is a use-after-free read; a spurious one
// is a leak, which the conservation counting elsewhere in this package would catch.
func TestExec_ArraySliceRetainsManagedElementsASan(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var got = 0
  for i in 0..<50 {
    var names: []string = ["alpha" ++ "!", "beta" ++ "!", "gamma" ++ "!"]
    let some = names.slice(1, 3)
    // names dies at the end of this iteration; some outlives it only within the body,
    // but the elements it shares must survive the parent's drop either way.
    got = got + some[0].len() + some[1].len()
  }
  if got == 550 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// The bounds are checked before anything is read, with the string method's
// one-test-for-every-way-this-is-wrong shape. An inverted range stays a trap rather than an
// empty array: it is a caller bug, and the empty array it would otherwise yield is
// indistinguishable from a correct empty slice.
func TestExec_ArraySliceTraps(t *testing.T) {
	t.Parallel()
	// **The bounds arrive through parameters.** A *provable* negative bound is a compile
	// error (lyra-E022, 08/26), so a literal one never reaches this trap — written
	// literally these rows would silently stop testing the trap and start testing the
	// front end. The trap is still the answer for a bound the compiler cannot settle.
	for _, c := range []struct{ name, bounds string }{
		{"end past the length", "0, 6"},
		{"start past the length", "9, 9"},
		{"inverted", "3, 1"},
		{"negative start", "-1, 2"},
		{"negative end", "0, -1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanic(t, `
let cut = (xs: []i64, a: i64, b: i64) -> []i64 => xs.slice(a, b)
let main = () -> void => {
  var xs: []i64 = [1, 2, 3, 4, 5]
  println(cut(xs, `+c.bounds+`).len())
}
`)
			if code != 101 {
				t.Fatalf("exited %d; want the trap's 101", code)
			}
			if !strings.Contains(out, "array slice out of range") {
				t.Errorf("output = %q; want the array-slice message", out)
			}
		})
	}
}
