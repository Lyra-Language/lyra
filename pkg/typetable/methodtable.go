package typetable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// MethodTable maps a call-site expression — one whose Function is a
// MemberExpr or TraitMethodPathExpr that the type-checker resolved to a
// trait-impl method — to the specific *ast.TraitMethodImpl it dispatches to.
// It is populated by the type-checker during call resolution and consulted by
// later passes (e.g. the purity checker) that need to know which method body
// a given call actually invokes, without re-deriving dispatch themselves.
type MethodTable struct {
	entries     map[*ast.FunctionCallExpr]*ast.TraitMethodImpl
	resolutions map[*ast.FunctionCallExpr]Resolution
	boundCalls  map[*ast.FunctionCallExpr]BoundMethodRef
	builtins    map[*ast.FunctionCallExpr]bool
	// The subset of `builtins` that put a value on the heap — `s.slice(…)` and
	// nothing else today. Separate from `builtins` rather than a value on it,
	// because "is a builtin" and "allocates" are asked by different passes.
	builtinAllocs map[*ast.FunctionCallExpr]bool
	// boundCandidates[call][concreteType] is the impl a `where`-bound call resolves to
	// once a specialization fixes the receiver's type variable. See SetBoundCandidates.
	boundCandidates map[*ast.FunctionCallExpr]map[string]Resolution
	// operatorResolutions[expr] is the `Ord` impl a comparison operator dispatches to.
	operatorResolutions map[ast.Expression]Resolution
	// operatorCandidates[expr][concreteType] is the impl an operator dispatches to once
	// a specialization fixes a type-variable operand. See SetOperatorCandidates.
	operatorCandidates map[ast.Expression]map[string]Resolution
}

// BoundMethodRef names a trait method reached by *abstract* dispatch — a call on
// a value of bare type-parameter type resolved through a `where` bound (e.g.
// `self.value.show()` with `t: Show`). There is no single concrete impl, so this
// records only the trait and method name; a consumer (the purity checker) joins
// over every impl of that trait method.
type BoundMethodRef struct {
	Trait  string
	Method string
}

func NewMethodTable() *MethodTable {
	return &MethodTable{
		entries:       make(map[*ast.FunctionCallExpr]*ast.TraitMethodImpl),
		boundCalls:    make(map[*ast.FunctionCallExpr]BoundMethodRef),
		builtins:      make(map[*ast.FunctionCallExpr]bool),
		builtinAllocs: make(map[*ast.FunctionCallExpr]bool),
	}
}

// SetBuiltinMethod records that dispatch resolved this call to a **compiler
// builtin method** — `x.wrapping_mul(y)`, `x.floor()`, `xs.len()`, `s.weak()` —
// rather than to a user function or a trait impl.
//
// It exists because the name alone cannot say so, and guessing wrong is expensive
// in both directions. A builtin method call reaches a consumer as a `MemberExpr`
// callee, so the purity pass sees the dotted name `x.wrapping_mul`, finds it in no
// table, and falls to the unresolved-callee default — `AllEffects`. That default is
// right for a genuinely unknown callee and badly wrong here: every builtin method
// is pure arithmetic, so it made `wrapping_mul` and friends unusable from any
// `pure`, `det` or `noalloc` function. Which is precisely the arithmetic that wants
// them — a PRNG, a hash, a checksum — and precisely the functions that want to be
// `det`.
//
// The alternative was to re-derive "is this a builtin method?" in the checker from
// the property name, which is a second copy of a question the typechecker has
// already answered definitively (CLAUDE.md rules 8 and 9), and one that cannot see
// the receiver's type — so a user's own `wrapping_mul` on their own type would have
// been silently declared pure.
//
// **`allocates` is part of the resolution, not a property of the name.** Almost
// every builtin method is arithmetic over a scalar and allocates nothing, which is
// the whole point above — but `s.slice(a, b)` builds a fresh ref-counted box,
// because a substring of a ref-counted string cannot borrow its parent's bytes
// (backend/llvm/string_methods.go). Recording it here rather than letting each
// consumer test the name keeps this the one place that knows, which matters
// because there are *three* copies of the "what does this call call?" ladder in
// the purity pass and a builtin that allocates is invisible to all of them
// otherwise: `noalloc` would accept a function that allocates on every call.
func (t *MethodTable) SetBuiltinMethod(call *ast.FunctionCallExpr, allocates bool) {
	if t == nil {
		return
	}
	t.builtins[call] = true
	if allocates {
		t.builtinAllocs[call] = true
	}
}

// IsBuiltinMethod reports whether this call resolved to a compiler builtin method.
// Nil-receiver-safe, like Get.
func (t *MethodTable) IsBuiltinMethod(call *ast.FunctionCallExpr) bool {
	return t != nil && t.builtins[call]
}

// BuiltinMethodAllocates reports whether this builtin-method call puts a value on
// the heap. Only `s.slice(…)` does today. Nil-receiver-safe, like Get.
func (t *MethodTable) BuiltinMethodAllocates(call *ast.FunctionCallExpr) bool {
	return t != nil && t.builtinAllocs[call]
}

func (t *MethodTable) Set(call *ast.FunctionCallExpr, method *ast.TraitMethodImpl) {
	t.entries[call] = method
}

// Get is nil-receiver-safe (returns no match) so callers that don't have a
// MethodTable available — e.g. tests checking purity without running the
// typechecker first — can pass nil instead of needing a special case.
// Resolution is everything dispatch worked out about a method call: which impl won,
// which method within it, and the trait's signature with Self substituted by the
// *concrete receiver* type.
//
// The signature is the part the backend cannot recompute: substituting Self and the
// trait's own type parameters is dispatch's job, and duplicating it in codegen would be
// a second implementation of "what is this method's type" free to disagree with the one
// that type-checked the call.
type Resolution struct {
	Impl      *ast.TraitImplStmt
	Method    *ast.TraitMethodImpl
	Signature *types.LambdaType
	// Bindings maps a generic impl's type variables to the concrete types they
	// unified with at this call site — `impl Unwrap<t> for Maybe<t>` dispatched on a
	// `Maybe<i64>` binds t→i64. Empty for a non-generic impl.
	//
	// Dispatch has always computed this (it is what `where` bounds are checked
	// against); carrying it here is what lets the body be *monomorphized*. Without
	// it every instantiation shared one emitted function, so a body that touched the
	// type variable could not lower at all and one that did not lower was called with
	// the wrong receiver type — invalid IR that Apple clang's opaque pointers cannot
	// distinguish.
	Bindings map[string]types.Type
}

// SpecKey identifies the *specialization* a resolution names: this method of this impl,
// at these bindings. Two call sites that solve to the same bindings share one emitted
// function; two that do not must not.
//
// It is one string for three consumers — the per-specialization ownership table, the
// backend's emitted-method cache, and the emitted symbol — because those three disagreeing
// is precisely the bug this exists to close. Bindings are sorted by variable name, since a
// map's iteration order is not stable and a symbol may not wobble between builds.
func (r Resolution) SpecKey() string {
	if r.Impl == nil || r.Method == nil {
		return ""
	}
	base := r.Impl.Type.GetName() + "$" + r.Impl.TraitName + "$" + r.Method.GetName()
	if len(r.Bindings) == 0 {
		return base
	}
	names := make([]string, 0, len(r.Bindings))
	for n := range r.Bindings {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", n, r.Bindings[n]))
	}
	return base + "<" + strings.Join(parts, ",") + ">"
}

// Lambda synthesizes the function this impl method *is*: the trait's signature supplies
// the types, the impl's clause supplies the parameter names and the body.
//
// It lives here rather than in the backend because two consumers need it — codegen, and
// the ownership pass that must analyze the same body once per specialization — and two
// constructions would be two answers to "what are this method's parameters", free to
// disagree. Building a LambdaExpr rather than a bespoke shape is equally deliberate: it
// means parameter binding, `own`-parameter framing and the void/typed return split all
// come from the same code a plain function goes through.
//
// The body is the impl clause's own node, not a copy, which is what makes the ownership
// table's per-node annotations line up with the nodes the backend later lowers.
//
// The receiver is simply the first parameter. `self` has no special status at run time;
// what makes it the receiver is that dispatch put the receiver's type in the signature's
// first position, which is exactly where the call site passes it.
func (r Resolution) Lambda() (*ast.LambdaExpr, error) {
	if r.Method == nil || r.Signature == nil {
		return nil, fmt.Errorf("trait method has no resolved signature — a trait method must declare one to be lowered")
	}
	clause := r.Method.Clause
	if len(clause.Patterns) != len(r.Signature.Parameters) {
		return nil, fmt.Errorf("trait method %q takes %d parameter(s) but its impl binds %d",
			r.Method.GetName(), len(r.Signature.Parameters), len(clause.Patterns))
	}
	params := make([]ast.Parameter, len(clause.Patterns))
	for i, pat := range clause.Patterns {
		// The signature's borrow modifier travels with the parameter, or the call site
		// and the body disagree about who owns the receiver. Everything downstream —
		// paramIsByRef, the owning-binding frame — reads ast.Parameter, so carrying it
		// here is what makes a `ref Self` a pointer on both sides.
		params[i] = ast.Parameter{
			AstBase:      ast.AstBase{Location: clause.GetLocation()},
			Pattern:      pat,
			Type:         r.Signature.Parameters[i].Type,
			TypeModifier: r.Signature.Parameters[i].Borrow,
		}
	}
	return &ast.LambdaExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: clause.GetLocation()}},
		Parameters: params,
		ReturnType: r.Signature.ReturnType,
		Body:       clause.Body,
	}, nil
}

// SetResolution records the full dispatch result for a call.
func (t *MethodTable) SetResolution(call *ast.FunctionCallExpr, r Resolution) {
	if t == nil {
		return
	}
	if t.resolutions == nil {
		t.resolutions = map[*ast.FunctionCallExpr]Resolution{}
	}
	t.resolutions[call] = r
	t.entries[call] = r.Method
}

// GetResolution returns the full dispatch result for a call. Nil-receiver-safe.
// SetBoundCandidates records, for a call dispatched through a `where` bound, the
// concrete resolution for **each** type that implements the bound trait, keyed by that
// type's `String()`.
//
// A bound call resolves abstractly at check time — the receiver is a type *variable*,
// so there is no single impl to name — but the backend lowers one specialization at a
// time, where the variable has been substituted for a concrete type. It cannot do the
// matching itself: `implTargetMatches` lives in the typechecker, and a second copy in
// the backend is the drift this codebase's hazard 8 is about. So the typechecker
// publishes every candidate and the backend picks the one its substitution names.
//
// Keyed by type rather than by specialization because that is what the backend can
// compute locally: it holds the substitution, not the enclosing specialization's key.
func (t *MethodTable) SetBoundCandidates(call *ast.FunctionCallExpr, byType map[string]Resolution) {
	if t == nil || len(byType) == 0 {
		return
	}
	if t.boundCandidates == nil {
		t.boundCandidates = map[*ast.FunctionCallExpr]map[string]Resolution{}
	}
	t.boundCandidates[call] = byType
}

// BoundCandidate returns the resolution for a bound call at a concrete receiver type.
func (t *MethodTable) BoundCandidate(call *ast.FunctionCallExpr, concrete string) (Resolution, bool) {
	if t == nil {
		return Resolution{}, false
	}
	r, ok := t.boundCandidates[call][concrete]
	return r, ok
}

// SetOperatorResolution records the impl a comparison *operator* dispatches to —
// `a <=> b` or `a < b` on a type that implements the prelude's `Ord`.
//
// Keyed by the operator expression rather than by a call, because there is no call
// node: `<=>` is its own AST node, and rewriting it into one would mean replacing a
// node the parent holds by pointer. Publishing the resolution instead keeps the
// operator's shape and gives the backend the callee it needs, the same arrangement
// SetBoundCandidates uses for a bound-dispatched call.
func (t *MethodTable) SetOperatorResolution(expr ast.Expression, r Resolution) {
	if t == nil {
		return
	}
	if t.operatorResolutions == nil {
		t.operatorResolutions = map[ast.Expression]Resolution{}
	}
	t.operatorResolutions[expr] = r
}

// SetOperatorCandidates is SetBoundCandidates for an *operator*: the resolution for
// each type implementing the trait, keyed by that type's `String()`, for a comparison
// whose operands are still a type variable at check time.
//
// `a == b` inside a generic body cannot resolve to an impl — `t` names none — but a
// specialization fixes it, and the impl must win there exactly as it does outside a
// generic. Without this, `p == q` used a type's `Eq` impl and `same(p, q)` silently
// used structural equality: one operator meaning two things depending on whether it was
// written inside a generic.
func (t *MethodTable) SetOperatorCandidates(expr ast.Expression, byType map[string]Resolution) {
	if t == nil || len(byType) == 0 {
		return
	}
	if t.operatorCandidates == nil {
		t.operatorCandidates = map[ast.Expression]map[string]Resolution{}
	}
	t.operatorCandidates[expr] = byType
}

// OperatorCandidate returns the impl an operator dispatches to at a concrete type.
func (t *MethodTable) OperatorCandidate(expr ast.Expression, concrete string) (Resolution, bool) {
	if t == nil {
		return Resolution{}, false
	}
	r, ok := t.operatorCandidates[expr][concrete]
	return r, ok
}

// OperatorResolution returns the impl a comparison operator dispatches to.
func (t *MethodTable) OperatorResolution(expr ast.Expression) (Resolution, bool) {
	if t == nil {
		return Resolution{}, false
	}
	r, ok := t.operatorResolutions[expr]
	return r, ok
}

func (t *MethodTable) GetResolution(call *ast.FunctionCallExpr) (Resolution, bool) {
	if t == nil {
		return Resolution{}, false
	}
	r, ok := t.resolutions[call]
	return r, ok
}

// Specializations returns one resolution per distinct SpecKey — every method body the
// program actually reaches, at every set of bindings it reaches it with.
//
// Deduped, because a body is analyzed and emitted per *specialization*, not per call site:
// ten calls at `t = i64` are one function. Sorted by key so a caller iterating this
// produces the same output every run, which matters for both the emitted module and the
// tables built from it.
func (t *MethodTable) Specializations() []Resolution {
	if t == nil {
		return nil
	}
	unique := map[string]Resolution{}
	for _, r := range t.resolutions {
		if key := r.SpecKey(); key != "" {
			unique[key] = r
		}
	}
	keys := make([]string, 0, len(unique))
	for k := range unique {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Resolution, 0, len(keys))
	for _, k := range keys {
		out = append(out, unique[k])
	}
	return out
}

func (t *MethodTable) Get(call *ast.FunctionCallExpr) (*ast.TraitMethodImpl, bool) {
	if t == nil {
		return nil, false
	}
	method, ok := t.entries[call]
	return method, ok
}

// SetBound records that call was resolved by abstract dispatch through a bound.
func (t *MethodTable) SetBound(call *ast.FunctionCallExpr, ref BoundMethodRef) {
	t.boundCalls[call] = ref
}

// GetBound is nil-receiver-safe, mirroring Get.
func (t *MethodTable) GetBound(call *ast.FunctionCallExpr) (BoundMethodRef, bool) {
	if t == nil {
		return BoundMethodRef{}, false
	}
	ref, ok := t.boundCalls[call]
	return ref, ok
}
