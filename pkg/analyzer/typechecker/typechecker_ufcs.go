package typechecker

import (
	"fmt"
	"sort"
	"strings"

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
	// Every declaration of the name this file can reach, narrowed to the ones that accept
	// this receiver — not the one the name's key happens to resolve to. See ufcsFunction.
	fn, res := tc.ufcsFunction(methodName, objType, member, loc)
	if res != ufcsMatch {
		return nil, res
	}
	recv, ok := ufcsReceiverParam(fn)
	if !ok {
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
	tc.noteUFCSModule(fn, loc)
	tc.noteResolvedCallee(call, fn)
	return fn, ufcsMatch
}

// callViaUFCS runs the UFCS rung: resolve methodName against the free functions the
// calling file can see and, on a match, rewrite the call and check it as the direct call
// it now is. The second result is false only when nothing matched — the caller's cue to
// try its next rung — so a refusal, whose diagnostic is already reported, does not fall
// through into a second and worse one.
//
// Checked against the *declaration*, not a signature: a generic callee's type variables
// are free until this call's arguments solve them, and solving is also what records the
// specialization the backend emits. With the receiver prepended, a `Maybe<i64>` receiver
// binds `t` exactly as it does when the call is written `map(m, f)`.
func (tc *TypeChecker) callViaUFCS(objType types.Type, methodName string, member *ast.MemberExpr, call *ast.FunctionCallExpr, pinReceiver func()) (types.Type, bool) {
	fn, res := tc.ufcsCandidate(objType, methodName, member, call)
	switch res {
	case ufcsNoMatch:
		return nil, false
	case ufcsRefused:
		return nil, true
	}
	// **An untyped literal receiver is settled here or not at all**, and which it is turns
	// on the candidate. Against a concrete `self` — the prelude's `(2.5).to_fixed(4)` — the
	// literal has to be its default width before the argument is checked, exactly as it
	// always was. Against a `self` mentioning a type variable, settling it *is* the bug:
	// the receiver becomes argument 0 below, and a call solves its variables from every
	// argument at once, so a receiver pinned to `i64` bound `t` before `w: u8` was
	// consulted and the call reported "cannot infer type variable t". Left untyped it
	// adopts what the other arguments bind, and falls back to its default only if nothing
	// does — the rule an untyped literal argument has followed since 08/22, now reaching
	// the one operand that was exempt from it.
	if recv, ok := ufcsReceiverParam(fn); !ok || !receiverIsGeneric(recv) {
		pinReceiver()
	}
	desugarUFCSCall(member, call)
	// The same E011 check the bare-call path makes, because this *is* the bare call now —
	// and the rung a reader would assume shares it. It did not: `unsafe` on a free
	// function was enforced at `data(xs)` and silently not at `xs.data()`, so the method
	// spelling of the standard library's own FFI helpers was the way around the keyword.
	// Latent since UFCS landed, and invisible until the first unsafe function with a
	// `self` receiver (`std.ffi`'s `data`) — hazard 8, in a resolution ladder rather than
	// a switch.
	tc.requireUnsafeCall(methodName, fn, call)
	return tc.inferLambdaCall(methodName, fn, call), true
}

// ufcsFunction picks the declaration a receiver of objType calls, from **every**
// declaration of methodName the calling file can reach.
//
// The obvious implementation — resolve the name to one declaration, then ask whether that
// one takes this receiver — is wrong, and wrong in a way that removes methods without
// saying so. A name resolves through a single key, and the candidates for a method call
// can be in different modules: when the prelude gained a `map` for `Result`, it took the
// bare key, `std.maybe`'s `map` for `Maybe` moved to a module-qualified one, and every
// `m.map(f)` in every program began reporting "member access on non-struct type
// Maybe<i64>" (08/04). Nothing was ambiguous and nothing was shadowed in the sense the
// reader would recognise; one lookup simply could not see the other declaration.
//
// So candidates are gathered by name (`FunctionsNamed`) and filtered by the three things
// that actually decide the call:
//
//   - it must **take a receiver** — the `self` opt-in, or it is call-only;
//   - it must be **reachable** — the file's own module and the prelude need no import,
//     anything else must be named in this file's imports, so what a file may call stays a
//     property of its own import list;
//   - it must **accept this receiver** — `receiverAccepts`, the predicate overload
//     resolution and trait dispatch both use.
//
// A candidate failing any of these is not an error here: the call falls through to "has no
// method", whose hint names the near-miss (`ufcsHint`).
//
// **A local declaration wins a tie.** Two reachable candidates accepting one receiver is
// otherwise ambiguous, and the module doing the asking is the one whose intent is least in
// doubt — the same rule the scope chain applies everywhere else. A tie that survives that
// is reported rather than broken arbitrarily, since the alternative is a call whose meaning
// depends on which module the resolver happened to visit first.
func (tc *TypeChecker) ufcsFunction(methodName string, objType types.Type, member *ast.MemberExpr, loc ast.Location) (*ast.LambdaExpr, ufcsResult) {
	var matches []*ast.LambdaExpr
	for _, fn := range tc.symTable.FunctionsNamed(methodName) {
		if _, isReceiver := ufcsReceiverParam(fn); !isReceiver {
			continue
		}
		if !tc.ufcsImported(fn, loc) {
			continue
		}
		if !receiverAcceptsValue(fn, member.Object, objType) {
			continue
		}
		matches = append(matches, fn)
	}
	switch len(matches) {
	case 0:
		return nil, ufcsNoMatch
	case 1:
		return matches[0], ufcsMatch
	}
	if local := tc.localCandidate(matches, loc); local != nil {
		return local, ufcsMatch
	}
	tc.addError(member.GetLocation(), SeverityError,
		"%s.%s is ambiguous: %s each define a %s taking this receiver — name the one you mean through its module, e.g. `%s.%s(%s, …)`",
		exprText(member.Object), methodName, tc.modulesOf(matches), methodName,
		tc.namespaceHint(matches, loc), methodName, exprText(member.Object))
	return nil, ufcsRefused
}

// namespaceHint picks a module qualifier the reader can actually write, for the ambiguity
// message: the namespace one of the candidates is imported under. The prelude has none —
// it is reachable precisely because nothing names it — so a candidate from there can never
// supply the hint, and one that is imported always can.
func (tc *TypeChecker) namespaceHint(matches []*ast.LambdaExpr, loc ast.Location) string {
	for _, fn := range matches {
		module := tc.symTable.ModuleOfFile[fn.GetLocation().File]
		if module == "" || module == tc.symTable.PreludeModule {
			continue
		}
		for _, imp := range tc.symTable.ImportsFor(loc.File) {
			if imp.Path == module && imp.IsNamespace() {
				return imp.Alias
			}
		}
		if idx := strings.LastIndex(module, "."); idx >= 0 {
			return module[idx+1:]
		}
		return module
	}
	return "module"
}

// localCandidate returns the one candidate declared in the asking file's own module, when
// exactly one is — the tiebreak ufcsFunction describes.
func (tc *TypeChecker) localCandidate(matches []*ast.LambdaExpr, loc ast.Location) *ast.LambdaExpr {
	asking := tc.symTable.ModuleOfFile[loc.File]
	var found *ast.LambdaExpr
	for _, fn := range matches {
		if tc.symTable.ModuleOfFile[fn.GetLocation().File] != asking {
			continue
		}
		if found != nil {
			return nil // two in one module: registration would have refused this
		}
		found = fn
	}
	return found
}

// modulesOf names the modules a set of candidates was declared in, for the ambiguity
// message. The entry module has no path to print, so it is named for what it is.
func (tc *TypeChecker) modulesOf(matches []*ast.LambdaExpr) string {
	names := make([]string, 0, len(matches))
	for _, fn := range matches {
		module := tc.symTable.ModuleOfFile[fn.GetLocation().File]
		if module == "" {
			module = "this file"
		}
		names = append(names, module)
	}
	sort.Strings(names)
	return strings.Join(names, " and ")
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

// receiverIsGeneric reports whether a `self` parameter's declared type mentions a type
// variable — `self: t` and `self: Maybe<t>` do, `self: string` does not. It is the question
// "can this call still settle its receiver's type?", which is what decides whether an
// untyped literal receiver may be deferred.
func receiverIsGeneric(recv *ast.Parameter) bool {
	vars := map[string]bool{}
	collectTypeVars(recv.Type, vars)
	return len(vars) > 0
}

// ufcsReceiverParam returns the function's `self` parameter, and whether it has one.
//
// The name is the opt-in, and `self` is already the word for a receiver in a trait impl,
// so the language gains no second one.
//
// It defers to ast.ReceiverParam, which is the same question overload registration asks
// when deciding whether two same-named declarations may coexist. The two must agree:
// a pair admitted as an overload set that this rung then refused to treat as methods
// would be a name no call site could reach.
//
// **The test is on the declared parameter, never on whether the function has clauses.**
// A multi-clause function is a candidate like any other: its head must bind plain names
// (clauseScrutinee enforces that — the clauses do the destructuring), so it can name one
// `self`, and by the time a body is lowered the clauses are an ordinary match. Excluding
// them by `len(LambdaClauses) > 0` would have made membership depend on *when* this runs:
// desugarClauses consumes the clauses, so the same function would be a candidate after
// its declaration was checked and not before.
func ufcsReceiverParam(fn *ast.LambdaExpr) (*ast.Parameter, bool) {
	return ast.ReceiverParam(fn)
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
func (tc *TypeChecker) ufcsHint(methodName string, recv types.Type, loc ast.Location) string {
	// **The fixed-array case is checked first, because it applies to both shapes and
	// is the only one that names an actual edit.** A fixed-array *binding* reaching a
	// `[]t` combinator is refused, and deliberately: the value already exists as a
	// stack `[N]T` while a `[]T` is a heap box, so widening it at a call would allocate
	// where nothing asked it to. (A *literal* receiver is admitted since 08/28 — it has
	// no prior shape to widen, so it is simply built as the `[]T` the receiver asks
	// for, and never reaches this hint.) Left to the branches below, an overloaded name
	// answers "map takes DynamicArray<t>, Maybe<t>, Result<t, e>", which is true and
	// still leaves the reader to work out that an annotation is the fix.
	if hint, ok := tc.fixedArrayHint(methodName, recv, loc); ok {
		return hint
	}
	// An overloaded name reaches here only when *no* member accepted the receiver, so
	// the useful thing to say is which receivers there are — the reader picked a real
	// method and applied it to the wrong type, and "has no method" alone reads as
	// though the name does not exist.
	if set, ok := tc.symTable.LookupOverloadsFrom(methodName, loc); ok {
		return fmt.Sprintf("; %s is overloaded on its receiver and takes %s",
			methodName, overloadReceiverTypes(set))
	}
	fn, ok := tc.symTable.LookupFunctionFrom(methodName, loc)
	if !ok {
		// Resolution is strict — a declaration in a module this file never imported, or
		// one reachable only through a namespace import, resolves to nothing from here.
		// The hint has to name it anyway, so the candidate is found by scanning the
		// declarations, the same set the UFCS rung itself considers.
		for _, candidate := range tc.symTable.FunctionsNamed(methodName) {
			if _, isRecv := ufcsReceiverParam(candidate); !isRecv {
				continue
			}
			if !tc.ufcsImported(candidate, loc) {
				return fmt.Sprintf("; %s takes a `self` receiver in module %q — import it to call it method-style",
					methodName, tc.symTable.ModuleOfFile[candidate.GetLocation().File])
			}
			fn, ok = candidate, true
			break
		}
		if !ok {
			return ""
		}
	}
	param, isReceiver := ufcsReceiverParam(fn)
	if !isReceiver {
		return fmt.Sprintf(
			"; %s is a free function — name its first parameter `self` to allow method syntax, or call %s(…)",
			methodName, methodName)
	}
	// Reachable because resolution's export rung answers for a module this file never
	// imported — which is exactly the case where naming the import is the hint.
	if !tc.ufcsImported(fn, loc) {
		return fmt.Sprintf("; %s takes a `self` receiver in module %q — import it to call it method-style",
			methodName, tc.symTable.ModuleOfFile[fn.GetLocation().File])
	}
	// It is a receiver function this file can see, so the receiver's *type* is what did
	// not fit — and the caller's message names only the type that failed, never the one
	// that would have worked. An overloaded name has said what it takes since it existed
	// (see above); a single-declaration one said nothing, which is the difference between
	// naming the symptom and naming the fix.
	if !isReceiver || param.Type == nil {
		return ""
	}
	want := tc.resolveTypeIfKnown(param.Type, fn.GetLocation())
	if want == nil {
		return ""
	}
	return fmt.Sprintf("; %s takes %s", methodName, want)
}

// fixedArrayHint names the edit for the one mismatch a reader cannot guess from the
// types alone: a fixed-array receiver against a combinator that takes a dynamic one.
//
// It fires for a *binding* (`let xs = [1, 2, 3]`); a literal receiver is admitted
// upstream and never arrives here.
//
// Keyed on *any* declaration of the name taking a dynamic array, so it covers the
// overloaded shape (`map`, declared for `[]t`, `Maybe<t>` and `Result<t, e>`) and the
// single one (`join`) with the same sentence. The annotation is spelled in source syntax —
// `[]string`, not `DynamicArray<string>` — because it is code the reader is about to type.
func (tc *TypeChecker) fixedArrayHint(methodName string, recv types.Type, loc ast.Location) (string, bool) {
	sa, isStatic := recv.(types.StaticArrayType)
	if !isStatic {
		return "", false
	}
	for _, fn := range tc.symTable.FunctionsNamed(methodName) {
		param, isReceiver := ufcsReceiverParam(fn)
		if !isReceiver || param.Type == nil || !tc.ufcsImported(fn, loc) {
			continue
		}
		if _, wantsDyn := tc.resolveTypeIfKnown(param.Type, fn.GetLocation()).(types.DynamicArrayType); !wantsDyn {
			continue
		}
		// The element is **defaulted** before it is named: an unannotated `[1, 2, 3]`
		// has untyped elements, which render as "integer literal" — a phrase, not a
		// type, so the suggested annotation would not compile. Same rule the generated
		// documentation follows: a spelling offered to a reader has to parse.
		return fmt.Sprintf(
			"; %s takes a dynamic array — annotate the value as `[]%s` (a `[%d]T` value is a fixed array, and widening it would allocate)",
			methodName, promoteToDefault(sa.ElementType), sa.Size), true
	}
	return "", false
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
