package driver

import (
	"fmt"
	"sort"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Closing the instantiation set — what a generic body's calls to *other* generics need.
//
// The typechecker records, per call site, the bindings it solved. Inside a generic body
// those bindings are written in terms of the enclosing body's own type variables:
// `unwrap<t>`'s call to `expect` records `expect<t=t>`, where the right-hand `t` is
// unwrap's. That is not a specialization — nothing can be emitted for it, a type variable
// having no representation — and lowering it directly is what produced
//
//	llvm: type variable "t" has no concrete type here
//
// for a prelude whose `unwrap` was written as `expect(self, "unwrap on a None")`.
//
// **The fix is composition, done once, here.** For each concrete specialization the
// program actually uses, walk its body: every generic call it contains is composed with
// that specialization's bindings, which turns `expect<t=t>` into `expect<t=i64>` — a real
// specialization that must be emitted even though no call site in the program names it.
// Composing transitively reaches what those bodies call in turn, so the set is closed to a
// fixpoint.
//
// **Why here and not in the backend**, which is where the failure surfaced: the ownership
// pass is run *per instantiation* (`OwnershipBySpec`) because whether a value is
// reference-counted is a property of the type argument. A specialization discovered after
// that pass would have no table of its own and would silently fall back to the
// program-wide one — analyzed generically, where a type variable is not managed, so a
// `t = string` body would emit no retains and no releases. That is the exact double-free
// this project already fixed once. Closing the set before ownership runs means every
// emitted specialization has a table computed at its own types.

// The closure's two bounds.
//
// **Polymorphic recursion does not terminate**, and cannot be made to: a function whose
// recursive call is at a *larger* type — `f<t>` calling `f<Box<t>>` — has infinitely many
// specializations, each needed by the last. Monomorphization is not defined for it, which
// is why every monomorphizing language refuses it (Rust reports it as a recursion-depth
// overflow). The alternative to a bound is not correctness, it is a compiler that
// allocates until it is killed.
//
// **Depth is the bound that does the work**, and a count alone is not enough — measured,
// not assumed. What diverges is the *type*: `Box<i64>`, `Box<Box<i64>>`, and so on, each
// binding one constructor deeper than the last. A plain count still terminates, but only
// after the set is enormous *and* each member is huge, so the cost of reaching it is
// quadratic in the cap — a 10000-count bound ran for minutes and passed a gigabyte of
// resident memory before firing, which is indistinguishable from the hang it was meant to
// prevent. Depth catches the same programs in a few dozen steps, each cheap, because it
// tests the thing that is actually growing.
//
// The count stays as a backstop for divergence that is *wide* rather than deep — many
// distinct shallow instantiations — which no known program produces but which depth would
// not catch.
const (
	maxSpecializationDepth = 24
	maxSpecializations     = 5000
)

// typeDepth is the constructor nesting of a type: `i64` is 1, `Box<i64>` 2,
// `Box<Box<i64>>` 3. Only what a binding can actually be built from is walked, which is
// the same set types.Substitute rebuilds.
func typeDepth(t types.Type) int {
	switch tt := t.(type) {
	case types.ParameterizedType:
		return 1 + maxDepth(tt.TypeArguments)
	case types.StaticArrayType:
		return 1 + typeDepth(tt.ElementType)
	case types.DynamicArrayType:
		return 1 + typeDepth(tt.ElementType)
	case types.TupleType:
		return 1 + maxDepth(tt.Elements)
	case types.WeakType:
		return 1 + typeDepth(tt.Inner)
	case types.RawPointerType:
		return 1 + typeDepth(tt.Pointee)
	case *types.LambdaType:
		if tt == nil {
			return 1
		}
		deepest := typeDepth(tt.ReturnType.Type)
		for _, p := range tt.Parameters {
			if d := typeDepth(p.Type); d > deepest {
				deepest = d
			}
		}
		return 1 + deepest
	}
	return 1
}

func maxDepth(ts []types.Type) int {
	deepest := 0
	for _, t := range ts {
		if d := typeDepth(t); d > deepest {
			deepest = d
		}
	}
	return deepest
}

// deepestBinding returns the nesting of the most deeply nested type in a substitution.
func deepestBinding(subst map[string]types.Type) int {
	deepest := 0
	for _, bound := range subst {
		if d := typeDepth(bound); d > deepest {
			deepest = d
		}
	}
	return deepest
}

// divergenceError is the one diagnostic both bounds produce: they are two detectors of the
// same condition, so a reader should not have to tell them apart.
func divergenceError(inst typetable.Instantiation) []diag.Diagnostic {
	return []diag.Diagnostic{{
		Location: inst.Func.GetLocation(),
		Severity: diag.SeverityError,
		Message: fmt.Sprintf(
			"generic function %q needs an unbounded number of specializations: each recursive call is at a *larger* type than the last (reached %s, nested %d deep), so the set is infinite and monomorphization cannot compile it. Make the recursive call at the same type, or put the recursive part behind a non-generic boundary",
			inst.Name, describeBinding(inst.Subst), deepestBinding(inst.Subst)),
	}}
}

// describeBinding renders the deepest binding for the divergence message, abbreviated.
//
// The type at the point of detection is two dozen constructors deep, and printing it in
// full buries the sentence that explains the problem under a line of `Box<Box<Box<…`.
// The reader needs the *shape* — which variable, growing in what — not the specific depth
// the bound happened to stop at.
func describeBinding(subst map[string]types.Type) string {
	var (
		name    string
		deepest int
	)
	for v, bound := range subst {
		if d := typeDepth(bound); d > deepest {
			name, deepest = v, d
		}
	}
	if name == "" {
		return "a growing type"
	}
	return fmt.Sprintf("`%s = %s`", name, abbreviateType(subst[name], 3))
}

// abbreviateType renders a type to at most depth levels, eliding the rest as `…`.
func abbreviateType(t types.Type, depth int) string {
	p, ok := t.(types.ParameterizedType)
	if !ok || len(p.TypeArguments) == 0 {
		return t.String()
	}
	if depth <= 0 {
		return "…"
	}
	return p.Name + "<" + abbreviateType(p.TypeArguments[0], depth-1) + ">"
}

// closeInstantiations expands res.Instantiations to its transitive closure under
// composition, returning a diagnostic if the set diverges.
func closeInstantiations(res *Result) []diag.Diagnostic {
	table := res.Instantiations
	if table == nil {
		return nil
	}
	// Seed with the specializations that are already concrete — the ones reached from
	// ordinary, non-generic code. A template cannot seed anything: it has no bindings to
	// compose *with*, which is the whole reason it is not emittable.
	seen := map[string]bool{}
	var worklist []typetable.Instantiation
	for _, inst := range table.Concrete() {
		if !seen[inst.Key()] {
			seen[inst.Key()] = true
			worklist = append(worklist, inst)
		}
	}
	for len(worklist) > 0 {
		current := worklist[0]
		worklist = worklist[1:]
		for _, call := range genericCallsIn(current.Func) {
			callee, ok := table.Get(call)
			if !ok {
				continue
			}
			composed := callee.Substituted(current.Subst, substituteTypeVars)
			if !composed.IsConcrete() {
				// A binding this specialization cannot settle. Reachable when a body
				// mentions a variable that is not one of its own — the front end reports
				// that (lyra-E031), so there is nothing useful to add here.
				continue
			}
			// Depth first, because it is the cheap test and the one that fires on the
			// shape that actually diverges — before the key of a hugely nested type is
			// ever built.
			if deepestBinding(composed.Subst) > maxSpecializationDepth {
				return divergenceError(composed)
			}
			if seen[composed.Key()] {
				continue
			}
			if len(seen) >= maxSpecializations {
				return divergenceError(composed)
			}
			seen[composed.Key()] = true
			table.Add(composed)
			worklist = append(worklist, composed)
		}
	}
	return nil
}

// genericCallsIn returns every call expression in a function's body, in a stable order.
//
// Stability matters because the closure's iteration order decides the order
// specializations are added, and that reaches the emitted module through
// `Instantiations.All()`. Sorting by source position keeps a build reproducible.
//
// Nested lambdas are included: a closure inside a generic body is lowered under the same
// substitution, so a generic call it makes needs the same composition.
func genericCallsIn(fn *ast.LambdaExpr) []*ast.FunctionCallExpr {
	if fn == nil || fn.Body == nil {
		return nil
	}
	var calls []*ast.FunctionCallExpr
	onExpr := func(e ast.Expression) bool {
		if call, ok := e.(*ast.FunctionCallExpr); ok {
			calls = append(calls, call)
		}
		return true
	}
	ast.WalkExpr(fn.Body, func(ast.Statement) bool { return true }, onExpr)
	sort.SliceStable(calls, func(i, j int) bool {
		a, b := calls[i].GetLocation(), calls[j].GetLocation()
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartCol < b.StartCol
	})
	return calls
}

// substituteTypeVars is the structural substitution the composition needs.
//
// It is `types.Substitute`, re-exported through a function value so Instantiation does not
// have to import it — typetable is below types' consumers in the dependency order, and the
// substitution walk is the backend's as much as anyone's.
func substituteTypeVars(t types.Type, subst map[string]types.Type) types.Type {
	return types.Substitute(t, subst)
}
