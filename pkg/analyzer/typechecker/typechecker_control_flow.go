package typechecker

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
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

// patternIsIrrefutable reports whether a pattern matches every value of its
// type — it binds and destructures but never tests. A tuple/struct is a
// *single-shape* aggregate (no tag to discriminate), so a pattern over one is
// irrefutable exactly when every sub-pattern is: `(a, b)` and `Pt { x, y }`
// always match, while `(0, b)` / `{ x: 0 }` can fail on the literal.
//
// This is the exhaustiveness counterpart of the backend's aggPatternTest, which
// returns a nil condition for precisely these patterns — keeping "no runtime test
// emitted" and "counts as exhaustive" the same judgment. Without it an irrefutable
// arm still drew a "not exhaustive" warning demanding an unreachable wildcard,
// which is both wrong and corrosive: it trains users to ignore the warning class
// that also covers genuinely non-exhaustive matches.
//
// A struct pattern may list a subset of fields (unlisted ones aren't tested), and
// the typechecker has already verified a named pattern's type against the
// scrutinee, so field coverage doesn't affect refutability. Deliberately not
// irrefutable: literal/range (test a value), data (tests a tag), array (tests a
// length), regex, and a `name @ inner` binding whose inner pattern is refutable.
func patternIsIrrefutable(pat ast.Pattern) bool {
	switch p := pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		// nil is a struct shorthand field (`{ x }`) — a binding leaf.
		return true
	case *ast.TuplePattern:
		for _, elem := range p.Elements {
			if !patternIsIrrefutable(elem) {
				return false
			}
		}
		return true
	case *ast.StructPattern:
		for _, f := range p.Fields {
			if !patternIsIrrefutable(f.Pattern) {
				return false
			}
		}
		return true
	case *ast.BindingPattern:
		return patternIsIrrefutable(p.Pattern)
	}
	return false
}

// aggregateMatchIsExhaustive reports whether a match over a tuple or struct
// scrutinee covers every value: an unguarded catch-all, or any unguarded
// irrefutable destructuring arm. A guarded arm never counts (its guard may fail).
func aggregateMatchIsExhaustive(arms []ast.MatchArm) bool {
	for _, arm := range arms {
		if arm.Guard == nil && patternIsIrrefutable(arm.Pattern) {
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
// type.
//
// requireType controls whether branch-type compatibility is enforced:
//   - true  (value context): both branches must have mutually assignable types;
//     an error is emitted and nil is returned when they don't.
//   - false (statement context): branches are type-checked individually for
//     errors within them, but a mismatch between the two is not an error
//     because the result is discarded. The policy propagates through else-if
//     chains so that `if a { T } else if b { U } else { V }` used as a
//     statement is always accepted regardless of T, U, V.
func (tc *TypeChecker) checkIfExpr(expr *ast.IfExpr, requireType bool) types.Type {
	// ── 1. condition must be bool ────────────────────────────────────────────
	if expr.Condition != nil {
		condType := tc.inferExprType(expr.Condition)
		if condType != nil && !types.IsBoolean(condType) {
			tc.addError(expr.Condition.GetLocation(), SeverityError,
				"if condition must be boolean, got %s", condType)
		}
		if lit, ok := expr.Condition.(*ast.BooleanLiteralExpr); ok {
			tc.addError(expr.Condition.GetLocation(), SeverityWarning,
				"condition is always %t", lit.Value)
		}
	}

	// ── 2. infer branch types ────────────────────────────────────────────────
	var thenType, elseType types.Type
	if expr.Then != nil {
		thenType = tc.inferExprType(expr.Then)
	}
	if expr.Else != nil {
		if innerIf, ok := expr.Else.(*ast.IfExpr); ok && !requireType {
			// Propagate statement context through else-if chains so that
			// `if a { T } else if b { U } else { V }` as a statement never
			// errors on branch mismatches at any level.
			tc.checkIfExpr(innerIf, false)
		} else {
			elseType = tc.inferExprType(expr.Else)
		}
	}

	// ── 3. branch compatibility (only when result is needed) ─────────────────
	if requireType && expr.Else != nil && thenType != nil && elseType != nil {
		common, ok := branchCommonType(thenType, elseType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"if/else branches have incompatible types: then is %s, else is %s",
				thenType, elseType)
			return nil
		}
		return common
	}

	if !requireType {
		return nil
	}

	// requireType (value context): the if's result is used, so a one-armed if
	// (no else) is invalid — it has no value when the condition is false.
	// (As a statement — requireType == false, handled above — a one-armed if
	// is fine: a conditional side effect.) This also correctly requires a
	// terminal else on an `if … else if …` chain used as a value: each inner
	// if is reached here with requireType == true (via inferExprType on the
	// else branch), so the last one, lacking an else, is flagged.
	if expr.Else == nil {
		tc.addError(expr.GetLocation(), SeverityError,
			"`if` used as a value must have an `else` branch")
		return nil
	}

	// Two-armed, but at least one branch type was unresolvable (a nested error
	// is already reported); return the then type as a best effort.
	return thenType
}

// withPatternBindings runs fn with the variables a match arm's pattern binds
// installed as parameter types (merged over any enclosing ones), so the arm body
// can reference them. It reuses walkDestructuredPattern to derive each binding's
// type from the scrutinee type, the same machinery a destructuring `let` uses.
func (tc *TypeChecker) withPatternBindings(pattern ast.Pattern, scrutineeType types.Type, fn func()) {
	old := tc.paramTypes
	merged := make(map[string]types.Type, len(old)+2)
	maps.Copy(merged, old)
	tc.paramTypes = merged
	// walkDestructuredPattern validates as it binds, but the arm-pattern check
	// (checkDataMatchArm/checkStructMatchArm/…) already reported any mismatch, so
	// discard errors from this pass to avoid duplicates — it's used here only for
	// its bindings.
	errCount := len(tc.errors)
	tc.walkDestructuredPattern(pattern, scrutineeType, func(name string, typ types.Type) {
		tc.paramTypes[name] = typ
	})
	tc.errors = tc.errors[:errCount]
	fn()
	tc.paramTypes = old
}

func (tc *TypeChecker) checkMatchExpr(expr *ast.MatchExpr) types.Type {
	scrutineeType := tc.inferExprType(expr.Scrutinee)
	if types.IsBoolean(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkBoolMatchArm(arm.Pattern)
		}
		if !boolMatchIsExhaustive(expr.MatchArms) {
			tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeNonExhaustiveMatch,
				"match on bool is not exhaustive: add arms for both `true` and `false`, or a wildcard `_ => ...`")
		}
	} else if types.IsNumeric(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkNumericMatchArm(arm.Pattern, scrutineeType)
		}
		if !tc.isNumericMatchExhaustive(expr.MatchArms, scrutineeType) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if types.IsString(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkStringMatchArm(arm.Pattern)
		}
		if !stringMatchIsExhaustive(expr.MatchArms) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if isRuneType(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkRuneMatchArm(arm.Pattern)
		}
		if !hasUnguardedCatchAll(expr.MatchArms) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on rune type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if types.IsArray(scrutineeType) {
		for _, arm := range expr.MatchArms {
			tc.checkArrayMatchArm(arm.Pattern, scrutineeType)
		}
		if !arrayMatchIsExhaustive(expr.MatchArms, scrutineeType) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if dt, ok := tc.resolveToDataType(scrutineeType); ok {
		for _, arm := range expr.MatchArms {
			tc.checkDataMatchArm(arm.Pattern, dt)
		}
		if exhaustive, missing := dataMatchIsExhaustive(expr.MatchArms, dt); !exhaustive {
			tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeNonExhaustiveMatch,
				"match on %s is not exhaustive: missing constructors: %s",
				dt.Name, strings.Join(missing, ", "))
		}
	} else if tt, ok := scrutineeType.(types.TupleType); ok {
		for _, arm := range expr.MatchArms {
			tc.checkTupleMatchArm(arm.Pattern, tt)
		}
		if !aggregateMatchIsExhaustive(expr.MatchArms) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on tuple type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	} else if st, ok := tc.resolveToNamedStructType(scrutineeType); ok {
		for _, arm := range expr.MatchArms {
			tc.checkStructMatchArm(arm.Pattern, st)
		}
		if !aggregateMatchIsExhaustive(expr.MatchArms) {
			tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeNonExhaustiveMatch,
				"match on struct type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
		}
	}
	tc.checkDuplicateMatchArms(expr.MatchArms)
	if len(expr.MatchArms) == 0 {
		return nil
	}
	// Fold the arms to a common type. Each arm body is inferred with its pattern's
	// bound variables in scope (`Some(x) => x + 1`), so an identifier a pattern
	// introduces resolves and gets its type recorded (via the paramTypes mechanism,
	// which also records each resolved identifier into the TypeTable the backend
	// reads). An untyped-literal result is left un-promoted so a bare `0` arm can
	// adapt to a concrete sibling (`Some x => x` with x: u8) or to the match's
	// outer context — rather than defaulting to i64 and clashing.
	var commonType types.Type
	for _, arm := range expr.MatchArms {
		var armType types.Type
		tc.withPatternBindings(arm.Pattern, scrutineeType, func() {
			// A guard (`Some(x) if x > 0`) is checked with the pattern's bindings in
			// scope — it may reference them — and must be a bool. Inferring it here
			// also records its sub-expressions in the TypeTable, which the backend
			// reads to lower the guard.
			if arm.Guard != nil {
				if gt := tc.inferExprType(arm.Guard.Condition); gt != nil && !types.IsBoolean(gt) {
					tc.addError(arm.Guard.Condition.GetLocation(), SeverityError,
						"match arm guard must be a bool, got %s", promoteToDefault(gt))
				}
			}
			armType = tc.inferExprType(arm.Body)
		})
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
				promoteToDefault(commonType), promoteToDefault(armType))
			return nil
		}
		commonType = next
	}
	if commonType == nil {
		return nil
	}
	// Push the resolved common width down onto any untyped-literal arm body (the
	// match-arm context propagation site), so every arm lowers at the match's
	// result type. When the common type is itself still untyped (all arms were
	// untyped literals), this is a no-op — an outer context (a declared return
	// type, an annotation) narrows the whole match later via propagateLiteralType.
	for _, arm := range expr.MatchArms {
		tc.withPatternBindings(arm.Pattern, scrutineeType, func() {
			tc.propagateLiteralType(arm.Body, commonType)
		})
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

// resolveToDataType returns the DataType underlying t, or (DataType{}, false)
// if t doesn't name one. Three forms of t are accepted:
//   - an already-resolved DataType, returned as-is;
//   - an UnresolvedType naming a non-generic data type;
//   - a ParameterizedType (`Maybe<i64>`, `Result<i64, string>`) naming a
//     *generic* data type — its constructors are returned with each
//     occurrence of a generic parameter (e.g. the `t` in `Some t`) substituted
//     for the corresponding concrete type argument (substituteGenerics), so a
//     caller pattern-matching/destructuring `Some(x)` against a `Maybe<i64>`
//     value gets `x: i64`, not the unsubstituted type variable `t`.
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
	if p, ok := t.(types.ParameterizedType); ok {
		decl, exists := tc.symTable.Types[p.Name]
		if !exists {
			return types.DataType{}, false
		}
		dt, ok := decl.Type.(types.DataType)
		if !ok || len(decl.GenericParams) != len(p.TypeArguments) {
			return types.DataType{}, false
		}
		subst := make(map[string]types.Type, len(decl.GenericParams))
		for i, gp := range decl.GenericParams {
			subst[gp.Name] = p.TypeArguments[i]
		}
		substituted := dt
		substituted.Constructors = make([]types.DataTypeConstructor, len(dt.Constructors))
		for i, ctor := range dt.Constructors {
			substituted.Constructors[i] = ctor
			substituted.Constructors[i].Params = make([]types.Type, len(ctor.Params))
			for j, param := range ctor.Params {
				substituted.Constructors[i].Params[j] = substituteGenerics(param, subst)
			}
		}
		return substituted, true
	}
	return types.DataType{}, false
}

// substituteGenerics replaces every occurrence of a generic type variable in
// t with its concrete binding from subst (param name -> argument type),
// recursing into the handful of compound type shapes a data constructor's
// payload realistically takes today (tuples and arrays). Anything else —
// including a generic nested inside a struct/another data type's payload —
// is left as-is rather than guessed; that's a sharper-edged case than this
// destructuring/match-arm support targets.
func substituteGenerics(t types.Type, subst map[string]types.Type) types.Type {
	switch tt := t.(type) {
	case types.GenericType:
		if concrete, ok := subst[tt.Name]; ok {
			return concrete
		}
		return tt
	case types.TupleType:
		elems := make([]types.Type, len(tt.Elements))
		for i, el := range tt.Elements {
			elems[i] = substituteGenerics(el, subst)
		}
		tt.Elements = elems
		return tt
	case types.DynamicArrayType:
		tt.ElementType = substituteGenerics(tt.ElementType, subst)
		return tt
	case types.StaticArrayType:
		tt.ElementType = substituteGenerics(tt.ElementType, subst)
		return tt
	case types.ParameterizedType:
		args := make([]types.Type, len(tt.TypeArguments))
		for i, a := range tt.TypeArguments {
			args[i] = substituteGenerics(a, subst)
		}
		tt.TypeArguments = args
		return tt
	}
	return t
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

// MissingMatchConstructors returns the constructors of dt that the match arms
// leave uncovered, in declaration order — the exact set the exhaustiveness
// check (lyra-E009) reports. Returns nil when the match is already exhaustive.
// Exported so the LSP's "Add missing match arms" code action can reuse the same
// computation rather than re-deriving it.
func MissingMatchConstructors(arms []ast.MatchArm, dt types.DataType) []string {
	_, missing := dataMatchIsExhaustive(arms, dt)
	return missing
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

// isRuneType reports whether t is the `rune` primitive. A rune is a code point
// (i32) but is deliberately excluded from IsNumeric — arithmetic on code points
// is meaningless — so match dispatch handles it on its own scalar path.
func isRuneType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	return ok && p.Name == types.Rune
}

// checkRuneMatchArm validates one arm's pattern against a `rune` scrutinee.
// Allowed patterns: a character literal ('a'), an identifier (binding), or a
// wildcard. A number/string/bool literal, a range, or a regex is a type error.
func (tc *TypeChecker) checkRuneMatchArm(pattern ast.Pattern) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.BindingPattern:
		tc.checkRuneMatchArm(p.Pattern)
	case *ast.LiteralPattern:
		if literalPatternKind(p.Value) != types.Rune {
			tc.addError(p.GetLocation(), SeverityError,
				"literal pattern %s is not a rune (character) value", p.Value)
		}
	case *ast.RangePattern:
		tc.addError(p.GetLocation(), SeverityError,
			"range patterns are not allowed on a rune scrutinee")
	case *ast.RegexPattern:
		tc.addError(p.GetLocation(), SeverityError,
			"regex patterns are not allowed on a rune scrutinee")
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"this pattern is not allowed on a rune scrutinee")
	}
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
		if isFloatType(scrutineeType) {
			if kind != types.UntypedFloat {
				tc.addError(p.GetLocation(), SeverityError,
					"literal pattern '%s' is not a float type", p.Value)
			} else {
				// A float literal pattern tests exact equality (it lowers to an
				// `fcmp oeq`) — the same precision hazard the `==`/`!=` operator
				// warns about (shared lyra-W008): a value off by an ULP silently
				// won't match. A range pattern (`0.0..<1.0`) is the reliable form.
				tc.addErrorCode(p.GetLocation(), SeverityWarning, diag.CodeImpreciseFloatEquality,
					"matching a float against the literal '%s' tests exact equality, which may be unreliable due to floating-point precision; use a range pattern instead", p.Value)
			}
		}
	}
}

// checkBoolMatchArm validates one arm's pattern against a bool scrutinee.
// Only true/false literal patterns, wildcards, and identifiers are valid.
func (tc *TypeChecker) checkBoolMatchArm(pattern ast.Pattern) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.BindingPattern:
		tc.checkBoolMatchArm(p.Pattern)
	case *ast.LiteralPattern:
		kind := literalPatternKind(p.Value)
		if kind != types.Boolean {
			tc.addError(p.GetLocation(), SeverityError,
				"literal pattern '%s' is not a boolean value (expected true or false)", p.Value)
		}
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"this pattern is not allowed on a bool scrutinee")
	}
}

// boolMatchIsExhaustive reports whether the arms cover both true and false.
// A wildcard/identifier makes it trivially exhaustive; otherwise both unguarded
// true and false literal arms must be present.
func boolMatchIsExhaustive(arms []ast.MatchArm) bool {
	if hasUnguardedCatchAll(arms) {
		return true
	}
	hasTrue, hasFalse := false, false
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		p, ok := arm.Pattern.(*ast.LiteralPattern)
		if !ok {
			continue
		}
		s, _ := p.Value.(string)
		switch s {
		case "true":
			hasTrue = true
		case "false":
			hasFalse = true
		}
	}
	return hasTrue && hasFalse
}

// checkTupleMatchArm validates one arm's pattern against a tuple scrutinee.
func (tc *TypeChecker) checkTupleMatchArm(pattern ast.Pattern, tt types.TupleType) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.BindingPattern:
		tc.checkTupleMatchArm(p.Pattern, tt)
	case *ast.TuplePattern:
		if len(p.Elements) != len(tt.Elements) {
			tc.addError(p.GetLocation(), SeverityError,
				"tuple pattern has %d element(s) but scrutinee has %d",
				len(p.Elements), len(tt.Elements))
			return
		}
		for i, elem := range p.Elements {
			tc.checkTuplePatternElement(elem, tt.Elements[i])
		}
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"expected tuple pattern, got %s", pattern.GetName())
	}
}

// checkTuplePatternElement validates a single element pattern against the
// tuple element's declared type.
func (tc *TypeChecker) checkTuplePatternElement(pattern ast.Pattern, elemType types.Type) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern, *ast.RestPattern:
		return
	case *ast.BindingPattern:
		tc.checkTuplePatternElement(p.Pattern, elemType)
	case *ast.LiteralPattern:
		if elemType == nil {
			return
		}
		elemPrim, ok := elemType.(types.PrimitiveType)
		if !ok {
			return
		}
		kind := literalPatternKind(p.Value)
		if !isAssignable(types.PrimitiveType{Name: kind}, elemPrim) {
			tc.addError(p.GetLocation(), SeverityError,
				"tuple element pattern %s does not match element type %s", p.Value, elemType)
		}
	}
}

// resolveToNamedStructType returns the NamedStructType underlying t, following
// UnresolvedType indirection, or (NamedStructType{}, false) when t is not a struct.
func (tc *TypeChecker) resolveToNamedStructType(t types.Type) (types.NamedStructType, bool) {
	if t == nil {
		return types.NamedStructType{}, false
	}
	if st, ok := t.(types.NamedStructType); ok {
		return st, true
	}
	if u, ok := t.(types.UnresolvedType); ok {
		if decl, exists := tc.symTable.Types[u.Name]; exists {
			if st, ok := decl.Type.(types.NamedStructType); ok {
				return st, true
			}
		}
	}
	return types.NamedStructType{}, false
}

// checkStructMatchArm validates one arm's pattern against a named struct scrutinee.
// Validates that struct pattern fields exist on the struct.
func (tc *TypeChecker) checkStructMatchArm(pattern ast.Pattern, st types.NamedStructType) {
	switch p := pattern.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return
	case *ast.BindingPattern:
		tc.checkStructMatchArm(p.Pattern, st)
	case *ast.StructPattern:
		// A named struct pattern (`Pt { … }`) must name the scrutinee's type; the
		// brace-only form (`{ … }`, Name "") matches any struct structurally.
		if p.Name != "" && p.Name != st.Name {
			tc.addError(p.GetLocation(), SeverityError,
				"struct pattern names %s but the scrutinee is %s", p.Name, st.Name)
			return
		}
		for _, field := range p.Fields {
			if !structHasField(st, field.Name) {
				tc.addError(field.GetLocation(), SeverityError,
					"struct %s has no field %q", st.Name, field.Name)
			}
		}
	default:
		tc.addError(pattern.GetLocation(), SeverityError,
			"expected struct pattern, got %s", pattern.GetName())
	}
}

func structHasField(st types.NamedStructType, name string) bool {
	for _, f := range st.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// checkDuplicateMatchArms warns about identical literal arms and overlapping
// numeric range intervals (which make later arms unreachable).
func (tc *TypeChecker) checkDuplicateMatchArms(arms []ast.MatchArm) {
	// Detect duplicate literal patterns across all scrutinee types.
	seen := make(map[string]bool)
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		p, ok := arm.Pattern.(*ast.LiteralPattern)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v", p.Value)
		if seen[key] {
			tc.addError(arm.Pattern.GetLocation(), SeverityWarning,
				"duplicate match arm: pattern %s is already covered by an earlier arm", key)
		}
		seen[key] = true
	}

	// Detect overlapping range intervals — only for arms that use RangePattern
	// (literal point duplicates are already caught above).
	type intervalLoc struct {
		lo, hi int64
		loc    ast.Location
	}
	var ivs []intervalLoc
	for _, arm := range arms {
		if arm.Guard != nil {
			continue
		}
		if _, isRange := arm.Pattern.(*ast.RangePattern); !isRange {
			continue
		}
		lo, hi, ok := armIntInterval(arm)
		if ok && lo <= hi {
			ivs = append(ivs, intervalLoc{lo, hi, arm.Pattern.GetLocation()})
		}
	}
	if len(ivs) < 2 {
		return
	}
	sort.Slice(ivs, func(i, j int) bool {
		return ivs[i].lo < ivs[j].lo
	})
	maxHi := ivs[0].hi
	for i := 1; i < len(ivs); i++ {
		if ivs[i].lo <= maxHi {
			tc.addError(ivs[i].loc, SeverityWarning,
				"overlapping match arm: this range overlaps with a previous arm")
		}
		if ivs[i].hi > maxHi {
			maxHi = ivs[i].hi
		}
	}
}

// literalPatternKind classifies a literal pattern as UntypedInt, UntypedFloat,
// Boolean, String, or Rune. A character literal is stored pre-decoded as an
// ast.RunePatternValue; every other kind is classified from its raw source text.
func literalPatternKind(value any) types.PrimitiveTypeName {
	if _, ok := value.(ast.RunePatternValue); ok {
		return types.Rune
	}
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
//
// The body is checked inside the loop's own scope (registered on the loop node by
// the collector, RecordScope(loop, loopScope)), which holds the loop variable(s) —
// so `for i in 0..<n { … i … }` resolves `i`. Without entering it the body was
// checked in the enclosing scope and every use of the loop variable was an
// "undefined identifier" (no existing test exercised a non-empty body, so the gap
// went unnoticed). Body-local declarations still don't resolve — the same
// value-copy `Body` limitation as the C-style loop (see checkForLoopExpr).
func (tc *TypeChecker) checkForInLoopExpr(expr *ast.ForInLoopExpr) types.Type {
	iterType := tc.inferExprType(expr.Iterable)
	if iterType != nil && !isIterableType(iterType) {
		tc.addError(expr.Iterable.GetLocation(), SeverityError,
			"cannot iterate over %s: expected an array, string, or range", iterType)
	}
	tc.enterScope(expr, func() {
		// Type the loop variable(s) from the iterable so the body resolves them —
		// otherwise a use of the loop variable has no recorded type (the backend then
		// can't lower it). The collector registered each as an untyped VarDeclStmt
		// placeholder in this scope; fill in its Type.
		if iterType != nil {
			tc.bindForInLoopVars(expr, iterType)
		}
		tc.inferBlockType(&expr.Body)
	})
	return nil
}

// bindForInLoopVars records the loop variable types for a for-in loop. The grammar
// exposes two names: Key (the first) and Value (the second, empty for a single
// variable). For a single variable (`for x in xs`) the name holds the element; for
// two (`for i, x in xs`) the first is the index (i64) and the second the element —
// the array convention (there is no map type yet).
func (tc *TypeChecker) bindForInLoopVars(expr *ast.ForInLoopExpr, iterType types.Type) {
	elemType := iterableElementType(iterType)
	if expr.Value == "" {
		tc.setLoopVarType(expr.Key, elemType)
		return
	}
	tc.setLoopVarType(expr.Key, types.PrimitiveType{Name: types.Int64})
	tc.setLoopVarType(expr.Value, elemType)
}

// setLoopVarType fills in the recorded type of a loop-variable placeholder in the
// current scope, so body references to it resolve.
func (tc *TypeChecker) setLoopVarType(name string, t types.Type) {
	if name == "" || t == nil {
		return
	}
	if sym, ok := tc.scope.Lookup(name); ok {
		if vd, ok := sym.(*ast.VarDeclStmt); ok {
			vd.Type = t
		}
	}
}

// iterableElementType returns the element type a for-in loop binds per iteration:
// an array's element, a string's `rune`, or a range's numeric type (a range over
// two untyped literals defaults to i64, matching an untyped literal binding).
func iterableElementType(t types.Type) types.Type {
	switch it := t.(type) {
	case types.StaticArrayType:
		return it.ElementType
	case types.DynamicArrayType:
		return it.ElementType
	case types.RangeType:
		// Prefer a concrete bound. If both bounds are untyped literals, keep the loop
		// variable *untyped* (assignable to any integer width, like an untyped literal)
		// rather than eagerly defaulting to i64 — so `for i in 0..<3 { t = i }` binds i
		// to whatever width t needs.
		if it.Start != nil && !isUntypedNumeric(it.Start) {
			return it.Start
		}
		if it.End != nil && !isUntypedNumeric(it.End) {
			return it.End
		}
		if it.Start != nil {
			return it.Start
		}
		if it.End != nil {
			return it.End
		}
		return types.PrimitiveType{Name: types.Int64}
	}
	if types.IsString(t) {
		return types.PrimitiveType{Name: types.Rune}
	}
	return nil
}

// isUntypedNumeric reports whether t is one of the internal untyped literal types,
// so iterableElementType prefers a concrete range bound over an untyped one.
func isUntypedNumeric(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt || p.Name == types.UntypedFloat
}

// checkForLoopExpr type-checks a C-style for loop.
//
// The whole loop is checked inside the loop's own scope (registered on the loop
// node by the collector, RecordScope(loop, loopScope)), which holds an init
// clause's variable. Entering it lets the init variable resolve in the condition,
// the post clause, and the body — so the three-clause `for var i = 0; i < n; i +=
// 1` form type-checks. The init clause is checked first so its variable's type is
// recorded before those uses.
//
// Known limitation: a `let`/`var` declared *inside* the body still doesn't
// resolve there. The collector puts body-locals in a child block scope keyed to
// the original body pointer, but ForLoopExpr.Body is a value copy, so
// inferBlockType's enterScope can't find that scope and the body is checked in
// loopScope directly. The loop variable lives in loopScope so it resolves; a
// body-local does not (see lyra/todo.md — fixing it means making Body a pointer).
func (tc *TypeChecker) checkForLoopExpr(expr *ast.ForLoopExpr) {
	tc.enterScope(expr, func() {
		if expr.Init != nil {
			tc.checkVarDecl(expr.Init)
		}
		if expr.Condition != nil {
			condType := tc.inferExprType(*expr.Condition)
			if condType != nil && !types.IsBoolean(condType) {
				tc.addError((*expr.Condition).GetLocation(), SeverityError,
					"for loop condition must be boolean, got %s", condType)
			}
			if lit, ok := (*expr.Condition).(*ast.BooleanLiteralExpr); ok {
				tc.addError((*expr.Condition).GetLocation(), SeverityWarning,
					"condition is always %t", lit.Value)
			}
		}
		if expr.Post != nil {
			tc.inferExprType(*expr.Post)
		}
		tc.inferBlockType(&expr.Body)
	})
}

// inferNullCoalescingExpr type-checks a `??` expression. The left operand must
// be a Maybe<T>: nullability is expressible only through Maybe<T>, never as an
// ambient property of every type, so `??` unwraps the optional. The result is
// the payload T unified with the default operand via branchCommonType.
//
// A non-optional left operand can never be null, which makes the `??` pointless;
// it is reported as a warning (lyra-W007). We still recover by treating the left
// type as the payload so one such mistake does not cascade into spurious
// incompatible-type errors.
func (tc *TypeChecker) inferNullCoalescingExpr(expr *ast.NullCoalescingExpr) types.Type {
	optType := tc.inferExprType(expr.Optional)
	defType := tc.inferExprType(expr.Default)
	if optType == nil || defType == nil {
		if optType != nil {
			return optType
		}
		return defType
	}

	// The left operand must be optional (Maybe<T>); unwrap it to its payload.
	payload := optType
	if kind, t, _, ok := tc.resultOrMaybeKind(optType); ok && kind == "Maybe" {
		payload = t
	} else {
		tc.addErrorCode(expr.Optional.GetLocation(), SeverityWarning,
			diag.CodeNonOptionalCoalescing,
			"left operand of `??` is never null: expected a Maybe<T>, got %s", optType)
	}

	common, ok := branchCommonType(payload, defType)
	if !ok {
		tc.addError(expr.GetLocation(), SeverityError,
			"null coalescing operands have incompatible types: left is %s, right is %s",
			payload, defType)
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
