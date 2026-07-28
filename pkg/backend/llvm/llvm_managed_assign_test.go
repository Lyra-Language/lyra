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

// Copying a plain **stack** aggregate (struct / tuple / `[N]T`) duplicates the fat
// pointers of its managed fields with *no retain* — a stack aggregate is a value, and
// the ownership pass has no deep-retain-on-copy. So a `string` field or element
// sitting in inline storage may be aliased by any number of unretained copies, and
// releasing the overwritten value on assignment dangles every one of them.
//
// Each program below copies the aggregate, assigns through one copy, then reads the
// managed value out of the *other* — a heap-use-after-free before the release was
// restricted to box-interior targets (releaseOldTarget / lvalueLoc.viaBox). All three
// exit 0 only if the original string is still intact, so they fail on a plain run as
// well as aborting under ASan.
func TestExec_ManagedAssignment_AliasedStackAggregate(t *testing.T) {
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
		{
			// The worst shape: no copy is visible in the source at all. A by-value `mut`
			// parameter *is* the copy, so assigning through it inside the callee would
			// release a string the caller still owns.
			"mut parameter callee",
			`struct Person { name: string }
let rename = (p: mut Person) -> void => {
  p.name = "x" ++ "y"
}
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  rename(p)
  if p.name == "ab" { 0 } else { 1 }
}`,
		},
	}

	clang, clangErr := exec.LookPath("clang")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
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
// is the whole safety argument: a target reached through a ref-counted box has no
// unretained alias (copying a `shared` value or a `[]T` copies the box pointer, which
// is itself managed and so retained), while a target in inline aggregate storage may
// have many. A behavioral test can only show the absence of a crash; this shows the
// release is present exactly where it is safe and absent exactly where it is not.
func TestEmit_ManagedAssignmentReleaseIR(t *testing.T) {
	// releasesInMain counts the release calls in @main only, so drop glue and the
	// runtime's own definitions don't perturb the count.
	releasesInMain := func(src string) int {
		t.Helper()
		ir, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		start := strings.Index(ir, "define i32 @main")
		if start < 0 {
			t.Fatalf("no @main in emitted IR:\n%s", ir)
		}
		body := ir[start:]
		if end := strings.Index(body, "\n}\n"); end >= 0 {
			body = body[:end]
		}
		return strings.Count(body, "@lyra_rc_release")
	}

	// Box-interior targets: the element/field lives in the box, so the overwritten
	// value is released (1) and the binding's own box is released at scope exit (1).
	for _, c := range []struct{ name, src string }{
		{"dynamic array element", `let main = () -> u8 => {
  var xs: []string = ["a" ++ "1"]
  xs[0] = "b" ++ "2"
  0
}`},
		{"shared struct field", `struct Person { name: string }
let main = () -> u8 => {
  var p: shared Person = Person { name: "a" ++ "b" }
  p.name = "x" ++ "y"
  0
}`},
	} {
		if n := releasesInMain(c.src); n != 2 {
			t.Errorf("%s: want 2 releases in @main (overwritten value + scope exit), got %d", c.name, n)
		}
	}

	// Inline (stack) aggregate targets: the slot may be aliased by an unretained
	// copy, so the release-old is suppressed — the overwritten value leaks, which is
	// the standing stack-aggregate behavior and the safe direction. Each want below
	// counts only the releases the program has *anyway*; one more than that is the
	// release-old, i.e. the use-after-free.
	for _, c := range []struct {
		name, src string
		want      int
	}{
		{"stack struct field", `struct Person { name: string }
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  p.name = "x" ++ "y"
  0
}`, 0}, // a stack binding is not managed, so it is never framed
		{"stack array element", `let main = () -> u8 => {
  var xs: [2]string = ["a" ++ "b", "c" ++ "d"]
  xs[0] = "x" ++ "y"
  0
}`, 0},
		// The root here *is* a box, but the final hop lands in the `Person` inlined
		// inside it — which a by-value read of `w.p` would duplicate unretained. The
		// one release is `w`'s own scope exit (running the Wrapper drop glue), not the
		// release-old.
		{"stack struct nested in a shared struct", `struct Person { name: string }
struct Wrapper { p: Person }
let main = () -> u8 => {
  var w: shared Wrapper = Wrapper { p: Person { name: "a" ++ "b" } }
  w.p.name = "x" ++ "y"
  0
}`, 1},
	} {
		if n := releasesInMain(c.src); n != c.want {
			t.Errorf("%s: want %d releases in @main (no release-old — the target may be aliased by an unretained copy), got %d", c.name, c.want, n)
		}
	}

	// The final hop is what decides, not the root: a `[]string` field reached through
	// a *stack* struct is still box-interior, so its element release must survive.
	// Copies of the struct duplicate the box pointer, and all of them name the same
	// element storage — ordinary aliasing, not a dangling duplicate.
	mixed := `struct Holder { items: []string }
let main = () -> u8 => {
  var h: Holder = Holder { items: ["a" ++ "1"] }
  h.items[0] = "b" ++ "2"
  0
}`
	if n := releasesInMain(mixed); n != 1 {
		t.Errorf("[]string field of a stack struct: want 1 release (the overwritten element), got %d", n)
	}
}
