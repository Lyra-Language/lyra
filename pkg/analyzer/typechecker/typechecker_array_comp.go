package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Array comprehensions — `[ x in xs | x > 0 | x * 2 ]`.
//
// Generators bind, guards filter, the result builds: the value is a **dynamic** array of
// whatever the result expression yields, `[]u`. Dynamic and not `[N]T` even when every
// source is fixed-size, because a guard decides per element how many survive, and that is
// a runtime question. A comprehension with no guard could in principle be sized statically;
// it deliberately is not, so that adding a guard to one never changes its type.
//
// The generator variable is typed here rather than by the collector, for the reason a
// for-in loop's is: the element type is not known until the source has been inferred, and
// only the typechecker infers. The collector registered a placeholder binding in a scope of
// its own (see collectArrayCompExpr) and this fills it in — which is also why the guards
// and result are checked *inside* that scope.

// inferArrayCompType checks a comprehension and returns `[]result`.
func (tc *TypeChecker) inferArrayCompType(comp *ast.ArrayCompExpr) types.Type {
	if len(comp.Generators) == 0 {
		tc.addError(comp.GetLocation(), SeverityError,
			"an array comprehension needs at least one generator, as in `[ x in xs | x ]`")
		return nil
	}
	var result types.Type
	tc.enterScope(comp, func() {
		for i := range comp.Generators {
			tc.bindGenerator(&comp.Generators[i])
		}
		for _, guard := range comp.Guards {
			tc.checkComprehensionGuard(guard)
		}
		result = tc.inferExprType(comp.Result)
	})
	if result == nil {
		return nil
	}
	// An untyped literal result settles to its default width, exactly as it would in an
	// array literal: the element type is the array's representation, so it cannot stay
	// untyped once the comprehension has a type.
	if types.IsNumeric(result) {
		result = promoteToDefault(result)
		tc.propagateExpectedType(comp.Result, result)
	}
	out := types.DynamicArrayType{ElementType: result}
	tc.typeTable.Set(comp, out)
	return out
}

// bindGenerator infers a generator's source and binds its name to the source's element
// type.
//
// `iterableElementType` is the same function a for-in loop uses, so `x` in
// `[ x in xs | … ]` and `x` in `for x in xs { … }` are typed by one rule — an array's
// element, a string's `rune`, a range's numeric type. Two definitions would drift, and the
// direction of the drift is the bad one: a comprehension that types its variable
// differently from the loop it is sugar for.
func (tc *TypeChecker) bindGenerator(gen *ast.Generator) {
	if gen.Value == nil {
		return
	}
	sourceType := tc.inferExprType(gen.Value)
	if sourceType == nil {
		return
	}
	sourceType = tc.resolveType(sourceType, gen.Value.GetLocation())
	elem := iterableElementType(sourceType)
	if elem == nil {
		tc.addError(gen.GetLocation(), SeverityError,
			"cannot iterate over %s in a comprehension — a generator's source must be an array, a string, or a range",
			sourceType)
		return
	}
	// A range over untyped bounds keeps them untyped (iterableElementType's rule), which
	// would leave the binding with no width. Settle it the way an unannotated literal
	// binding settles.
	if types.IsNumeric(elem) {
		elem = promoteToDefault(elem)
	}
	tc.setLoopVarType(gen.Identifier, elem)
	tc.typeTable.Set(gen.Value, sourceType)
}

// checkComprehensionGuard requires a guard to be a bool.
//
// The grammar admits a bare identifier or call here as well as a boolean expression, so
// `[ x in xs | flag | x ]` parses; the type is what rejects a non-bool, and it names the
// section rather than the expression, since "guard" is the word the reader would use.
func (tc *TypeChecker) checkComprehensionGuard(guard ast.Expression) {
	guardType := tc.inferExprType(guard)
	if guardType == nil {
		return
	}
	if !types.IsBoolean(types.StripNewtype(guardType)) {
		tc.addError(guard.GetLocation(), SeverityError,
			"a comprehension guard must be a bool, got %s", guardType)
	}
}
