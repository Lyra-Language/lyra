// Package llvm is the (in-progress) LLVM IR backend for Lyra. It lowers a typed
// program from pkg/driver to textual LLVM IR, which llc/clang then compiles.
//
// # Status: skeleton
//
// Emit currently produces a minimal, valid module that defines the entry point
// with a PLACEHOLDER body (it returns 0 regardless of the source). The real
// lowering is not implemented yet — this package exists to give that work a home
// with the wiring already in place (cmd/lyrac calls it; the Backend contract is
// satisfied; a smoke test pins the output shape).
//
// # Where to build
//
// The lowering grows out from lowerEntry in roughly this order (see
// lyra/todo.md's backend section):
//
//  1. lowerType(t types.Type) — Lyra type → LLVM type (i8..i64/u* → iN, f16/32/64
//     → half/float/double, bool → i1, struct → named %T, data/sum → a tagged
//     union { tag, payload } per DATA_LAYOUT.md). `stack` values lower by value,
//     `shared` values to a `ptr` to a ref-counted box — see ALLOCATION.md for the
//     full representation (retain/release, ownership modifiers, arena, runtime
//     shims). The two docs compose: the sum-type layout is the payload; the flavor
//     decides inline vs boxed.
//  2. Replace lowerEntry's placeholder `ret` with real lowering of
//     entry.Lambda's body: constants, then arithmetic/calls, then let/if/blocks.
//  3. Runtime shims: print, and the overflow trap for todo #2 (via
//     llvm.sadd.with.overflow); the builtin overflow-arithmetic methods
//     (typechecker/builtins.go) lower to two's-complement +/-/* and
//     llvm.{s,u}{add,sub}.sat.
//
// The IR here is assembled as text to keep the skeleton dependency-free. A
// natural next step is to swap in github.com/llir/llvm for a structured IR
// builder (SSA values, basic blocks, verification) instead of string assembly.
package llvm

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/backend"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Backend is the LLVM IR code generator.
type Backend struct{}

// New returns an LLVM backend.
func New() *Backend { return &Backend{} }

// Compile-time assertion that Backend satisfies the contract.
var _ backend.Backend = (*Backend)(nil)

// Name identifies the target.
func (*Backend) Name() string { return "llvm" }

// Emit lowers the program to textual LLVM IR.
//
// SKELETON: only the entry-function shell is emitted, with a placeholder body.
// Replace lowerEntry's body with real lowering; grow the type/expression/
// statement lowering alongside it.
func (b *Backend) Emit(res *driver.Result, entry *driver.EntryPoint) ([]byte, error) {
	if res == nil || res.Program == nil || entry == nil {
		return nil, fmt.Errorf("llvm: nil program or entry point")
	}
	var m module
	m.comment("Lyra -> LLVM IR (skeleton output; body lowering not implemented)")
	m.comment(fmt.Sprintf("source declares %d top-level statement(s)", len(res.Program.Statements)))
	m.blank()
	b.lowerEntry(&m, entry)
	return []byte(m.String()), nil
}

// lowerEntry emits the `main` definition. The process exit code is the entry
// function's return value (0 for a void entry).
//
// TODO(backend): lower entry.Lambda.Body here. For an i64 entry the return value
// is the body's value; for a void entry, `ret i64 0`. Today it always returns 0,
// so a built program is a valid no-op that exits 0 — proof the toolchain path
// works, not a real translation.
func (b *Backend) lowerEntry(m *module, entry *driver.EntryPoint) {
	_ = entry // will drive the body lowering
	m.line("define i64 @main() {")
	m.line("entry:")
	m.line("  ret i64 0")
	m.line("}")
}

// module accumulates LLVM IR text. A structured IR library (llir/llvm) would
// replace this when the lowering outgrows string assembly.
type module struct{ b strings.Builder }

func (m *module) line(s string)    { m.b.WriteString(s); m.b.WriteByte('\n') }
func (m *module) comment(s string) { m.line("; " + s) }
func (m *module) blank()           { m.b.WriteByte('\n') }
func (m *module) String() string   { return m.b.String() }
