package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/abi"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Passing an aggregate **by value** across the C boundary.
//
// This is the half `pkg/abi` exists for: that package decides *what* a struct becomes on
// a given target, and this one emits it. The division matters because the decision is the
// part that can be silently wrong — a misclassification links cleanly and computes
// garbage — so it is made in one place, validated against clang by `abi_diff_test.go`,
// and consumed here without second-guessing.
//
// **Only an `extern` takes this path.** Lyra's own calling convention is Lyra's; a Lyra
// function passing a struct keeps passing it the way it always has, and nothing about
// `declareFunctionAs` changes. An extern with no aggregate in its signature gets no plan
// at all, so the overwhelmingly common foreign call is untouched.

// externPlan is how one foreign signature crosses: a class per parameter, plus the return.
// nil means "nothing here is an aggregate" and the ordinary path applies.
type externPlan struct {
	params []abiSlot
	ret    abiSlot
	// retIndirect means the caller allocates the return and passes it as a leading
	// `sret` pointer; the function itself returns void.
	retIndirect bool
	llRet       lltypes.Type
}

// abiSlot is one Lyra parameter (or the return) and what it lowers to.
type abiSlot struct {
	lyra      types.Type
	aggregate bool
	class     abi.Class
	// llTypes are the LLVM parameters this slot becomes: one for a scalar, one per Part
	// for a Direct aggregate, and a single pointer for an Indirect one.
	llTypes []lltypes.Type
	// aggTy is the aggregate's own LLVM type, for the alloca the coercion goes through.
	aggTy lltypes.Type
}

// planExtern classifies a foreign signature, or returns nil when no aggregate crosses.
func (l *lowerer) planExtern(ext *ast.ExternDeclStmt) (*externPlan, error) {
	sig := ext.Signature
	if sig == nil {
		return nil, nil
	}
	// **Enter the extern's module before resolving anything in its signature.**
	// `declareExterns` walks the program from the top level, where `tc`-side module
	// scoping is not installed, and a named type in a foreign signature is its own
	// module's to resolve — so without this `Big` stays an `UnresolvedType`,
	// `isCAggregate` answers false, and the whole classifier is skipped: the declaration
	// comes out as `%main__Big` where clang expects a pointer, which links and computes
	// garbage. `declareFunctionAs` has the same line for the same reason, and
	// COMPLETED.md 09/09 states the rule this is the third instance of.
	//
	// It is invisible without a `module` header, which every real program has and every
	// paste-sized reproduction does not — the backend harness prepends `module main`
	// precisely so this class of bug cannot hide.
	defer l.enterModuleOf(ext.GetLocation())()
	anyAggregate := l.isCAggregate(sig.ReturnType.Type)
	for _, p := range sig.Parameters {
		if l.isCAggregate(p.Type) {
			anyAggregate = true
		}
	}
	if !anyAggregate {
		return nil, nil
	}
	if l.target == abi.Unknown {
		return nil, fmt.Errorf(
			"llvm: `extern %s` passes an aggregate by value, and this build has no calling "+
				"convention for the target. Pass it by pointer (`^T`) instead, or build for a "+
				"target the compiler classifies (aarch64, x86-64 System V)",
			ext.Name)
	}

	plan := &externPlan{}
	for i, p := range sig.Parameters {
		slot, err := l.planSlot(p.Type, false)
		if err != nil {
			return nil, fmt.Errorf("llvm: `extern %s` parameter %d: %w", ext.Name, i+1, err)
		}
		plan.params = append(plan.params, slot)
	}
	ret, err := l.planSlot(sig.ReturnType.Type, true)
	if err != nil {
		return nil, fmt.Errorf("llvm: `extern %s` return: %w", ext.Name, err)
	}
	plan.ret = ret
	switch {
	case !ret.aggregate:
		plan.llRet = ret.llTypes[0]
	case ret.class.Indirect:
		plan.retIndirect = true
		plan.llRet = lltypes.Void
	default:
		plan.llRet = returnLLType(ret)
	}
	return plan, nil
}

// planSlot classifies one type in parameter or return position.
func (l *lowerer) planSlot(t types.Type, isReturn bool) (abiSlot, error) {
	if t == nil {
		return abiSlot{}, fmt.Errorf("missing type")
	}
	if _, isVoid := t.(types.VoidType); isVoid {
		return abiSlot{lyra: t, llTypes: []lltypes.Type{lltypes.Void}}, nil
	}
	if !l.isCAggregate(t) {
		ll, err := l.lowerType(t)
		if err != nil {
			return abiSlot{}, err
		}
		return abiSlot{lyra: t, llTypes: []lltypes.Type{ll}}, nil
	}
	agg, err := l.flattenAggregate(t)
	if err != nil {
		return abiSlot{}, err
	}
	aggTy, err := l.lowerType(l.resolveForLayout(t))
	if err != nil {
		return abiSlot{}, err
	}
	class := abi.Classify(l.target, agg, isReturn)
	slot := abiSlot{lyra: t, aggregate: true, class: class, aggTy: aggTy}
	if class.Indirect {
		slot.llTypes = []lltypes.Type{lltypes.NewPointer(aggTy)}
		return slot, nil
	}
	for _, part := range class.Parts {
		slot.llTypes = append(slot.llTypes, partLLType(part))
	}
	return slot, nil
}

// returnLLType is the type a Direct aggregate return is spelled as.
//
// Three shapes, and they are clang's rather than a choice: a single part that is *not* a
// struct return is that part; an AAPCS64 homogeneous float aggregate comes back as the
// aggregate's **own** type; and a SysV multi-eightbyte return is a literal struct of its
// parts, since a function returns once.
func returnLLType(slot abiSlot) lltypes.Type {
	if !slot.class.ReturnAsStruct {
		return slot.llTypes[0]
	}
	if len(slot.llTypes) == 1 {
		return slot.aggTy
	}
	return lltypes.NewStruct(slot.llTypes...)
}

// partLLType is the LLVM spelling of one classified piece.
func partLLType(p abi.Part) lltypes.Type {
	switch p.Kind {
	case abi.PartInt:
		return lltypes.NewInt(uint64(p.Bits))
	case abi.PartFloat:
		return lltypes.Float
	case abi.PartDouble:
		return lltypes.Double
	case abi.PartFloatArray:
		return lltypes.NewArray(uint64(p.Count), lltypes.Float)
	case abi.PartDoubleArray:
		return lltypes.NewArray(uint64(p.Count), lltypes.Double)
	case abi.PartIntArray:
		return lltypes.NewArray(uint64(p.Count), lltypes.I64)
	case abi.PartFloatVec2:
		return lltypes.NewVector(2, lltypes.Float)
	}
	return lltypes.I64
}

// declareExternWithPlan builds the `declare` a planned signature calls for.
func (l *lowerer) declareExternWithPlan(symbol string, plan *externPlan) *ir.Func {
	var params []*ir.Param
	if plan.retIndirect {
		// The caller's buffer, first. `sret` is not decoration: it tells the target
		// backend this pointer *is* the return, which on x86-64 means rdi and a copy of
		// the pointer back in rax — behaviour a plain pointer parameter does not get.
		p := ir.NewParam("agg.result", lltypes.NewPointer(plan.ret.aggTy))
		p.Attrs = append(p.Attrs, ir.SRet{Typ: plan.ret.aggTy})
		params = append(params, p)
	}
	for i, slot := range plan.params {
		for j, ll := range slot.llTypes {
			p := ir.NewParam(fmt.Sprintf("a%d_%d", i, j), ll)
			if slot.aggregate && slot.class.Indirect && l.target == abi.X86_64SysV {
				// SysV passes a MEMORY aggregate as a *copy on the stack*, which is what
				// `byval` says; AAPCS64 passes a plain pointer and clang emits no
				// attribute there. Measured from clang, not assumed.
				p.Attrs = append(p.Attrs, ir.Byval{Typ: slot.aggTy})
			}
			params = append(params, p)
		}
	}
	return l.module.NewFunc(symbol, plan.llRet, params...)
}

// lowerExternCall emits a call to a foreign function whose signature carries an aggregate.
//
// Every coercion goes **through memory**, which is not a fallback but the only thing that
// can be said: an aggregate's bytes are being reinterpreted as the registers the ABI names,
// and LLVM has no way to spell that on an SSA value. `alloca`, store the aggregate, bitcast
// the slot to the coerced shape, load the pieces — exactly what clang emits at `-O0`, and
// what the optimizer folds away.
func (l *lowerer) lowerExternCall(block *ir.Block, e *ast.FunctionCallExpr, fn *ir.Func, plan *externPlan) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != len(plan.params) {
		return nil, nil, fmt.Errorf("llvm: %s expects %d argument(s), got %d",
			fn.GlobalIdent.Ident(), len(plan.params), len(e.Arguments))
	}
	var args []value.Value

	// The return buffer comes first when the return is indirect.
	var retSlot value.Value
	if plan.retIndirect {
		retSlot = block.Parent.Blocks[0].NewAlloca(plan.ret.aggTy)
		args = append(args, retSlot)
	}

	for i, arg := range e.Arguments {
		v, nextBlock, err := l.lowerExpr(block, arg)
		if err != nil {
			return nil, nil, err
		}
		block = nextBlock
		if diverged(v, block) {
			return nil, block, nil
		}
		slot := plan.params[i]
		if !slot.aggregate {
			args = append(args, v)
			continue
		}
		pieces, err := l.coerceAggregateOut(block, v, slot)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, pieces...)
	}

	call := block.NewCall(fn, args...)
	if !plan.ret.aggregate {
		if _, isVoid := plan.llRet.(*lltypes.VoidType); isVoid {
			return call, block, nil
		}
		return call, block, nil
	}
	if plan.retIndirect {
		return block.NewLoad(plan.ret.aggTy, retSlot), block, nil
	}
	return l.coerceAggregateIn(block, call, plan.ret), block, nil
}

// coerceAggregateOut turns an aggregate value into the register pieces the ABI passes.
func (l *lowerer) coerceAggregateOut(block *ir.Block, v value.Value, slot abiSlot) ([]value.Value, error) {
	store := l.aggregateSpill(block, v, slot)
	if slot.class.Indirect {
		// A memory argument is the address of the caller's copy. The copy matters: the
		// callee may write through it, and `byval` means it gets one of its own on SysV.
		return []value.Value{store}, nil
	}
	coerceTy := lltypes.NewStruct(slot.llTypes...)
	as := block.NewBitCast(store, lltypes.NewPointer(coerceTy))
	pieces := make([]value.Value, 0, len(slot.llTypes))
	for i, ll := range slot.llTypes {
		p := block.NewGetElementPtr(coerceTy, as, i32c(0), i32c(int64(i)))
		pieces = append(pieces, block.NewLoad(ll, p))
	}
	return pieces, nil
}

// coerceAggregateIn turns a returned register value back into the aggregate.
func (l *lowerer) coerceAggregateIn(block *ir.Block, ret value.Value, slot abiSlot) value.Value {
	retTy := returnLLType(slot)
	// The slot has to hold whichever of the two shapes is larger — a `{u8,u8}` comes back
	// in an `i16` on aarch64 and in an `i64` when passed, and storing the wider one into a
	// slot sized for the narrower writes past it.
	store := l.allocaFor(block, slot.aggTy, retTy)
	as := block.NewBitCast(store, lltypes.NewPointer(retTy))
	block.NewStore(ret, as)
	return block.NewLoad(slot.aggTy, store)
}

// aggregateSpill stores v into a stack slot big enough for both its own shape and the
// coerced one, and answers the slot.
func (l *lowerer) aggregateSpill(block *ir.Block, v value.Value, slot abiSlot) value.Value {
	var coerceTy lltypes.Type = slot.aggTy
	if !slot.class.Indirect {
		coerceTy = lltypes.NewStruct(slot.llTypes...)
	}
	store := l.allocaFor(block, slot.aggTy, coerceTy)
	as := block.NewBitCast(store, lltypes.NewPointer(slot.aggTy))
	block.NewStore(v, as)
	return store
}

// allocaFor reserves a slot that holds both shapes, allocating the larger and bitcasting
// for the smaller. Under-allocating here is a stack overwrite rather than a wrong value,
// which is the failure this whole area specialises in.
func (l *lowerer) allocaFor(block *ir.Block, aggTy, coerceTy lltypes.Type) value.Value {
	entry := block.Parent.Blocks[0]
	if llSizeOf(coerceTy) > llSizeOf(aggTy) {
		return entry.NewAlloca(coerceTy)
	}
	return entry.NewAlloca(aggTy)
}

// llSizeOf is the byte size of a lowered type, for the "which shape is bigger" question
// alone. It answers 0 for anything it does not know, which biases allocaFor toward the
// aggregate's own type — the safe direction, since that is the shape the value has.
func llSizeOf(t lltypes.Type) int {
	switch v := t.(type) {
	case *lltypes.IntType:
		return int((v.BitSize + 7) / 8)
	case *lltypes.FloatType:
		switch v.Kind {
		case lltypes.FloatKindFloat:
			return 4
		case lltypes.FloatKindDouble:
			return 8
		}
		return 8
	case *lltypes.PointerType:
		return pointerSize
	case *lltypes.ArrayType:
		return int(v.Len) * llSizeOf(v.ElemType)
	case *lltypes.VectorType:
		return int(v.Len) * llSizeOf(v.ElemType)
	case *lltypes.StructType:
		total := 0
		for _, f := range v.Fields {
			total += llSizeOf(f)
		}
		return total
	}
	return 0
}
