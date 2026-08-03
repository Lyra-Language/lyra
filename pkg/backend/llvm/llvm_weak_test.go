package llvm

import (
	"strings"
	"testing"
)

// A `weak` reference is a non-owning pointer, so a `weak` field lowers to an
// opaque `i8*` — pointer-sized, which makes a recursive type finite (the whole
// point: `weak` breaks a `shared`-cycle leak, and along the way the size cycle).
// The two below cover the layout; the runtime semantics live in
// llvm_weak_runtime_test.go, and the constructible-field case — `Maybe<weak T>`,
// which is what a cycle back-edge actually needs — in TestExec_WeakOptionalField.

func TestEmit_WeakField_LowersToPointer(t *testing.T) {
	t.Parallel()
	src := `struct Node { value: i64, parent: weak Node }
let main = () -> u8 => 0`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// The recursive struct is finite: the weak field is a pointer, not Node by value.
	if !strings.Contains(ir, "%Node = type { i64, i8* }") {
		t.Errorf("expected %%Node with a pointer weak field, got:\n%s", ir)
	}
}

// A program declaring a weak-broken recursive type compiles and runs (the weak
// field never crashes layout the way it used to crash parseType).
func TestExec_WeakRecursiveTypeBuilds(t *testing.T) {
	t.Parallel()
	src := `struct Node { value: i64, parent: weak Node }
data List = Nil | Cons(i64, weak List)
let main = () -> u8 => 7`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d, want 7", got)
	}
}

// A `weak` **field** is what the feature exists for, and until now it could not be
// built: a field must be initialized, and there is no empty weak, so the only
// spelling of a cycle back-edge is an *optional* one — `Maybe<weak T>` (todo.md,
// "a `weak` field is unconstructible").
//
// What blocked it was not `weak` at all. A generic instantiation used by value
// inside another type reached SizeAndAlign as a `ParameterizedType`, a shape none
// of its cases match, so boxing the enclosing `shared` value failed with "cannot
// size a `shared Node` payload yet" — and the same failure hit `Maybe<i64>`, which
// is why the first two cases below carry no `weak` at all. resolveForLayout now
// normalizes through resolveInstantiation, the choke point every other
// shape-reading site already uses (generic_types.go).
func TestExec_WeakOptionalField(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	// Declared here rather than taken from std/prelude.lyra, as every other test in
	// this package does: the backend tests are self-contained so a prelude edit
	// cannot silently change what they exercise.
	const maybe = "data Maybe<t> = None | Some(t)\n"
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// No `weak` anywhere: a plain generic field in a `shared` struct. This is
			// the actual regression, and it failing is what made the weak case look
			// like a weak problem.
			"a generic field in a shared struct",
			`struct Box { n: i64, p: Maybe<i64> }
			 let main = () -> u8 => {
			   let b: shared Box = Box { n: 1, p: Some(9) }
			   var out = 0
			   match b.p { Some(v) => { out = v; out }, None => 0 }
			   u8(out)
			 }`,
			9,
		},
		{
			// The same shape one level down: the instantiation is the payload of
			// another generic, so resolution has to recurse rather than stop at the
			// first normalization.
			"a nested generic field in a shared struct",
			`struct Box { n: i64, p: Maybe<Maybe<i64>> }
			 let main = () -> u8 => {
			   let b: shared Box = Box { n: 1, p: Some(Some(4)) }
			   var out = 0
			   match b.p {
			     Some(inner) => {
			       match inner { Some(v) => { out = v; out }, None => 0 }
			     },
			     None => 0,
			   }
			   u8(out)
			 }`,
			4,
		},
		{
			// The point of the exercise: a back-edge stored in a field and read back
			// through an upgrade.
			"a weak back-edge read through the field",
			`struct Node { n: i64, parent: Maybe<weak Node> }
			 let main = () -> u8 => {
			   let root: shared Node = Node { n: 10, parent: None }
			   let kid: shared Node = Node { n: 2, parent: Some(root.weak()) }
			   var out = 0
			   match kid.parent {
			     Some(w) => { if let p = w { out = p.n } ; out },
			     None => 0,
			   }
			   u8(out)
			 }`,
			10,
		},
		{
			// A real cycle: parent owns child (`shared`), child points back (`weak`).
			// This is the shape refcounting cannot free on its own and the reason
			// `weak` exists at all (ALLOCATION.md — no cycle collector).
			"a parent/child cycle broken by the weak edge",
			`struct Node { n: i64, parent: Maybe<weak Node>, kid: Maybe<shared Node> }
			 let main = () -> u8 => {
			   let mut parent: shared Node = Node { n: 3, parent: None, kid: None }
			   let child: shared Node = Node { n: 4, parent: Some(parent.weak()), kid: None }
			   parent.kid = Some(child)
			   var out = 0
			   match child.parent {
			     Some(w) => { if let p = w { out = p.n } ; out },
			     None => 0,
			   }
			   u8(out)
			 }`,
			3,
		},
		{
			// The dead-referent path through the optional field: the `Some` is still
			// there (the field is set), but the upgrade inside it fails. The two
			// failures a reader must tell apart — "no back-edge" and "the referent is
			// gone" — are distinct branches, which is why the field is
			// `Maybe<weak T>` and not a nullable weak.
			"the field is Some but the referent is gone",
			`struct Node { n: i64, parent: Maybe<weak Node> }
			 let orphan = () -> Maybe<weak Node> => {
			   let tmp: shared Node = Node { n: 99, parent: None }
			   Some(tmp.weak())
			 }
			 let main = () -> u8 => {
			   var out = 0
			   match orphan() {
			     Some(w) => { if let p = w { out = p.n } else { out = 7 } ; out },
			     None => 0,
			   }
			   u8(out)
			 }`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := maybe + c.src
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}
