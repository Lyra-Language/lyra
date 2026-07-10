package typetable

import "github.com/Lyra-Language/lyra/pkg/ast"

// MethodTable maps a call-site expression — one whose Function is a
// MemberExpr or TraitMethodPathExpr that the type-checker resolved to a
// trait-impl method — to the specific *ast.TraitMethodImpl it dispatches to.
// It is populated by the type-checker during call resolution and consulted by
// later passes (e.g. the purity checker) that need to know which method body
// a given call actually invokes, without re-deriving dispatch themselves.
type MethodTable struct {
	entries    map[*ast.FunctionCallExpr]*ast.TraitMethodImpl
	boundCalls map[*ast.FunctionCallExpr]BoundMethodRef
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
