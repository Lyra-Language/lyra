package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Array comprehensions — `[ x in xs | x > 0 | x * 2 ]` lowered to a fill loop.
//
// The shape is: allocate one box, walk the generators as nested loops, and store each
// result that survives the guards at a running count. The count — not the capacity —
// becomes the box's length at the end.
//
// **Capacity is the product of the sources' upper bounds, and the box is allocated at that
// size before anything is known about how many elements survive.** A guard filters, so the
// final length is somewhere between zero and the capacity, and there are only three ways to
// handle that: run the generators twice (once to count, once to fill), grow the box as it
// fills, or over-allocate once and record the real length. Running twice is wrong rather
// than slow — a guard may call a function, and evaluating it twice per element makes the
// number of calls a detail of the lowering. Growing needs a reallocation primitive the
// language does not have. Over-allocating costs memory on a filtering comprehension and
// nothing on a mapping one, which is the common case, and it keeps every guard evaluated
// exactly once. That is the trade recorded here so a future change knows what it is undoing.
//
// **A source drives its own loop** (`compSource.emit`) rather than exposing an index. The
// first version assumed every source could answer "the value at index i", which is true of
// an array and of a range but *not* of a string: UTF-8 is variable width, so its walk is a
// byte cursor whose advance is whatever the decoder just consumed. Letting each source emit
// its own loop is what admits all three without a shape per kind leaking into the nesting.
//
// **The capacity must bound the loop by construction, not by agreement.** Writing past the
// box is memory corruption, so it is not enough for the count formula to *match* the loop
// under the conditions the author expects — the loop has to be incapable of running more
// times than the capacity allows. An array's is its length. A string's is its **byte**
// length, which bounds its rune count because no encoded rune is shorter than a byte. A
// range's is computed up front and the loop then runs exactly that many times (see
// rangeSource), so a degenerate step yields an empty array rather than a fill loop racing
// past the allocation.

// compSource is a generator's source reduced to what the fill loop needs.
type compSource struct {
	capacity value.Value // an upper bound on the iteration count, for the allocation
	elemLL   lltypes.Type
	// emit runs body once per element, having written that element into slot. It returns
	// the block execution continues in after the loop.
	emit func(block *ir.Block, slot value.Value, body func(*ir.Block) (*ir.Block, error)) (*ir.Block, error)
}

// lowerArrayComp lowers a comprehension to a freshly allocated `[]T`.
func (l *lowerer) lowerArrayComp(block *ir.Block, e *ast.ArrayCompExpr) (value.Value, *ir.Block, error) {
	compType, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for an array comprehension")
	}
	dynType, ok := compType.(types.DynamicArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: an array comprehension must have a dynamic array type, got %s", compType)
	}
	elemLL, err := l.lowerType(dynType.ElementType)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(dynType.ElementType))
	if !ok {
		return nil, nil, fmt.Errorf("llvm: cannot size array comprehension element type %s", dynType.ElementType)
	}
	stride := alignUp(elemSize, elemAlign)

	// The generator bindings belong to the comprehension, exactly as a loop variable
	// belongs to its loop: scope them so they cannot leak into what follows.
	defer l.pushLocalScope()()

	if err := dependentGenerator(e.Generators); err != nil {
		return nil, nil, err
	}
	// Every source is materialized **once, before the loops**, which is what makes the
	// capacity computable up front. It is also exactly why a source may not depend on an
	// earlier generator's binding — see dependentGenerator.
	sources := make([]compSource, 0, len(e.Generators))
	for i := range e.Generators {
		var src compSource
		src, block, err = l.lowerCompSource(block, &e.Generators[i])
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, src)
	}

	capacity := sources[0].capacity
	for _, src := range sources[1:] {
		capacity = block.NewMul(capacity, src.capacity)
	}

	boxTy := DynArrayBoxType(elemLL)
	// Length 0 for now: the count is only known once the guards have run, and is stored
	// at the end. The buffer is sized at the capacity, which is the over-allocation this
	// file's header comment explains.
	box := l.dynArrayAlloc(block, boxTy, elemLL, i64c(0), capacity, int64(stride))

	fn := block.Parent
	entry := fn.Blocks[0]
	countSlot := entry.NewAlloca(lltypes.I64)
	block.NewStore(i64c(0), countSlot)

	// Bind each generator into a slot its loop writes per iteration, so the guards and
	// result read them as ordinary locals.
	slots := make([]value.Value, len(sources))
	for i, src := range sources {
		slots[i] = entry.NewAlloca(src.elemLL)
		l.locals[e.Generators[i].Identifier] = slots[i]
	}

	after, err := l.emitCompLoops(block, e, sources, slots, boxTy, box, countSlot, elemLL, 0)
	if err != nil {
		return nil, nil, err
	}
	// The *count*, not the capacity: the box holds as many elements as survived the guards.
	count := after.NewLoad(lltypes.I64, countSlot)
	after.NewStore(count, dynArrayLenPtr(after, boxTy, box))
	return box, after, nil
}

// emitCompLoops emits generator `depth`'s loop, recursing for the generators inside it and
// emitting the guard-and-store at the innermost level.
//
// Recursion rather than iteration because each loop's body *is* the next loop: the blocks
// have to nest, and the innermost body is the only one that stores.
func (l *lowerer) emitCompLoops(block *ir.Block, e *ast.ArrayCompExpr, sources []compSource, slots []value.Value,
	boxTy *lltypes.StructType, box value.Value, countSlot value.Value, elemLL lltypes.Type, depth int,
) (*ir.Block, error) {
	if depth == len(sources) {
		return l.emitCompBody(block, e, boxTy, box, countSlot, elemLL)
	}
	return sources[depth].emit(block, slots[depth], func(body *ir.Block) (*ir.Block, error) {
		return l.emitCompLoops(body, e, sources, slots, boxTy, box, countSlot, elemLL, depth+1)
	})
}

// emitCompBody is the innermost level: test the guards, and on success store the result at
// the running count.
func (l *lowerer) emitCompBody(block *ir.Block, e *ast.ArrayCompExpr, boxTy *lltypes.StructType,
	box value.Value, countSlot value.Value, elemLL lltypes.Type,
) (*ir.Block, error) {
	fn := block.Parent
	skip := fn.NewBlock("")

	// Guards are conjunctive and short-circuit: each one that fails jumps straight to
	// `skip`, so a later guard is not evaluated for an element an earlier one rejected.
	// That matters for the same reason the single-evaluation property above does — a
	// guard may call a function.
	cur := block
	for _, guard := range e.Guards {
		v, next, err := l.lowerExpr(cur, guard)
		if err != nil {
			return nil, err
		}
		pass := fn.NewBlock("")
		next.NewCondBr(v, pass, skip)
		cur = pass
	}

	result, cur, err := l.lowerExpr(cur, e.Result)
	if err != nil {
		return nil, err
	}
	result, err = l.coerceAggregateElem(cur, result, elemLL, e.Result)
	if err != nil {
		return nil, err
	}
	n := cur.NewLoad(lltypes.I64, countSlot)
	cur.NewStore(result, dynArrayElemPtr(cur, boxTy, box, n))
	cur.NewStore(cur.NewAdd(n, i64c(1)), countSlot)
	cur.NewBr(skip)
	return skip, nil
}

// countedSource builds a source that runs exactly `count` times, binding `at(i)` each time.
// Arrays and ranges are both this; only the binding differs.
func countedSource(count value.Value, elemLL lltypes.Type, at func(b *ir.Block, i value.Value) value.Value) compSource {
	return compSource{
		capacity: count,
		elemLL:   elemLL,
		emit: func(block *ir.Block, slot value.Value, body func(*ir.Block) (*ir.Block, error)) (*ir.Block, error) {
			fn := block.Parent
			iSlot := fn.Blocks[0].NewAlloca(lltypes.I64)
			block.NewStore(i64c(0), iSlot)

			cond := fn.NewBlock("")
			bodyBlock := fn.NewBlock("")
			inc := fn.NewBlock("")
			exit := fn.NewBlock("")
			block.NewBr(cond)

			i := cond.NewLoad(lltypes.I64, iSlot)
			cond.NewCondBr(cond.NewICmp(enum.IPredSLT, i, count), bodyBlock, exit)

			ib := bodyBlock.NewLoad(lltypes.I64, iSlot)
			bodyBlock.NewStore(at(bodyBlock, ib), slot)
			bodyEnd, err := body(bodyBlock)
			if err != nil {
				return nil, err
			}
			if bodyEnd.Term == nil {
				bodyEnd.NewBr(inc)
			}
			ic := inc.NewLoad(lltypes.I64, iSlot)
			inc.NewStore(inc.NewAdd(ic, i64c(1)), iSlot)
			inc.NewBr(cond)
			return exit, nil
		},
	}
}

// dependentGenerator reports a generator whose source mentions an earlier generator's
// binding — `[ xs in grid, x in xs | x ]`.
//
// It type-checks, and it is the one shape the lowering above cannot serve: sources are
// materialized once before the loops, so a dependent one would be evaluated against a
// binding that has no value yet, and its length would be wrong for every iteration but the
// first. Supporting it means moving each source's materialization inside the enclosing
// loop, which in turn means the capacity is no longer known before the loops run — a
// different allocation strategy, not a small change.
//
// Refused loudly rather than mis-lowered (hazard 5). The message names the fix that exists
// today, since a nested comprehension expresses the same thing.
func dependentGenerator(generators []ast.Generator) error {
	bound := map[string]bool{}
	for i := range generators {
		gen := &generators[i]
		if gen.Value != nil {
			var offender string
			ast.WalkExpr(gen.Value, func(ast.Statement) bool { return true }, func(x ast.Expression) bool {
				if id, ok := x.(*ast.IdentifierExpr); ok && bound[id.Name] {
					offender = id.Name
				}
				return true
			})
			if offender != "" {
				return fmt.Errorf(
					"llvm: a comprehension generator whose source depends on an earlier generator (%q in %q's source) is not implemented yet — write it as a comprehension over a comprehension instead",
					offender, gen.Identifier)
			}
		}
		bound[gen.Identifier] = true
	}
	return nil
}

// lowerCompSource reduces a generator's source to a capacity and a loop emitter.
func (l *lowerer) lowerCompSource(block *ir.Block, gen *ast.Generator) (compSource, *ir.Block, error) {
	srcType, ok := l.recordedType(gen.Value)
	if !ok {
		return compSource{}, nil, fmt.Errorf("llvm: no type recorded for a comprehension generator's source")
	}
	if _, isRange := srcType.(types.RangeType); isRange {
		return l.rangeSource(block, gen)
	}
	if types.IsString(srcType) {
		return l.stringSource(block, gen)
	}
	switch it := srcType.(type) {
	case types.StaticArrayType:
		var (
			arrPtr  value.Value
			arrayTy *lltypes.ArrayType
			err     error
		)
		if types.AllocationOf(srcType) == types.Shared {
			arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, gen.Value)
		} else {
			arrPtr, arrayTy, block, err = l.arrayLValue(block, gen.Value)
		}
		if err != nil {
			return compSource{}, nil, err
		}
		return countedSource(i64c(int64(it.Size)), arrayTy.ElemType, func(b *ir.Block, i value.Value) value.Value {
			return b.NewLoad(arrayTy.ElemType, b.NewGetElementPtr(arrayTy, arrPtr, i64c(0), i))
		}), block, nil
	case types.DynamicArrayType:
		elem, err := l.lowerType(it.ElementType)
		if err != nil {
			return compSource{}, nil, err
		}
		boxTy := DynArrayBoxType(elem)
		var box value.Value
		box, block, err = l.lowerExpr(block, gen.Value)
		if err != nil {
			return compSource{}, nil, err
		}
		length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		return countedSource(length, elem, func(b *ir.Block, i value.Value) value.Value {
			return b.NewLoad(elem, dynArrayElemPtr(b, boxTy, box, i))
		}), block, nil
	}
	// The grammar admits an identifier, a range, an array literal or a string literal as a
	// source, so reaching here means an identifier bound to something none of the above —
	// a tuple, a struct. Those are not iterable at all (the typechecker says so first),
	// which makes this the backstop rather than a deferral.
	return compSource{}, nil, fmt.Errorf(
		"llvm: a comprehension over %s is not implemented — a generator's source must be an array, a range, or a string", srcType)
}

// rangeSource walks `start..<end` / `start..<=end` with an optional step.
//
// **The iteration count is computed first and the loop then runs exactly that many times**,
// rather than the loop re-testing `i < end` as a for-in loop does. That is what makes the
// capacity a real bound instead of a prediction: a comprehension writes into a box sized
// from this count, so a loop that could outrun it would corrupt memory rather than produce
// a wrong answer. Deriving the count once and driving the loop from it means the two cannot
// disagree, whatever the bounds and step turn out to be at run time.
//
// A non-positive step yields a count of zero — an empty array. That answer diverges from
// `for-in`, which traps on a runtime non-positive step (08/12): here the count is computed
// up front, so "never advances" has a defined size, where a re-testing loop's only
// alternatives are a trap or the runaway it used to be. The division is guarded against a
// zero divisor for the same reason: `sdiv` by zero is undefined, and undefined is not an
// acceptable answer to a degenerate range.
func (l *lowerer) rangeSource(block *ir.Block, gen *ast.Generator) (compSource, *ir.Block, error) {
	rng, ok := gen.Value.(*ast.RangeExpr)
	if !ok {
		return compSource{}, nil, fmt.Errorf("llvm: a comprehension's range source is not a range expression (%T)", gen.Value)
	}
	iType, _ := l.rangeIntType(rng)

	coerce := func(b *ir.Block, ex ast.Expression) (value.Value, *ir.Block, error) {
		v, b, err := l.lowerExpr(b, ex)
		if err != nil {
			return nil, nil, err
		}
		vi, ok := v.Type().(*lltypes.IntType)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: range bound is not an integer (%s)", v.Type())
		}
		if vi.BitSize != iType.BitSize {
			vs, _ := l.getIntSignedness(ex)
			v = coerceIntWidth(b, v, vs, iType)
		}
		return v, b, nil
	}
	start, block, err := coerce(block, rng.Start)
	if err != nil {
		return compSource{}, nil, err
	}
	end, block, err := coerce(block, rng.End)
	if err != nil {
		return compSource{}, nil, err
	}
	var step value.Value = constant.NewInt(iType, 1)
	if rng.Step != nil {
		if step, block, err = coerce(block, rng.Step); err != nil {
			return compSource{}, nil, err
		}
	}

	// span is always measured *along* the range's direction, so the count arithmetic below
	// is the same for both: a descending range spans start - end. Direction comes from the
	// operator, never from which bound is larger — `5..<1` is an ascending range that
	// happens to be empty, and must produce nothing rather than quietly counting down.
	descending := types.RangeDescends(rng.EndOperator)
	var span value.Value
	if descending {
		span = block.NewSub(start, end)
	} else {
		span = block.NewSub(end, start)
	}
	if !types.RangeExcludesEnd(rng.EndOperator) {
		span = block.NewAdd(span, constant.NewInt(iType, 1))
	}
	zero := constant.NewInt(iType, 0)
	stepPos := block.NewICmp(enum.IPredSGT, step, zero)
	spanPos := block.NewICmp(enum.IPredSGT, span, zero)
	// A zero divisor is undefined even on a path whose result is discarded, so the divisor
	// is made safe before the division rather than after.
	safeStep := block.NewSelect(stepPos, step, constant.NewInt(iType, 1))
	// ceil(span / step), for a positive span and step.
	rounded := block.NewAdd(block.NewSub(span, constant.NewInt(iType, 1)), safeStep)
	raw := block.NewSDiv(rounded, safeStep)
	count := block.NewSelect(block.NewAnd(stepPos, spanPos), raw, zero)

	// The counter is i64 (every fill loop's is); the bound value is computed in the range's
	// own width, so `x` is typed exactly as the typechecker recorded it.
	count64 := widenIndexToI64(block, count, iType)
	return countedSource(count64, iType, func(b *ir.Block, i value.Value) value.Value {
		idx := narrowIndexFrom64(b, i, iType)
		offset := b.NewMul(idx, step)
		if descending {
			return b.NewSub(start, offset)
		}
		return b.NewAdd(start, offset)
	}), block, nil
}

// widenIndexToI64 / narrowIndexFrom64 move between the fill loop's i64 counter and a
// range's own integer width. A range's count is non-negative by construction (rangeSource
// clamps it), so the widening is a zero-extend and the narrowing cannot lose a value the
// loop will actually reach.
func widenIndexToI64(block *ir.Block, v value.Value, from *lltypes.IntType) value.Value {
	switch {
	case from.BitSize == 64:
		return v
	case from.BitSize < 64:
		return block.NewZExt(v, lltypes.I64)
	default:
		return block.NewTrunc(v, lltypes.I64)
	}
}

func narrowIndexFrom64(block *ir.Block, v value.Value, to *lltypes.IntType) value.Value {
	switch {
	case to.BitSize == 64:
		return v
	case to.BitSize < 64:
		return block.NewTrunc(v, to)
	default:
		return block.NewZExt(v, to)
	}
}

// stringSource walks a string's **runes**, UTF-8 decoded.
//
// This is the source the index model could not serve: a rune's width is whatever the
// decoder just consumed, so the walk is a byte cursor rather than an index. The capacity is
// the **byte** length, which bounds the rune count because no encoded rune occupies less
// than one byte — an over-approximation exactly like a guard's, and safe for the same
// reason.
func (l *lowerer) stringSource(block *ir.Block, gen *ast.Generator) (compSource, *ir.Block, error) {
	str, block, err := l.lowerExpr(block, gen.Value)
	if err != nil {
		return compSource{}, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return compSource{}, nil, fmt.Errorf("llvm: a comprehension's string source did not lower to a string (%s)", str.Type())
	}
	data := block.NewExtractValue(str, 0)
	byteLen := block.NewExtractValue(str, 1)
	decode := l.utf8DecodeFunc()

	return compSource{
		capacity: byteLen,
		elemLL:   lltypes.I32, // a rune
		emit: func(block *ir.Block, slot value.Value, body func(*ir.Block) (*ir.Block, error)) (*ir.Block, error) {
			fn := block.Parent
			entry := fn.Blocks[0]
			biSlot := entry.NewAlloca(lltypes.I64) // byte cursor
			cpSlot := entry.NewAlloca(lltypes.I32) // decode out-param
			block.NewStore(i64c(0), biSlot)

			cond := fn.NewBlock("")
			bodyBlock := fn.NewBlock("")
			inc := fn.NewBlock("")
			exit := fn.NewBlock("")
			block.NewBr(cond)

			bi := cond.NewLoad(lltypes.I64, biSlot)
			cond.NewCondBr(cond.NewICmp(enum.IPredULT, bi, byteLen), bodyBlock, exit)

			biB := bodyBlock.NewLoad(lltypes.I64, biSlot)
			n := bodyBlock.NewCall(decode, data, biB, cpSlot)
			bodyBlock.NewStore(bodyBlock.NewLoad(lltypes.I32, cpSlot), slot)
			bodyEnd, err := body(bodyBlock)
			if err != nil {
				return nil, err
			}
			if bodyEnd.Term == nil {
				bodyEnd.NewBr(inc)
			}
			biI := inc.NewLoad(lltypes.I64, biSlot)
			inc.NewStore(inc.NewAdd(biI, n), biSlot) // advance by the decoded byte count
			inc.NewBr(cond)
			return exit, nil
		},
	}, block, nil
}
