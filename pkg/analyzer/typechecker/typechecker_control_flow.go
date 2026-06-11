package typechecker

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/regex"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func hasUnguardedCatchAll(arms []ast.MatchArm) bool {
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		switch arm.Pattern.(type) {
		case *ast.WildcardPattern, *ast.IdentifierPattern:
			return true
		}
	}
	return false
}

// inferBlockType returns the type of a block expression — the type of its last
// expression statement. Returns nil for an empty block or one whose last
// statement is not an ExpressionStmt (e.g. a declaration or return).
func (tc *TypeChecker) inferBlockType(block *ast.BlockExpr) types.Type {
	var result types.Type
	tc.enterScope(block, func() {
		if len(block.Statements) == 0 {
			return
		}
		for _, stmt := range block.Statements {
			tc.checkNode(stmt) // type-check every statement, not just the last
		}
		last := block.Statements[len(block.Statements)-1]
		if exprStmt, ok := last.(*ast.ExpressionStmt); ok {
			result = tc.inferExprType(exprStmt.Expression)
		}
	})
	return result
}

// checkIfExpr type-checks an if(/else) expression and returns its inferred
// type. Two invariants are enforced:
//
//  1. The condition must be bool (when its type is inferable).
//  2. When an else branch is present and both branch types are inferable,
//     the branches must have mutually assignable types.
//
// One-armed ifs (no else) are not required to have a meaningful type: the
// result value is discarded when the expression is used as a statement, and
// requiring an else would break the extremely common pattern
// `if cond { do_something() }`.
func (tc *TypeChecker) checkIfExpr(expr *ast.IfExpr) types.Type {
	// ── 1. condition must be bool ────────────────────────────────────────────
	if expr.Condition != nil {
		condType := tc.inferExprType(expr.Condition)
		if condType != nil && !types.IsBoolean(condType) {
			tc.addError(expr.Condition.GetLocation(), SeverityError,
				"if condition must be boolean, got %s", condType)
		}
	}

	// ── 2. infer branch types ────────────────────────────────────────────────
	var thenType, elseType types.Type
	if expr.Then != nil {
		thenType = tc.inferExprType(expr.Then)
	}
	if expr.Else != nil {
		elseType = tc.inferExprType(expr.Else)
	}

	// ── 3. branch compatibility (only when both branches exist) ──────────────
	if expr.Else != nil && thenType != nil && elseType != nil {
		common, ok := branchCommonType(thenType, elseType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"if/else branches have incompatible types: then is %s, else is %s",
				thenType, elseType)
			return nil
		}
		return common
	}

	// One-armed if, or at least one branch type is unresolvable.
	return thenType
}

func (tc *TypeChecker) checkMatchExpr(expr *ast.MatchExpr) types.Type {
	scrutineeType := tc.inferExprType(expr.Scrutinee)
	if types.IsNumeric(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkNumericMatchArm(arm.Pattern, scrutineeType)
		}
		if !tc.isNumericMatchExhaustive(expr.MatchArms, scrutineeType) {
			tc.addError(expr.GetLocation(), SeverityWarning,
				"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if types.IsString(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkStringMatchArm(arm.Pattern)
		}
		if !stringMatchIsExhaustive(expr.MatchArms) {
			tc.addError(expr.GetLocation(), SeverityWarning,
				"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if types.IsArray(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkArrayMatchArm(arm.Pattern, scrutineeType)
		}
		if !arrayMatchIsExhaustive(expr.MatchArms, scrutineeType) {
			tc.addError(expr.GetLocation(), SeverityWarning,
				"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if dt, ok := tc.resolveToDataType(scrutineeType); ok {
		for _, arm := range expr.MatchArms {
			tc.checkDataMatchArm(arm.Pattern, dt)
		}
		if exhaustive, missing := dataMatchIsExhaustive(expr.MatchArms, dt); !exhaustive {
			tc.addError(expr.GetLocation(), SeverityWarning,
				"match on %s is not exhaustive: missing constructors: %s",
				dt.Name, strings.Join(missing, ", "))
		}
	}
	// Check that all arms yield a compatible type. Untyped literals are
	// promoted to their default concrete type first so that a bare `2` reads
	// as `int` rather than `integer literal` in error messages.
	if len(expr.MatchArms) == 0 {
		return nil
	}
	var commonType types.Type
	for _, arm := range expr.MatchArms {
		armType := promoteToDefault(tc.inferExprType(arm.Body))
		if armType == nil {
			continue // body type unresolvable — skip rather than false-positive
		}
		if commonType == nil {
			commonType = armType
			continue
		}
		next, ok := branchCommonType(commonType, armType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"match arms have incompatible types: %s vs %s",
				commonType, armType)
			return nil
		}
		commonType = next
	}
	return commonType
}

func (tc *TypeChecker) checkArrayMatchArm(pattern ast.Pattern, scrutineeType types.Type) {
	if pattern == nil {
		return
	}
	// Extract the element type — works for both static and dynamic arrays.
	var elemType types.Type
	switch at := scrutineeType.(type) {
	case types.DynamicArrayType:
		elemType = at.ElementType
	case types.StaticArrayType:
		elemType = at.ElementType
	}

	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		// Catch-all — always valid.
	case *ast.BindingPattern:
		// The binding name is always valid; check the inner pattern.
		tc.checkArrayMatchArm(p.Pattern, scrutineeType)
	case *ast.ArrayPattern:
		for _, elem := range p.Elements {
			tc.checkArrayPatternElement(elem, elemType)
		}
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"expected array pattern, got %s", pattern.GetName())
	}
}

// checkArrayPatternElement validates a single element pattern against the
// array's element type. Structural patterns (nested arrays, structs, etc.)
// that can't be verified without a richer type system are silently allowed.
func (tc *TypeChecker) checkArrayPatternElement(elem ast.Pattern, elemType types.Type) {
	if elemType == nil {
		return
	}
	switch p := elem.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern, *ast.RestPattern:
		// Always valid.
	case *ast.BindingPattern:
		tc.checkArrayPatternElement(p.Pattern, elemType)
	case *ast.LiteralPattern:
		kind := literalPatternKind(p.Value)
		elemPrim, ok := elemType.(types.PrimitiveType)
		if !ok {
			return
		}
		if !isAssignable(types.PrimitiveType{Name: kind}, elemPrim) {
			tc.addError(p.GetLocation(), SeverityError,
				"element pattern %s does not match array element type %s", p.Value, elemType)
		}
	}
}

// arrayMatchIsExhaustive reports whether the arms fully cover all arrays.
// Two patterns unconditionally cover every array:
//  1. A wildcard or unguarded identifier.
//  2. An array pattern whose only element is a rest spread: [...rest] —
//     this matches arrays of any length.
func arrayMatchIsExhaustive(arms []ast.MatchArm, _ types.Type) bool {
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		switch p := arm.Pattern.(type) {
		case *ast.WildcardPattern, *ast.IdentifierPattern:
			return true
		case *ast.BindingPattern:
			// A binding wrapping a wildcard or identifier is also a catch-all
			// (the inner pattern succeeds on every value).
			switch p.Pattern.(type) {
			case *ast.WildcardPattern, *ast.IdentifierPattern:
				return true
			}
		case *ast.ArrayPattern:
			// [...rest] covers every length.
			if len(p.Elements) == 1 {
				if _, ok := p.Elements[0].(*ast.RestPattern); ok {
					return true
				}
			}
		}
	}
	return false
}

// findDataTypeByConstructor searches the registered type declarations for the
// DataType that owns a constructor named ctorName. Returns (DataType, true) on
// success and (DataType{}, false) when no such constructor is found.
func (tc *TypeChecker) findDataTypeByConstructor(ctorName string) (types.DataType, bool) {
	for _, decl := range tc.symTable.Types {
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, ctor := range dt.Constructors {
			if ctor.Name == ctorName {
				return dt, true
			}
		}
	}
	return types.DataType{}, false
}

// resolveToDataType returns the DataType underlying t, or (DataType{}, false) if
// t is neither a DataType nor an UnresolvedType that names a DataType.
func (tc *TypeChecker) resolveToDataType(t types.Type) (types.DataType, bool) {
	if t == nil {
		return types.DataType{}, false
	}
	if dt, ok := t.(types.DataType); ok {
		return dt, true
	}
	if u, ok := t.(types.UnresolvedType); ok {
		if decl, exists := tc.symTable.Types[u.Name]; exists {
			if dt, ok := decl.Type.(types.DataType); ok {
				return dt, true
			}
		}
	}
	return types.DataType{}, false
}

// checkDataMatchArm validates one arm's pattern against a data-type scrutinee.
// Only DataPattern, WildcardPattern, and IdentifierPattern are legal; the
// DataPattern constructor name must exist in the data type's declaration.
func (tc *TypeChecker) checkDataMatchArm(pattern ast.Pattern, dt types.DataType) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.DataPattern:
		for _, ctor := range dt.Constructors {
			if ctor.Name == p.Name {
				return
			}
		}
		tc.addError(p.GetLocation(), SeverityError,
			"%s is not a constructor of %s", p.Name, dt.Name)
	case *ast.BindingPattern:
		tc.checkDataMatchArm(p.Pattern, dt)
	case *ast.LiteralPattern:
		tc.addError(p.GetLocation(), SeverityError,
			"literal patterns are not allowed on a data type scrutinee")
	case *ast.RegexPattern:
		tc.addError(p.GetLocation(), SeverityError,
			"regex patterns are not allowed on a data type scrutinee")
	case *ast.RangePattern:
		tc.addError(p.GetLocation(), SeverityError,
			"range patterns are not allowed on a data type scrutinee")
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"this pattern is not allowed on a data type scrutinee")
	}
}

// dataMatchIsExhaustive reports whether the match arms fully cover all
// constructors of dt. Returns (true, nil) when a wildcard or unguarded
// identifier is present. Returns (true, nil) when every constructor has at
// least one unguarded DataPattern arm. Otherwise returns (false, missingNames)
// where missingNames lists the uncovered constructors in declaration order.
func dataMatchIsExhaustive(arms []ast.MatchArm, dt types.DataType) (bool, []string) {
	// A wildcard or unguarded identifier catches every possible value.
	if hasUnguardedCatchAll(arms) {
		return true, nil
	}

	// Collect constructor names that are covered by unguarded DataPattern arms.
	covered := make(map[string]bool, len(dt.Constructors))
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		if dp, ok := arm.Pattern.(*ast.DataPattern); ok {
			covered[dp.Name] = true
		}
	}

	var missing []string
	for _, ctor := range dt.Constructors {
		if !covered[ctor.Name] {
			missing = append(missing, ctor.Name)
		}
	}
	return len(missing) == 0, missing
}

// checkStringMatchArm validates one arm's pattern against a `string`
// scrutinee. Allowed patterns: string literal, regex literal, identifier
// (binding), or wildcard. Anything else — number/bool literal, range, etc. —
// is a type error.
func (tc *TypeChecker) checkStringMatchArm(pattern ast.Pattern) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.RegexPattern:
		// Validate the regex itself at compile time so users learn about
		// syntax errors at type-check time rather than at runtime.
		if _, err := regex.Compile(p.Pattern); err != nil {
			tc.addError(p.GetLocation(), SeverityError,
				"invalid regex pattern r/%s/: %s", p.Pattern, err.Error())
		}
	case *ast.BindingPattern:
		tc.checkStringMatchArm(p.Pattern)
	case *ast.LiteralPattern:
		kind := literalPatternKind(p.Value)
		if kind != types.String {
			tc.addError(p.GetLocation(), SeverityError,
				"literal pattern '%s' is not a string type", p.Value)
		}
	case *ast.RangePattern:
		tc.addError(p.GetLocation(), SeverityError,
			"range patterns are not allowed on string scrutinees")
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"this pattern is not allowed on a string scrutinee")
	}
}

// stringMatchIsExhaustive reports whether at least one arm unconditionally
// catches every string — a wildcard or an unguarded identifier. String
// literals and regex literals can only cover finite or partial subsets of
// the language, so they never on their own make the match exhaustive (we
// don't try to prove regex unions cover Σ*).
func stringMatchIsExhaustive(arms []ast.MatchArm) bool {
	return hasUnguardedCatchAll(arms)
}

// isNumericMatchExhaustive reports whether the arms of a numeric match
// expression cover all possible values of scrutineeType.
//
// Two strategies are tried in order:
//  1. Wildcard / unguarded identifier — trivially covers every value.
//  2. Interval analysis — for fixed-width integer types only. Collects the
//     inclusive [lo, hi] interval from each unguarded LiteralPattern or
//     RangePattern and checks whether their union spans [typeMin, typeMax].
func (tc *TypeChecker) isNumericMatchExhaustive(arms []ast.MatchArm, scrutineeType types.Type) bool {
	// Fast path: a wildcard or unguarded identifier catches everything.
	if numericMatchIsExhaustive(arms) {
		return true
	}
	// For fixed-width integer types, attempt interval coverage analysis.
	p, ok := scrutineeType.(types.PrimitiveType)
	if !ok {
		return false
	}
	typeMin, typeMax, boundsKnown := intTypeBounds(p.Name)
	if !boundsKnown {
		return false
	}
	return integerIntervalsExhaustive(arms, typeMin, typeMax)
}

// intTypeBounds returns the inclusive [min, max] for fixed-width integer types.
// Platform-sized (int/uint), untyped, and float types return ok=false because
// their range is either unknown or too large to reason about discretely.
func intTypeBounds(name types.PrimitiveTypeName) (min, max int64, ok bool) {
	switch name {
	case types.Int8:
		return math.MinInt8, math.MaxInt8, true
	case types.UInt8:
		return 0, math.MaxUint8, true
	case types.Int16:
		return math.MinInt16, math.MaxInt16, true
	case types.UInt16:
		return 0, math.MaxUint16, true
	case types.Int32:
		return math.MinInt32, math.MaxInt32, true
	case types.UInt32:
		return 0, math.MaxUint32, true
	case types.Int64:
		return math.MinInt64, math.MaxInt64, true
		// UInt64: max is 2^64-1 which overflows int64; skip range-based exhaustiveness.
	}
	return 0, 0, false
}

// extractIntFromExpr extracts a compile-time int64 value from an expression
// that appears as a range-pattern bound. Handles IntegerLiteralExpr and
// NegationExpr wrapping one (for negative bounds like -128).
func extractIntFromExpr(e ast.Expression) (int64, bool) {
	if e == nil {
		return 0, false
	}
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		return v.Value, true
	case *ast.NegationExpr:
		inner, ok := v.Operand.(*ast.IntegerLiteralExpr)
		if !ok {
			return 0, false
		}
		return -inner.Value, true
	}
	return 0, false
}

// armIntInterval returns the inclusive [lo, hi] integer interval that an
// unguarded arm's pattern covers. Returns ok=false for guarded arms, arms
// whose pattern is not a numeric literal/range, or bounds that cannot be
// statically evaluated.
func armIntInterval(arm ast.MatchArm) (lo, hi int64, ok bool) {
	if arm.Guard != nil {
		return 0, 0, false
	}
	switch p := arm.Pattern.(type) {
	case *ast.LiteralPattern:
		s, isStr := p.Value.(string)
		if !isStr {
			return 0, 0, false
		}
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return 0, 0, false
		}
		return n, n, true
	case *ast.RangePattern:
		start, startOk := extractIntFromExpr(p.Start)
		end, endOk := extractIntFromExpr(p.End)
		if !startOk || !endOk {
			return 0, 0, false
		}
		if p.EndOperator == "<" { // exclusive end (..<)
			if end == math.MinInt64 {
				return 0, 0, false // underflow
			}
			end--
		}
		return start, end, true
	}
	return 0, 0, false
}

// integerIntervalsExhaustive reports whether the unguarded pattern arms
// collectively cover every integer in [typeMin, typeMax] without gaps.
func integerIntervalsExhaustive(arms []ast.MatchArm, typeMin, typeMax int64) bool {
	type interval struct{ lo, hi int64 }
	var ivs []interval
	for _, arm := range arms {
		lo, hi, ok := armIntInterval(arm)
		if ok && lo <= hi {
			ivs = append(ivs, interval{lo, hi})
		}
	}
	if len(ivs) == 0 {
		return false
	}
	sort.Slice(ivs, func(i, j int) bool {
		if ivs[i].lo != ivs[j].lo {
			return ivs[i].lo < ivs[j].lo
		}
		return ivs[i].hi > ivs[j].hi // wider first when lo ties
	})
	// Walk the sorted intervals. nextNeeded is the smallest value not yet covered.
	nextNeeded := typeMin
	for _, iv := range ivs {
		if iv.lo > nextNeeded {
			return false // gap before this interval
		}
		if iv.hi >= typeMax {
			return true // coverage reaches the upper bound
		}
		if iv.hi >= nextNeeded {
			nextNeeded = iv.hi + 1
		}
	}
	return false
}

// numericMatchIsExhaustive reports whether at least one arm unconditionally
// catches every value — i.e. a WildcardPattern or an unguarded IdentifierPattern.
func numericMatchIsExhaustive(arms []ast.MatchArm) bool {
	return hasUnguardedCatchAll(arms)
}

func (tc *TypeChecker) checkNumericMatchArm(pattern ast.Pattern, scrutineeType types.Type) {
	// RangePattern needs no check: the grammar restricts both start and end to
	// number literals, so numeric bounds are guaranteed by the parser.
	switch p := pattern.(type) {
	case *ast.RegexPattern:
		tc.addError(p.GetLocation(), SeverityError,
			"regex patterns are not allowed on a numeric scrutinee")
	case *ast.LiteralPattern:
		kind := literalPatternKind(p.Value)
		if isIntType(scrutineeType) && kind != types.UntypedInt {
			tc.addError(p.GetLocation(), SeverityError,
				"literal pattern '%s' is not an integer type", p.Value)
		}
		if isFloatType(scrutineeType) && kind != types.UntypedFloat {
			tc.addError(p.GetLocation(), SeverityError,
				"literal pattern '%s' is not a float type", p.Value)
		}
	}
}

// literalPatternKind classifies a literal pattern's raw source text as
// UntypedInt, UntypedFloat, Boolean, or String.
func literalPatternKind(value any) types.PrimitiveTypeName {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	switch {
	case s == "true" || s == "false":
		return types.Boolean
	case len(s) > 0 && s[0] == '"':
		return types.String
	case strings.Contains(s, "."):
		return types.UntypedFloat
	default:
		return types.UntypedInt
	}
}

// isIterableType reports whether t can be used as the iterable in a for-in loop.
// Valid iterables are arrays, strings, and range expressions.
func isIterableType(t types.Type) bool {
	if types.IsArray(t) || types.IsString(t) {
		return true
	}
	_, ok := t.(types.RangeType)
	return ok
}

// inferRangeExpr validates that both ends of a range expression are numeric and
// mutually compatible, validates the step if present, and returns a RangeType.
func (tc *TypeChecker) inferRangeExpr(expr *ast.RangeExpr) types.Type {
	startType := tc.inferExprType(expr.Start)
	endType := tc.inferExprType(expr.End)

	if startType != nil && !types.IsNumeric(startType) {
		tc.addError(expr.Start.GetLocation(), SeverityError,
			"range start must be numeric, got %s", startType)
		startType = nil
	}
	if endType != nil && !types.IsNumeric(endType) {
		tc.addError(expr.End.GetLocation(), SeverityError,
			"range end must be numeric, got %s", endType)
		endType = nil
	}

	var commonType types.Type
	if startType != nil && endType != nil {
		ct, ok := branchCommonType(startType, endType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"range operands have incompatible types: start is %s, end is %s",
				startType, endType)
		} else {
			commonType = ct
		}
	}

	if expr.Step != nil {
		stepType := tc.inferExprType(expr.Step)
		if stepType != nil && !types.IsNumeric(stepType) {
			tc.addError(expr.Step.GetLocation(), SeverityError,
				"range step must be numeric, got %s", stepType)
		} else if stepType != nil && commonType != nil {
			if _, ok := branchCommonType(stepType, commonType); !ok {
				tc.addError(expr.Step.GetLocation(), SeverityError,
					"range step type %s is not compatible with range operand type %s",
					stepType, commonType)
			}
		}
	}

	return types.RangeType{Start: startType, End: endType}
}

// checkForInLoopExpr validates that the iterable expression is actually iterable
// (array, string, or range), then type-checks the loop body.
func (tc *TypeChecker) checkForInLoopExpr(expr *ast.ForInLoopExpr) types.Type {
	iterType := tc.inferExprType(expr.Iterable)
	if iterType != nil && !isIterableType(iterType) {
		tc.addError(expr.Iterable.GetLocation(), SeverityError,
			"cannot iterate over %s: expected an array, string, or range", iterType)
	}
	tc.inferBlockType(&expr.Body)
	return nil
}

// checkForLoopExpr type-checks a C-style for loop.
//
// When there is no init clause, all condition variables live in an outer
// scope that the typechecker can reach, so the condition operands are
// validated via checkBooleanBinaryOpExpr. When an init clause is present,
// the init-declared variable lives in the loop's own scope, which cannot be
// reached via the scope table (because ForLoopExpr.Body stores a value copy
// of the BlockExpr, not the original pointer that was registered); skipping
// the condition check in that case avoids false "undefined identifier" errors.
func (tc *TypeChecker) checkForLoopExpr(expr *ast.ForLoopExpr) {
	if expr.Init == nil && expr.Condition != nil {
		condType := tc.inferExprType(*expr.Condition)
		if condType != nil && !types.IsBoolean(condType) {
			tc.addError((*expr.Condition).GetLocation(), SeverityError,
				"for loop condition must be boolean, got %s", condType)
		}
	}
	tc.inferBlockType(&expr.Body)
}

// inferNullCoalescingExpr type-checks a ?? expression. Both sides are
// inferred and must unify via branchCommonType; the result is the common type.
func (tc *TypeChecker) inferNullCoalescingExpr(expr *ast.NullCoalescingExpr) types.Type {
	optType := tc.inferExprType(expr.Optional)
	defType := tc.inferExprType(expr.Default)
	if optType == nil || defType == nil {
		if optType != nil {
			return optType
		}
		return defType
	}
	common, ok := branchCommonType(optType, defType)
	if !ok {
		tc.addError(expr.GetLocation(), SeverityError,
			"null coalescing operands have incompatible types: left is %s, right is %s",
			optType, defType)
		return nil
	}
	return common
}

// branchCommonType returns the common type for two if/else branches and
// whether they are compatible. Exact equality wins first; then untyped→concrete
// widening (e.g. untyped int + i32 → i32); otherwise the types are incompatible.
func branchCommonType(a, b types.Type) (types.Type, bool) {
	if types.TypesEqual(a, b) {
		return a, true
	}
	// Untyped widening: if a is assignable to b, b is the more concrete type.
	if isAssignable(a, b) {
		return b, true
	}
	// Symmetric case.
	if isAssignable(b, a) {
		return a, true
	}
	return nil, false
}
