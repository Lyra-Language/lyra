package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Monomorphization: one emitted function per distinct instantiation of a generic
// one. A type variable has no representation, so `identity` cannot be compiled —
// only `identity` with `t = i64` can.
//
// **By substitution, not by cloning the AST.** The typechecker recorded, per call
// site, the bindings it solved (typetable.InstantiationTable); this pass lowers the
// *same shared body* once per distinct binding set with a substitution installed on
// the lowerer. Both places a type enters codegen already funnel through one
// accessor each — `lowerType` for a type written in the source and `recordedType`
// for one read off the TypeTable — so substituting there is enough to make the
// whole body concrete, including the widths of its locals and the signedness of its
// arithmetic.
//
// Cloning the AST per instantiation was the alternative, and it is what a naive
// monomorphizer does. It would mean deep-copying every node and then either
// re-running the typechecker on each copy or hand-patching a parallel TypeTable —
// far more machinery, and two ways for a specialization to disagree with the
// generic body it came from. Substitution keeps exactly one body.
//
// The trade-off worth naming: because the body is checked once, generically, a
// diagnostic that depends on the *instantiated* types (an overflow that only
// happens at u8) is not reported per specialization. That is the same bargain
// C++ templates make in reverse, and it is fine here because every check that
// matters for soundness — arity, assignability, the borrow rules — is either
// generic or performed at the call site against the substituted signature.

// specializations returns every distinct instantiation the program uses, in a
// stable order so the emitted module is deterministic.
func (l *lowerer) specializations() []typetable.Instantiation {
	return l.res.Instantiations.All()
}

// declareSpecializations emits the signature of every specialization before any
// body, so a call resolves to one that already exists — the same two-pass shape
// named and lifted functions use.
func (l *lowerer) declareSpecializations() error {
	for _, inst := range l.specializations() {
		if err := l.declareSpecialization(inst); err != nil {
			return err
		}
	}
	return nil
}

func (l *lowerer) declareSpecialization(inst typetable.Instantiation) error {
	if len(inst.Func.LambdaClauses) > 0 {
		return fmt.Errorf("llvm: a multi-clause generic function is not implemented yet (%q)", inst.Name)
	}
	if inst.Func.ReturnType.Type == nil {
		return fmt.Errorf("llvm: generic function %q needs a return type annotation", inst.Name)
	}
	// A **managed** type argument is refused, and this is the substitution approach's
	// real limit rather than an oversight.
	//
	// The ownership pass analyzes the generic body *once*, where every type variable
	// is a `GenericType` — which is not managed, so no retain, release, or drop is
	// recorded anywhere in it. At a managed instantiation those decisions are simply
	// wrong: `pick(a: t, b: t) -> t` at `t = string` returns a reference nobody
	// retained and leaves the caller's temporaries to be released twice (measured: an
	// ASan abort, 2 allocations against 3 releases). Substituting types cannot fix
	// that, because the ownership *decisions* were already made generically.
	//
	// The fix is to run the ownership pass per instantiation, which means teaching it
	// to take a substitution and produce a table per specialization — a real change to
	// its plumbing, and the natural next slice. Until then this is a loud error: a
	// generic function over scalars is genuinely useful and completely sound, and
	// silently miscompiling the managed case to reach it is not a trade worth making.
	for name, arg := range inst.Subst {
		if ownership.OwnsManaged(arg, l.res.SymbolTable) {
			return fmt.Errorf("llvm: instantiating generic function %q at a reference-counted type "+
				"(%s = %s) is not implemented yet — the ownership analysis runs on the generic body, "+
				"so retains and releases for it were never computed", inst.Name, name, arg)
		}
	}
	// The substitution is active while the signature is lowered, so a type variable
	// in a parameter or return position becomes its concrete binding.
	restore := l.pushTypeSubst(inst.Subst)
	defer restore()

	retType, err := l.lowerType(inst.Func.ReturnType.Type)
	if err != nil {
		return err
	}
	irParams := make([]*ir.Param, 0, len(inst.Func.Parameters))
	for _, param := range inst.Func.Parameters {
		if param.DefaultValue != nil {
			return fmt.Errorf("llvm: default parameter values are not implemented yet (%q)", inst.Name)
		}
		irParam, err := l.lowerParameter(param)
		if err != nil {
			return err
		}
		irParams = append(irParams, irParam)
	}
	symbol := inst.Symbol()
	l.specialized[inst.Key()] = l.module.NewFunc(symbol, retType, irParams...)
	l.specializedParams[inst.Key()] = inst.Func.Parameters
	return nil
}

// defineSpecializations lowers each specialization's body with its substitution
// installed. Bodies come last, after every non-generic function, so a
// specialization may call anything.
func (l *lowerer) defineSpecializations() error {
	for _, inst := range l.specializations() {
		restore := l.pushTypeSubst(inst.Subst)
		err := l.defineSpecialization(inst)
		restore()
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *lowerer) defineSpecialization(inst typetable.Instantiation) error {
	irFn := l.specialized[inst.Key()]
	if irFn == nil {
		return fmt.Errorf("llvm: specialization %s was not declared", inst.Key())
	}
	return l.defineFunctionInto(irFn, inst.Func, inst.Name)
}

// pushTypeSubst installs a type-variable substitution for the duration of lowering
// one specialization, returning the restore. Nested substitutions are not composed
// — a specialization's body is lowered with exactly its own bindings — so a generic
// function calling *another* generic function at a variable-dependent instantiation
// is rejected rather than silently mis-specialized (see substituteCallee).
func (l *lowerer) pushTypeSubst(subst map[string]types.Type) func() {
	prev := l.typeSubst
	l.typeSubst = subst
	return func() { l.typeSubst = prev }
}

// applyTypeSubst replaces type variables in t with their concrete bindings. It is
// called from the two accessors every lowering decision already goes through, so a
// specialization's body sees concrete types everywhere without any node being
// rewritten.
func (l *lowerer) applyTypeSubst(t types.Type) types.Type {
	if len(l.typeSubst) == 0 || t == nil {
		return t
	}
	return substituteTypeVars(t, l.typeSubst)
}

// substituteTypeVars is the structural substitution: a GenericType leaf becomes its
// binding, and every composite type a signature or body can mention is rebuilt with
// its parts substituted.
func substituteTypeVars(t types.Type, subst map[string]types.Type) types.Type {
	switch tt := t.(type) {
	case types.GenericType:
		if concrete, ok := subst[tt.Name]; ok {
			return concrete
		}
		return tt
	case types.StaticArrayType:
		tt.ElementType = substituteTypeVars(tt.ElementType, subst)
		return tt
	case types.DynamicArrayType:
		tt.ElementType = substituteTypeVars(tt.ElementType, subst)
		return tt
	case types.TupleType:
		elems := make([]types.Type, len(tt.Elements))
		for i, e := range tt.Elements {
			elems[i] = substituteTypeVars(e, subst)
		}
		tt.Elements = elems
		return tt
	case types.WeakType:
		tt.Inner = substituteTypeVars(tt.Inner, subst)
		return tt
	}
	return t
}

// specializedFuncFor returns the emitted function a generic call resolves to, and
// false when the call is not generic.
func (l *lowerer) specializedFuncFor(call *ast.FunctionCallExpr) (*ir.Func, []ast.Parameter, bool) {
	inst, ok := l.res.Instantiations.Get(call)
	if !ok {
		return nil, nil, false
	}
	fn := l.specialized[inst.Key()]
	if fn == nil {
		return nil, nil, false
	}
	return fn, l.specializedParams[inst.Key()], true
}
