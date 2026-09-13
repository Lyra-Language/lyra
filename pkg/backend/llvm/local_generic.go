package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// A **generic declared inside a function** — `let idf<t> = (a: t) -> t => a` in `main`.
//
// It is both things this backend already knows how to lower, and neither on its own. Like a
// top-level generic it has no single representation, so it is emitted once per
// instantiation the typechecker recorded; like any local lambda it may capture the
// enclosing function's bindings, so each of those is a *closure* — a lifted function and an
// environment — rather than a plain function. Until 09/13 it was lowered as the second
// alone, at its unsolved type variables, and every program declaring one failed with *"type
// variable t has no concrete type here"* after checking clean.
//
// So the two arrangements compose:
//
//   - **lifting**: one closure per (lambda, instantiation), keyed by the instantiation's
//     key and lowered under its substitution and its ownership table — the same closureKey
//     a lambda inside a generic body is lifted under;
//   - **the declaration**: one closure value per instantiation, built where the `let`
//     stands, in a slot of its own and framed like any closure binding;
//   - **the call**: the slot for the instantiation this call solved to.
//
// A local generic inside a *generic* function is refused by name: its body may mention the
// enclosing function's variables as well as its own, and an instantiation carries only its
// own.

// localGenericSlotName is the l.locals key of one instantiation's closure value. `<` cannot
// appear in a Lyra identifier, so it cannot collide with a binding.
func localGenericSlotName(name string, inst typetable.Instantiation) string {
	return name + "<" + inst.Key()
}

// collectLocalGenerics finds every local generic and the instantiations to emit for it, and
// marks the lambdas that must not be lifted generically — the local generics themselves and
// everything nested inside them.
func (l *lowerer) collectLocalGenerics(program *ast.Program, entry *ast.LambdaExpr) error {
	l.localGenericInsts = map[*ast.LambdaExpr][]typetable.Instantiation{}
	l.localGenericExcluded = map[*ast.LambdaExpr]bool{}
	topLevel := map[*ast.LambdaExpr]bool{entry: true}
	for _, node := range program.Statements {
		if decl, ok := node.(*ast.VarDeclStmt); ok {
			if lam, ok := decl.Value.(*ast.LambdaExpr); ok {
				topLevel[lam] = true
			}
		}
	}
	for _, node := range program.Statements {
		decl, ok := node.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		owner, ok := decl.Value.(*ast.LambdaExpr)
		if !ok {
			continue
		}
		for _, lam := range nestedLambdasIn(owner) {
			if len(lam.GenericParams) == 0 {
				continue
			}
			if isGenericLambda(owner) {
				return fmt.Errorf("llvm: a generic declared inside the generic function %q is not implemented yet (at %s) — declare it at the top level",
					decl.Name, lam.GetLocation().Pretty())
			}
			l.localGenericExcluded[lam] = true
			for _, inner := range nestedLambdasIn(lam) {
				l.localGenericExcluded[inner] = true
			}
		}
	}
	for _, inst := range l.res.Instantiations.Concrete() {
		if !topLevel[inst.Func] && l.localGenericExcluded[inst.Func] {
			l.localGenericInsts[inst.Func] = append(l.localGenericInsts[inst.Func], inst)
		}
	}
	return l.refuseCapturedLocalGenerics(program)
}

// refuseCapturedLocalGenerics names the one shape this does not lower: a lambda calling a
// local generic that **captures** something. The generic has no single closure value to
// capture, and re-capturing its captures at the call would read a `var` as it is at the
// call rather than as it was at the declaration — a change of meaning, so it is refused
// instead. A captureless one is not a capture at all (captures.go).
func (l *lowerer) refuseCapturedLocalGenerics(program *ast.Program) error {
	for _, node := range program.Statements {
		decl, ok := node.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		owner, ok := decl.Value.(*ast.LambdaExpr)
		if !ok {
			continue
		}
		generics := map[string]bool{}
		for _, lam := range nestedLambdasIn(owner) {
			if l.isLocalGeneric(lam) {
				for _, name := range localGenericNames(owner, lam) {
					generics[name] = true
				}
			}
		}
		if len(generics) == 0 {
			continue
		}
		for _, lam := range append(nestedLambdasIn(owner), owner) {
			for _, c := range l.res.Captures.Of(lam) {
				if generics[c.Name] {
					return fmt.Errorf("llvm: %q is a generic declared inside %q that captures a binding, and the lambda at %s calls it; that is not implemented yet — pass what it captures as an argument, or declare it at the top level",
						c.Name, decl.Name, lam.GetLocation().Pretty())
				}
			}
		}
	}
	return nil
}

// localGenericNames is the name(s) owner's body binds lam to.
func localGenericNames(owner, lam *ast.LambdaExpr) []string {
	var names []string
	ast.WalkExprChildren(owner, func(s ast.Statement) bool {
		if decl, ok := s.(*ast.VarDeclStmt); ok && decl.Value == ast.Expression(lam) {
			names = append(names, decl.Name)
		}
		return true
	}, nil)
	return names
}

// isLocalGeneric reports whether fn is a generic declared inside a function.
func (l *lowerer) isLocalGeneric(fn *ast.LambdaExpr) bool {
	return len(fn.GenericParams) > 0 && l.localGenericExcluded[fn]
}

// withLocalInstantiation runs f with one instantiation's substitution, site and key
// installed — and its ownership table when own is set, for a body.
func (l *lowerer) withLocalInstantiation(inst typetable.Instantiation, own bool, f func() error) error {
	defer l.pushTypeSubst(inst.Subst)()
	defer l.pushSpecSite(inst.Site)()
	defer l.pushSpecKey(inst.Key())()
	if own {
		defer l.pushSpecOwnership(inst.Key())()
	}
	return f()
}

// declareLocalGenerics declares one lifted closure per instantiation, and the lambdas
// nested inside it under the same key.
func (l *lowerer) declareLocalGenerics() error {
	for _, insts := range l.localGenericInstsSorted() {
		for _, inst := range insts {
			err := l.withLocalInstantiation(inst, false, func() error {
				if err := l.declareClosure(inst.Func); err != nil {
					return err
				}
				for _, inner := range nestedLambdasIn(inst.Func) {
					if err := l.declareClosure(inner); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// defineLocalGenerics lowers those closures' bodies.
func (l *lowerer) defineLocalGenerics() error {
	for _, insts := range l.localGenericInstsSorted() {
		for _, inst := range insts {
			err := l.withLocalInstantiation(inst, true, func() error {
				if err := l.defineClosure(inst.Func); err != nil {
					return err
				}
				for _, inner := range nestedLambdasIn(inst.Func) {
					if err := l.defineClosure(inner); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// localGenericInstsSorted is the instantiation lists in source order of their lambdas, so
// the emitted module does not depend on map iteration.
func (l *lowerer) localGenericInstsSorted() [][]typetable.Instantiation {
	var lams []*ast.LambdaExpr
	for lam := range l.localGenericInsts {
		lams = append(lams, lam)
	}
	sortLambdasBySource(lams)
	out := make([][]typetable.Instantiation, 0, len(lams))
	for _, lam := range lams {
		out = append(out, l.localGenericInsts[lam])
	}
	return out
}

func sortLambdasBySource(lams []*ast.LambdaExpr) {
	less := func(a, b ast.Location) bool {
		if a.File != b.File {
			return a.File < b.File
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartCol < b.StartCol
	}
	for i := 1; i < len(lams); i++ {
		for j := i; j > 0 && less(lams[j].GetLocation(), lams[j-1].GetLocation()); j-- {
			lams[j], lams[j-1] = lams[j-1], lams[j]
		}
	}
}

// lowerLocalGenericDecl lowers `let name<t> = lambda` inside a function: one closure value
// per instantiation, each in its own framed slot. A local generic nothing calls emits
// nothing, as an unused top-level generic does.
func (l *lowerer) lowerLocalGenericDecl(block *ir.Block, vds *ast.VarDeclStmt, lam *ast.LambdaExpr) (*ir.Block, error) {
	entry := block.Parent.Blocks[0]
	for _, inst := range l.localGenericInsts[lam] {
		var v value.Value
		var ty types.Type
		err := l.withLocalInstantiation(inst, false, func() error {
			var err error
			v, block, err = l.lowerLambdaExpr(block, lam)
			if err != nil {
				return err
			}
			ty, _ = l.recordedType(lam)
			return nil
		})
		if err != nil {
			return nil, err
		}
		slot := entry.NewAlloca(v.Type())
		block.NewStore(v, slot)
		l.locals[localGenericSlotName(vds.Name, inst)] = slot
		if l.needsDrop(ty) {
			l.addManagedBinding(slot, ty)
		}
	}
	return block, nil
}

// lowerLocalGenericCall calls the closure value of the instantiation this call solved to,
// through its instantiated signature.
func (l *lowerer) lowerLocalGenericCall(block *ir.Block, e *ast.FunctionCallExpr, name string, inst typetable.Instantiation) (value.Value, *ir.Block, error) {
	var callee value.Value
	if slot, ok := l.locals[localGenericSlotName(name, inst)]; ok {
		elem, err := slotElemType(slot)
		if err != nil {
			return nil, nil, err
		}
		callee = block.NewLoad(elem, slot)
	} else if len(l.res.Captures.Of(inst.Func)) == 0 {
		// Called from inside another lambda, which did not capture it (captures.go's
		// outerGenerics): with nothing to capture, its closure is the lifted function and
		// the pinned empty environment, and can be built right here.
		err := l.withLocalInstantiation(inst, false, func() error {
			var err error
			callee, block, err = l.lowerLambdaExpr(block, inst.Func)
			return err
		})
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("llvm: the local generic %q is not in scope at %s", name, e.GetLocation().Pretty())
	}
	lt, ok := substituteTypeVars(localGenericSignature(inst.Func), inst.Subst).(*types.LambdaType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: the local generic %q has no function signature", name)
	}
	return l.lowerIndirectCallOn(block, e, callee, lt)
}

// localGenericSignature is the declared signature of a local generic, as a function type.
func localGenericSignature(fn *ast.LambdaExpr) *types.LambdaType {
	params := make([]types.ParameterType, 0, len(fn.Parameters))
	for _, p := range fn.Parameters {
		params = append(params, types.ParameterType{Type: p.Type})
	}
	return &types.LambdaType{Parameters: params, ReturnType: fn.ReturnType}
}
