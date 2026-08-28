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

// specializations returns every instantiation to emit, in a stable order so the
// emitted module is deterministic.
//
// **Concrete ones only.** The table also holds *templates* — a generic call made from
// inside a generic body, whose bindings are the enclosing body's own type variables
// (`unwrap<t>` calling `expect` records `expect<t=t>`). Those are not specializations and
// have no representation; emitting one is what produced "type variable t has no concrete
// type here". The driver has already composed each template against every specialization
// that reaches it and added the results here, so the concrete set is complete.
func (l *lowerer) specializations() []typetable.Instantiation {
	return l.res.Instantiations.Concrete()
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
	defer l.pushSpecSite(inst.Site)()
	// …and the module, so a named type in that signature resolves under the key it was
	// registered with. Every other function-lowering path does this (lowerFunction,
	// defineFunction, the entry point); the specialization path did not, so
	// `l.currentLoc` was whatever the previous item left behind and a **private**
	// module-scoped type argument was looked up under its bare name. It is keyed
	// `<module>::<name>` (rule 4), so the lookup missed and the build failed with
	// `unknown named type`. A `pub` type resolved from anywhere and worked, which is what
	// made the bug look like it was about generics rather than about visibility.
	defer l.enterModuleOf(inst.Func.GetLocation())()

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
	// A lambda written inside this body is lifted to a function of its own, and its
	// signature mentions the type variables this instantiation binds — so it is declared
	// **here**, under the substitution, once per specialization. The program-wide pass
	// deliberately skips a generic body for exactly this reason (collectNestedLambdas).
	defer l.pushSpecKey(inst.Key())()
	for _, lam := range nestedLambdasIn(inst.Func) {
		if err := l.declareClosure(lam); err != nil {
			return err
		}
	}
	return nil
}

// defineSpecializations lowers each specialization's body with its substitution
// installed. Bodies come last, after every non-generic function, so a
// specialization may call anything.
func (l *lowerer) defineSpecializations() error {
	for _, inst := range l.specializations() {
		restoreSubst := l.pushTypeSubst(inst.Subst)
		restoreSite := l.pushSpecSite(inst.Site)
		restoreOwn := l.pushSpecOwnership(inst.Key())
		err := l.defineSpecialization(inst)
		restoreOwn()
		restoreSite()
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
	// Installed for the body as well as for the closures below it: the **creation site**
	// inside the body resolves the lifted function through the same closureKeyFor, so
	// the two agree only while this specialization is named.
	defer l.pushSpecKey(inst.Key())()
	if err := l.defineFunctionInto(irFn, inst.Func, inst.Name); err != nil {
		return err
	}
	// The bodies of this specialization's own closures, after the body that creates
	// them — never during it, which is the re-entrancy collectNestedLambdas' comment
	// explains. They see this instantiation's substitution *and* its ownership table,
	// both of which are installed by the caller: a closure over a `t = string` is
	// reference-counted where the same node at `t = i64` is not.
	for _, lam := range nestedLambdasIn(inst.Func) {
		if err := l.defineClosure(lam); err != nil {
			return err
		}
	}
	return nil
}

// pushSpecKey installs the name of the instantiation being lowered, and is what
// closureKeyFor reads. It travels with pushTypeSubst rather than standing alone — the
// key is meaningless without the substitution it names — but is pushed separately
// because declareSpecialization needs it only after the signature is emitted.
func (l *lowerer) pushSpecKey(key string) func() {
	prev := l.specKey
	l.specKey = key
	return func() { l.specKey = prev }
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

// pushSpecSite installs the location a specialization was requested from, alongside its
// substitution. The two belong together: the substitution supplies the concrete types and
// this says which module their *names* were resolved in.
func (l *lowerer) pushSpecSite(site ast.Location) func() {
	prev := l.specSite
	l.specSite = site
	return func() { l.specSite = prev }
}

// pushSpecOwnership installs the ownership table computed for one instantiation,
// alongside its type substitution — the two always travel together, since the table
// was computed *under* that substitution.
func (l *lowerer) pushSpecOwnership(key string) func() {
	prev := l.specOwnership
	l.specOwnership = l.res.OwnershipBySpec[key]
	return func() { l.specOwnership = prev }
}

// pushMethodOwnership is the same for a trait-impl method body, whose table is keyed by
// the resolution's SpecKey rather than by an instantiation's.
//
// A missing entry installs nil, which falls back to the program-wide table — the behaviour
// every method body had before there were per-method tables at all. That is the right
// fallback: the program-wide table simply has no annotations for these nodes, so the body
// lowers without retains or releases rather than with someone else's.
func (l *lowerer) pushMethodOwnership(specKey string) func() {
	prev := l.specOwnership
	l.specOwnership = l.res.OwnershipByMethod[specKey]
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

// substituteTypeVars is `types.Substitute`, kept as a name this package's call sites
// already use. The walk itself lives in pkg/types because the driver needs the same one
// to compose a caller's bindings into a callee's before the specialization set is closed —
// and a switch over composite types that exists twice drifts (CLAUDE.md hazard 8; the
// move turned up `*LambdaType`, which this copy never handled, so a generic combinator
// taking a callback kept a type variable in its signature).
func substituteTypeVars(t types.Type, subst map[string]types.Type) types.Type {
	return types.Substitute(t, subst)
}

// specializedFuncFor returns the emitted function a generic call resolves to, and
// false when the call is not generic.
//
// **The active substitution is composed in first.** A call sitting inside a generic body
// recorded its callee's bindings in terms of *that body's* type variables, so the key it
// names is a template rather than a specialization: lowering `unwrap<t=i64>`, the call to
// `expect` is recorded as `expect<t=t>` and only becomes `expect<t=i64>` once this
// specialization's own bindings are applied. Outside a generic body `l.typeSubst` is
// empty and composition is the identity, so an ordinary call resolves exactly as before.
//
// The composed specialization is guaranteed to have been declared: the driver closed the
// instantiation set under this same composition before ownership ran, so anything
// reachable this way is in `l.specialized`. A miss therefore means the two disagreed, and
// returning false here would report it as "unknown function" — the loud error below is a
// better failure, and hazard 5 is the reason it is an error at all rather than a guess.
func (l *lowerer) specializedFuncFor(call *ast.FunctionCallExpr) (*ir.Func, []ast.Parameter, bool) {
	inst, ok := l.res.Instantiations.Get(call)
	if !ok {
		return nil, nil, false
	}
	key := inst.Substituted(l.typeSubst, types.Substitute).Key()
	fn := l.specialized[key]
	if fn == nil {
		return nil, nil, false
	}
	return fn, l.specializedParams[key], true
}
