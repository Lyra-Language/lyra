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
	length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
	elemAt := func(b *ir.Block, i int64) value.Value {
		return b.NewLoad(elemLL, dynArrayElemPtr(b, boxTy, box, i64c(i)))
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
	// Per-arm binding scope — see lowerScalarMatch.
	armScope := l.pushLocalScope()
	defer armScope()
	for _, arm := range e.MatchArms {
		armScope()
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
			// An arm-scoped frame: a `[head, ...tail]` pattern *allocates* its tail, so
			// unlike every other array binding it owns a reference that must be released
			// when the arm's body finishes. It cannot go in the enclosing frame, whose
			// releases run on every path — an arm that didn't match never allocated, and
			// releasing its uninitialized slot faults. Scoping the frame to the arm puts
			// the release exactly on the path that did the allocation. Arms that bind no
			// tail simply push and pop an empty frame.
			l.pushManagedFrame()
			matched, err := l.lowerArrayPatternMatch(current, next, p, box, length, elemAt, elemLL, arrType.ElementType, isBool, signed)
			if err != nil {
				l.popManagedFrame()
				return nil, nil, err
			}
			armBody := func(b *ir.Block, body ast.Expression) error {
				val, end, err := l.lowerExpr(b, body)
				if err != nil {
					return err
				}
				if end.Term == nil {
					// Release before merging. A body whose value *is* the tail is safe: the
					// ownership pass records a Retain on that read (a pattern binding is
					// never last-use-eligible), so the escaping value carries its own +1.
					if err := l.releaseTopManagedFrame(end); err != nil {
						return err
					}
					end.NewBr(merge)
					incomings = append(incomings, incoming{val, end})
				}
				return nil
			}
			// A guard is tested *after* the pattern's bindings exist, so a `[h, ...t]`
			// arm has already allocated its tail by the time the guard runs — and a
			// failing guard falls through to the next arm, skipping the body's release.
			// Give the guard its own false-edge block that releases the arm frame first.
			// (The *pattern*'s own failure edges need no such treatment: the length and
			// element tests all branch before bindTailSubArray allocates anything.)
			armNext := next
			if arm.Guard != nil && len(l.managedFrames[len(l.managedFrames)-1]) > 0 {
				guardFail := fn.NewBlock("")
				if err := l.releaseTopManagedFrame(guardFail); err != nil {
					l.popManagedFrame()
					return nil, nil, err
				}
				guardFail.NewBr(next)
				armNext = guardFail
			}
			err = l.lowerGuardedArmBody(matched, arm.Guard, arm.Body, armNext, armBody)
			l.popManagedFrame()
			if err != nil {
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
func (l *lowerer) lowerArrayPatternMatch(current, next *ir.Block, p *ast.ArrayPattern, box, length value.Value, elemAt func(*ir.Block, int64) value.Value, elemLL lltypes.Type, elemLyra types.Type, isBool, signed bool) (*ir.Block, error) {
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

	// Length test: an exact arity for a fixed pattern; a bare `[...rest]` matches any
	// length; `[h, ...t]` needs at least the fixed elements it names.
	afterLen := fn.NewBlock("")
	switch {
	case hasRest && fixedCount == 0:
		current.NewBr(afterLen)
	case hasRest:
		current.NewCondBr(current.NewICmp(enum.IPredSGE, length, i64c(fixedCount)), afterLen, next)
	default:
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
		if fixedCount == 0 {
			// A whole-array `[...rest]` aliases the scrutinee — a borrow, so it is bound
			// without framing (the scrutinee's own binding still owns the box).
			slot := fn.Blocks[0].NewAlloca(box.Type())
			matched.NewStore(box, slot)
			l.locals[restName] = slot
			return matched, nil
		}
		var err error
		matched, err = l.bindTailSubArray(matched, restName, box, length, fixedCount, elemLyra)
		if err != nil {
			return nil, err
		}
	}
	return matched, nil
}

// bindTailSubArray builds the `tail` of a `[head, ...tail]` pattern: a **fresh**
// `[]T` box holding the elements from fixedCount onward, bound under restName.
//
// Unlike every other array-pattern binding this is not a borrow — a sub-array has
// no existing storage to alias, since the elements it needs are a suffix of a box
// whose header describes the *whole* array. So it allocates, copies, and is framed
// like a local managed binding, released at the arm's scope exit.
//
// Disposal needs no ownership-pass change, which is what this was long filed as
// blocked on. The pass keys managed-ness off the recorded type, so it already sees
// `tail` as a managed value; and because a pattern binding is not a `VarDeclStmt`
// it is never last-use-eligible, so every *owning* use inside the arm (returning
// it, passing it to an `own` parameter) records a plain Retain rather than a
// transfer. That +1 is exactly what the escape needs, and the frame release
// balances the box's own reference. The cost is one retain/release pair versus a
// transfer — refcount traffic, not a leak.
//
// A **managed element type** is retained per element: the new box's drop glue will
// release each element when the tail dies, so without the dup the source array and
// the tail would both free them.
func (l *lowerer) bindTailSubArray(block *ir.Block, name string, box, length value.Value, fixedCount int64, elemLyra types.Type) (*ir.Block, error) {
	if elemLyra == nil {
		return nil, fmt.Errorf("llvm: `[head, ...%s]`: the array has no element type", name)
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(elemLyra))
	if !ok {
		return nil, fmt.Errorf("llvm: cannot size `[head, ...%s]` element type %s", name, elemLyra)
	}
	stride := int64(alignUp(elemSize, elemAlign))
	srcTy := DynArrayBoxType(elemLL)
	fn := block.Parent

	l.ensureRCRuntime()
	// tailLen = length - fixedCount (>= 0: the length test above already passed).
	tailLen := block.NewSub(length, i64c(fixedCount))
	byteSize := block.NewAdd(i64c(int64(dynArrayHeaderSize)), block.NewMul(tailLen, i64c(stride)))
	tailI8 := block.NewCall(l.rcAlloc, byteSize) // rc = 1
	tail := block.NewBitCast(tailI8, lltypes.NewPointer(srcTy))
	block.NewStore(tailLen, dynArrayLenPtr(block, srcTy, tail))

	// Copy loop: tail[i] = src[fixedCount + i], for i in [0, tailLen).
	idx := fn.Blocks[0].NewAlloca(lltypes.I64)
	block.NewStore(i64c(0), idx)
	head := fn.NewBlock("")
	body := fn.NewBlock("")
	done := fn.NewBlock("")
	block.NewBr(head)
	i := head.NewLoad(lltypes.I64, idx)
	head.NewCondBr(head.NewICmp(enum.IPredSLT, i, tailLen), body, done)

	bi := body.NewLoad(lltypes.I64, idx)
	srcElem := body.NewLoad(elemLL, dynArrayElemPtr(body, srcTy, box, body.NewAdd(bi, i64c(fixedCount))))
	cur := body
	if l.needsDrop(elemLyra) {
		// The tail owns its copy of each managed element (its drop glue releases
		// them), so the reference is duplicated rather than moved out of the source.
		cur, err = l.emitRetainValue(cur, srcElem, elemLyra)
		if err != nil {
			return nil, err
		}
	}
	cur.NewStore(srcElem, dynArrayElemPtr(cur, srcTy, tail, bi))
	cur.NewStore(cur.NewAdd(bi, i64c(1)), idx)
	cur.NewBr(head)

	slot := fn.Blocks[0].NewAlloca(tail.Type())
	done.NewStore(tail, slot)
	l.locals[name] = slot
	l.addManagedBinding(slot, types.DynamicArrayType{ElementType: elemLyra})
	return done, nil
}
