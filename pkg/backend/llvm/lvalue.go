package llvm

import (
	"fmt"
	"slices"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerLValueAssignment lowers an interior-mutation statement — `xs[i] = v`,
// `p.x = v`, or any path built from those (`grid[i].y = v`, `p.arr[i] = v`,
// `line.start.x = v`, `m[i][j] = v`). It computes the target location's address via
// lvalueAddress (which walks the path) and stores the value there, mutating in
// place. The typechecker (checkLValueAssignment) has already verified the root
// binding is mutable (a `var`, a `let mut`, or a `mut`/`own` parameter), that the
// value's type matches the target, and that no `readonly` field is written.
//
// A **managed** target (a `string`/`[]T` element or field) owns whatever it holds, so
// overwriting it must release the old reference or that reference leaks — but only
// when the slot genuinely owns it rather than sharing a caller's. See
// releaseOldTarget.
//
// Deferred, loud error: an optional member target (`p?.x = v`).
func (l *lowerer) lowerLValueAssignment(block *ir.Block, stmt *ast.LValueAssignmentStmt) (*ir.Block, error) {
	loc, block, err := l.lvalueAddress(block, stmt.Target)
	if err != nil {
		return nil, err
	}
	if loc.ty == nil {
		return nil, fmt.Errorf("llvm: no type recorded for assignment target")
	}
	targetLL, err := l.lowerType(loc.ty)
	if err != nil {
		return nil, err
	}
	v, block, err := l.lowerExpr(block, stmt.Value)
	if err != nil {
		return nil, err
	}
	// The typechecker already narrowed the value to the target type; coerce
	// defensively so a residual int-width mismatch fixes rather than emitting bad IR.
	v, err = l.coerceAggregateElem(block, v, targetLL, stmt.Value)
	if err != nil {
		return nil, err
	}
	// The new value is computed *before* the release below, so `xs[i] = xs[i] ++ y`
	// — which reads the old element — is safe.
	if l.releaseOldTarget(loc, stmt.Target) {
		old := block.NewLoad(targetLL, loc.ptr)
		if err := l.lowerManagedRelease(block, old, loc.ty); err != nil {
			return nil, err
		}
	}
	block.NewStore(v, loc.ptr)
	return block, nil
}

// releaseOldTarget reports whether the assignment may release the managed value the
// target slot currently holds. It may only do so when the slot's storage is *not
// reachable through an unretained duplicate*, and the deciding fact is whether the
// final hop went through a ref-counted box (loc.viaBox).
//
// Why the box matters. Every way of *reading a managed value* out of a container —
// `let s = p.name`, `let e = xs[i]`, `let q = p` on a managed binding — goes through
// the ownership pass's retain, so it yields its own +1. The one copy that does not
// is a whole **inline (stack) aggregate**: `let q = p` on a struct/tuple/`[N]T` copies
// the aggregate *by value*, duplicating the fat pointers of its managed fields with
// no retain, and so does passing or returning one. So a managed slot sitting inside
// inline storage may be aliased by an arbitrary number of unretained copies, and
// releasing it here would dangle every one of them (an ASan-confirmed
// use-after-free: `let q = p; p.name = …; q.name` freed the box `q.name` still
// pointed at).
//
// A slot reached *through a box* has no such alias: copying a `shared` value or a
// `[]T` copies the box **pointer** — which is managed, hence retained — so every
// copy observes the same slot rather than a private duplicate of it. Overwriting it
// is then ordinary aliasing, and releasing the old value is correct.
//
// Only the *final* hop is consulted, because crossing into a box re-establishes that
// invariant: in `p.arr[i] = v` (a stack struct holding a `[]string`), copies of `p`
// duplicate the box pointer but all name the same element storage, so the element
// release is safe even though `p` itself is an inline aggregate.
//
// An **owning root** is the second way to say yes, and the one deep-retain-on-copy
// bought. When the path is rooted at a binding the backend framed — a local
// `let`/`var`, or an `own` parameter — that binding holds a +1 on everything it owns,
// so the old field value is genuinely its reference to drop. Copies of the aggregate
// now carry their own +1 (ownership.OwnsManaged makes a stack-aggregate copy an
// owning position and the retain glue dups each managed field), so releasing through
// the original can no longer dangle them. Before that, this case *was* the
// use-after-free above and the release had to be suppressed entirely.
//
// A **by-reference `mut` parameter** root is the third yes, and the one that
// by-reference parameter passing bought. Its slot *is* the caller's storage, so the
// managed value being overwritten is the caller's own reference — which must be
// released here or it leaks, and which is safe to release precisely because the
// callee holds the caller's address rather than an unretained duplicate of it. While
// `mut` was passed by value, this case had to be refused: the parameter copy shared
// the caller's reference, so releasing through it freed a value the caller still
// owned (the third and nastiest shape of the original use-after-free, since the
// aliasing copy was the parameter passing itself and so invisible in the source).
// That refusal leaked instead, which is the leak this closes.
func (l *lowerer) releaseOldTarget(loc lvalueLoc, target ast.Expression) bool {
	if !ownership.IsManaged(loc.ty) {
		return false
	}
	return loc.viaBox || l.lvalueRootIsOwning(target)
}

// lvalueRootIsOwning reports whether the binding an lvalue path is rooted at is an
// owning slot — one the backend framed, so it holds a +1 on what it owns and will
// deep-release it at scope exit.
//
// Framing is the exact signal to test, because it is the same condition under which
// the ownership pass grants that +1: lowerVarDecl and defineFunction frame precisely
// the bindings whose initializer or argument was coerced to owned.
func (l *lowerer) lvalueRootIsOwning(e ast.Expression) bool {
	for {
		switch t := e.(type) {
		case *ast.IdentifierExpr:
			slot, ok := l.locals[t.Name]
			if !ok {
				return false
			}
			return l.slotIsOwning(slot)
		case *ast.MemberExpr:
			e = t.Object
		case *ast.IndexExpr:
			e = t.Object
		default:
			return false // unrecognized root: assume borrowed, i.e. leak rather than dangle
		}
	}
}

// slotIsFramed reports whether an alloca is recorded in any live scope frame — the
// backend's record that the binding owns a reference and will release it at its
// scope exit.
// slotIsOwning reports whether the binding held in slot owns the managed value it
// currently holds — i.e. whether overwriting it drops a genuine reference rather than
// a borrowed alias someone else is still counting.
//
// Two ways it holds. The slot is *framed*, meaning the backend recorded it for a
// scope-exit release: a local `let`/`var`, or an `own` parameter (which the caller
// transferred to). Or it is a by-reference `mut` parameter, whose slot *is* the
// caller's storage — the caller framed it and holds the +1, so the old value there is
// a real reference to drop, not a duplicate to dangle.
//
// Everything else is a borrow, most importantly a **by-value `string`/`ref`
// parameter**: the callee's copy shares the caller's reference, so releasing through
// it frees a value the caller still owns and will release again. That was an
// ASan-confirmed heap-use-after-free (07/30) — `(s: string) => { s = "l" ++ "1"  s }`
// freed the argument the caller was about to release — and it survived this long
// because the ASan suite was not instrumenting memory accesses at all.
//
// Both the whole-binding reassignment path (lowerVarReassignment) and the interior
// lvalue path (releaseOldTarget) ask this one question, because they are the same
// question: the two disagreeing is precisely a leak or a double free.
func (l *lowerer) slotIsOwning(slot value.Value) bool {
	return l.byRefParams[slot] || l.slotIsFramed(slot)
}

func (l *lowerer) slotIsFramed(slot value.Value) bool {
	return slices.ContainsFunc(l.managedFrames, func(frame []managedSlot) bool {
		return slices.ContainsFunc(frame, func(m managedSlot) bool { return m.slot == slot })
	})
}

// lvalueLoc is an assignable location: its address, the Lyra type stored there, and
// whether the hop that reached it went *through a ref-counted box* (a `shared`
// struct/array or a dynamic array) rather than through inline aggregate storage.
// viaBox is the aliasing fact releaseOldTarget needs, and it describes the **final**
// hop only — each hop overwrites it, since crossing into a box makes everything
// below that point unaliased again.
type lvalueLoc struct {
	ptr    value.Value
	ty     types.Type
	viaBox bool
}

// lvalueAddress returns the address (and Lyra type) of an assignable location,
// recursing over the path: an **identifier** root resolves to its alloca; a
// **`.field`** hop geps into the object's stack-struct storage; an **`[i]`** hop
// geps to the array element (bounds-checked, negative-from-end). Because it recurses
// on the object, arbitrary mixes nest — `grid[i].y`, `p.arr[i]`, `m[i][j]`,
// `line.start.x`. A fixed-size array is addressed through its storage; a `shared` or
// dynamic array — and a `shared` struct — through its box (loaded from the object's
// location).
func (l *lowerer) lvalueAddress(block *ir.Block, e ast.Expression) (lvalueLoc, *ir.Block, error) {
	loc, end, err := l.lvalueAddressRaw(block, e)
	// Normalize the location's type once, here, rather than at each hop: a field
	// or element declared as a newtype arrives as that name, and every consumer of
	// `ty` asks a *representation* question (which LLVM type, is it managed, which
	// release shape). Answering those against the wrapper silently skipped the
	// release of an overwritten `newtype Email = string` field — and, had only the
	// managed test been fixed, would have released a string fat pointer as if it
	// were a bare box pointer. One place, so the two can't disagree.
	loc.ty = l.stripNewtype(loc.ty)
	return loc, end, err
}

func (l *lowerer) lvalueAddressRaw(block *ir.Block, e ast.Expression) (lvalueLoc, *ir.Block, error) {
	switch t := e.(type) {
	case *ast.IdentifierExpr:
		slot, ok := l.slotFor(t.Name)
		if !ok {
			return lvalueLoc{}, nil, fmt.Errorf("llvm: assignment to unbound identifier %q", t.Name)
		}
		// A slot is addressable whether it is an entry-block alloca or a by-reference
		// `mut` parameter (whose slot is the caller's storage — writing through it is
		// the whole point of the modifier).
		if _, err := slotElemType(slot); err != nil {
			return lvalueLoc{}, nil, fmt.Errorf("llvm: identifier %q is not addressable", t.Name)
		}
		lyraType, _ := l.recordedType(t)
		// A bare identifier is only ever the *root* of a path here — the collector
		// routes a whole-binding assignment to VarReassignmentStmt — so viaBox is
		// never read for this case; false is the conservative answer regardless.
		return lvalueLoc{ptr: slot, ty: lyraType}, block, nil

	case *ast.MemberExpr:
		return l.memberFieldAddress(block, t)

	case *ast.IndexExpr:
		return l.indexElemAddress(block, t)
	}
	return lvalueLoc{}, nil, fmt.Errorf("llvm: unsupported assignment target path element %T", e)
}

// memberFieldAddress computes the address of `obj.field` by taking the object's
// address and gep-ing to the named field of its (stack) struct type.
func (l *lowerer) memberFieldAddress(block *ir.Block, e *ast.MemberExpr) (lvalueLoc, *ir.Block, error) {
	if e.Optional {
		return lvalueLoc{}, nil, fmt.Errorf("llvm: optional member assignment (?.) not implemented")
	}
	objType, ok := l.recordedType(e.Object)
	if !ok {
		return lvalueLoc{}, nil, fmt.Errorf("llvm: no type recorded for member-assignment object")
	}
	fields, ok := l.namedStructFields(objType)
	if !ok {
		return lvalueLoc{}, nil, fmt.Errorf("llvm: field assignment on non-struct type %s is not implemented", objType)
	}
	idx := -1
	var fieldType types.Type
	for i, f := range fields {
		if f.Name == e.Property.Name {
			idx, fieldType = i, f.Type
			break
		}
	}
	if idx < 0 {
		return lvalueLoc{}, nil, fmt.Errorf("llvm: struct has no field %q", e.Property.Name)
	}

	// A `shared` struct is a pointer to its box `{ i64 rc, payload }`, so the field
	// is addressed through the box (loaded from the object's slot): box → payload
	// (field 1) → field idx — the write counterpart to lowerMemberExpr's read.
	if types.AllocationOf(objType) == types.Shared {
		box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		payloadTy, err := l.lowerType(types.WithAllocation(objType, types.Stack))
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		fieldPtr := block.NewGetElementPtr(SharedBoxType(payloadTy), box,
			i32c(0), i32c(boxPayloadField), constant.NewInt(lltypes.I32, int64(idx)))
		return lvalueLoc{ptr: fieldPtr, ty: fieldType, viaBox: true}, block, nil
	}

	// A stack struct: the object's address *is* the struct storage; gep into it. The
	// field therefore lives in an inline aggregate that a by-value copy of the object
	// duplicates without retaining — viaBox stays false (see releaseOldTarget).
	obj, block, err := l.lvalueAddress(block, e.Object)
	if err != nil {
		return lvalueLoc{}, nil, err
	}
	structTy, err := l.lowerType(objType)
	if err != nil {
		return lvalueLoc{}, nil, err
	}
	fieldPtr := block.NewGetElementPtr(structTy, obj.ptr,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, int64(idx)))
	return lvalueLoc{ptr: fieldPtr, ty: fieldType}, block, nil
}

// indexElemAddress computes the address of `obj[i]`. A fixed-size array is addressed
// through the object's own storage (its alloca / field slot); a `shared` or dynamic
// array through its box, loaded from that storage. The index is bounds-checked
// against the compile-time size or the runtime length.
func (l *lowerer) indexElemAddress(block *ir.Block, e *ast.IndexExpr) (lvalueLoc, *ir.Block, error) {
	objType, ok := l.recordedType(e.Object)
	if !ok {
		return lvalueLoc{}, nil, fmt.Errorf("llvm: no type recorded for index-assignment object")
	}
	switch at := objType.(type) {
	case types.StaticArrayType:
		elemLL, err := l.lowerType(at.ElementType)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		arrayTy := lltypes.NewArray(uint64(at.Size), elemLL)
		size := constant.NewInt(lltypes.I64, int64(at.Size))
		if types.AllocationOf(objType) == types.Shared {
			box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
			if err != nil {
				return lvalueLoc{}, nil, err
			}
			adjusted, block, err := l.boundsCheckedIndex(block, e, size)
			if err != nil {
				return lvalueLoc{}, nil, err
			}
			// box → SharedBox { i64 rc, [N x T] } field 1 → element.
			elemPtr := block.NewGetElementPtr(SharedBoxType(arrayTy), box,
				i32c(0), i32c(boxPayloadField), adjusted)
			return lvalueLoc{ptr: elemPtr, ty: at.ElementType, viaBox: true}, block, nil
		}
		// A stack `[N]T`: addressed through the object's own storage, so the element
		// lives in an inline aggregate a by-value copy duplicates without retaining —
		// viaBox stays false (see releaseOldTarget).
		obj, block, err := l.lvalueAddress(block, e.Object) // pointer to [N x T]
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		adjusted, block, err := l.boundsCheckedIndex(block, e, size)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		elemPtr := block.NewGetElementPtr(arrayTy, obj.ptr, constant.NewInt(lltypes.I64, 0), adjusted)
		return lvalueLoc{ptr: elemPtr, ty: at.ElementType}, block, nil

	case types.DynamicArrayType:
		elemLL, err := l.lowerType(at.ElementType)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		boxTy := DynArrayBoxType(elemLL)
		box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		adjusted, block, err := l.boundsCheckedIndex(block, e, length)
		if err != nil {
			return lvalueLoc{}, nil, err
		}
		elemPtr := dynArrayElemPtr(block, boxTy, box, adjusted)
		return lvalueLoc{ptr: elemPtr, ty: at.ElementType, viaBox: true}, block, nil
	}
	return lvalueLoc{}, nil, fmt.Errorf("llvm: index assignment into %s is not implemented", objType)
}

// lvalueBoxPtr loads the box pointer a `shared`/dynamic-array location holds: the
// object's *address* (from lvalueAddress) points at the box-pointer slot, so this
// loads it. Reading the box pointer this way (rather than via lowerExpr) keeps the
// object out of the ownership retain/release hooks — the assignment only mutates the
// box in place, it doesn't take a reference.
func (l *lowerer) lvalueBoxPtr(block *ir.Block, objExpr ast.Expression, objType types.Type) (value.Value, *ir.Block, error) {
	loc, block, err := l.lvalueAddress(block, objExpr)
	if err != nil {
		return nil, nil, err
	}
	boxPtrTy, err := l.lowerType(objType) // ptr to the box
	if err != nil {
		return nil, nil, err
	}
	return block.NewLoad(boxPtrTy, loc.ptr), block, nil
}

// boundsCheckedIndex lowers the index of `e` and returns the bounds-trapped i64
// offset — one unsigned compare against size, [0, size), with a negative caught by
// its sign-extension (out-of-range traps via lyra_panic_index_out_of_bounds; the
// negative-counts-from-the-end reading was removed 08/12). Unlike the read path it
// does not consult the value-range analysis's IndexInBounds — an assignment target
// isn't marked — so the check is always emitted.
func (l *lowerer) boundsCheckedIndex(block *ir.Block, e *ast.IndexExpr, size value.Value) (value.Value, *ir.Block, error) {
	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	signed, _ := l.getIntSignedness(e.Index)
	idx64 := coerceIntWidth(block, idx, signed, lltypes.I64)
	oob := block.NewICmp(enum.IPredUGE, idx64, size)
	block = l.emitTrapIf(block, oob, l.panicIndexOOBFunc())
	return idx64, block, nil
}
