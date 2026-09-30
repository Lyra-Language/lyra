package typechecker_test

import "testing"

// `pointer_at(address)` — a pointer to a fixed address, for a console's hardware
// registers — and `read_volatile`/`write_volatile` (09/30). `pointer_at` is typed as
// `nullptr` is: untyped, pinned by its context, lyra-E069 when nothing pins it.

func TestPointerAt_PinnedByAnnotationReturnAndParameter(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let control = () -> ^mut u16 => unsafe { pointer_at(0xC00004) }
let poke = (p: ^mut u8, v: u8) -> void => unsafe { p.write_volatile(v) }
let main = () -> void => {
  let port: ^mut u16 = unsafe { pointer_at(0xC00000) }
  unsafe {
    port.write_volatile(0x0EEE)
    control().write_volatile(0x8144)
    poke(pointer_at(0xFF0000), 1)
  }
  let status = unsafe { control().read_volatile() }
}`, false))
}

// **A context reaches through an `unsafe` block**, which is where every `pointer_at`
// sits. It did not until 09/30, for `nullptr` either: `let p: ^u8 = unsafe { nullptr }`
// was lyra-E069 with the annotation right there.
func TestPointerAt_ContextReachesIntoAnUnsafeBlock(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  let p: ^u8 = unsafe { nullptr }
  let q: ^mut u16 = unsafe { pointer_at(0xC00004) }
}`, false))
}

func TestPointerAt_Refusals(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"outside unsafe", `let main = () -> void => {
  let p: ^mut u16 = pointer_at(0xC00004)
}`, "`pointer_at` makes a pointer to any address, so it requires an `unsafe` block"},
		{"nothing pins it", `let main = () -> void => {
  let p = unsafe { pointer_at(0xC00004) }
}`, "pointer_at: cannot tell what this points at"},
		{"not an integer", `let main = () -> void => {
  let p: ^u8 = unsafe { pointer_at("vdp") }
}`, "pointer_at: the address must be an integer"},
		{"volatile write through ^T", `let main = () -> void => {
  let p: ^u16 = unsafe { pointer_at(0xC00004) }
  unsafe { p.write_volatile(1) }
}`, "`write_volatile` writes through ^u16, which is read-only"},
		{"volatile outside unsafe", `let main = () -> void => {
  let p: ^mut u16 = unsafe { pointer_at(0xC00004) }
  p.write_volatile(1)
}`, "`write_volatile` reads or writes through a raw pointer, so it requires an `unsafe` block"},
		{"volatile of a managed value", `let main = () -> void => {
  var s = "hi"
  let p: ^mut string = unsafe { &mut s }
  unsafe { p.write_volatile("yo") }
}`, "`write_volatile` reads or writes a number, a bool or a pointer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Errorf("expected an error containing %q; got %v", c.want, res.errors)
			}
		})
	}
}
