// Package backend defines the contract between Lyra's front-end (a typed program
// from pkg/driver) and a code generator. A concrete backend — e.g.
// pkg/backend/llvm — implements Backend to lower an analyzed, error-free program
// to a target artifact.
package backend

import "github.com/Lyra-Language/lyra/pkg/driver"

// Backend lowers a fully-typed, error-free program to a target artifact.
//
// Emit is called by the compiler (cmd/lyrac) only after driver.Analyze reports
// no errors and driver.ResolveEntryPoint succeeds, so an implementation may
// assume the program type-checks and entry is valid. Everything a backend needs
// is reachable from res: the AST (res.Program), resolved expression types
// (res.TypeTable), symbols (res.SymbolTable), and trait-method dispatch
// (res.MethodTable).
type Backend interface {
	// Name identifies the target, e.g. "llvm".
	Name() string
	// Emit lowers the program to the target's textual or binary form.
	Emit(res *driver.Result, entry *driver.EntryPoint) ([]byte, error)
}
