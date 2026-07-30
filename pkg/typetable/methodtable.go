package typetable

import (
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
