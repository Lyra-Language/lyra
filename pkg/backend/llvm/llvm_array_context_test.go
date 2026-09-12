package llvm

import "testing"

// An array literal mixing a **solved** and an **unsolved** generic element lowers at the
// context's element width, not at the width its own elements joined to.
//
// A front-end test cannot establish this. The elements narrowing is only half the fix —
// assignability and the backend both read the type recorded for the literal **node**, so a
// version that narrowed the leaves and left the node alone would type-check and then build
// `Maybe<i64>` payloads into a `Maybe<u8>` array. Running it is what says which happened.
func TestExec_ArrayLiteralTakesItsContextsElementType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let main = () -> void => {
  let xs: []Maybe<u8> = [Some(200), None]
  let ys: [2]Maybe<u8> = [Some(201), None]
  print("${xs[0].unwrap_or(0)} ${xs[1].unwrap_or(7)} ${ys[0].unwrap_or(0)}")
}`, "")
	if out != "200 7 201" {
		t.Fatalf("want %q, got %q", "200 7 201", out)
	}
}

// A struct field seeds a generic call's type arguments, and the result lowers. The field's
// declaration is the only thing that says what `bag_new()` builds — no argument mentions
// the variable at all.
func TestExec_StructFieldSeedsAGenericConstructor(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Holder { b: Bag<string> }
let main = () -> void => {
  var h = Holder { b: bag_new() }
  h.b.items.push("x")
  print("${h.b.items.len()} ${h.b.items[0]}")
}`, "")
	if out != "1 x" {
		t.Fatalf("want %q, got %q", "1 x", out)
	}
}

// The generic caller's lambda, lowered. `sort` delegates to `sort_by` with an unannotated
// comparator, which is the prelude's own spelling since 09/11 — so this exercises the
// shipped code path rather than a reconstruction of it.
func TestExec_UnannotatedComparatorInAGenericCaller(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let mysort<t> where t: Ord = (self: mut []t) -> void => self.sort_by((a, b) => a.compare(b))
let main = () -> void => {
  var xs: []i64 = [5, 3, 9, 1]
  xs.mysort()
  let ys = [30, 10, 20].sorted()
  print("${xs[0]}${xs[3]} ${ys[0]}${ys[2]}")
}`, "")
	if out != "19 1030" {
		t.Fatalf("want %q, got %q", "19 1030", out)
	}
}
