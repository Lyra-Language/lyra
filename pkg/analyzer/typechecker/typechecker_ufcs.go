package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// UFCS — method syntax for a free function. `m.unwrap_or(0)` resolves to the free
// function `unwrap_or(m, 0)` when that function opts in by **naming its first parameter
// `self`**. Every other function stays call-only, so adding a helper to a module cannot
// change what `x.f()` means elsewhere in it.
//
// **A matching call is rewritten in place** (desugarUFCSCall): the receiver becomes
// Arguments[0] and the callee becomes an ordinary identifier, so everything after this
// point — generic solving, purity, ownership, captures, the backend — sees a direct call
// and needs no notion of UFCS at all.
//
// That rewrite is the load-bearing decision, not a shortcut. The alternative, keeping the
// member shape and teaching each consumer about an implicit receiver, has a failure mode
// with no diagnostic: the purity pass indexes arguments *positionally* against the
// declaration's parameters (`callableParams`, `checker/purity.go`), so a receiver sitting
// outside Arguments shifts every index by one and each callback is checked against the
// declared bound of the parameter to its right — and a function-typed argument satisfies
// the wrong function-typed parameter quietly. Trait methods, whose receiver genuinely is
// implicit, pay exactly that tax through `methodArgumentAt`. Desugaring means arguments
// and parameters line up by construction.
//
// Mutating a call mid-check has precedent: applyDefaultArguments fills omitted trailing
// arguments from the declaration for the same reason — so that the checker and the backend
// both see a call that passes everything explicitly. This is that move at the front of the
// list, and it is structurally idempotent: once Function is an IdentifierExpr, this rung is
// never reached for that node again.
type ufcsResult int

const (
	// ufcsNoMatch: not a UFCS call. The caller tries the next rung.
	ufcsNoMatch ufcsResult = iota
	// ufcsMatch: the returned function is the callee.
	ufcsMatch
	// ufcsRefused: it *is* a UFCS call and it is rejected, with the diagnostic already
	// reported. The caller must not fall through, or a second, worse error follows.
	ufcsRefused
)

// ufcsCandidate resolves methodName against the free functions the calling file can see,
// and reports whether it may be called on a receiver of objType.
func (tc *TypeChecker) ufcsCandidate(objType types.Type, methodName string, member *ast.MemberExpr, call *ast.FunctionCallExpr) (*ast.LambdaExpr, ufcsResult) {
	loc := member.GetLocation()
	// Resolved as the *calling file* sees the name. Privacy is structural — another
	// module's private function lives under a module-qualified key — so a private
	// callee is simply not found here, exactly as it is for a bare call.
	fn, ok := tc.symTable.LookupFunctionFrom(methodName, loc)
	if !ok {
		return nil, ufcsNoMatch
	}
	recv, ok := ufcsReceiverParam(fn)
	if !ok {
		return nil, ufcsNoMatch
	}
	if !tc.ufcsImported(fn, loc) {
		// Reachability follows the file's own import list, so what a file may call is
		// decided by what it imports rather than by what some other file imported.
		// Not an error of its own: fall through to "has no method", which carries a
		// hint naming the import (see ufcsHint).
		return nil, ufcsNoMatch
	}
	// An `own` receiver is refused rather than supported: `own` *transfers*, and the
	// receiver syntax hides the transfer at the call site. Use-after-move (lyra-E019)
	// catches a later read either way, so this is about the move staying legible —
	// which is the same reason this language makes costs visible elsewhere.
	if paramOwnsArgument(recv.TypeModifier) {
		rest := ""
		if len(call.Arguments) > 0 {
			rest = ", …"
		}
		tc.addError(loc, SeverityError,
			"%s takes an `own` receiver, so it cannot be called method-style — write %s(%s%s) so the move is visible",
			methodName, methodName, exprText(member.Object), rest)
		return nil, ufcsRefused
	}
	// Does the receiver fit the `self` parameter? Same predicate trait dispatch uses to
	// match an impl against a receiver, so "this receiver matches that declaration"
	// has one definition: the function's own type variables act as wildcards and bind
	// to the receiver's subterms, and everything else must match structurally.
	if !unifyGenericTarget(recv.Type, objType, lambdaTypeVars(fn), map[string]types.Type{}) {
		return nil, ufcsNoMatch
	}
	tc.noteUFCSModule(fn, loc)
	return fn, ufcsMatch
}

// noteUFCSModule records that the file at loc reached fn's module through a UFCS call,
// so the unused-import check does not advise deleting the import that permitted it.
// Only a cross-module call is recorded: a file's own module and the prelude are reachable
// with no import, so neither can be the reason one exists.
func (tc *TypeChecker) noteUFCSModule(fn *ast.LambdaExpr, loc ast.Location) {
	callee := tc.symTable.ModuleOfFile[fn.GetLocation().File]
	if callee == "" || callee == tc.symTable.ModuleOfFile[loc.File] || callee == tc.symTable.PreludeModule {
		return
	}
	if tc.ufcsModules == nil {
		tc.ufcsModules = map[string]map[string]bool{}
	}
	if tc.ufcsModules[loc.File] == nil {
		tc.ufcsModules[loc.File] = map[string]bool{}
	}
	tc.ufcsModules[loc.File][callee] = true
}

// ufcsReceiverParam returns the function's `self` parameter, and whether it has one.
//
// The name is the opt-in, and `self` is already the word for a receiver in a trait impl,
// so the language gains no second one.
//
// **The test is on the declared parameter, never on whether the function has clauses.**
// A multi-clause function is a candidate like any other: its head must bind plain names
// (clauseScrutinee enforces that — the clauses do the destructuring), so it can name one
// `self`, and by the time a body is lowered the clauses are an ordinary match. Excluding
// them by `len(LambdaClauses) > 0` would have made membership depend on *when* this runs:
// desugarClauses consumes the clauses, so the same function would be a candidate after
// its declaration was checked and not before.
func ufcsReceiverParam(fn *ast.LambdaExpr) (*ast.Parameter, bool) {
	if fn == nil || len(fn.Parameters) == 0 {
		return nil, false
	}
	recv := &fn.Parameters[0]
	if recv.Pattern == nil || recv.Pattern.GetName() != "self" {
		return nil, false
	}
	return recv, true
}

// ufcsImported reports whether the file at loc may reach fn's module: its own module
// needs no import, the prelude is implicitly imported everywhere, and anything else must
// be named in the file's imports.
func (tc *TypeChecker) ufcsImported(fn *ast.LambdaExpr, loc ast.Location) bool {
	return ufcsImportedIn(tc.symTable, fn, loc)
}

func ufcsImportedIn(symTable *symbols.SymbolTable, fn *ast.LambdaExpr, loc ast.Location) bool {
	callee := symTable.ModuleOfFile[fn.GetLocation().File]
	if callee == symTable.ModuleOfFile[loc.File] {
		return true
	}
	if symTable.PreludeModule != "" && callee == symTable.PreludeModule {
		return true
	}
	for _, imp := range symTable.ImportsFor(loc.File) {
		if imp.Path == callee {
			return true
		}
	}
	return false
}

// UFCSCallable reports whether fn may be called method-style on a receiver of objType
// from the file at loc.
//
// It is the same rule the UFCS rung applies, exported so the language server can offer
// exactly the calls that will compile. Two copies of "is this a method on that" would
// drift immediately, and the direction of the drift is the bad one: a completion list
// that suggests calls the checker then rejects.
//
// The `own` receiver is *not* rejected here. Refusing it is a diagnostic the checker
// owns, and a completion list that silently omits the function would leave the user with
// no way to learn why — the error naming the call form is the better teacher.
func UFCSCallable(symTable *symbols.SymbolTable, fn *ast.LambdaExpr, objType types.Type, loc ast.Location) bool {
	recv, ok := ufcsReceiverParam(fn)
	if !ok || symTable == nil || objType == nil {
		return false
	}
	if !ufcsImportedIn(symTable, fn, loc) {
		return false
	}
	return unifyGenericTarget(recv.Type, objType, lambdaTypeVars(fn), map[string]types.Type{})
}

// desugarUFCSCall rewrites `recv.f(a, b)` into `f(recv, a, b)`.
//
// The synthesized callee carries the *property's* location, not the whole member
// expression's, so a diagnostic about the callee underlines `f` and the editor's
// go-to-definition on it reaches the free function. The receiver keeps its own node and
// location, so an argument-type error about it points where the reader wrote it. The call
// node itself is untouched, which matters: instantiations are keyed by it.
func desugarUFCSCall(member *ast.MemberExpr, call *ast.FunctionCallExpr) {
	callee := &ast.IdentifierExpr{Name: member.Property.Name}
	callee.Location = member.Property.GetLocation()
	call.Arguments = append([]ast.Expression{member.Object}, call.Arguments...)
	call.Function = callee
}

// ufcsHint explains a failed member call when a free function of that name does exist —
// the two near-misses worth naming, since "has no field or method" alone sends the reader
// looking for a method that was never going to be there.
func (tc *TypeChecker) ufcsHint(methodName string, loc ast.Location) string {
	fn, ok := tc.symTable.LookupFunctionFrom(methodName, loc)
	if !ok {
		return ""
	}
	if _, isReceiver := ufcsReceiverParam(fn); !isReceiver {
		return fmt.Sprintf(
			"; %s is a free function — name its first parameter `self` to allow method syntax, or call %s(…)",
			methodName, methodName)
	}
	if !tc.ufcsImported(fn, loc) {
		return fmt.Sprintf("; %s takes a `self` receiver in module %q — import it to call it method-style",
			methodName, tc.symTable.ModuleOfFile[fn.GetLocation().File])
	}
	// It is a receiver function this file can see, so the receiver's type is what did
	// not fit; the message the caller is about to print already names that type.
	return ""
}

// exprText renders a receiver expression for a diagnostic that suggests the call form.
// Only the shapes worth naming get their source spelling; anything else becomes a
// placeholder rather than a Go struct dump (GetName on a literal still renders as one —
// see todo.md's "Known bugs").
func exprText(e ast.Expression) string {
	switch x := e.(type) {
	case *ast.IdentifierExpr:
		return x.Name
	case *ast.MemberExpr:
		return fmt.Sprintf("%s.%s", exprText(x.Object), x.Property.Name)
	default:
		return "receiver"
	}
}
