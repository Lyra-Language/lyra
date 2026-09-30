package llvm

import (
	"strings"
	"testing"
)

// `pointer_at(address)` and `read_volatile`/`write_volatile` are what a console's
// hardware registers need (09/30, the Genesis target): a pointer to a fixed address, and
// accesses LLVM performs exactly as written. The host can run everything but a real
// hardware address, so these check the semantics here and the IR's shape.

func TestExec_VolatileAccess(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The last of several writes is what a read sees — the ordinary meaning,
			// which the IR test below shows is not reached by dropping the earlier ones.
			"writes then a read",
			`let main = () -> void => {
			   var reg: u16 = 1
			   unsafe {
			     let p: ^mut u16 = &mut reg
			     p.write_volatile(0x8144)
			     p.write_volatile(7)
			   }
			   let seen = unsafe { (&reg).read_volatile() }
			   println("${seen} ${reg}")
			 }`,
			"7 7",
		},
		{
			// An untyped literal is stored at the pointee's width, as `p^ = v` stores it.
			"a literal at the pointee's width",
			`let main = () -> void => {
			   var b: u8 = 0
			   unsafe { (&mut b).write_volatile(200) }
			   println("${b}")
			 }`,
			"200",
		},
		{
			// `pointer_at` at address 0 is the null pointer: the one address the host can
			// name without owning storage there. Pinned by the annotation through the
			// `unsafe` block around it.
			"pointer_at makes the pointer its context asks for",
			`let main = () -> void => {
			   let p: ^u8 = unsafe { pointer_at(0) }
			   let q: ^mut u16 = unsafe { pointer_at(0) }
			   println("${p == nullptr} ${q == nullptr}")
			 }`,
			"true true",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, "module main\n"+tc.src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// **Two volatile writes to one address are two stores**, which is the whole point: the
// VDP's control port takes a register write, then another, and an optimizer that sees two
// plain stores to one address keeps only the last. `pointer_at`'s address reaches the IR
// as an `inttoptr` at the pinned type.
func TestEmit_VolatileWritesAreEachKept(t *testing.T) {
	t.Parallel()
	ir := emitWithPrelude(t, `module main
let control = () -> ^mut u16 => unsafe { pointer_at(0xC00004) }
let main = () -> void => {
  let port = control()
  unsafe {
    port.write_volatile(0x8144)
    port.write_volatile(0x8F02)
  }
}
`)
	if !strings.Contains(ir, "inttoptr i64 12582916 to i16*") {
		t.Errorf("expected pointer_at to lower to an inttoptr at ^mut u16; IR:\n%s", ir)
	}
	if n := strings.Count(ir, "store volatile i16"); n != 2 {
		t.Errorf("expected two volatile stores, got %d", n)
	}
}
