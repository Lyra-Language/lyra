package llvm

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// **A `const` fixed array of literals is data, not code** (09/30). A `const` is otherwise
// inlined at each use — right for a number, and for a small table the optimizer folds —
// but a table of tile or palette words inlined is an aggregate built on the stack by one
// store per element at every use: 512 words took 4.8 KB of code and 2 KB of stack, and a
// Genesis program has 4 KB of stack for everything. So such a const is a constant global
// instead, which lands in `.rodata` (ROM on a console), is read in place, and has an
// address — `&TILES[i]` hands a pointer into it to code that uploads it.
//
// What qualifies is exactly what can be a constant initializer: `#[…]` and `#[v; n]` whose
// leaves lower to constants (numbers, runes, booleans), nested arrays of them included —
// and **structs and tuples of them**, whose lowering is a chain of `insertvalue`s on
// constants that `foldInsertValues` folds into one (09/30: a struct table was rebuilt on
// the stack at every read, which is also how Vega's animation steps met the M68k backend's
// dropped stack index). Anything else keeps the inlining it had.

// staticConstCandidate reports whether a const declaration could be static data: its
// recorded type is a fixed array and its value a fixed-array literal. Whether every leaf
// is a constant is only known once types can be lowered (staticConstant).
func (l *lowerer) staticConstCandidate(vd *ast.VarDeclStmt) bool {
	t, ok := l.recordedType(vd.Value)
	if !ok {
		return false
	}
	if _, isFixed := types.StripNewtype(t).(types.StaticArrayType); !isFixed {
		return false
	}
	switch v := vd.Value.(type) {
	case *ast.ArrayLiteralExpr:
		return v.Fixed
	case *ast.ArrayRepeatExpr:
		return v.Fixed
	}
	return false
}

// staticConstant is expr as an LLVM constant of type ty, or false when some leaf is not a
// constant.
func (l *lowerer) staticConstant(expr ast.Expression, ty lltypes.Type) (constant.Constant, bool) {
	switch v := expr.(type) {
	case *ast.ArrayLiteralExpr:
		at, ok := ty.(*lltypes.ArrayType)
		if !ok || uint64(len(v.Elements)) != at.Len {
			return nil, false
		}
		elems := make([]constant.Constant, len(v.Elements))
		for i, e := range v.Elements {
			c, ok := l.staticConstant(e, at.ElemType)
			if !ok {
				return nil, false
			}
			elems[i] = c
		}
		return constant.NewArray(at, elems...), true
	case *ast.ArrayRepeatExpr:
		at, ok := ty.(*lltypes.ArrayType)
		if !ok {
			return nil, false
		}
		c, ok := l.staticConstant(v.Value, at.ElemType)
		if !ok {
			return nil, false
		}
		elems := make([]constant.Constant, at.Len)
		for i := range elems {
			elems[i] = c
		}
		return constant.NewArray(at, elems...), true
	}
	// A leaf: lowered into a detached block, and a constant only if lowering emitted
	// nothing — a literal does not — or only `insertvalue`s that fold to one (a struct of
	// literals). Anything else is not static data.
	//
	// The block belongs to a throwaway function, never added to the module: lowering some
	// leaves takes a slot in its function's entry block — a data value is built through
	// memory (DATA_LAYOUT.md) — and a parentless block was a nil dereference there. Such a
	// leaf emits instructions that do not fold, so its table stays inlined.
	scratch := ir.NewFunc("", lltypes.Void).NewBlock("")
	lowered, _, err := l.lowerExpr(scratch, expr)
	if err != nil {
		return nil, false
	}
	c, ok := foldInsertValues(scratch, lowered)
	if !ok {
		return nil, false
	}
	// A literal reaches here at the width propagation left it; the element's is the one
	// that counts.
	if ci, isInt := c.(*constant.Int); isInt {
		if it, wantInt := ty.(*lltypes.IntType); wantInt && !ci.Typ.Equal(it) {
			return constant.NewInt(it, ci.X.Int64()), true
		}
	}
	if !c.Type().Equal(ty) {
		return nil, false
	}
	return c, true
}

// foldInsertValues is result as a constant when block holds nothing but `insertvalue`s
// whose aggregates and elements are constants (or earlier ones of these) — the chain a
// struct, tuple or data literal of literals lowers to. False for any other instruction.
func foldInsertValues(block *ir.Block, result value.Value) (constant.Constant, bool) {
	folded := map[value.Value]constant.Constant{}
	constOf := func(v value.Value) (constant.Constant, bool) {
		if c, ok := folded[v]; ok {
			return c, true
		}
		c, ok := v.(constant.Constant)
		return c, ok
	}
	for _, inst := range block.Insts {
		iv, ok := inst.(*ir.InstInsertValue)
		if !ok {
			return nil, false
		}
		base, ok := constOf(iv.X)
		if !ok {
			return nil, false
		}
		elem, ok := constOf(iv.Elem)
		if !ok {
			return nil, false
		}
		c, ok := insertConstant(base, elem, iv.Indices)
		if !ok {
			return nil, false
		}
		folded[iv] = c
	}
	return constOf(result)
}

// insertConstant is aggregate with the element at path replaced by elem.
func insertConstant(aggregate, elem constant.Constant, path []uint64) (constant.Constant, bool) {
	if len(path) == 0 {
		return elem, true
	}
	fields, ok := constantElements(aggregate)
	if !ok || path[0] >= uint64(len(fields)) {
		return nil, false
	}
	inner, ok := insertConstant(fields[path[0]], elem, path[1:])
	if !ok {
		return nil, false
	}
	fields[path[0]] = inner
	switch t := aggregate.Type().(type) {
	case *lltypes.StructType:
		return constant.NewStruct(t, fields...), true
	case *lltypes.ArrayType:
		return constant.NewArray(t, fields...), true
	}
	return nil, false
}

// constantElements is a fresh slice of a constant aggregate's elements — an undefined or
// zero one's are undefined or zero in turn.
func constantElements(c constant.Constant) ([]constant.Constant, bool) {
	var elemTypes []lltypes.Type
	switch t := c.Type().(type) {
	case *lltypes.StructType:
		elemTypes = t.Fields
	case *lltypes.ArrayType:
		for i := uint64(0); i < t.Len; i++ {
			elemTypes = append(elemTypes, t.ElemType)
		}
	default:
		return nil, false
	}
	out := make([]constant.Constant, len(elemTypes))
	switch v := c.(type) {
	case *constant.Struct:
		copy(out, v.Fields)
	case *constant.Array:
		copy(out, v.Elems)
	case *constant.Undef:
		for i, et := range elemTypes {
			out[i] = constant.NewUndef(et)
		}
	case *constant.ZeroInitializer:
		for i, et := range elemTypes {
			out[i] = constant.NewZeroInitializer(et)
		}
	default:
		return nil, false
	}
	return out, true
}

// declareStaticConsts gives each candidate const a private constant global, or returns it
// to the inlined consts when some leaf is not a constant.
func (l *lowerer) declareStaticConsts() error {
	for _, vd := range l.staticConstDecls {
		t, _ := l.recordedType(vd.Value)
		ty, err := l.lowerType(t)
		if err != nil {
			return err
		}
		init, ok := l.staticConstant(vd.Value, ty)
		if !ok {
			l.consts[l.funcKey(vd.Name, vd.GetLocation())] = vd
			continue
		}
		symbol := "lyra_const_" + vd.Name
		if module := l.res.SymbolTable.ModuleOfFile[vd.GetLocation().File]; module != "" {
			symbol = "lyra_const_" + module + "." + vd.Name
		}
		g := l.module.NewGlobalDef(symbol, init)
		g.Immutable = true
		// Private, but not unnamed_addr: `&TILES` makes the address observable, so two
		// tables with the same words must stay two tables.
		g.Linkage = enum.LinkagePrivate
		l.globals[l.funcKey(vd.Name, vd.GetLocation())] = g
	}
	return nil
}
