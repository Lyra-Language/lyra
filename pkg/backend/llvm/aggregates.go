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
	recorded, ok := l.res.TypeTable.Get(e)
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
	recorded, ok := l.res.TypeTable.Get(e)
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
	objType, ok := l.res.TypeTable.Get(e.Object)
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
	// A `shared` object is a pointer to its box `{ i64 rc, payload }`; read the field
	// through the box (getelementptr box → payload → field, then load) rather than
	// extractvalue on an inline struct.
	if ptr, ok := obj.Type().(*lltypes.PointerType); ok {
		boxTy, ok := ptr.ElemType.(*lltypes.StructType)
		if !ok || len(boxTy.Fields) != 2 {
			return nil, nil, fmt.Errorf("llvm: member access on non-box pointer %s", obj.Type())
		}
		payloadTy, ok := boxTy.Fields[1].(*lltypes.StructType)
		if !ok || idx >= len(payloadTy.Fields) {
			return nil, nil, fmt.Errorf("llvm: `shared` field access on non-struct payload %s", boxTy.Fields[1])
		}
		fieldPtr := block.NewGetElementPtr(boxTy, obj,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1), constant.NewInt(lltypes.I32, int64(idx)))
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
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[s.Name]; ok {
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
	recorded, ok := l.res.TypeTable.Get(e)
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

	// Build the inline tagged-union value first (stripping any `shared` flavor,
	// which would lower to a box pointer); it's boxed at the end if shared.
	stackDt := types.WithAllocation(dt, types.Stack).(types.DataType)
	llType, err := l.lowerType(stackDt)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data type %q did not lower to a struct", dt.Name)
	}

	// Alloca the union in the entry block (mem2reg-promotable), then fill it.
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(unionTy)

	// Store the tag (field 0).
	tagTy := unionTy.Fields[0].(*lltypes.IntType)
	tagPtr := block.NewGetElementPtr(unionTy, slot,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0))
	block.NewStore(constant.NewInt(tagTy, int64(tag)), tagPtr)

	// Store the payload (field 1, the blob) reinterpreted as this variant's
	// payload struct — only when the variant carries fields (a nullary variant of
	// a type that *has* payloads just leaves the blob undefined).
	if len(fields) > 0 {
		payloadStructTy, err := l.dataPayloadStructType(ctor)
		if err != nil {
			return nil, nil, err
		}
		var payload value.Value = constant.NewUndef(payloadStructTy)
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
			payload = block.NewInsertValue(payload, v, uint64(i))
		}
		blobPtr := block.NewGetElementPtr(unionTy, slot,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
		typedPtr := block.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
		block.NewStore(payload, typedPtr)
	}

	union := block.NewLoad(unionTy, slot)
	// A `shared` data value is heap-allocated in a ref-counted box; the value is the
	// box pointer. (This is how a recursive `shared` payload field — a `Cons`'s
	// `shared List` — is filled: the nested constructor is boxed here.)
	if types.AllocationOf(dt) == types.Shared {
		// Perceus reuse: if this construction is the reuse target of an enclosing
		// reuse-match and that match's token is still live, write into the reclaimed
		// box instead of allocating. Consuming the token clears it so a later
		// construction (or the arm's fall-through free) doesn't touch it again.
		if l.reuseToken != nil && l.res.Ownership.IsReuseTarget(srcExpr) {
			token := l.reuseToken
			l.reuseToken = nil
			return l.lowerBoxSharedReuse(block, union, stackDt, token)
		}
		boxed, err := l.lowerBoxShared(block, union, stackDt)
		return boxed, block, err
	}
	return union, block, nil
}

// dataPayloadStructType is the LLVM struct of a variant's payload fields, in
// order — what gets stored into (and later read from) the union's payload blob.
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
