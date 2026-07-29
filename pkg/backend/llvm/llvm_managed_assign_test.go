package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// Assigning to a *managed* location (a `string` array element or struct field)
// releases whatever the slot held and takes ownership of the new value — so the
// refcount stays balanced. These verify the value is updated and (under ASan) that
// no double-free or use-after-free occurs.
func TestExec_ManagedAssignment(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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

// Copying a plain **stack** aggregate (struct / tuple / `[N]T`) duplicates the fat
// pointers of its managed fields with *no retain* — a stack aggregate is a value, and
// the ownership pass has no deep-retain-on-copy. So a `string` field or element
// sitting in inline storage may be aliased by any number of unretained copies, and
// releasing the overwritten value on assignment dangles every one of them.
//
// Each program below copies the aggregate, assigns through one copy, then reads the
// managed value out of the *other* — a heap-use-after-free before the release was
// restricted to box-interior targets (releaseOldTarget / lvalueLoc.viaBox). Both
// exit 0 only if the original string is still intact, so they fail on a plain run as
// well as aborting under ASan.
//
// A `mut` parameter used to be the third case here — the by-value parameter copy was
// itself the invisible alias. It is no longer an alias at all: `mut` is passed by
// reference, so the callee writes through to the caller's own storage. That case now
// lives in TestExec_MutParameter_WritesReachCaller, which asserts the write *lands*.
func TestExec_ManagedAssignment_AliasedStackAggregate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			// A struct copy: `q` holds an unretained duplicate of p.name.
			"struct field",
			`struct Person { name: string }
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  let q = p
  p.name = "x" ++ "y"
  if q.name == "ab" { 0 } else { 1 }
}`,
		},
		{
			// A fixed-size array copy: `ys` holds unretained duplicates of xs's elements.
			"fixed-size array element",
			`let main = () -> u8 => {
  var xs: [2]string = ["a" ++ "b", "c" ++ "d"]
  let ys = xs
  xs[0] = "x" ++ "y"
  if ys[0] == "ab" { 0 } else { 1 }
}`,
		},
	}

	clang, clangErr := exec.LookPath("clang")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != 0 {
				t.Errorf("expected exit 0 (original string intact), got %d", got)
			}
			if clangErr != nil || !asanAvailable(t, clang) {
				t.Skip("ASan runtime not available; ran without it")
			}
			if got := buildAndRunASan(t, clang, c.src); got != 0 {
				t.Errorf("under ASan: expected exit 0, got %d", got)
			}
		})
	}
}

// TestEmit_ManagedAssignmentReleaseIR pins *where* the release-old is emitted, which
// is the whole safety argument. A managed target must release the value it is
// overwriting or that value leaks — but only when the slot genuinely owns it. Two
// ways for that to hold (releaseOldTarget): the slot lives inside a ref-counted box,
// or the path is rooted at an owning binding. The one case that must still refuse is
// a *borrowed* root, where the slot shares the caller's reference.
//
// A behavioral test can only show the absence of a crash; this shows the release is
// present exactly where it is owed and absent exactly where it would steal.
func TestEmit_ManagedAssignmentReleaseIR(t *testing.T) {
	t.Parallel()
	// releasesIn counts @lyra_rc_release calls in one function body. That is a direct
	// release of a *managed* value; a stack aggregate's deep release is a call to its
	// drop glue instead, so it is deliberately not counted here — this test is about
	// the release-old decision, not total traffic (TestEmit_DeepRetainConservation
	// covers totals).
	releasesIn := func(fn, src string) int {
		t.Helper()
		ir, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		body, ok := llFuncBodies(ir)[fn]
		if !ok {
			t.Fatalf("no @%s in emitted IR:\n%s", fn, ir)
		}
		return strings.Count(body, "@lyra_rc_release")
	}

	for _, c := range []struct {
		name, fn, src string
		want          int
		why           string
	}{
		{
			name: "dynamic array element", fn: "main", want: 2,
			why: "release-old, plus the array box at scope exit",
			src: `let main = () -> u8 => {
  var xs: []string = ["a" ++ "1"]
  xs[0] = "b" ++ "2"
  0
}`,
		},
		{
			name: "shared struct field", fn: "main", want: 2,
			why: "release-old through the box, plus the box at scope exit",
			src: `struct Person { name: string }
let main = () -> u8 => {
  var p: shared Person = Person { name: "a" ++ "b" }
  p.name = "x" ++ "y"
  0
}`,
		},
		{
			// Inline storage, but rooted at an owning `var`: the binding holds the +1 on
			// the old field value, so it is its reference to drop. This is what
			// deep-retain-on-copy re-enabled — it was suppressed (and leaked) before,
			// because a copy of the struct had no +1 of its own.
			name: "stack struct field, owning root", fn: "main", want: 1,
			why: "release-old; the binding's own deep release is a drop-glue call, not counted here",
			src: `struct Person { name: string }
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  p.name = "x" ++ "y"
  0
}`,
		},
		{
			name: "stack array element, owning root", fn: "main", want: 1,
			why: "release-old; the array's deep release is a drop-glue call",
			src: `let main = () -> u8 => {
  var xs: [2]string = ["a" ++ "b", "c" ++ "d"]
  xs[0] = "x" ++ "y"
  0
}`,
		},
		{
			name: "stack struct nested in a shared struct", fn: "main", want: 2,
			why: "release-old, plus the shared box at scope exit",
			src: `struct Person { name: string }
struct Wrapper { p: Person }
let main = () -> u8 => {
  var w: shared Wrapper = Wrapper { p: Person { name: "a" ++ "b" } }
  w.p.name = "x" ++ "y"
  0
}`,
		},
		{
			// The final hop is what decides the box case, not the root: a `[]string`
			// field reached through a stack struct is still box-interior.
			name: "[]string field of a stack struct", fn: "main", want: 1,
			why: "release-old for the element; the struct's deep release is a drop-glue call",
			src: `struct Holder { items: []string }
let main = () -> u8 => {
  var h: Holder = Holder { items: ["a" ++ "1"] }
  h.items[0] = "b" ++ "2"
  0
}`,
		},
		{
			// A `mut` parameter is passed **by reference**, so its slot is the caller's own
			// storage — the overwritten string is a genuine reference to drop, not a
			// duplicate to dangle. While `mut` was passed by value this case had to be
			// refused (the parameter copy shared the caller's reference), which leaked;
			// by-reference passing is what closes that leak. Counted in the *callee*.
			name: "by-reference mut parameter root", fn: "rename", want: 1,
			why: "a by-ref `mut` param names the caller's slot, so the old value must be released or it leaks",
			src: `struct Person { name: string }
let rename = (p: mut Person) -> void => {
  p.name = "x" ++ "y"
}
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  rename(p)
  0
}`,
		},
		{
			// The same shape with `own`: the caller transferred, so the callee *does* own
			// the field and must release it. The contrast with the case above is the
			// whole point of keying on the parameter's mode.
			name: "owned parameter root", fn: "rename", want: 1,
			why: "an `own` parameter is framed, so the old field value is the callee's to drop",
			src: `struct Person { name: string }
let rename = (p: own Person) -> void => {
  p.name = "x" ++ "y"
}
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  rename(p)
  0
}`,
		},
	} {
		if n := releasesIn(c.fn, c.src); n != c.want {
			t.Errorf("%s: want %d releases in @%s (%s), got %d", c.name, c.want, c.fn, c.why, n)
		}
	}
}
