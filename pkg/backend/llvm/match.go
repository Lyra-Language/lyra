package llvm

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerMatch lowers a `match` expression, dispatching on the scrutinee kind:
// `data` (tag switch, or the if-else ladder when a payload value test or a guard
// rules the switch out), struct/tuple (shared aggregate ladder), and bool/integer
// scalar (comparison ladder). String, float, and array scrutinees are deferred
// with a loud error (those types don't lower at all).
func (l *lowerer) lowerMatch(block *ir.Block, e *ast.MatchExpr) (value.Value, *ir.Block, error) {
	scrutType, ok := l.recordedType(e.Scrutinee)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for match scrutinee")
	}
	if dt, ok := l.resolveDataType(scrutType); ok {
		return l.lowerDataMatch(block, e, dt)
	}
	if st, ok := l.resolveStructType(scrutType); ok {
		return l.lowerStructMatch(block, e, st)
	}
	if tt, ok := l.resolveTupleType(scrutType); ok {
		return l.lowerTupleMatch(block, e, tt)
	}
	if dyn, ok := scrutType.(types.DynamicArrayType); ok {
		return l.lowerArrayMatch(block, e, dyn)
	}
	// A scalar scrutinee (bool, a concrete integer, a float, or a string) lowers to
	// an if-else ladder of comparisons. Detected by whether the scrutinee's
	// primitive maps to an LLVM integer (i1 for bool, iN for ints), float, or the
	// string fat-pointer struct.
	if prim, ok := scrutType.(types.PrimitiveType); ok {
		if ll, ok := LLVMPrimitive(prim.Name); ok {
			switch ll.(type) {
			case *lltypes.IntType, *lltypes.FloatType:
				return l.lowerScalarMatch(block, e, prim)
			case *lltypes.StructType: // string
				if prim.Name == types.String {
					return l.lowerScalarMatch(block, e, prim)
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("llvm: match on %s not implemented yet (only data types, structs, tuples, and integer/bool/float/string scalars)", scrutType)
}

// matchBindsWhole reports whether any arm binds the whole scrutinee to a name (an
// identifier catch-all other than `_`). For a `shared` scrutinee that means the
// box pointer must be kept available for the binding, not only the unboxed union.
func matchBindsWhole(e *ast.MatchExpr) bool {
	for _, arm := range e.MatchArms {
		if ip, ok := arm.Pattern.(*ast.IdentifierPattern); ok && ip.Name != "_" {
			return true
		}
	}
	return false
}

// matchHasGuard reports whether any arm carries an `if` guard.
func matchHasGuard(e *ast.MatchExpr) bool {
	for _, arm := range e.MatchArms {
		if arm.Guard != nil {
			return true
		}
	}
	return false
}

// lowerGuardedArmBody lowers an arm's body starting from `matched` — a block in
// which the pattern has already matched and its bindings are installed. With no
// guard it lowers the body directly there. With a guard it evaluates the guard
// condition in `matched` (the bindings are in scope, so `Some(x) if x > 0` works)
// and cond-branches to the body (guard true) or to `next` (guard false — so the
// following arm is tried, exactly as a failed pattern test does). `lowerBody` is
// the ladder's per-arm epilogue: lower the body and wire its value into the merge
// phi.
func (l *lowerer) lowerGuardedArmBody(matched *ir.Block, guard *ast.GuardExpr, body ast.Expression, next *ir.Block, lowerBody func(*ir.Block, ast.Expression) error) error {
	if guard == nil {
		return lowerBody(matched, body)
	}
	guardVal, gEnd, err := l.lowerExpr(matched, guard.Condition)
	if err != nil {
		return err
	}
	bodyBlock := matched.Parent.NewBlock("")
	gEnd.NewCondBr(guardVal, bodyBlock, next)
	return lowerBody(bodyBlock, body)
}

// matchMerge is the value-producing half of every `match`, whatever shape its arms
// take: the block the arms converge on, and the phi that picks whichever one ran.
//
// All three lowerings need exactly this and nothing more — the if-else ladder
// (lowerMatchLadder), the `data` tag switch (lowerDataMatch) and the array ladder
// (lowerArrayMatch) — and each carried its own copy until 08/05, including a local
// `type incoming struct` declared three times over. The bookkeeping is small but the
// invariant in it is not: an arm only reaches the merge **if its block is still open**,
// so a body that diverged (a `return`, a `panic`) must contribute neither a branch nor
// a phi incoming, and a match whose arms *all* diverged has no value at all rather than
// an empty phi.
type matchMerge struct {
	block     *ir.Block
	incomings []*ir.Incoming
	// void records that some arm reached the merge without producing a value — a
	// `match` used as a *statement*, whose arms end in an assignment or another
	// statement form. The whole match then has no value, exactly as a one-armed or
	// void `if` has none (lowerIf's "reaching branch is void" rule), rather than a
	// phi over a nil operand.
	void bool
}

// newMatchMerge creates the block the arms will converge on.
func newMatchMerge(fn *ir.Func) *matchMerge {
	return &matchMerge{block: fn.NewBlock("")}
}

// arm records one lowered arm. `end` is the block its body finished in and `val` the
// value it produced; a sealed `end` means the arm diverged and is dropped.
func (m *matchMerge) arm(val value.Value, end *ir.Block) {
	if end.Term != nil {
		return
	}
	end.NewBr(m.block)
	// A *reaching* arm with no value makes the match void. Note the two nil cases are
	// different and only one lands here: an arm that **diverged** returned above with
	// its block already sealed and contributes nothing, while an arm that merely
	// produced no value still reaches the merge and control still continues past the
	// match — there is just nothing to phi.
	//
	// `isVoidResult` rather than `val == nil`, because "produced no value" has two
	// spellings: a builtin hands back nil, and a call to a user-defined `void` function
	// hands back the `ir.Call`, non-nil with a void type. `match x { 0 => f(), _ => g() }`
	// over void functions built `phi void` and clang refused the module — the `if` form
	// of this is the twin in control_flow.go, and both had to be fixed in one change.
	if isVoidResult(val) {
		m.void = true
		return
	}
	m.incomings = append(m.incomings, ir.NewIncoming(val, end))
}

// value finishes the merge, yielding the phi and the block to keep lowering into. A
// nil value means every arm diverged, so there is nothing to merge.
func (m *matchMerge) value() (value.Value, *ir.Block) {
	// A void arm means the match is a statement, so there is no value even though
	// other arms may have produced one — mixing them into a phi would give the match
	// a value on some paths and not others. (The typechecker has already established
	// the arms agree; a mix here means the match is being used for effect and one arm
	// happens to end in an expression.)
	if m.void || len(m.incomings) == 0 {
		return nil, m.block
	}
	return m.block.NewPhi(m.incomings...), m.block
}

// lowerScalarMatch lowers a `match` on a scalar scrutinee — a bool, a concrete
// integer, a float, or a string — as an if-else ladder: each non-catch-all arm
// becomes a comparison (`icmp`/`fcmp` for a numeric literal, a byte-equality test
// for a string literal, a two-sided range check for a range pattern) that
// cond-brs to the arm body or on to the next test, in source order
// (first match wins). A wildcard or identifier arm is an unconditional match that
// ends the ladder (an identifier binds the scrutinee value); later arms are
// unreachable. An `if` guard adds a second test after the pattern (via
// lowerGuardedArmBody): when it fails, control falls through to the next arm, so a
// *guarded* catch-all doesn't end the ladder. Arm bodies feed a merge phi, so the
// match is a value like `if`.
//
// (A pure-literal integer match would lower more compactly to an LLVM `switch`;
// the ladder is used uniformly because a range pattern — `0..<10` — isn't a single
// switch case, and a float match has no switch form at all. The optimizer recovers
// the switch for the integer literal-only shape.)
//
// The fall-through past the last test is `unreachable`: a bool match covering
// true/false, or an exhaustive integer match, never reaches it; a non-exhaustive
// (or float, which always needs a wildcard) match was already warned by the
// typechecker.
func (l *lowerer) lowerScalarMatch(block *ir.Block, e *ast.MatchExpr, scrutPrim types.PrimitiveType) (value.Value, *ir.Block, error) {
	scrut, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	scrutTy := scrut.Type()
	switch scrutTy.(type) {
	case *lltypes.IntType, *lltypes.FloatType:
	case *lltypes.StructType: // string fat pointer
		if !isStringLLVMType(scrutTy) {
			return nil, nil, fmt.Errorf("llvm: scalar match scrutinee lowered to an unexpected struct (%s)", scrutTy)
		}
	default:
		return nil, nil, fmt.Errorf("llvm: scalar match scrutinee did not lower to an integer, float, or string (%s)", scrutTy)
	}
	isBool := scrutPrim.Name == types.Boolean
	signed := IsSignedInt(scrutPrim.Name)

	// The ladder itself is lowerMatchLadder's — a scalar match is that same shape with
	// a comparison for `test` and nothing to bind. It had its own copy of the merge
	// block, the incoming/phi bookkeeping, the per-arm scope reset, the catch-all
	// handling and the seal until 08/05, which is four invariants maintained twice.
	return l.lowerMatchLadder(block, e, scrut,
		func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
			return l.scalarMatchTest(b, scrut, pat, isBool, signed)
		},
		// A scalar pattern is a test, never a binding: the only name a scalar arm can
		// introduce is a catch-all identifier, and the driver binds that from `whole`.
		func(*ir.Block, ast.Pattern) error { return nil },
	)
}

// scalarMatchTest builds the i1 "does the scrutinee match this pattern?" test for
// one arm of a scalar match: an `icmp eq` for a literal, or a two-sided range
// check (`scrut >= lo && scrut </<= hi`) for a range pattern. signed selects the
// signed vs unsigned comparison predicates. A float scrutinee is delegated to
// floatScalarMatchTest (dispatched on the already-lowered value's LLVM type, so
// isBool/signed are ignored there).
func (l *lowerer) scalarMatchTest(block *ir.Block, scrut value.Value, pattern ast.Pattern, isBool, signed bool) (value.Value, error) {
	if _, isFloat := scrut.Type().(*lltypes.FloatType); isFloat {
		return l.floatScalarMatchTest(block, scrut, pattern)
	}
	if isStringLLVMType(scrut.Type()) {
		return l.stringScalarMatchTest(block, scrut, pattern)
	}
	intTy := scrut.Type().(*lltypes.IntType)
	switch p := pattern.(type) {
	case *ast.LiteralPattern:
		// A rune pattern ('a') is pre-decoded to its code point; compare directly.
		if rv, ok := p.Value.(ast.RunePatternValue); ok {
			return block.NewICmp(enum.IPredEQ, scrut, constant.NewInt(intTy, int64(rv))), nil
		}
		s, ok := p.Value.(string)
		if !ok {
			return nil, fmt.Errorf("llvm: unexpected literal pattern value %T", p.Value)
		}
		if isBool {
			var bit int64
			switch s {
			case "true":
				bit = 1
			case "false":
				bit = 0
			default:
				return nil, fmt.Errorf("llvm: non-bool literal pattern %q on a bool scrutinee", s)
			}
			return block.NewICmp(enum.IPredEQ, scrut, constant.NewInt(intTy, bit)), nil
		}
		// Parsed as a big.Int rather than an int64: a 128-bit scrutinee may be matched
		// against a 128-bit literal, which is exactly the case an int64 parse rejects
		// with a message about strconv rather than about the program.
		n, ok := new(big.Int).SetString(s, 0)
		if !ok {
			return nil, fmt.Errorf("llvm: invalid integer literal pattern %q", s)
		}
		c := constant.NewInt(intTy, 0)
		c.X = n
		return block.NewICmp(enum.IPredEQ, scrut, c), nil
	case *ast.RangePattern:
		// An open bound (`0..`, `..<10`) *omits* its comparison rather than
		// clamping to the type's limit. An absent bound means exactly the type's
		// own min or max, so the comparison it would emit is a tautology — and on
		// an unsigned scrutinee `x >= 0` is not merely redundant but the kind of
		// always-true compare the range analysis reports as lyra-W011 elsewhere.
		// Both bounds absent does not parse (rangeBounds requires one).
		var tests []value.Value
		if p.Start != nil {
			lo, ok := constIntFromExpr(p.Start, intTy)
			if !ok {
				return nil, fmt.Errorf("llvm: unsupported range start in match pattern")
			}
			gePred := enum.IPredUGE
			if signed {
				gePred = enum.IPredSGE
			}
			tests = append(tests, block.NewICmp(gePred, scrut, lo))
		}
		if p.End != nil {
			hi, ok := constIntFromExpr(p.End, intTy)
			if !ok {
				return nil, fmt.Errorf("llvm: unsupported range end in match pattern")
			}
			var hiPred enum.IPred
			switch {
			case types.RangeExcludesEnd(p.EndOperator) && signed:
				hiPred = enum.IPredSLT
			case types.RangeExcludesEnd(p.EndOperator):
				hiPred = enum.IPredULT
			case signed:
				hiPred = enum.IPredSLE
			default:
				hiPred = enum.IPredULE
			}
			tests = append(tests, block.NewICmp(hiPred, scrut, hi))
		}
		switch len(tests) {
		case 0:
			return nil, fmt.Errorf("llvm: range match pattern has neither bound")
		case 1:
			return tests[0], nil
		default:
			return block.NewAnd(tests[0], tests[1]), nil
		}
	default:
		return nil, fmt.Errorf("llvm: match pattern %T not implemented for a scalar scrutinee", pattern)
	}
}

// constIntFromExpr builds an integer constant of type ty from a range-bound
// expression: an integer literal, or a negated one (`-128`). Returns ok=false for
// a bound the backend can't fold at compile time.
func constIntFromExpr(e ast.Expression, ty *lltypes.IntType) (value.Value, bool) {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		// Set the big.Int rather than going through the int64 constructor, so a
		// >64-bit bound is the constant it says rather than 0 (llir's constant.Int is
		// a big.Int underneath).
		c := constant.NewInt(ty, 0)
		c.X = v.BigValue()
		return c, true
	case *ast.NegationExpr:
		if inner, ok := v.Operand.(*ast.IntegerLiteralExpr); ok {
			c := constant.NewInt(ty, 0)
			c.X = new(big.Int).Neg(inner.BigValue())
			return c, true
		}
	}
	return nil, false
}

// floatScalarMatchTest is the float counterpart to scalarMatchTest's integer
// path: an `fcmp oeq` for a literal, or a two-sided ordered range check
// (`scrut >= lo && scrut </<= hi`) for a range pattern. Float comparisons use the
// ordered predicates (false when either side is NaN). A literal float pattern is an
// exact-equality request — the typechecker already warns that float `==` is
// precision-sensitive; the pattern is the user's explicit choice.
func (l *lowerer) floatScalarMatchTest(block *ir.Block, scrut value.Value, pattern ast.Pattern) (value.Value, error) {
	floatTy := scrut.Type().(*lltypes.FloatType)
	switch p := pattern.(type) {
	case *ast.LiteralPattern:
		s, ok := p.Value.(string)
		if !ok {
			return nil, fmt.Errorf("llvm: unexpected literal pattern value %T", p.Value)
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("llvm: invalid float literal pattern %q: %v", s, err)
		}
		return block.NewFCmp(enum.FPredOEQ, scrut, floatConst(floatTy, f)), nil
	case *ast.RangePattern:
		// Open bounds as in the integer path above. Note this keeps the ordered
		// predicates on both sides, so a NaN scrutinee still fails an open range —
		// `0.0..` is "at least 0.0", not "not less than 0.0".
		var tests []value.Value
		if p.Start != nil {
			lo, ok := constFloatFromExpr(p.Start, floatTy)
			if !ok {
				return nil, fmt.Errorf("llvm: unsupported range start in float match pattern")
			}
			tests = append(tests, block.NewFCmp(enum.FPredOGE, scrut, lo))
		}
		if p.End != nil {
			hi, ok := constFloatFromExpr(p.End, floatTy)
			if !ok {
				return nil, fmt.Errorf("llvm: unsupported range end in float match pattern")
			}
			hiPred := enum.FPredOLE
			if types.RangeExcludesEnd(p.EndOperator) {
				hiPred = enum.FPredOLT
			}
			tests = append(tests, block.NewFCmp(hiPred, scrut, hi))
		}
		switch len(tests) {
		case 0:
			return nil, fmt.Errorf("llvm: range match pattern has neither bound")
		case 1:
			return tests[0], nil
		default:
			return block.NewAnd(tests[0], tests[1]), nil
		}
	default:
		return nil, fmt.Errorf("llvm: match pattern %T not implemented for a float scrutinee", pattern)
	}
}

// constFloatFromExpr builds a float constant of type ty from a range-bound
// expression: a float literal, an integer literal (a whole-number bound such as
// `0` written on a float range), or either negated. Returns ok=false for a bound
// the backend can't fold at compile time.
func constFloatFromExpr(e ast.Expression, ty *lltypes.FloatType) (value.Value, bool) {
	switch v := e.(type) {
	case *ast.FloatLiteralExpr:
		return floatConst(ty, v.Value), true
	case *ast.IntegerLiteralExpr:
		f, _ := new(big.Float).SetInt(v.BigValue()).Float64()
		return floatConst(ty, f), true
	case *ast.NegationExpr:
		switch inner := v.Operand.(type) {
		case *ast.FloatLiteralExpr:
			return floatConst(ty, -inner.Value), true
		case *ast.IntegerLiteralExpr:
			f, _ := new(big.Float).SetInt(inner.BigValue()).Float64()
			return floatConst(ty, -f), true
		}
	}
	return nil, false
}

// stringScalarMatchTest is the string counterpart to scalarMatchTest's integer
// path: a literal arm (`"yes" =>`) tests string equality against the scrutinee
// (lowerStringEquality). The pattern's raw source text is quoted (unlike a
// StringLiteralExpr, whose Value the collector already unescaped), so we strip the
// surrounding quotes to get the bytes. An *escaped* pattern (containing a
// backslash) is deferred with a loud error rather than risking a mismatch against
// the collector's Lyra-specific unescaping. Regex patterns are also deferred.
func (l *lowerer) stringScalarMatchTest(block *ir.Block, scrut value.Value, pattern ast.Pattern) (value.Value, error) {
	lp, ok := pattern.(*ast.LiteralPattern)
	if !ok {
		return nil, fmt.Errorf("llvm: match pattern %T not implemented for a string scrutinee (only string literals; regex patterns deferred)", pattern)
	}
	raw, ok := lp.Value.(string)
	if !ok {
		return nil, fmt.Errorf("llvm: unexpected literal pattern value %T", lp.Value)
	}
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, fmt.Errorf("llvm: expected a quoted string pattern, got %q", raw)
	}
	content := raw[1 : len(raw)-1]
	if strings.ContainsRune(content, '\\') {
		return nil, fmt.Errorf("llvm: escaped string patterns not implemented yet (%q)", raw)
	}
	lit := l.lowerStringConstant(block, content)
	return l.lowerStringEquality(block, scrut, lit), nil
}
