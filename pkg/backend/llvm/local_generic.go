package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
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
//   - **the call**: the slot for the instantiation this call solved to;
//   - **a lambda calling one** captures those slots — one closure value per instantiation
//     it calls (localGenericCaptures) — so the generic's own captures are read as they were
//     at its declaration, as with any closure over a closure. A captureless one is not
//     captured at all; its closure is built at the call.
//
// **Inside a generic function** an instantiation also binds the enclosing function's
// variables — the typechecker records them as identity bindings and composition settles
// them per outer specialization (withEnclosingTypeVars) — so each outer specialization has
// its own set, and a declaration builds only the ones consistent with the substitution in
// force.

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
	return nil
}

// localGenericCaptures answers, for a capture named name in fn, whether that name is a local
// generic — and if so, the closure values fn must capture in its place: one per
// instantiation fn's body calls, under the name its declaration's slot has and the
// signature that instantiation gives it.
//
// **This is what keeps a capture a capture.** A lambda calling a local generic that captures
// a binding could instead re-capture that binding and rebuild the generic's closure at the
// call, but then a `var` would be read as it is when the lambda is created rather than as it
// was when the generic was declared — the same program meaning something else. Capturing the
// closure values the declaration built is what an ordinary closure over an ordinary closure
// does, and nesting composes: a lambda's unpacked captures are its locals, under the same
// names, for a lambda inside it to capture in turn (09/13).
func (l *lowerer) localGenericCaptures(fn *ast.LambdaExpr, name string) ([]captures.Capture, bool) {
	seen := map[string]bool{}
	var out []captures.Capture
	isGeneric := false
	ast.WalkExprChildren(fn, nil, func(e ast.Expression) bool {
		call, ok := e.(*ast.FunctionCallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Function.(*ast.IdentifierExpr)
		if !ok || ident.Name != name {
			return true
		}
		inst, ok := l.res.Instantiations.Get(call)
		if !ok || !l.isLocalGeneric(inst.Func) {
			return true
		}
		isGeneric = true
		inst = inst.Substituted(l.typeSubst, types.Substitute)
		slotName := localGenericSlotName(name, inst)
		if seen[slotName] {
			return true
		}
		seen[slotName] = true
		out = append(out, captures.Capture{
			Name: slotName,
			Type: substituteTypeVars(localGenericSignature(inst.Func), inst.Subst),
		})
		return true
	})
	return out, isGeneric
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
		if !l.agreesWithSubst(inst) {
			continue // another specialization of the enclosing generic function's
		}
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

// agreesWithSubst reports whether inst binds every variable the substitution in force binds,
// to the same type — whether it belongs to the specialization being lowered.
func (l *lowerer) agreesWithSubst(inst typetable.Instantiation) bool {
	for name, bound := range l.typeSubst {
		if own, ok := inst.Subst[name]; ok && !types.TypesEqual(own, bound) {
			return false
		}
	}
	return true
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
		return nil, nil, fmt.Errorf("llvm: no closure for the local generic %q at %s — neither declared in this scope nor captured", name, e.GetLocation().Pretty())
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
