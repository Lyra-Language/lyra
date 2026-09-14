package typechecker

import (
	"math/big"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// foldPatternConstants rewrites every `const` bound of a range pattern in pat — at any
// depth — to the literal it folds to, so `LOW..<=HIGH` reaches every later reader as
// `10..<=20`.
//
// **A rewrite, for the reason arrayRepeatCount's is one.** A range bound is read by the
// value check (checkPatternLiterals), exhaustiveness and overlap (patternInterval), the
// value-range pass and both backend match lowerings, and every one of them folds a
// *literal*. Teaching each to resolve a name would be five copies of "what does this const
// mean", in passes that mostly have no scope at hand; one rewrite in the scope the pattern
// is written in is the single answer.
//
// Idempotent: a bound that is already a literal is left alone, which matters because a
// `match` arm is reached both by checkMatchExpr and by the destructuring walk.
func (tc *TypeChecker) foldPatternConstants(pat ast.Pattern) {
	ast.WalkPattern(pat, func(p ast.Pattern) bool {
		if rp, ok := p.(*ast.RangePattern); ok {
			rp.Start = tc.foldRangeBound(rp.Start)
			rp.End = tc.foldRangeBound(rp.End)
		}
		return true
	})
}

// foldRangeBound answers the literal a `const` range bound stands for, or the bound
// unchanged when it is not a name. The grammar admits a number literal or a
// `const_identifier` there, so a name is the only thing to resolve.
//
// A name that is not a `const`, or a `const` that is not a number, is reported and left
// in place: the error stops the build, and the backend's own refusal of a non-literal
// bound (rule 5) stays behind it.
func (tc *TypeChecker) foldRangeBound(bound ast.Expression) ast.Expression {
	id, isName := bound.(*ast.IdentifierExpr)
	if !isName {
		return bound
	}
	loc := id.GetLocation()
	init, isConst := tc.constInitializer(id.Name)
	if !isConst {
		tc.addError(loc, SeverityError,
			"a range-pattern bound must be a number or a `const`; %s is not a `const`", id.Name)
		return bound
	}
	// Record the name's type before it is replaced: the node lives on in
	// RangePattern.ConstBounds, where hover finds it as the use of the const it is.
	tc.inferExprType(id)
	if n, ok := ast.FoldBigExpr(id, tc.constInitializer); ok {
		if lit, fits := integerLiteralOf(n, loc); fits {
			return lit
		}
		tc.addError(loc, SeverityError,
			"range-pattern bound %s is %s, which is outside the 128-bit range", id.Name, n)
		return bound
	}
	if f, ok := tc.foldConstFloat(init, map[string]bool{id.Name: true}); ok {
		return floatLiteralOf(f, loc)
	}
	tc.addError(loc, SeverityError,
		"range-pattern bound %s must be a compile-time number; its `const` initializer does not fold to one", id.Name)
	return bound
}

// foldConstFloat folds a `const` float initializer: a float literal, its negation, or a
// name for another such `const`. Float arithmetic is not folded — a bound computed in
// floating point is one whose exact value the author cannot see, which is the thing a
// range pattern on a float must not be vague about.
func (tc *TypeChecker) foldConstFloat(e ast.Expression, seen map[string]bool) (float64, bool) {
	switch v := e.(type) {
	case *ast.FloatLiteralExpr:
		return v.Value, true
	case *ast.NegationExpr:
		f, ok := tc.foldConstFloat(v.Operand, seen)
		return -f, ok
	case *ast.IdentifierExpr:
		if seen[v.Name] {
			return 0, false
		}
		init, ok := tc.constInitializer(v.Name)
		if !ok {
			return 0, false
		}
		seen[v.Name] = true
		return tc.foldConstFloat(init, seen)
	}
	return 0, false
}

// integerLiteralOf spells n the way the collector spells a written bound: a non-negative
// literal, wrapped in a NegationExpr when n is negative — the shape extractIntFromExpr and
// the backend's constIntFromExpr read. It reports false beyond the 128-bit magnitude a
// literal can carry.
func integerLiteralOf(n *big.Int, loc ast.Location) (ast.Expression, bool) {
	mag := new(big.Int).Abs(n)
	if mag.Cmp(ast.Uint128Max) > 0 {
		return nil, false
	}
	lit := &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Base:     ast.IntegerBase10,
	}
	switch {
	case mag.IsInt64():
		lit.Value = mag.Int64()
	case mag.IsUint64():
		lit.Value = int64(mag.Uint64())
		lit.Unsigned = true
	default:
		lit.Wide = mag
	}
	if n.Sign() >= 0 {
		return lit, true
	}
	return &ast.NegationExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  lit,
	}, true
}

// floatLiteralOf is integerLiteralOf's float twin. A float literal carries its sign, so
// no NegationExpr is needed — constNumericFromExpr and the backend's float bound reader
// take either.
func floatLiteralOf(f float64, loc ast.Location) ast.Expression {
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    f,
	}
}
