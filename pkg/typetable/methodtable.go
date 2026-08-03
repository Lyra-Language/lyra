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
		entries:    make(map[*ast.FunctionCallExpr]*ast.TraitMethodImpl),
		boundCalls: make(map[*ast.FunctionCallExpr]BoundMethodRef),
	}
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
