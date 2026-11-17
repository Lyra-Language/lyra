package llvm

import "testing"

// TestExec_ARefThroughAPointerIsLentInPlace: `p^` given to a `ref` parameter lends the
// storage the pointer names, as a binding is lent — not a copy of it. The callee writes
// through a second pointer to the same storage and reads the change back through its `ref`;
// a copy would still say 1. Vega's maps live in one RAM area a scene unpacks into, read
// through such pointers: copied, every read of a map put the map on the 68000's stack.
func TestExec_ARefThroughAPointerIsLentInPlace(t *testing.T) {
	t.Parallel()
	src := `var area: [8]u16 = #[1, 2, 3, 4, 5, 6, 7, 8]

let view = () -> ^[4]u16 => unsafe { pointer_at((&area).address() + 4) }

let seen = (cells: ref [4]u16, poke: ^mut u16) -> u16 => {
  unsafe { poke^ = 40 }
  cells[0]
}

let main = () -> u8 => {
  let poke: ^mut u16 = unsafe { &mut area[2] }
  let through_block = seen(unsafe { view()^ }, poke)
  area[2] = 3
  let p = view()
  let through_deref = unsafe { seen(p^, poke) }
  u8(through_block + through_deref)
}
`
	if got := buildAndRun(t, src); got != 80 {
		t.Fatalf("exit %d, want 80: a ref through p^ read a copy", got)
	}
}
