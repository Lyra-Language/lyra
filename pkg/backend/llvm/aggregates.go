package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerTupleLiteralExpr lowers tuple construction (`Point(3, 4)`, `(1, 2)`) to an
// SSA aggregate: start from an undef struct and `insertvalue` each element in
// declaration order. The result is a first-class struct *value* — a `let` binding
// then allocas it by its `.Type()` and store/loads it like any scalar (mem2reg
// promotes it), and lowerTupleIndexExpr reads elements back with `extractvalue`.
//
// Only tuple-typed literals build a plain aggregate here. A capitalized call the
// typechecker resolved to a data constructor (`Cons(1, tail)`) records a DataType,
// not a TupleType — that's a positional variant, routed to lowerDataConstruction
// (the tagged union, DATA_LAYOUT.md).
func (l *lowerer) lowerTupleLiteralExpr(block *ir.Block, e *ast.TupleLiteralExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for tuple literal")
	}
	if dt, ok := recorded.(types.DataType); ok {
		return l.lowerDataConstruction(block, dt, e.Name, e.Elements, e)
	}
	tupleType, ok := recorded.(types.TupleType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: tuple literal lowering not implemented for %s", recorded)
	}
	llType, err := l.lowerType(tupleType)
	if err != nil {
		return nil, nil, err
	}
	structType, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: tuple type %s did not lower to a struct", tupleType)
	}

	var agg value.Value = constant.NewUndef(structType)
	for i, elemExpr := range e.Elements {
		var elemVal value.Value
		elemVal, block, err = l.lowerExpr(block, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		elemVal, err = l.coerceAggregateElem(block, elemVal, structType.Fields[i], elemExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, elemVal, uint64(i))
	}
	return agg, block, nil
}

// lowerTupleIndexExpr lowers positional tuple access (`pair.0`) to an
// `extractvalue` on the (already first-class) struct value the object lowers to.
// The typechecker validated the index is in range, so it maps straight to the
// struct element position.
func (l *lowerer) lowerTupleIndexExpr(block *ir.Block, e *ast.TupleIndexExpr) (value.Value, *ir.Block, error) {
	obj, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := obj.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: tuple index on non-struct value of type %s", obj.Type())
	}
	return block.NewExtractValue(obj, uint64(e.Index)), block, nil
}

// lowerStructInstanceExpr lowers struct construction (`Node { value: 3 }`) to a
// first-class struct value, the same insertvalue-over-undef shape as a tuple.
// The one extra concern is ordering: a struct literal names its fields and may
// list them in any order, but the LLVM struct is in *declaration* order — so the
// fields are keyed by name and built in the declared order (also the index each
// insertvalue targets).
//
// Deferred with a loud error: record-update syntax (`P { base | f: v }`), a
// missing field relying on a default value, and an inline-record data
// constructor (which records the owning DataType, not a struct — that's the
// data/tagged-union work).
func (l *lowerer) lowerStructInstanceExpr(block *ir.Block, e *ast.StructInstanceExpr) (value.Value, *ir.Block, error) {
	if e.BaseStruct != nil {
		return nil, nil, fmt.Errorf("llvm: struct record-update syntax not implemented yet (%q)", e.Name)
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for struct instance %q", e.Name)
	}
	structType, ok := recorded.(types.NamedStructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: struct instance lowering not implemented for %s", recorded)
	}
	// Build the inline struct payload first (stripping any `shared` flavor, which
	// would otherwise lower to a box pointer); it's boxed at the end if shared.
	stackStructType := types.WithAllocation(structType, types.Stack).(types.NamedStructType)
	llType, err := l.lowerType(stackStructType)
	if err != nil {
		return nil, nil, err
	}
	structTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: struct type %s did not lower to a struct", structType.Name)
	}

	// Key each supplied field's value by name (a positional literal field — Name
	// "" — takes the declared name at that position), then build in declared order.
	valueByName := make(map[string]ast.Expression, len(e.Fields))
	for i, f := range e.Fields {
		name := f.Name
		if name == "" {
			name = structType.Fields[i].Name
		}
		valueByName[name] = f.Value
	}

	var agg value.Value = constant.NewUndef(structTy)
	for i, declField := range structType.Fields {
		valExpr, ok := valueByName[declField.Name]
		if !ok {
			return nil, nil, fmt.Errorf("llvm: struct %s field %q has no value (default values not implemented yet)", structType.Name, declField.Name)
		}
		var v value.Value
		v, block, err = l.lowerExpr(block, valExpr)
		if err != nil {
			return nil, nil, err
		}
		// Reconcile a residual width mismatch, as the tuple-literal and data-payload
		// paths above already do. A struct literal is the third way into an aggregate
		// and had none of this, so the one mismatch that is a loud error on a tuple
		// would have been an llir panic here.
		v, err = l.coerceAggregateElem(block, v, structTy.Fields[i], valExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, v, uint64(i))
	}
	// A `shared`-flavored construction is heap-allocated in a ref-counted box; the
	// value is the box pointer. The flavor is recorded on this node only when it's
	// the direct initializer of a `shared`-annotated binding (checkVarDecl).
	if types.AllocationOf(recorded) == types.Shared {
		// Stack strips the Shared flavor so the payload sizes as its inline struct,
		// not pointer-sized (WithAllocation treats Unspecified as "no change").
		boxed, err := l.lowerBoxShared(block, agg, types.WithAllocation(structType, types.Stack))
		return boxed, block, err
	}
	return agg, block, nil
}

// lowerMemberExpr lowers struct field access (`node.value`) to an `extractvalue`
// on the object's struct value. The field's position comes from the object's
// declared struct type (looked up by name), since the LLVM struct type carries
// no field names. A method call (`obj.method()`) never reaches here — it's a
// FunctionCallExpr whose callee is the MemberExpr — so this is field access only.
func (l *lowerer) lowerMemberExpr(block *ir.Block, e *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if e.Optional {
		return nil, nil, fmt.Errorf("llvm: optional member access (?.) not implemented yet")
	}
	objType, ok := l.recordedType(e.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for member-access object")
	}
	fields, ok := l.namedStructFields(objType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: field access on non-struct type %s not implemented", objType)
	}
	idx := -1
	for i, f := range fields {
		if f.Name == e.Property.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil, fmt.Errorf("llvm: struct has no field %q", e.Property.Name)
	}
	obj, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	// A `shared` object is a pointer to its box `{ strong, weak, payload }`; read the
	// field through the box (getelementptr box → payload → field, then load) rather
	// than extractvalue on an inline struct.
	if ptr, ok := obj.Type().(*lltypes.PointerType); ok {
		boxTy, ok := ptr.ElemType.(*lltypes.StructType)
		if !ok || len(boxTy.Fields) != boxPayloadField+1 {
			return nil, nil, fmt.Errorf("llvm: member access on non-box pointer %s", obj.Type())
		}
		payloadTy, ok := boxTy.Fields[boxPayloadField].(*lltypes.StructType)
		if !ok || idx >= len(payloadTy.Fields) {
			return nil, nil, fmt.Errorf("llvm: `shared` field access on non-struct payload %s", boxTy.Fields[boxPayloadField])
		}
		fieldPtr := block.NewGetElementPtr(boxTy, obj,
			i32c(0), i32c(boxPayloadField), constant.NewInt(lltypes.I32, int64(idx)))
		return block.NewLoad(payloadTy.Fields[idx], fieldPtr), block, nil
	}
	if _, ok := obj.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: member access on non-struct value of type %s", obj.Type())
	}
	return block.NewExtractValue(obj, uint64(idx)), block, nil
}

// namedStructFields returns the declared fields (name + order) of a named-struct
// type. It resolves an UnresolvedType — which is how a field or binding typed as
// another named struct is recorded — through the symbol table, so nested field
// access (`line.start.x`) finds the inner struct's fields too.
func (l *lowerer) namedStructFields(t types.Type) ([]types.StructField, bool) {
	switch s := t.(type) {
	case types.NamedStructType:
		return s.Fields, true
	case types.AnonymousStructType:
		// Field access reads a *position*, and for an anonymous struct the type's own
		// field order is that position — the same order lowerAnonymousStructType builds
		// the LLVM struct in and lowerAnonymousStructInstanceExpr places values in. One
		// order, established by the type, so the three cannot disagree.
		return s.Fields, true
	case types.UnresolvedType:
		if decl, ok := l.lookupTypeDecl(s.Name); ok {
			if ns, ok := decl.Type.(types.NamedStructType); ok {
				return ns.Fields, true
			}
		}
	}
	return nil, false
}

// lowerDataConstructorExpr lowers a nullary data constructor (`Red`, `Nil`,
// `None` — a DataConstructorExpr with no payload). It records the owning DataType,
// so construction is just materializing the union with the variant's tag and no
// payload.
func (l *lowerer) lowerDataConstructorExpr(block *ir.Block, e *ast.DataConstructorExpr) (value.Value, *ir.Block, error) {
	if e.Value != nil {
		return nil, nil, fmt.Errorf("llvm: non-nullary DataConstructorExpr %q not expected here", e.Constructor)
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for data constructor %q", e.Constructor)
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data constructor %q did not record a data type (got %s)", e.Constructor, recorded)
	}
	return l.lowerDataConstruction(block, dt, e.Constructor, nil, e)
}

// lowerDataConstruction materializes a `data` value of type dt for the variant
// ctorName with positional argument expressions args (empty for a nullary
// variant). Following DATA_LAYOUT.md, it goes through memory rather than an SSA
// aggregate, because the payload blob (`[K x iA]`) is reinterpreted as this
// variant's payload struct: alloca the union, store the tag, and — for a variant
// with a payload — GEP the blob field, bitcast it to the variant's payload-struct
// pointer, and store the built payload struct. The union value is then loaded back
// as a first-class value so it flows through `let`/calls like a tuple or struct.
//
// A `shared` payload field (a recursive variant like `Cons(i64, shared List)`)
// needs ref-counted-box allocation (ALLOCATION.md), which isn't lowered yet, so
// it errors loudly. Inline-record variants (`Node { … }`) route through
// lowerStructInstanceExpr and are deferred there.
func (l *lowerer) lowerDataConstruction(block *ir.Block, dt types.DataType, ctorName string, args []ast.Expression, srcExpr ast.Expression) (value.Value, *ir.Block, error) {
	tag := -1
	var ctor types.DataTypeConstructor
	for i, c := range dt.Constructors {
		if c.Name == ctorName {
			tag, ctor = i, c
			break
		}
	}
	if tag < 0 {
		return nil, nil, fmt.Errorf("llvm: data type %q has no constructor %q", dt.Name, ctorName)
	}
	fields := ctor.FieldTypes()
	if len(args) != len(fields) {
		return nil, nil, fmt.Errorf("llvm: constructor %q expects %d argument(s), got %d", ctorName, len(fields), len(args))
	}

	// Lower the payload arguments first — an argument containing a branch moves the
	// insertion point — then materialize the union from the resulting values.
	var payloadFields []value.Value
	if len(fields) > 0 {
		payloadStructTy, err := l.dataPayloadStructType(ctor)
		if err != nil {
			return nil, nil, err
		}
		for i, argExpr := range args {
			var v value.Value
			v, block, err = l.lowerExpr(block, argExpr)
			if err != nil {
				return nil, nil, err
			}
			v, err = l.coerceAggregateElem(block, v, payloadStructTy.Fields[i], argExpr)
			if err != nil {
				return nil, nil, err
			}
			payloadFields = append(payloadFields, v)
		}
	}

	union, err := l.buildDataValue(block, dt, tag, ctor, payloadFields)
	if err != nil {
		return nil, nil, err
	}

	// A `shared` data value is heap-allocated in a ref-counted box; the value is the
	// box pointer. (This is how a recursive `shared` payload field — a `Cons`'s
	// `shared List` — is filled: the nested constructor is boxed here.)
	if types.AllocationOf(dt) == types.Shared {
		stackDt, ok := types.WithAllocation(dt, types.Stack).(types.DataType)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: data type %q did not survive allocation stripping", dt.Name)
		}
		// Perceus reuse: if this construction is the reuse target of an enclosing
		// reuse-match and that match's token is still live, write into the reclaimed
		// box instead of allocating. Consuming the token clears it so a later
		// construction (or the arm's fall-through free) doesn't touch it again.
		if l.reuseToken != nil && l.ownership().IsReuseTarget(srcExpr) {
			token := l.reuseToken
			l.reuseToken = nil
			return l.lowerBoxSharedReuse(block, union, stackDt, token)
		}
		boxed, err := l.lowerBoxShared(block, union, stackDt)
		return boxed, block, err
	}
	return union, block, nil
}

// buildDataValue materializes the inline tagged union for variant ctor (declaration
// index `tag`) of dt from already-lowered payload field values: alloca the union,
// store the tag, and store the payload struct through a bitcast of the blob field
// (DATA_LAYOUT.md).
//
// It is the **write-side mirror of extractDataPayload**, and the one place the
// `{ tag, payload-blob }` encoding is written. Two callers reach it from opposite
// directions: lowerDataConstruction, after lowering its argument *expressions*, and
// `?` (try.go), which propagates an error payload it extracted from a *different*
// instantiation's union and so has values rather than expressions. Sharing this is
// what keeps the two from drifting apart on the layout.
//
// The value returned is the inline union. Boxing a `shared` flavor stays with the
// caller, because only the caller knows whether a Perceus reuse token applies.
//
// Field types must already match the variant's payload struct exactly — a residual
// int-width mismatch is a loud error here, not a silent coercion. lowerDataConstruction
// reconciles widths through coerceAggregateElem before it calls in, where it still has
// the source expression the signedness must be read from.
func (l *lowerer) buildDataValue(block *ir.Block, dt types.DataType, tag int, ctor types.DataTypeConstructor, fields []value.Value) (value.Value, error) {
	stackDt, ok := types.WithAllocation(dt, types.Stack).(types.DataType)
	if !ok {
		return nil, fmt.Errorf("llvm: data type %q did not survive allocation stripping", dt.Name)
	}
	llType, err := l.lowerType(stackDt)
	if err != nil {
		return nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, fmt.Errorf("llvm: data type %q did not lower to a struct", dt.Name)
	}
	tagTy, ok := unionTy.Fields[0].(*lltypes.IntType)
	if !ok {
		return nil, fmt.Errorf("llvm: data type %q has a non-integer tag (%s)", dt.Name, unionTy.Fields[0])
	}

	// Alloca the union in the entry block (mem2reg-promotable), then fill it.
	slot := block.Parent.Blocks[0].NewAlloca(unionTy)
	tagPtr := block.NewGetElementPtr(unionTy, slot,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0))
	block.NewStore(constant.NewInt(tagTy, int64(tag)), tagPtr)

	// Store the payload (field 1, the blob) reinterpreted as this variant's payload
	// struct — only when the variant carries fields (a nullary variant of a type that
	// *has* payloads just leaves the blob undefined).
	if len(fields) > 0 {
		payloadStructTy, err := l.dataPayloadStructType(ctor)
		if err != nil {
			return nil, err
		}
		if len(fields) != len(payloadStructTy.Fields) {
			return nil, fmt.Errorf("llvm: constructor %q takes %d payload field(s), got %d",
				ctor.Name, len(payloadStructTy.Fields), len(fields))
		}
		var payload value.Value = constant.NewUndef(payloadStructTy)
		for i, f := range fields {
			if !f.Type().Equal(payloadStructTy.Fields[i]) {
				return nil, fmt.Errorf("llvm: cannot store %s into field %d of constructor %q (expected %s)",
					f.Type(), i, ctor.Name, payloadStructTy.Fields[i])
			}
			payload = block.NewInsertValue(payload, f, uint64(i))
		}
		blobPtr := block.NewGetElementPtr(unionTy, slot,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
		typedPtr := block.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
		block.NewStore(payload, typedPtr)
	}
	return block.NewLoad(unionTy, slot), nil
}

// dataPayloadStructType is the LLVM struct of a variant's payload fields, in
// order — what gets written by buildDataValue and read back by extractDataPayload.
// A `shared` field lowers to a pointer (lowerType), which is also what makes a
// recursive `shared` reference finite.
func (l *lowerer) dataPayloadStructType(ctor types.DataTypeConstructor) (*lltypes.StructType, error) {
	fieldTypes := ctor.FieldTypes()
	fields := make([]lltypes.Type, len(fieldTypes))
	for i, p := range fieldTypes {
		ft, err := l.lowerType(p)
		if err != nil {
			return nil, err
		}
		fields[i] = ft
	}
	return lltypes.NewStruct(fields...), nil
}

// structFieldIndexAndType returns a struct field's position and declared type by
// name (the type may be an UnresolvedType for a nested named type — resolved by
// the recursive caller).
func structFieldIndexAndType(st types.NamedStructType, name string) (int, types.Type, bool) {
	for i, f := range st.Fields {
		if f.Name == name {
			return i, f.Type, true
		}
	}
	return 0, nil, false
}

// findConstructor returns a data type's constructor by name, plus its
// declaration-order index (the variant tag).
func findConstructor(dt types.DataType, name string) (types.DataTypeConstructor, int, bool) {
	for i, c := range dt.Constructors {
		if c.Name == name {
			return c, i, true
		}
	}
	return types.DataTypeConstructor{}, 0, false
}

// coerceAggregateElem defensively reconciles a lowered aggregate element value
// with its destination field type before an insertvalue/store. A well-typed
// program already has matching widths — the typechecker's context-directed
// literal-width propagation narrows a tuple/struct/data-payload literal element
// to its declared field type — so this is normally the identity. It exists so a
// residual int-width mismatch is coerced (trunc/ext, widening signedness read
// from the source expr's Lyra type) rather than letting llir panic inside
// NewInsertValue (as `insertvalue elem type mismatch, expected i8, got i64`); a
// non-int mismatch it can't reconcile is a loud error, never a panic.
func (l *lowerer) coerceAggregateElem(block *ir.Block, v value.Value, dst lltypes.Type, src ast.Expression) (value.Value, error) {
	if v.Type().Equal(dst) {
		return v, nil
	}
	dstInt, dstOk := dst.(*lltypes.IntType)
	srcInt, srcOk := v.Type().(*lltypes.IntType)
	if dstOk && srcOk {
		signed := false
		if dstInt.BitSize > srcInt.BitSize {
			// Only a widening ext depends on the source signedness; a narrowing
			// trunc is width-only. Fall back to unsigned if the type is unknown.
			if s, err := l.getIntSignedness(src); err == nil {
				signed = s
			}
		}
		return coerceIntWidth(block, v, signed, dstInt), nil
	}
	return nil, fmt.Errorf("llvm: aggregate element type mismatch: cannot store %s into %s", v.Type(), dst)
}

// lowerAnonymousStructInstanceExpr builds `{ x: 1, y: "s" }`.
//
// The one thing it does that the named path does not have to: **fields are placed by
// name, in the recorded type's order**, because an anonymous struct's identity is its
// fields and a literal may write them in any order — `{ y: "s", x: 1 }` is the same
// value. The named path can index positionally because the declaration fixes the order
// for every literal of that type; here the *annotation* fixes it, and the literal is
// free to disagree.
func (l *lowerer) lowerAnonymousStructInstanceExpr(block *ir.Block, e *ast.AnonymousStructInstanceExpr) (value.Value, *ir.Block, error) {
	if e.BaseStruct != nil {
		return nil, nil, fmt.Errorf("llvm: anonymous struct record-update syntax not implemented yet")
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for anonymous struct")
	}
	structType, ok := recorded.(types.AnonymousStructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: anonymous struct lowering not implemented for %s", recorded)
	}
	stackType, ok := types.WithAllocation(structType, types.Stack).(types.AnonymousStructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: anonymous struct did not survive allocation stripping")
	}
	llType, err := l.lowerType(stackType)
	if err != nil {
		return nil, nil, err
	}
	structTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: anonymous struct %s did not lower to a struct", structType)
	}

	valueByName := make(map[string]ast.Expression, len(e.Fields))
	for _, f := range e.Fields {
		valueByName[f.Name] = f.Value
	}

	var agg value.Value = constant.NewUndef(structTy)
	for i, declField := range structType.Fields {
		valExpr, ok := valueByName[declField.Name]
		if !ok {
			return nil, nil, fmt.Errorf("llvm: anonymous struct field %q has no value", declField.Name)
		}
		var v value.Value
		v, block, err = l.lowerExpr(block, valExpr)
		if err != nil {
			return nil, nil, err
		}
		if diverged(v, block) {
			return nil, block, nil
		}
		v, err = l.coerceAggregateElem(block, v, structTy.Fields[i], valExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, v, uint64(i))
	}
	if types.AllocationOf(recorded) == types.Shared {
		boxed, err := l.lowerBoxShared(block, agg, stackType)
		return boxed, block, err
	}
	return agg, block, nil
}
