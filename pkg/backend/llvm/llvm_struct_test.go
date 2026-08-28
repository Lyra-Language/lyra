package llvm

import (
	"strings"
	"testing"
)

// Struct instances lower end to end: construction (`Node { value: 3 }`) builds a
// first-class struct value via insertvalue in declaration order, and field access
// (`n.value`) reads an element back via extractvalue. Run via buildAndRun, so a
// wrong field position or a width mismatch shows up as the wrong exit code (or
// clang rejecting the IR).

func TestExec_StructInstances(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"single field",
			`struct Node {
				value: u8,
			}
			let main = () -> u8 => {
				let n = Node { value: 42 }
				n.value
			}
			`,
			42,
		},
		// The literal lists fields out of declaration order (`y` before `x`); the
		// result must still index the declared position, so `p.y` is 9 not 3.
		{
			"out-of-order literal fields",
			`struct Pt {
				x: u8,
				y: u8,
			}
			let main = () -> u8 => {
				let p = Pt { y: 9, x: 3 }
				p.y
			}
			`,
			9,
		},
		// Both fields read and combined — the fields are real values feeding a
		// computation. Also exercises the width-propagation fix: without it the
		// literals would lower at i64 and mismatch the u8 fields.
		{
			"arithmetic on fields",
			`struct Pt {
				x: u8,
				y: u8,
			}
			let main = () -> u8 => {
				let p = Pt { x: 20, y: 22 }
				p.x + p.y
			}
			`,
			42,
		},
		// A struct field that is itself a struct: nested construction and a
		// two-level field access (`o.inner.v`), which resolves the inner struct's
		// fields through the symbol table (the field's type is an UnresolvedType).
		{
			"nested struct",
			`struct Inner {
				v: u8,
			}
			struct Outer {
				inner: Inner,
			}
			let main = () -> u8 => {
				let o = Outer { inner: Inner { v: 7 } }
				o.inner.v
			}
			`,
			7,
		},
		// A struct constructed at a call site, passed by value, and its field read
		// inside the callee.
		{
			"struct through a call",
			`struct Node {
				value: u8,
			}
			let get = (n: Node) -> u8 => n.value
			let main = () -> u8 => get(Node { value: 5 })
			`,
			5,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_StructInstanceIR pins the emitted shape: construction builds the
// declared struct type via insertvalue and field access reads it via extractvalue.
func TestEmit_StructInstanceIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `
	struct Node {
		value: u8,
	}
	let main = () -> u8 => {
		let n = Node { value: 42 }
		n.value
	}
	`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"%Node = type { i8 }",
		"insertvalue",
		"extractvalue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_StructRecordUpdate_Deferred: record-update syntax isn't lowered yet,
// so it errors loudly rather than silently ignoring the base.
func TestEmit_StructRecordUpdate_Deferred(t *testing.T) {
	t.Parallel()
	src := `
	struct Node {
		value: u8,
	}
	let main = () -> u8 => {
		let a = Node { value: 1 }
		let b = Node { a | value: 2 }
		b.value
	}
	`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: record-update syntax is not implemented yet")
	}
	if !strings.Contains(err.Error(), "record-update") {
		t.Errorf("expected a record-update error, got: %v", err)
	}
}

// Assigning to a struct field whose type is a generic data type must lower.
//
// It did not until 08/15, and failed as `llvm: unknown named type "Maybe"` — a backend
// error for a front-end omission. A nullary construction like `None` solves no type
// parameter, so it stays the bare *declaration* until some context completes it
// (propagateInstantiation), and an assignment through a member target was not one of the
// sites applying that context. The comment sitting in checkLValueAssignment said as much
// — "this path does no literal propagation at all … whether it *should* is a separate
// question" — a deferred question that was already this bug.
//
// Found writing std.tui's key decoder, whose reader clears its lookahead with exactly
// this statement. Four lines reproduce it, which is the surprising part: nothing about
// the shape is exotic.
func TestExec_AssignNoneToAGenericStructField(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Holder { pending: Maybe<rune> }
let main = () -> void => {
  var h = Holder { pending: Some('a') };
  h.pending = None;
  match h.pending { Some(r) => println("some ${r}"), None => println("none") }
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "none" {
		t.Errorf("assigning None to a Maybe field gave %q, want \"none\"", got)
	}
}

// The mirror: assigning a *solved* construction to the same field, so the fix is not
// simply "stop checking assignments to generic fields".
func TestExec_AssignSomeToAGenericStructField(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Holder { pending: Maybe<i64> }
let main = () -> void => {
  var h = Holder { pending: None };
  h.pending = Some(7);
  match h.pending { Some(v) => println("some ${v}"), None => println("none") }
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "some 7" {
		t.Errorf("assigning Some(7) to a Maybe field gave %q, want \"some 7\"", got)
	}
}

// **An array literal assigned to a field takes the field's type.** Until 08/22 this path
// did no literal propagation at all, so the literal chose its own flavour — a *fixed*
// `[1]string` for the non-empty one and nothing at all for the empty one — and both
// reached the backend as hard errors on a program the front end had passed:
// `cannot store [1 x { i8*, i64, i64 }] into { i64, … }*`, and `unknown type: %!s(<nil>)`.
//
// A literal's flavour is chosen by what it is *used as*, and an assignment through a
// member target never told it. Found writing `std.tui`'s `Renderer`, where `self.prev = []`
// is simply how a field is cleared.
func TestExec_AnArrayLiteralAssignedToAFieldTakesItsType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Log { lines: []string, counts: []i64 }
let clear_it = (self: mut Log) -> void => { self.lines = [] }
let refill = (self: mut Log) -> void => { self.lines = ["a", "b"] }
let widen = (self: mut Log) -> void => { self.counts = [7] }
let main = () -> void => {
  var l = Log { lines: ["x"], counts: [1, 2, 3] }
  l.clear_it()
  print("${l.lines.len()} ")
  l.refill()
  print("${l.lines.len()} ${l.lines[1]} ")
  l.widen()
  print("${l.counts.len()} ${l.counts[0]}")
}
`, "")
	if got := strings.TrimSpace(out); got != "0 2 b 1 7" {
		t.Errorf("field assignment = %q; want \"0 2 b 1 7\"", got)
	}
}

// The same rule one target-kind over: an *element* of an array of arrays is a member/index
// target too, and takes the element type for the same reason.
func TestExec_AnArrayLiteralAssignedToAnElementTakesItsType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  var grid: [][]i64 = [[1], [2]]
  grid[0] = []
  grid[1] = [7, 8, 9]
  print("${grid[0].len()} ${grid[1].len()} ${grid[1][2]}")
}
`, "")
	if got := strings.TrimSpace(out); got != "0 3 9" {
		t.Errorf("element assignment = %q; want \"0 3 9\"", got)
	}
}

// Struct field defaults lower (08/28). They never had: the front end checked them and
// the backend refused with `field "age" has no value (default values not implemented
// yet)`, so a defaulted field could not be omitted at all — and the *grammar* refused
// `Person {}` on top of that, which is what made the gap look like it was only about
// the all-defaulted case. The fix follows default *arguments*: the typechecker fills
// omitted fields from the declaration (applyDefaultFields), so the backend lowers an
// ordinary literal and knows nothing about defaults.
func TestExec_StructFieldDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{
			// The empty literal the grammar used to refuse.
			"every field defaulted",
			`struct Person { name: string = "anon", age: i64 = 41 }
			 let main = () -> u8 => {
			   let p = Person {}
			   println("${p.name} ${p.age}")
			   0
			 }`,
			"anon 41\n",
		},
		{
			// The shape the todo entry offered as the workaround, which also failed.
			"one field defaulted, one supplied",
			`struct Person { name: string, age: i64 = 7 }
			 let main = () -> u8 => {
			   let p = Person { name: "ada" }
			   println("${p.name} ${p.age}")
			   0
			 }`,
			"ada 7\n",
		},
		{
			// A supplied value wins over the default, in any field order.
			"a supplied value overrides its default",
			`struct Person { name: string = "anon", age: i64 = 7 }
			 let main = () -> u8 => {
			   let p = Person { age: 30 }
			   println("${p.name} ${p.age}")
			   0
			 }`,
			"anon 30\n",
		},
		{
			// Two literals omitting one field share the declaration's default AST node
			// (the same sharing applyDefaultArguments does); each must still evaluate
			// to its own value.
			"two literals sharing one default",
			`struct Counter { label: string = "c", n: i64 = 1 }
			 let main = () -> u8 => {
			   let a = Counter {}
			   let b = Counter { n: 5 }
			   println("${a.label}${a.n} ${b.label}${b.n}")
			   0
			 }`,
			"c1 c5\n",
		},
		{
			// A *managed* default: the box the default builds is owned by each literal
			// that uses it, so a shared AST node must not mean a shared reference.
			"a managed default",
			`struct Tag { text: string = "a" ++ "b" }
			 let main = () -> u8 => {
			   let x = Tag {}
			   let y = Tag {}
			   println("${x.text}${y.text}")
			   0
			 }`,
			"abab\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 {
				t.Errorf("%s: exited %d; want 0", c.name, code)
			}
			if out != c.wantOut {
				t.Errorf("%s: stdout = %q; want %q", c.name, out, c.wantOut)
			}
		})
	}
}

// The managed default under AddressSanitizer, with conservation accounting. Two
// literals that both omit a defaulted field share the declaration's **AST node** (the
// same sharing applyDefaultArguments does), so the risk is a shared *reference*: the
// expression is lowered once per site and each box must be owned, retained and released
// on its own. A shared box would show up as a double free here.
func TestExec_StructFieldDefaults_ManagedASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `struct Tag { text: string = "a" ++ "b" }
	 let main = () -> u8 => {
	   let x = Tag {}
	   let y = Tag {}
	   if x.text == y.text { 0 } else { 1 }
	 }`
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("ASan run exited %d; want 0", got)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	// **Count the drop-glue calls, not the `lyra_rc_release`s.** A struct holding a
	// managed field is freed through generated glue (`emitOwnedValue`), so the release
	// is emitted *once* in a shared function that each instance calls — a raw textual
	// count reads 2 allocations against 1 release and looks like a leak. The invariant
	// that actually holds is one allocation and one drop per literal; the shared
	// default node must not collapse them into one box.
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	dropCalls := strings.Count(ir, "call void @lyra_drop_")
	if allocs != 2 {
		t.Errorf("expected one allocation per literal (2), got %d", allocs)
	}
	if dropCalls != allocs {
		t.Errorf("expected one drop per allocation: %d allocations, %d drop-glue calls", allocs, dropCalls)
	}
}
