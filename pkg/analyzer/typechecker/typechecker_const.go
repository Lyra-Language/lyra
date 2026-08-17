package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkConstInitializer verifies that a `const` binding's initializer is a
// compile-time constant. A const must be evaluable at compile time, so its value
// may only be:
//   - a literal (integer, float, string, bool, char, regex),
//   - a reference to another `const`,
//   - a purely-constant expression built from the above: unary (`-x`, `!x`),
//     arithmetic / boolean binary ops, string concatenation, a **conversion**
//     (`f64(N)`, `u8(200)`) of a constant operand, or an array/tuple literal whose
//     elements are themselves all constant.
//
// Anything that depends on runtime state — a function call, a non-const variable,
// an interpolated string, a member/index access, etc. — is rejected with
// lyra-E012. The check reports the first (outermost) offending sub-expression so
// the author is pointed at the part that isn't constant.
func (tc *TypeChecker) checkConstInitializer(_ string, expr ast.Expression) {
	offender, ok := tc.firstNonConstant(expr)
	if ok {
		return
	}
	tc.addErrorCode(offender.GetLocation(), SeverityError, diag.CodeNonConstantConstInitializer,
		"`const` initializer must be a compile-time constant: %s is not constant",
		describeNonConstant(offender))
}

// conversionTargetOf reports the primitive a call converts to, when the call is a
// conversion (`u8(x)`, `string(x)`) rather than an ordinary call. It asks
// types.ConversionTargetName — the one shared answer to "is this callee a
// conversion?", so this cannot drift from the conversion rules themselves.
func conversionTargetOf(expr ast.Expression) (types.PrimitiveTypeName, bool) {
	call, ok := expr.(*ast.FunctionCallExpr)
	if !ok || len(call.Arguments) != 1 {
		return "", false
	}
	id, ok := call.Function.(*ast.IdentifierExpr)
	if !ok {
		return "", false
	}
	return types.ConversionTargetName(id.Name)
}

// firstNonConstant walks expr and returns the first sub-expression that is not a
// compile-time constant. The bool is true when expr is fully constant (offender
// is then nil); false when offender holds the non-constant node to report.
func (tc *TypeChecker) firstNonConstant(expr ast.Expression) (ast.Expression, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr, *ast.FloatLiteralExpr, *ast.StringLiteralExpr,
		*ast.BooleanLiteralExpr, *ast.CharacterLiteralExpr, *ast.RegexLiteralExpr:
		return nil, true

	case *ast.IdentifierExpr:
		// A constant identifier is one bound by `const`. The grammar tags const
		// references via IsConst (SCREAMING_CASE), and resolution confirms it.
		if e.IsConst {
			return nil, true
		}
		if sym, ok := tc.scope.Lookup(e.Name); ok {
			if decl, ok := sym.(*ast.VarDeclStmt); ok && decl.IsConstant() {
				return nil, true
			}
		}
		return expr, false

	case *ast.NegationExpr:
		return tc.firstNonConstant(e.Operand)

	case *ast.NotBooleanExpr:
		return tc.firstNonConstant(e.Expression)

	case *ast.MathBinaryOpExpr:
		if off, ok := tc.firstNonConstant(e.Left); !ok {
			return off, false
		}
		return tc.firstNonConstant(e.Right)

	case *ast.BooleanBinaryOpExpr:
		if off, ok := tc.firstNonConstant(e.Left); !ok {
			return off, false
		}
		return tc.firstNonConstant(e.Right)

	case *ast.StringConcatExpr:
		if off, ok := tc.firstNonConstant(e.Left); !ok {
			return off, false
		}
		return tc.firstNonConstant(e.Right)

	case *ast.FunctionCallExpr:
		// A **conversion** of a constant operand is constant. It is spelled as a call,
		// which is why it landed in the default arm and was refused — but `f64(HEIGHT)`
		// depends on nothing but HEIGHT, and a `const` is *inlined as its value
		// expression* and lowered like any other code (llvm.go's identifier arm), so
		// accepting it changes nothing about what the program computes. It only stops
		// refusing a derivation that has to be written out by hand otherwise.
		//
		// This replaced a diagnostic that suggested `const A: u8 = 200` instead of
		// `const A = u8(200)`. The annotation still works and is often the nicer
		// spelling, but it cannot express a conversion *inside* a larger expression —
		// `X_LEN * ASPECT * f64(HEIGHT) / f64(WIDTH)` has no annotation that rescues it,
		// which is what made the advice a workaround rather than an answer.
		//
		// Recursing into the operand rather than accepting outright is what keeps
		// `u8(x)` for a runtime `x` refused, and improves its message: the offender is
		// now the variable, which is the thing that is not constant.
		if _, isConv := conversionTargetOf(e); isConv && len(e.Arguments) == 1 {
			return tc.firstNonConstant(e.Arguments[0])
		}
		return expr, false

	case *ast.ArrayLiteralExpr:
		for _, el := range e.Elements {
			if off, ok := tc.firstNonConstant(el); !ok {
				return off, false
			}
		}
		return nil, true

	case *ast.TupleLiteralExpr:
		for _, el := range e.Elements {
			if off, ok := tc.firstNonConstant(el); !ok {
				return off, false
			}
		}
		return nil, true

	default:
		// Function calls, member/index access, interpolated strings, struct
		// instances, if/match/blocks, etc. all depend on more than literals.
		return expr, false
	}
}

// describeNonConstant gives a short, human-readable label for the offending
// expression in the const-initializer diagnostic.
func describeNonConstant(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		return "variable `" + e.Name + "`"
	case *ast.FunctionCallExpr:
		return "a function call"
	case *ast.MemberExpr:
		return "a member access"
	case *ast.IndexExpr:
		return "an index access"
	case *ast.InterpolatedStringExpr:
		return "an interpolated string"
	case *ast.StructInstanceExpr, *ast.AnonymousStructInstanceExpr:
		return "a struct instance"
	default:
		return "this expression"
	}
}
