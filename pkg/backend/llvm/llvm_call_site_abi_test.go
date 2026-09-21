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
