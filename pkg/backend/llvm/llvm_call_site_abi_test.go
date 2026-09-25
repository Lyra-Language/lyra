package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/abi"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// **A call site's ABI attributes must match the declaration's**, and this is the test that
// says so on a machine where getting it wrong is invisible (09/20).
//
// `byval` and `sret` are not decoration on the `declare`: the attributes *on the call* are
// what lower the call. With `byval` on the declaration alone, LLVM passed the aggregate's
// address in a register while the C function read a copy from the stack — a segmentation
// fault on x86-64 the first time the callee touched a field.
//
// **It survived because every machine here is aarch64**, where a MEMORY aggregate is passed
// as a plain pointer and clang emits no attribute, so the missing one changes nothing. The
// formatter — the self-hosting probe, which walks a tree-sitter CST by passing a 32-byte
// `Node` to C over and over — crashed on the architecture CI runs and nowhere a developer
// would look. CI could not say so either: the test that exercises it skips without the
// tree-sitter runtime, which CI did not install until the same day.
//
// So the guard is the **IR for a named target**, not a program's behaviour: it fails on any
// host, including the ones where running the code would not.
func TestEmit_CallSiteCarriesTheAggregateAttributes(t *testing.T) {
	t.Parallel()
	// Thirty-two bytes: MEMORY on SysV (over the 16-byte limit), which is `byval` in and
	// `sret` out — the shape of tree-sitter's TSNode, which is where this was found.
	const src = `module main
struct Big { a: i64, b: i64, c: i64, d: i64 }
unsafe extern takes_big: (v: Big) -> i64
unsafe extern makes_big: () -> Big
let main = () -> void => {
  let v = unsafe { makes_big() }
  println("${unsafe { takes_big(v) }}")
}
`
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	out, err := NewForTarget(abi.X86_64SysV).Emit(res, ep)
	if err != nil {
		t.Fatal(err)
	}
	ir := string(out)
	for _, want := range []struct{ what, needle string }{
		{"the argument's call site", "call i64 @takes_big(%Big* byval(%Big)"},
		{"the return buffer's call site", "call void @makes_big(%Big* sret(%Big)"},
	} {
		if !strings.Contains(ir, want.needle) {
			t.Errorf("%s dropped its attribute; wanted a line containing %q.\n"+
				"A declaration's byval/sret does not lower the call — the call's own does:\n%s",
				want.what, want.needle, callLines(ir))
		}
	}
}

// The call instructions mentioning either foreign function, for a failure message that
// shows what was emitted instead of the whole module.
func callLines(ir string) string {
	var out []string
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "@takes_big") || strings.Contains(line, "@makes_big") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}

// **A narrow integer crossing to C is extended, and says how** (09/25): `zeroext` for
// `u8`/`u16`, `signext` for `i8`/`i16`, on the declaration *and* the call, in both
// directions — the spelling clang gives the same C prototype.
//
// clang-compiled C relies on the caller having cleaned the upper bits (Apple arm64 makes it
// the caller's job; x86-64 compilers assume it). Without the attribute the low byte is
// right and the rest of the register is left over: SDL read a computed colour component as
// a huge value, clamped it, and drew cyan instead of grey. A constant argument hides it,
// which is why nothing here saw it until a binding passed a computed one.
//
// Covers both declaration paths — a plain signature, and one with an aggregate in it,
// whose parameters are flattened and may be led by an `sret` pointer — and a newtype over
// a narrow integer, which crosses as its base.
func TestEmit_NarrowIntegersCrossExtended(t *testing.T) {
	t.Parallel()
	const src = `module main
newtype Channel = u8
struct Big { a: i64, b: i64, c: i64, d: i64 }
unsafe extern narrow: (a: u8, b: i8, c: u16, d: i16, e: i32, f: Channel) -> u8
unsafe extern narrow_signed: () -> i16
unsafe extern planned: (v: Big, k: u8, n: i8) -> Big
let main = () -> void => {
  let x: i64 = 0x123456789
  let v = unsafe { planned(Big { a: 1, b: 2, c: 3, d: 4 }, u8(x & 255), i8(x & 127)) }
  let r = unsafe { narrow(u8(x & 255), i8(x & 127), u16(x & 65535), i16(x & 32767), 5, Channel(u8(x & 255))) }
  println("${r} ${unsafe { narrow_signed() }} ${v.a}")
}
`
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	for _, target := range []abi.Target{abi.AArch64, abi.X86_64SysV} {
		out, err := NewForTarget(target).Emit(res, ep)
		if err != nil {
			t.Fatal(err)
		}
		ir := string(out)
		if !plannedCallExtends(ir) {
			t.Errorf("%s: the planned call lost its extensions:\n%s", target, narrowLines(ir))
		}
		for _, want := range []struct{ what, needle string }{
			{"the plain declaration", "declare zeroext i8 @narrow(i8 zeroext %p.a, i8 signext %p.b, i16 zeroext %p.c, i16 signext %p.d, i32 %p.e, i8 zeroext %p.f)"},
			{"the plain call", "call zeroext i8 @narrow(i8 zeroext"},
			{"a signed return", "declare signext i16 @narrow_signed()"},
			// Past the `sret` pointer and the flattened aggregate: the slot arithmetic.
			{"the planned declaration", "i8 zeroext %a1_0, i8 signext %a2_0)"},
		} {
			if !strings.Contains(ir, want.needle) {
				t.Errorf("%s: %s lost its extension; wanted a line containing %q:\n%s",
					target, want.what, want.needle, narrowLines(ir))
			}
		}
	}
}

// plannedCallExtends reports whether the call to `planned` carries both extensions.
func plannedCallExtends(ir string) bool {
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "call void @planned(") {
			return strings.Contains(line, "i8 zeroext %") && strings.Contains(line, "i8 signext %")
		}
	}
	return false
}

func narrowLines(ir string) string {
	var out []string
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "@narrow") || strings.Contains(line, "@planned") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}
