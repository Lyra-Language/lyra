package llvm

import "testing"

// A generic struct's field default is typed per instantiation, so each literal filling it
// gets its own copy where nothing in the default binds a name — an array, a tuple, a struct
// literal, a constructor applied (10/30; a leaf since 10/27). Shared, the second instantiation
// was refused: "cannot assign []Maybe<i64> to []Maybe<string>".
func TestExec_AGenericStructsDefaultIsTypedPerInstantiation(t *testing.T) {
	t.Parallel()
	got := buildAndRunASanWithPrelude(t, `struct Pt<e> { x: e, n: i64 }
struct Spec<e> {
  label: string,
  slots: []Maybe<e> = [None, None],
  pair: (Maybe<e>, i64) = (None, 3),
  origin: Maybe<Pt<e>> = None,
  nested: Maybe<Maybe<e>> = Some(None),
}
let main = () -> u8 => {
  let a = Spec::<i64> { label: "a" }
  let b = Spec::<string> { label: "b" }
  let c = Spec::<i64> { label: "c" }
  let deep = match (a.nested, b.nested) { (Some(None), Some(None)) => 1, _ => 0 }
  u8(a.slots.len() + b.slots.len() + c.slots.len() + a.pair.1 + b.pair.1 + deep)
}`)
	if got != 13 {
		t.Errorf("exit %d, want 2 + 2 + 2 + 3 + 3 + 1 = 13", got)
	}
}

// A lambda default binds names, so it stays the declaration's one node, and two literals of
// one instantiation share it. The backend lifted it once per literal into one function —
// two `entry` blocks, "Terminator found in the middle of a basic block" (10/30), in a plain
// struct as much as a generic one.
func TestExec_ALambdaDefaultFillsSeveralLiterals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"a plain struct", `struct Spec {
  label: string,
  on_edit: (string) -> Maybe<i64> = (s: string) => Some(s.len()),
}
let main = () -> u8 => {
  let a = Spec { label: "a" }
  let b = Spec { label: "b" }
  let x = match (a.on_edit)("xyz") { Some(v) => v, None => 0 }
  let y = match (b.on_edit)("hello") { Some(v) => v, None => 0 }
  u8(x + y)
}`, 8},
		{"a generic struct at one instantiation", `struct Spec<e> {
  label: string,
  on_edit: (string) -> Maybe<e> = (_: string) => None,
}
let main = () -> u8 => {
  let a = Spec::<i64> { label: "a" }
  let b = Spec::<i64> { label: "b" }
  let x = match (a.on_edit)("x") { Some(v) => v, None => 1 }
  let y = match (b.on_edit)("y") { Some(v) => v, None => 2 }
  u8(x + y)
}`, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
