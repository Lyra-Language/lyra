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

// Array comprehensions — `[ x in xs | x > 0 | x * 2 ]` lowered to a fill loop.
//
// The shape is: allocate one box, walk the generators as nested counter loops, and store
// each result that survives the guards at a running count. The count — not the capacity —
// becomes the box's length at the end.
//
// **Capacity is the product of the source lengths, and the box is deliberately allocated
// at that size before anything is known about how many elements survive.** A guard filters,
// so the final length is somewhere between zero and the capacity, and there are only three
// ways to handle that: run the generators twice (once to count, once to fill), grow the box
// as it fills, or over-allocate once and record the real length. Running twice is wrong
// rather than slow — a guard may call a function, and evaluating it twice per element makes
// the number of calls a detail of the lowering. Growing needs a reallocation primitive the
// language does not have. Over-allocating costs memory on a filtering comprehension and
// nothing on a mapping one, which is the common case, and it keeps every guard evaluated
// exactly once. That is the trade recorded here so a future change knows what it is undoing.
//
// **Sources become a uniform index loop.** An array yields its element at index `i`, a
// range yields `start + i*step`. Both are then "length, plus a function from an index to a
// value", which is what lets one nested-loop emitter serve both instead of a loop shape per
// source kind — the same consolidation the for-in lowering did not do, and the reason it
// has three separate functions.

// compSource is a generator's source reduced to what the loop needs: how many iterations,
// and the value bound on each.
type compSource struct {
	length value.Value                                  // iteration count, i64
	elemLL lltypes.Type                                 // the bound variable's LLVM type
	bind   func(b *ir.Block, i value.Value) value.Value // the value at index i
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

	// capacity = the product of the source lengths, which bounds how many results the
	// nested loops can produce.
	capacity := sources[0].length
	for _, src := range sources[1:] {
		capacity = block.NewMul(capacity, src.length)
	}

	l.ensureRCRuntime()
	boxTy := DynArrayBoxType(elemLL)
	byteLen := block.NewAdd(i64c(int64(dynArrayHeaderSize)), block.NewMul(capacity, i64c(int64(stride))))
	boxI8 := block.NewCall(l.rcAlloc, byteLen) // i8*, rc = 1
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))

	fn := block.Parent
	entry := fn.Blocks[0]
	countSlot := entry.NewAlloca(lltypes.I64)
	block.NewStore(i64c(0), countSlot)

	// Bind each generator into a slot the nested loops write per iteration, so the guards
	// and result read them as ordinary locals.
	slots := make([]value.Value, len(sources))
	for i, src := range sources {
		slots[i] = entry.NewAlloca(src.elemLL)
		l.locals[e.Generators[i].Identifier] = slots[i]
	}

	done := fn.NewBlock("")
	after, err := l.emitCompLoops(block, e, sources, slots, boxTy, box, countSlot, elemLL, 0, done)
	if err != nil {
		return nil, nil, err
	}
	if after.Term == nil {
		after.NewBr(done)
	}
	// The *count*, not the capacity: the box holds as many elements as survived the guards.
	count := done.NewLoad(lltypes.I64, countSlot)
	done.NewStore(count, dynArrayLenPtr(done, boxTy, box))
	return box, done, nil
}

// emitCompLoops emits generator `depth`'s loop, recursing for the generators inside it and
// emitting the guard-and-store at the innermost level.
//
// Recursion rather than iteration because each loop's body *is* the next loop: the blocks
// have to nest, and the innermost body is the only one that stores. `done` is the block
// every loop's exit eventually falls through to.
func (l *lowerer) emitCompLoops(block *ir.Block, e *ast.ArrayCompExpr, sources []compSource, slots []value.Value,
	boxTy *lltypes.StructType, box value.Value, countSlot value.Value, elemLL lltypes.Type, depth int, done *ir.Block,
) (*ir.Block, error) {
	if depth == len(sources) {
		return l.emitCompBody(block, e, boxTy, box, countSlot, elemLL)
	}
	fn := block.Parent
	entry := fn.Blocks[0]
	iSlot := entry.NewAlloca(lltypes.I64)
	block.NewStore(i64c(0), iSlot)

	cond := fn.NewBlock("")
	body := fn.NewBlock("")
	inc := fn.NewBlock("")
	exit := fn.NewBlock("")
	block.NewBr(cond)

	i := cond.NewLoad(lltypes.I64, iSlot)
	cond.NewCondBr(cond.NewICmp(enum.IPredSLT, i, sources[depth].length), body, exit)

	// Bind this generator's variable for the iteration, then emit whatever is inside.
	ib := body.NewLoad(lltypes.I64, iSlot)
	body.NewStore(sources[depth].bind(body, ib), slots[depth])
	bodyEnd, err := l.emitCompLoops(body, e, sources, slots, boxTy, box, countSlot, elemLL, depth+1, done)
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

// lowerCompSource reduces a generator's source to a length and an index→value function.
func (l *lowerer) lowerCompSource(block *ir.Block, gen *ast.Generator) (compSource, *ir.Block, error) {
	srcType, ok := l.recordedType(gen.Value)
	if !ok {
		return compSource{}, nil, fmt.Errorf("llvm: no type recorded for a comprehension generator's source")
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
		return compSource{
			length: i64c(int64(it.Size)),
			elemLL: arrayTy.ElemType,
			bind: func(b *ir.Block, i value.Value) value.Value {
				return b.NewLoad(arrayTy.ElemType, b.NewGetElementPtr(arrayTy, arrPtr, i64c(0), i))
			},
		}, block, nil
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
		return compSource{
			length: length,
			elemLL: elem,
			bind: func(b *ir.Block, i value.Value) value.Value {
				return b.NewLoad(elem, dynArrayElemPtr(b, boxTy, box, i))
			},
		}, block, nil
	}
	// A range or a string source. Both are meaningful and neither is implemented: a range
	// needs its iteration count derived from start/end/step (including the inclusive and
	// negative-step cases), and a string yields *runes*, whose count is not its byte
	// length — so the capacity rule above would be wrong for it as well as the walk.
	// Loudly deferred rather than approximated (hazard 5).
	return compSource{}, nil, fmt.Errorf(
		"llvm: a comprehension over %s is not implemented yet — a generator's source must be an array", srcType)
}
