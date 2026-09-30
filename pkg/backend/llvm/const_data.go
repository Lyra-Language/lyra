package llvm

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
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
// leaves lower to constants with no instructions (numbers, runes, booleans), nested
// arrays of them included. Anything else keeps the inlining it had.

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
	// nothing — a literal does not, and anything that does is not static data.
	scratch := ir.NewBlock("")
	value, _, err := l.lowerExpr(scratch, expr)
	if err != nil || len(scratch.Insts) != 0 {
		return nil, false
	}
	c, ok := value.(constant.Constant)
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
