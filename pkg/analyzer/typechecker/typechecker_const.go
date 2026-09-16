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
//     (`f64(N)`, `u8(200)`) of a constant operand, a **float builtin** over constant
//     operands (`5.0.sqrt()`, `2.0.pow(10.0)`, `2.7.floor()`), or an array/tuple literal
//     whose elements are themselves all constant.
//
// Anything that depends on runtime state — a function call, a non-const variable,
// an interpolated string, a member/index access, etc. — is rejected with
// lyra-E012. The check reports the first (outermost) offending sub-expression so
// the author is pointed at the part that isn't constant.
func (tc *TypeChecker) checkConstInitializer(_ string, expr ast.Expression) {
	pending := len(tc.constBuiltinCalls)
	offender, ok := tc.firstNonConstant(expr)
	if ok {
		return
	}
	// Refused for some other reason, so drop the builtin calls this walk accepted on the
	// way down: they are owed no verification sweep, and leaving them queued would report
	// a second diagnostic about an initializer already reported.
	tc.constBuiltinCalls = tc.constBuiltinCalls[:pending]
	tc.addErrorCode(offender.GetLocation(), SeverityError, diag.CodeNonConstantConstInitializer,
		"`const` initializer must be a compile-time constant: %s is not constant",
		describeNonConstant(offender))
}

// isFloatBuiltinName reports whether name is one of the compiler's float builtins — the
// three registries in builtins.go, which is what makes this one question rather than a
// fourth list to keep in step (rule 8).
//
// **A name, not a resolution.** This walk runs before the initializer is inferred, so a
// declaration shadowing a builtin of the same name is indistinguishable here; that is what
// verifyConstBuiltinCalls exists to catch.
func isFloatBuiltinName(name string) bool {
	return floatUnaryMathOps[name] || floatBinaryMathOps[name] || floatRoundingOps[name]
}

// floatBuiltinCallShape reads a call that *might* be a float builtin, in either spelling:
// the method form `5.0.sqrt()` (receiver plus arguments) and the free form `sqrt(5.0)`
// (first argument is the receiver). It answers the method name and every operand the
// constancy walk must recurse into — the receiver included, since a runtime receiver is
// what keeps `x.sqrt()` refused.
//
// A call whose callee is neither shape answers false, and the caller's default refuses it.
func floatBuiltinCallShape(call *ast.FunctionCallExpr) (string, []ast.Expression, bool) {
	switch callee := call.Function.(type) {
	case *ast.MemberExpr:
		return callee.Property.Name, append([]ast.Expression{callee.Object}, call.Arguments...), true
	case *ast.IdentifierExpr:
		// A free-form builtin takes its receiver from the first argument, so a bare
		// `sqrt()` is not one — and is left to report as the undefined function it is.
		if len(call.Arguments) == 0 {
			return "", nil, false
		}
		return callee.Name, call.Arguments, true
	}
	return "", nil, false
}

// verifyConstBuiltinCalls is the second half of the float-builtin arm, run once the whole
// program has been inferred: every call the constancy walk accepted by *name* is asked
// whether it actually resolved to a compiler builtin, and refused if it did not.
//
// **Because a name does not identify a declaration (rule 9).** The builtins are consulted
// last in member dispatch, so a trait method named `sqrt` implemented for `f64` wins over
// the compiler's — and a `const` holding a call to *that* is not compile-time at all: it
// inlines a real call at every use site, once per site, effects included. Matching on the
// name alone would accept it silently, which is the failure this pass is for.
//
// A sweep rather than a check in place, on the `checkUnpinnedNullPtrs` model: the answer
// only exists once inference has run, and "no builtin was recorded here" is a not-yet
// until the pass is over.
//
// **A call that resolved to nothing is passed over in silence**, because it has already
// been reported — `5.sqrt()` is an i64 receiver with no such method, which is the most
// likely way to get here and gets `lyra-E001` naming the real problem. Reporting it again
// as "not constant" would be a second diagnostic for one mistake, and a misleading one:
// it would blame a declared method where none exists. Only a call that genuinely resolved
// to a declaration is refused here.
func (tc *TypeChecker) verifyConstBuiltinCalls() {
	for _, call := range tc.constBuiltinCalls {
		if tc.methodTable.IsBuiltinMethod(call) || !tc.callResolved(call) {
			continue
		}
		name := "this method"
		if spelled, _, ok := floatBuiltinCallShape(call); ok {
			name = "`" + spelled + "`"
		}
		tc.addErrorCode(call.GetLocation(), SeverityError, diag.CodeNonConstantConstInitializer,
			"`const` initializer must be a compile-time constant: %s resolves to a declaration that shadows the compiler's float builtin, so it is a function call",
			name)
	}
	tc.constBuiltinCalls = nil
}

// callResolved reports whether dispatch found this call something to call, so the sweep can
// stay quiet about one that was already refused with an error of its own.
//
// **The two spellings fail differently, so both signals are needed.**
//
// A *method* callee that resolved is recorded — a trait impl in the method table, a bound
// candidate beside it, a receiver-overloaded callee in the type table — and one that
// resolved to nothing is recorded nowhere. So for that spelling, "found in none of the
// three" is exactly "already reported", which is what keeps `5.sqrt()` to its one
// diagnostic about an i64 receiver.
//
// A *free* callee is the opposite: an ordinary call to a declared function appears in none
// of those three either (`TypeTable.Callee` holds only receiver-overloaded calls), so the
// same test would read a perfectly resolved call as unresolved — and let through the very
// case this sweep exists for, a user's own `sqrt` shadowing the builtin. For that spelling
// the question is asked as the negative instead: `SetUnresolvedCallee` is the flag the
// typechecker sets precisely when it has reported a callee it could not find.
//
// A free call that *was* desugared never reaches here — its callee is a MemberExpr by now,
// and a resolved builtin short-circuits on IsBuiltinMethod before this is asked.
func (tc *TypeChecker) callResolved(call *ast.FunctionCallExpr) bool {
	if _, ok := tc.methodTable.Get(call); ok {
		return true
	}
	if _, ok := tc.methodTable.GetBound(call); ok {
		return true
	}
	if _, ok := tc.typeTable.Callee(call); ok {
		return true
	}
	if _, isFreeForm := call.Function.(*ast.IdentifierExpr); isFreeForm {
		return !tc.typeTable.IsUnresolvedCallee(call)
	}
	return false
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
		// A **float builtin over constant operands is constant**: `5.0.sqrt()`,
		// `2.0.pow(10.0)`, `2.7.floor()`. Same grounds as the conversion arm above — a
		// `const` is inlined as its value *expression* and lowered like any other code, and
		// these lower to an LLVM intrinsic or a libm call over literal operands, which the
		// optimizer folds to the number. So accepting them changes nothing about what the
		// program computes; it stops refusing a derivation that otherwise has to be
		// hand-computed and pasted in as digits, which is a typo nothing catches.
		//
		// **Two caveats, both real and both deliberate.** At `-O0` nothing folds, so the
		// call survives at each use site — and a const is inlined at *every* one, so a
		// const in a loop is a call per iteration there. And LLVM folds a libm call by
		// calling the **host's** libm, which is exact for `sqrt` (IEEE 754 requires it
		// correctly rounded) but not for the transcendentals, where a cross-compile can
		// bake the build host's last ulp into the target's binary.
		//
		// The receiver and every argument are walked rather than accepted outright, which
		// is what keeps `x.sqrt()` for a runtime `x` refused with the variable named.
		//
		// **Both spellings, because a builtin may be written either way**: `5.0.sqrt()` and
		// `sqrt(5.0)` are the same call, and the free form is folded onto the method form by
		// `desugarBuiltinFreeCall` — but that happens during *inference*, and this walk runs
		// before it. So the identifier callee has to be recognised here too; by the time the
		// verification sweep looks at the queued call, the desugar has been and gone and it
		// is a MemberExpr like any other.
		if name, operands, ok := floatBuiltinCallShape(e); ok && isFloatBuiltinName(name) {
			for _, operand := range operands {
				if off, ok := tc.firstNonConstant(operand); !ok {
					return off, false
				}
			}
			tc.constBuiltinCalls = append(tc.constBuiltinCalls, e)
			return nil, true
		}
		return expr, false

	case *ast.ArrayLiteralExpr:
		for _, el := range e.Elements {
			if off, ok := tc.firstNonConstant(el); !ok {
				return off, false
			}
		}
		return nil, true

	case *ast.StructInstanceExpr:
		// `const WHITE: Color = Color { r: 245, g: 245, b: 245, a: 255 }` is as constant
		// as the array literal above, and for the same reason: a struct literal computes
		// nothing, it *is* its fields, so it is constant exactly when they are.
		//
		// **This is not compile-time evaluation**, which is what it was mistaken for when
		// it was refused. Nothing here runs a constructor or folds a call — the walk is
		// structural, the same one the array arms take, and a field holding a call is
		// still refused with that call named. What it removes is a rule that made
		// `Color { … }` less constant than `[245, 245, 245, 255]`, which nothing about
		// either justified.
		//
		// A record-update base (`Other { base | r: 1 }`) is refused, and the offender is
		// the **literal** rather than the base: the base is very often a `const` and
		// naming it produces "variable `BASE` is not constant" about a thing that
		// plainly is. What is not constant is the update form.
		if e.BaseStruct != nil {
			return e, false
		}
		for _, f := range e.Fields {
			if f.Value == nil {
				continue
			}
			if off, ok := tc.firstNonConstant(f.Value); !ok {
				return off, false
			}
		}
		return nil, true

	case *ast.AnonymousStructInstanceExpr:
		for _, f := range e.Fields {
			if f.Value == nil {
				continue
			}
			if off, ok := tc.firstNonConstant(f.Value); !ok {
				return off, false
			}
		}
		return nil, true

	case *ast.ArrayRepeatExpr:
		// `const XS = [7; 3]` is as constant as `const XS = [1, 2, 3]`, and was refused as
		// "not a compile-time constant" without this arm.
		//
		// **Both halves must be constant here**, unlike the newtype-literal rule in
		// assignable.go where only the value is consulted. The questions differ: there,
		// provenance is about where the *elements* came from and a runtime length is still
		// a fine `[]T`; here the whole value has to be known at compile time, and a runtime
		// count means it is not.
		if off, ok := tc.firstNonConstant(e.Value); !ok {
			return off, false
		}
		if e.Count != nil {
			if off, ok := tc.firstNonConstant(e.Count); !ok {
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
		// Function calls, member/index access, interpolated strings, if/match/blocks
		// and the rest all depend on more than literals. **Struct instances left this
		// list on 09/09**: a struct literal computes nothing and is constant exactly
		// when its fields are, which is the same rule the array and tuple arms above
		// apply.
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
	case *ast.StructInstanceExpr:
		// A struct literal is constant when its fields are (09/09), so reaching here
		// means the *update* form, which is the only shape of it that is not.
		if e.BaseStruct != nil {
			return "a struct update"
		}
		return "a struct instance"
	case *ast.AnonymousStructInstanceExpr:
		return "a struct instance"
	default:
		return "this expression"
	}
}
