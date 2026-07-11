// Package llvm is the (in-progress) LLVM IR backend for Lyra. It lowers a typed
// program from pkg/driver to LLVM IR (built with github.com/llir/llvm), which
// llc/clang then compiles.
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
//  1. lowerType(t types.Type) — Lyra type → an llir `types.Type` (i8..i64/u* → iN,
//     f16/32/64 → half/float/double, bool → i1, struct → a struct type, data/sum
//     → a tagged union { tag, payload } per DATA_LAYOUT.md). `stack` values lower
//     by value, `shared` values to a pointer to a ref-counted box — see
//     ALLOCATION.md. The two docs compose: the sum-type layout is the payload; the
//     flavor decides inline vs boxed. layout.go/runtime.go provide the building
//     blocks — LLVMPrimitive, SharedBoxType, TagType, DataUnionType, SizeAndAlign,
//     and declareRuntime (wired into Emit) — for lowerType to dispatch over.
//  2. Replace lowerEntry's placeholder `ret` with real lowering of entry.Lambda's
//     body: constants, then arithmetic/calls, then let/if/blocks. Model mutable
//     locals as `alloca` + load/store (let mem2reg build SSA) rather than hand-
//     writing phi nodes.
//  3. Runtime shims: print, and the overflow trap for todo #2 (via
//     llvm.sadd.with.overflow); the builtin overflow-arithmetic methods
//     (typechecker/builtins.go) lower to two's-complement +/-/* and
//     llvm.{s,u}{add,sub}.sat.
package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"

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

// Emit lowers the program to LLVM IR text.
//
// SKELETON: only the entry-function shell is emitted, with a placeholder body.
// Replace lowerEntry's body with real lowering; grow the type/expression/
// statement lowering alongside it.
func (b *Backend) Emit(res *driver.Result, entry *driver.EntryPoint) ([]byte, error) {
	if res == nil || res.Program == nil || entry == nil {
		return nil, fmt.Errorf("llvm: nil program or entry point")
	}
	m := ir.NewModule()
	declareRuntime(m)
	b.lowerEntry(m, entry)
	return []byte(m.String()), nil
}

// lowerEntry defines `@main`. The process exit code is the entry function's
// return value (0 for a void entry).
//
// TODO(backend): lower entry.Lambda.Body here. For an i64 entry the return value
// is the body's value; for a void entry, `ret i64 0`. Today it always returns 0,
// so a built program is a valid no-op that exits 0 — proof the toolchain path
// works, not a real translation.
func (b *Backend) lowerEntry(m *ir.Module, entry *driver.EntryPoint) {
	_ = entry // will drive the body lowering
	fn := m.NewFunc("main", types.I64)
	entryBlock := fn.NewBlock("entry")
	entryBlock.NewRet(constant.NewInt(types.I64, 0))
}
