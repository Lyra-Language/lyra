package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// This file lowers `??` — null coalescing. `a ?? b` is `?`'s value-position sibling
// (try.go) and the same match in disguise:
//
//	match a {
//	  Some(v) => v,
//	  None    => b,
//	}
//
// Everything `?` needs beyond a match — rebuilding the failure at the enclosing
// return type, the transfer-vs-duplicate split on the propagated payload — `??` does
// not: nothing leaves the expression, so both arms feed one phi and the ordinary
// merge rules apply. The default is **lazy** — an arm, evaluated only when the
// optional is None — which is why `m ?? panic("missing")` is a meaningful spelling
// and why this cannot lower both operands eagerly and select between them.
//
// Ownership follows the match rules (ownership.go's NullCoalescingExpr case): the
// optional is borrowed as a scrutinee, the default arm is coerced to owned by its own
// node's marks, and the merged value is a uniformly-owned temporary the enclosing
// statement releases once. The Some arm's payload is the one value with no node of
// its own to mark, so its +1 is emitted here directly — the same arrangement as `?`'s
// failure rewrap.
func (l *lowerer) lowerNullCoalescing(block *ir.Block, e *ast.NullCoalescingExpr) (value.Value, *ir.Block, error) {
	opT, ok := l.recordedType(e.Optional)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for the left operand of `??`")
	}
	// A left operand that is not a Maybe can never be null. The typechecker warned
	// (lyra-W007) and recovered so one mistake would not cascade, but there is no
	// meaning here to lower — refusing loudly beats inventing one (rule 5), and the
	// fix is deleting the `??`.
	opDt, ok := l.resolveDataType(opT)
	if !ok {
		return nil, nil, fmt.Errorf(
			"llvm: `??` on a %s, which can never be null — remove the `??` (lyra-W007)", opT)
	}
	shape, err := canonicalTryShape(opDt)
	if err != nil || shape.kind != "Maybe" {
		return nil, nil, fmt.Errorf(
			"llvm: `??` on a %s, which can never be null — remove the `??` (lyra-W007)", opT)
	}
	fieldTypes := shape.success.FieldTypes()
	if len(fieldTypes) != 1 {
		return nil, nil, fmt.Errorf(
			"llvm: `??` on a Maybe whose Some carries %d fields", len(fieldTypes))
	}

	// The operand is a borrow for the duration of the test, exactly as a match
	// scrutinee is; a `shared` Maybe arrives as a box pointer, so unbox it first.
	whole, block, err := l.lowerExpr(block, e.Optional)
	if err != nil {
		return nil, nil, err
	}
	scrut, err := l.unboxSharedData(block, whole)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `??` operand did not lower to a struct (%s)", scrut.Type())
	}
	tagTy, ok := unionTy.Fields[0].(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `??` operand union has a non-integer tag (%s)", unionTy.Fields[0])
	}

	fn := block.Parent
	someBlock := fn.NewBlock("")
	noneBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	isSome := block.NewICmp(enum.IPredEQ,
		block.NewExtractValue(scrut, 0), constant.NewInt(tagTy, int64(shape.successTag)))
	block.NewCondBr(isSome, someBlock, noneBlock)

	// Some: the payload, coerced to +1 when managed — the scrutinee still owns (and
	// drops) its copy, so this is a duplicate, never a move: the match-arm binding rule.
	payload, err := l.extractDataPayload(someBlock, scrut, shape.success)
	if err != nil {
		return nil, nil, err
	}
	someVal := someBlock.NewExtractValue(payload, 0)
	if err := l.deepRetain(someBlock, someVal, fieldTypes[0]); err != nil {
		return nil, nil, err
	}
	someBlock.NewBr(mergeBlock)

	// None: the default, lazily. Its own temporaries stay pending and flush in their
	// production blocks (the not-dominating case of flushStmtTemps); its value's +1 is
	// its own node's Retain mark, applied by lowerExpr's wrapper. A diverging default
	// (`m ?? panic("…")`) seals its block and feeds the phi nothing.
	defVal, noneEnd, err := l.lowerBranchValue(noneBlock, e.Default)
	if err != nil {
		return nil, nil, err
	}
	noneReaches := noneEnd.Term == nil
	if noneReaches {
		noneEnd.NewBr(mergeBlock)
	}

	incomings := []*ir.Incoming{ir.NewIncoming(someVal, someBlock)}
	if noneReaches {
		if defVal == nil {
			return nil, nil, fmt.Errorf("llvm: `??` default produced no value")
		}
		incomings = append(incomings, ir.NewIncoming(defVal, noneEnd))
	}
	return mergeBlock.NewPhi(incomings...), mergeBlock, nil
}
