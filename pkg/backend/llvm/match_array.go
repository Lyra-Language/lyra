package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerArrayMatch lowers a `match` on a dynamic array `[]T` as an if-else ladder
// (first match wins), the array analogue of lowerScalarMatch. Each array-pattern
// arm is a length test AND per-element literal/range tests; a wildcard/identifier
// arm is an unconditional catch-all (an identifier binds the whole array box). Arm
// bodies feed a merge phi, so the match is a value like `if`.
//
// Element bindings (`[a, b]`) and a whole-array rest (`[...rest]`) are *borrows* —
// bound into l.locals but not framed for release, since reading an element or
// aliasing the whole array consumes no reference (the scrutinee's own binding still
// owns the array). An out-of-bounds element read is avoided by testing the length
// *before* touching any element (the element tests run in a block reached only when
// the length already matched).
//
// Deferred, loud errors: a `[head, ...tail]` pattern binding a *tail sub-array*
// (needs an allocation + copy), a rest anywhere but last, and a nested non-scalar
// element pattern. A fixed-size-array (`[N]T`) scrutinee is also deferred here (it
// reaches lowerMatch's array case only for `[]T`).
func (l *lowerer) lowerArrayMatch(block *ir.Block, e *ast.MatchExpr, arrType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	elemLL, err := l.lowerType(arrType.ElementType)
	if err != nil {
		return nil, nil, err
	}
	boxTy := DynArrayBoxType(elemLL)
	box, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	length := block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))
	elemAt := func(b *ir.Block, i int64) value.Value {
		return b.NewLoad(elemLL, b.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), i64c(i)))
	}
	isBool, signed := false, false
	if ep, ok := arrType.ElementType.(types.PrimitiveType); ok {
		isBool = ep.Name == types.Boolean
		signed = IsSignedInt(ep.Name)
	}

	fn := block.Parent
	merge := fn.NewBlock("")
	type incoming struct {
		val value.Value
		end *ir.Block
	}
	var incomings []incoming
	lowerBody := func(b *ir.Block, body ast.Expression) error {
		val, end, err := l.lowerExpr(b, body)
		if err != nil {
			return err
		}
		if end.Term == nil {
			end.NewBr(merge)
			incomings = append(incomings, incoming{val, end})
		}
		return nil
	}

	current := block
	sealed := false
	for _, arm := range e.MatchArms {
		switch p := arm.Pattern.(type) {
		case *ast.WildcardPattern, *ast.IdentifierPattern:
			if ip, ok := p.(*ast.IdentifierPattern); ok && ip.Name != "_" {
				slot := fn.Blocks[0].NewAlloca(box.Type())
				current.NewStore(box, slot)
				l.locals[ip.Name] = slot // borrow of the whole array
			}
			if arm.Guard == nil {
				if err := lowerBody(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
			} else {
				next := fn.NewBlock("")
				if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerBody); err != nil {
					return nil, nil, err
				}
				current = next
			}
		case *ast.ArrayPattern:
			next := fn.NewBlock("")
			matched, err := l.lowerArrayPatternMatch(current, next, p, box, length, elemAt, elemLL, isBool, signed)
			if err != nil {
				return nil, nil, err
			}
			if err := l.lowerGuardedArmBody(matched, arm.Guard, arm.Body, next, lowerBody); err != nil {
				return nil, nil, err
			}
			current = next
		default:
			return nil, nil, fmt.Errorf("llvm: match pattern %T not implemented for an array scrutinee", arm.Pattern)
		}
		if sealed {
			break
		}
	}
	if !sealed {
		l.sealMatchFallthrough(current)
	}

	if len(incomings) == 0 {
		return nil, merge, nil
	}
	incs := make([]*ir.Incoming, len(incomings))
	for i, in := range incomings {
		incs[i] = ir.NewIncoming(in.val, in.end)
	}
	return merge.NewPhi(incs...), merge, nil
}

// lowerArrayPatternMatch tests one `[...]` array-pattern arm against the scrutinee
// and installs its bindings, returning the block in which the pattern has matched
// (from which the caller lowers the body). A failed length or element test branches
// to `next` (the following arm). The length is tested first, so element accesses in
// the test/bind stage are always in bounds.
func (l *lowerer) lowerArrayPatternMatch(current, next *ir.Block, p *ast.ArrayPattern, box, length value.Value, elemAt func(*ir.Block, int64) value.Value, elemLL lltypes.Type, isBool, signed bool) (*ir.Block, error) {
	fn := current.Parent

	// Split off a trailing `...rest`.
	elems := p.Elements
	hasRest := false
	restName := ""
	if n := len(elems); n > 0 {
		if rp, ok := elems[n-1].(*ast.RestPattern); ok {
			hasRest = true
			restName = rp.Identifier
			elems = elems[:n-1]
		}
	}
	for _, el := range elems {
		if _, ok := el.(*ast.RestPattern); ok {
			return nil, fmt.Errorf("llvm: a `...rest` array pattern must be the last element")
		}
	}
	fixedCount := int64(len(elems))
	if hasRest && fixedCount > 0 {
		return nil, fmt.Errorf("llvm: `[head, ...tail]` array patterns (binding a tail sub-array) not implemented yet")
	}

	// Length test: an exact arity for a fixed pattern; `[...rest]` matches any length.
	afterLen := fn.NewBlock("")
	if hasRest {
		current.NewBr(afterLen)
	} else {
		current.NewCondBr(current.NewICmp(enum.IPredEQ, length, i64c(fixedCount)), afterLen, next)
	}

	// Element literal/range tests — the length now matches, so every index is valid.
	var cond value.Value
	for i, el := range elems {
		test := el
		if bp, ok := el.(*ast.BindingPattern); ok {
			test = bp.Pattern // `x @ <pat>`: test the inner pattern
		}
		var t value.Value
		var err error
		switch test.(type) {
		case *ast.LiteralPattern, *ast.RangePattern:
			t, err = l.scalarMatchTest(afterLen, elemAt(afterLen, int64(i)), test, isBool, signed)
			if err != nil {
				return nil, err
			}
		case *ast.IdentifierPattern, *ast.WildcardPattern:
			// no test
		default:
			return nil, fmt.Errorf("llvm: array element pattern %T not implemented", el)
		}
		if t != nil {
			if cond == nil {
				cond = t
			} else {
				cond = afterLen.NewAnd(cond, t)
			}
		}
	}
	matched := afterLen
	if cond != nil {
		matched = fn.NewBlock("")
		afterLen.NewCondBr(cond, matched, next)
	}

	// Bindings — element bindings borrow the element; a whole-array `...rest` borrows
	// the scrutinee. Neither is framed (the array's own binding owns the storage).
	for i, el := range elems {
		name := ""
		switch ep := el.(type) {
		case *ast.IdentifierPattern:
			if ep.Name != "_" {
				name = ep.Name
			}
		case *ast.BindingPattern:
			name = ep.Name
		}
		if name != "" {
			slot := fn.Blocks[0].NewAlloca(elemLL)
			matched.NewStore(elemAt(matched, int64(i)), slot)
			l.locals[name] = slot
		}
	}
	if hasRest && restName != "" && restName != "_" {
		slot := fn.Blocks[0].NewAlloca(box.Type())
		matched.NewStore(box, slot)
		l.locals[restName] = slot
	}
	return matched, nil
}
