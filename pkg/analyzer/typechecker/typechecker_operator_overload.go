package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Arithmetic and bitwise operator overloading.
//
// `a + b` on a user type resolves to a trait method named `(_+_)`, exactly as
// `a.show()` resolves to one named `show` — the same impl matching, the same Self
// substitution, the same ambiguity rule (resolveTraitMethodNamed is one function,
// not two).
//
// **The compiler knows no trait by name here**, which is the difference from
// `Eq`/`Ord`. Those two *are* the comparison operators — `<` and `<=>` must agree, so
// one trait owns them and a second mechanism would be a coherence question with no
// answer. Arithmetic has no such invariant: `+` on a matrix and `+` on a duration are
// unrelated operations, and nothing is gained by insisting they come from one trait.
// So the dispatch key is the **method name**, and the trait is whatever the author
// declared:
//
//	trait Add { (_+_): (Self, Self) -> Self }
//	impl Add for Vec2 { (_+_) = (self, other) => Vec2 { x: self.x + other.x, … } }
//
// Two traits providing the same operator for one type is an ambiguity, reported
// where the operator is written — the same answer the identifier path gives for two
// traits declaring one method name.
//
// **A primitive is never routed here.** `1 + 1` stays a machine add whatever impls
// exist, which is the rule `dispatchEq` and `dispatchOrdCompare` already follow: an
// operator whose meaning on the built-in types can be changed from a library is a
// language whose arithmetic no reader can trust.

// overloadableBinaryOperators maps a math operator to the trait-method name that
// overloads it — the ten operators that are *both* spellable as a method name and
// real operators in the language.
//
// Two gaps fall out of that intersection, and both are the grammar's rather than this
// file's. `%%` (truncating remainder) is an operator with no method spelling, and
// `**` is a method spelling with no operator; neither can be reached, so neither is
// listed. The comparison operators are refused at collection (lyra-E039) because the
// compiler owns them, and `&&`/`||` are refused because a function call cannot
// short-circuit — a `(_&&_)` impl would evaluate its right operand, which is the one
// thing those operators promise not to do.
var overloadableBinaryOperators = map[ast.MathBinaryOp]bool{
	ast.MathBinaryOpAdd:    true,
	ast.MathBinaryOpSub:    true,
	ast.MathBinaryOpMul:    true,
	ast.MathBinaryOpDiv:    true,
	ast.MathBinaryOpMod:    true,
	ast.MathBinaryOpBitAnd: true,
	ast.MathBinaryOpBitOr:  true,
	ast.MathBinaryOpBitXor: true,
	ast.MathBinaryOpShl:    true,
	ast.MathBinaryOpShr:    true,
}

// dispatchBinaryOperator resolves `left <op> right` to a user impl, records it for the
// backend, and returns the result type. ok=false means no impl applies and the caller
// proceeds with the built-in numeric rules (which will report the operands).
func (tc *TypeChecker) dispatchBinaryOperator(expr *ast.MathBinaryOpExpr, left, right types.Type) (types.Type, bool) {
	if left == nil || !overloadableBinaryOperators[expr.Operator] {
		return nil, false
	}
	name := ast.MethodName{Kind: ast.MethodNameKindBinary, Value: string(expr.Operator)}
	return tc.dispatchOperator(expr, name, left, []types.Type{right},
		[]ast.Expression{expr.Right}, string(expr.Operator))
}

// dispatchCompoundOperator is `x += y` — the same dispatch as the binary form, keyed
// on the operator the assignment applies (`ast.MathAssignOp.BinaryOp`, the one mapping
// both the typechecker and the backend read) and on the *target's* type as the
// receiver.
//
// The resolution is recorded on the assignment node, so the backend calls the impl and
// stores the result rather than trying to emit machine arithmetic on a struct.
func (tc *TypeChecker) dispatchCompoundOperator(expr *ast.MathAssignOpExpr, binOp ast.MathBinaryOp, target types.Type) (types.Type, bool) {
	if target == nil || !overloadableBinaryOperators[binOp] {
		return nil, false
	}
	// Record the target's type on the left node. A compound assignment never infers its
	// own left-hand side — it resolves the binding instead — so nothing else puts a type
	// there, and the backend needs one: that is how it recovers the *substituted*
	// receiver type when the operator resolved through a bound and only the
	// specialization names the impl.
	tc.typeTable.Set(expr.Left, target)
	name := ast.MethodName{Kind: ast.MethodNameKindBinary, Value: string(binOp)}
	return tc.dispatchOperator(expr, name, target, []types.Type{tc.inferExprType(expr.Right)},
		[]ast.Expression{expr.Right}, string(expr.Operator))
}

// dispatchUnaryOperator is the prefix form — `-v` and `~v`, whose method names are
// `(-_)` and `(~_)`. Kind is what tells prefix `-` from binary `-`: they are different
// methods with the same spelling, and a type may implement either without the other.
func (tc *TypeChecker) dispatchUnaryOperator(expr ast.Expression, op string, operand types.Type) (types.Type, bool) {
	if operand == nil {
		return nil, false
	}
	name := ast.MethodName{Kind: ast.MethodNameKindPrefix, Value: op}
	return tc.dispatchOperator(expr, name, operand, nil, nil, op)
}

// dispatchOperator is the shared body: refuse a primitive receiver, resolve the impl,
// check the operand arity and types against the trait's declared signature, publish
// the resolution, and answer with the signature's return type.
//
// `args`/`argExprs` are the operands *after* the receiver — one for a binary operator,
// none for a prefix one — so the arity check is the signature's own, not a count this
// function has to know.
func (tc *TypeChecker) dispatchOperator(
	expr ast.Expression, name ast.MethodName, recv types.Type,
	args []types.Type, argExprs []ast.Expression, opText string,
) (types.Type, bool) {
	// A built-in scalar keeps its built-in operator. Checked before the lookup rather
	// than after, so an impl written for `i64` is inert instead of intermittently
	// winning — the same rule the comparison operators follow.
	//
	// The receiver is deliberately NOT newtype-stripped here (08/12). A newtype over a
	// scalar is not the scalar: the built-in numeric rule refuses it ("operands must be
	// numeric, got Cents and Cents"), which is the opt-in working — so the impl lookup
	// is exactly where a newtype must be allowed to proceed. Stripping first made a
	// scalar newtype operator-dead from *both* sides: no machine arithmetic (the
	// numeric rule sees the nominal type) and no impl either (this guard saw the base
	// and bailed), so `impl Add for Cents` parsed, collected, and was silently inert —
	// found 08/12 when lyra-E043 began naming an operator impl as the opt-in path for
	// newtype arithmetic, and the recommended path did not work.
	if types.IsNumeric(recv) || isRuneType(recv) || types.IsBoolean(recv) || types.IsString(recv) {
		return nil, false
	}
	if g, isVar := recv.(types.GenericType); isVar {
		if result, ok := tc.dispatchOperatorViaBound(expr, name, g, args, argExprs, opText); ok {
			return result, true
		}
		// No bound provides it, and unlike `==` there is no structural rule to fall back
		// on. The message names *both* readings, because the author meant one of them and
		// the compiler cannot tell which: the old "operands must be numeric" was true of a
		// type parameter and said nothing about what to do next.
		tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeOperatorNotOverloaded,
			"operator %s: %s is a type parameter — built-in arithmetic needs a numeric type, "+
				"and an overloaded `%s` needs a `where %s: Trait` bound whose trait declares `(_%s_)`",
			opText, recv, opText, g.Name, opText)
		return nil, true
	}

	matches := tc.resolveTraitMethodNamed(recv, name, "")
	switch len(matches) {
	case 0:
		return nil, false
	case 1:
	default:
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s on %s is ambiguous: %s each provide it — a type may take an operator from only one trait",
			opText, recv, traitNamesOf(matches))
		return nil, true
	}
	m := matches[0]
	if m.Signature == nil {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: the %s impl for %s declares no signature for it",
			opText, m.Impl.TraitName, recv)
		return nil, true
	}
	// The receiver is parameter 0, as it is for every trait method, so a binary
	// operator's signature has two parameters and a prefix one's has one.
	want := len(args) + 1
	if len(m.Signature.Parameters) != want {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: %s declares `%s` with %d parameter(s); an operator taking %d operand(s) needs %d (the receiver is the first)",
			opText, m.Impl.TraitName, name.Value, len(m.Signature.Parameters), len(args), want)
		return nil, true
	}
	for i, arg := range args {
		param := m.Signature.Parameters[i+1].Type
		if arg == nil || param == nil {
			continue
		}
		// Narrow an untyped literal operand to the declared parameter first, so
		// `v * 2` against a `(Self, i64) -> Self` impl is the i64 the impl asked for
		// rather than the literal default — the same courtesy an ordinary call gets.
		tc.propagateExpectedType(argExprs[i], param)
		if got, ok := tc.typeTable.Get(argExprs[i]); ok {
			arg = got
		}
		if !isAssignable(arg, param) {
			tc.addError(argExprs[i].GetLocation(), SeverityError,
				"operator %s: %s's `%s` takes %s on the right, got %s",
				opText, m.Impl.TraitName, name.Value, param, arg)
			return nil, true
		}
	}
	tc.checkImplConstraints(m, expr.GetLocation())
	tc.methodTable.SetOperatorResolution(expr, typetable.Resolution{
		Impl:      m.Impl,
		Method:    m.Method,
		Signature: m.Signature,
		Bindings:  m.Bindings,
	})
	// This concrete dispatch fixes the impl's own variables, which makes it an
	// instantiation like any other — and the impl's *body* has bound-dispatched sites of
	// its own. `Box<Box<i64>> + Box<Box<i64>>` selects `impl Add for Box<t>` at
	// `t = Box<i64>`, whose body's `self.v + o.v` must then find the same impl one level
	// down. Nothing else reaches that: the outer call site published for the outer type,
	// and the inner site was checked when `t` was still a variable.
	tc.publishImplBodyCandidates(m)
	result := m.Signature.ReturnType.Type
	if result == nil {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: %s's `%s` declares no return type", opText, m.Impl.TraitName, name.Value)
		return nil, true
	}
	tc.typeTable.Set(expr, result)
	return result, true
}

// dispatchOperatorViaBound resolves an operator whose receiver is a bare type parameter,
// through the `where` bounds in scope: `let sum<t> where t: Add = (a: t, b: t) -> t => a + b`.
//
// This is `dispatchViaGenericBound` for a node that is not a call, and it is deliberately
// the same three steps in the same order — check against the trait's declared signature,
// record the abstract resolution for the purity pass, publish one concrete resolution per
// implementing type for the backend. Abstract dispatch has no single impl to name, so
// **both** of the last two are needed: the first keeps a `pure` function from silently
// admitting an impure impl, the second is how the specialization finds the impl it wants.
//
// The bound is checked at the instantiation by the machinery that already exists
// (lyra-E036), so nothing here has to ask whether `t` really implements the trait — only
// what the trait says the operator does.
func (tc *TypeChecker) dispatchOperatorViaBound(
	expr ast.Expression, name ast.MethodName, g types.GenericType,
	args []types.Type, argExprs []ast.Expression, opText string,
) (types.Type, bool) {
	for _, traitName := range tc.genericBounds[g.Name] {
		trait, ok := tc.symTable.LookupTraitFrom(traitName, expr.GetLocation())
		if !ok {
			continue
		}
		tm := findTraitMethodNamed(trait, name)
		if tm == nil || tm.Signature == nil {
			continue
		}
		// Self is the type *parameter* here, not a concrete type: inside the body `t` is
		// what the operands are, and the signature has to speak in those terms for the
		// operand check below to mean anything.
		sig := substituteSelf(tm.Signature, g)
		want := len(args) + 1
		if len(sig.Parameters) != want {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: %s declares `%s` with %d parameter(s); an operator taking %d operand(s) needs %d (the receiver is the first)",
				opText, traitName, name.Value, len(sig.Parameters), len(args), want)
			return nil, true
		}
		for i, arg := range args {
			param := sig.Parameters[i+1].Type
			if arg == nil || param == nil {
				continue
			}
			tc.propagateExpectedType(argExprs[i], param)
			if got, ok := tc.typeTable.Get(argExprs[i]); ok {
				arg = got
			}
			if !isAssignable(arg, param) {
				tc.addError(argExprs[i].GetLocation(), SeverityError,
					"operator %s: %s's `%s` takes %s on the right, got %s",
					opText, traitName, name.Value, param, arg)
				return nil, true
			}
		}
		tc.methodTable.SetOperatorBound(expr, typetable.BoundMethodRef{Trait: traitName, Method: name.Key()})
		tc.methodTable.SetOperatorCandidates(expr, tc.boundCandidatesByType(traitName, name))
		result := sig.ReturnType.Type
		if result == nil {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: %s's `%s` declares no return type", opText, traitName, name.Value)
			return nil, true
		}
		tc.typeTable.Set(expr, result)
		return result, true
	}
	return nil, false
}
