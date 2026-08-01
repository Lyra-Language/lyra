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
	// A managed type argument works: the ownership pass was analyzed once *per
	// instantiation* (driver.OwnershipBySpec), so this specialization's retains,
	// releases, and drops were computed at the concrete type rather than at the type
	// variable. Reading the program-wide table here instead is what made `t = string`
	// a double free — see the `ownership()` accessor.
	// The substitution is active while the signature is lowered, so a type variable
	// in a parameter or return position becomes its concrete binding.
	restore := l.pushTypeSubst(inst.Subst)
	defer restore()

	retType, err := l.lowerType(inst.Func.ReturnType.Type)
	if err != nil {
		return err
	}
	// A defaulted parameter is an ordinary parameter in a specialization too: the
	// typechecker fills every omitted argument before the call is solved, so the default
	// participates in inference like any other argument and this signature is complete.
	irParams := make([]*ir.Param, 0, len(inst.Func.Parameters))
	for i, param := range inst.Func.Parameters {
		irParam, err := l.lowerParameter(param, i)
		if err != nil {
			return err
		}
		irParams = append(irParams, irParam)
	}
	// Prefixed like any other user function (userSymbol): a specialization is still
	// user code, and its symbol has to be unique across modules and safe against libc.
	symbol := "lyra." + inst.Symbol()
	l.specialized[inst.Key()] = l.module.NewFunc(symbol, retType, irParams...)
	l.specializedParams[inst.Key()] = inst.Func.Parameters
	return nil
}

// defineSpecializations lowers each specialization's body with its substitution
// installed. Bodies come last, after every non-generic function, so a
// specialization may call anything.
func (l *lowerer) defineSpecializations() error {
	for _, inst := range l.specializations() {
		restoreSubst := l.pushTypeSubst(inst.Subst)
		restoreOwn := l.pushSpecOwnership(inst.Key())
		err := l.defineSpecialization(inst)
		restoreOwn()
		restoreSubst()
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

// ownership returns the ownership table for the code currently being lowered: the
// program-wide one normally, and the *specialization's own* table inside a generic
// body.
//
// They cannot be one table. Every decision the ownership pass makes turns on
// whether a value is reference-counted, which is a property of the type argument —
// so a generic body is analyzed once per instantiation, and the same AST node
// carries different annotations in each. Reading the program-wide table inside a
// specialization is exactly the bug this replaced: analyzed generically, a type
// variable is not managed, so `pick(a: t, b: t) -> t` recorded no retain on its
// result and no release for the caller's temporaries — correct at `t = i64`, a
// double free at `t = string`.
func (l *lowerer) ownership() *ownership.Table {
	if l.specOwnership != nil {
		return l.specOwnership
	}
	return l.res.Ownership
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

// pushSpecOwnership installs the ownership table computed for one instantiation,
// alongside its type substitution — the two always travel together, since the table
// was computed *under* that substitution.
func (l *lowerer) pushSpecOwnership(key string) func() {
	prev := l.specOwnership
	l.specOwnership = l.res.OwnershipBySpec[key]
	return func() { l.specOwnership = prev }
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
	case types.ParameterizedType:
		// `Box<t>` inside a generic body, and the nested arguments of `Box<Box<i64>>`.
		// Substituting these is what makes one instantiation's identity concrete —
		// without it `Box<t>` at two different bindings mangles to the same name and
		// the two would share a layout.
		args := make([]types.Type, len(tt.TypeArguments))
		for i, a := range tt.TypeArguments {
			args[i] = substituteTypeVars(a, subst)
		}
		tt.TypeArguments = args
		return tt
	case types.NamedStructType:
		// Substituting a *declaration's* contents is how an instantiation's layout is
		// built (generic_types.go): `struct Box<t> { value: t }` at `t = i64` is a
		// struct whose field is i64. Fields are copied rather than written in place —
		// the declaration is shared by every instantiation, so mutating it would let
		// the first one lowered decide the rest.
		fields := make([]types.StructField, len(tt.Fields))
		copy(fields, tt.Fields)
		for i := range fields {
			fields[i].Type = substituteTypeVars(fields[i].Type, subst)
		}
		tt.Fields = fields
		return tt
	case types.DataType:
		ctors := make([]types.DataTypeConstructor, len(tt.Constructors))
		copy(ctors, tt.Constructors)
		for i := range ctors {
			params := make([]types.Type, len(ctors[i].Params))
			for j, p := range ctors[i].Params {
				params[j] = substituteTypeVars(p, subst)
			}
			ctors[i].Params = params
		}
		tt.Constructors = ctors
		return tt
	case types.AnonymousStructType:
		fields := make([]types.StructField, len(tt.Fields))
		copy(fields, tt.Fields)
		for i := range fields {
			fields[i].Type = substituteTypeVars(fields[i].Type, subst)
		}
		tt.Fields = fields
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
