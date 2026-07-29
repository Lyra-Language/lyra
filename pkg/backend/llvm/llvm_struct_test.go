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
