package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// generatorContext is the `gen` body currently being checked: what it is called, for a
// diagnostic, and the element type its `-> Seq<t>` names, which every `yield` in it is
// checked against. nil when the annotation was not a sequence, so a yield inside a
// mis-annotated body is not reported a second time.
type generatorContext struct {
	name string
	elem types.Type
}

// checkYieldExpr types `yield e`: the value must be assignable to the enclosing
// sequence's element, narrowed first so `yield 0` inside a `-> Seq<u8>` yields a u8. The
// expression itself has no value — a sequence hands the element to its consumer and
// resumes — so it is void, which is also what keeps `let x = yield e` a type error.
//
// A `yield` outside any `gen` body is lyra-E006, reported by the checker pass before
// this one runs; here the value is still inferred, so its own errors surface.
func (tc *TypeChecker) checkYieldExpr(e *ast.YieldExpr) types.Type {
	valueType := tc.inferExprType(e.Value)
	if g := tc.enclosingGen; g != nil && g.elem != nil && valueType != nil {
		valueType, _ = tc.contextualType(e.Value, g.elem, valueType)
		tc.propagateExpectedType(e.Value, g.elem)
		if t, ok := tc.typeTable.Get(e.Value); ok && t != nil {
			valueType = t
		}
		if !tc.assignableValue(e.Value, valueType, g.elem) {
			tc.addError(e.GetLocation(), SeverityError,
				"%s: cannot yield %s from a Seq<%s>", g.name, valueType, g.elem)
		}
	}
	tc.typeTable.Set(e, types.VoidType{})
	return types.VoidType{}
}

// checkYieldFromExpr types `yield from s`: every element of `s` is yielded in turn, so
// `s` may be anything a `for-in` walks — another sequence, an array, a string, a range —
// and its element must be assignable to the enclosing sequence's.
func (tc *TypeChecker) checkYieldFromExpr(e *ast.YieldFromExpr) types.Type {
	sourceType := tc.inferExprType(e.Generator)
	if sourceType != nil {
		sourceType = tc.resolveType(sourceType, e.Generator.GetLocation())
	}
	if g := tc.enclosingGen; g != nil && sourceType != nil {
		elem := iterableElementType(sourceType)
		switch {
		case elem == nil:
			tc.addError(e.Generator.GetLocation(), SeverityError,
				"%s: cannot yield from %s: expected a Seq, an array, a string, or a range", g.name, sourceType)
		case g.elem != nil && !isAssignable(elem, g.elem):
			tc.addError(e.GetLocation(), SeverityError,
				"%s: cannot yield %s elements from a Seq<%s>", g.name, elem, g.elem)
		}
	}
	tc.typeTable.Set(e, types.VoidType{})
	return types.VoidType{}
}
