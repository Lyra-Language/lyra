package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Trait default methods: `trait G { name: (Self) -> string
//                                   shout: (Self) -> string = (self) => self.name() ++ "!" }`
//
// An impl that does not provide `shout` gets the trait's body; one that does overrides it.
//
// # Self is a type variable here, and that is the whole design
//
// A default body is written once and runs for every implementing type, which is the
// definition of a generic function — so it is checked once, with `self` typed as
// `types.GenericType{Name: "Self"}` bounded by the declaring trait, and monomorphized per
// implementing type exactly as `let f<t> where t: Show` is. Everything the feature needs
// already existed for that path and is reused rather than rebuilt:
//
//   - `self.name()` inside the body is a call on a value of type-variable type, which is
//     dispatchViaGenericBound — it records the abstract resolution and publishes one
//     concrete candidate per implementing type (SetBoundCandidates);
//   - the backend picks the candidate its specialization names, substituting Bindings
//     through recordedType, so the shared body lowers correctly per type;
//   - the ownership pass analyzes that body once per specialization, at those bindings.
//
// The alternative was to deep-copy the default clause into every impl that lacks it, so
// each got its own AST nodes. That needs a full expression/statement cloner the compiler
// does not have, and a missing case in it is a silently *shared* subtree — hazard 8 with
// a miscompile at the end of it. Type variables are the mechanism this compiler already
// has for "one body, many types".
//
// **The name `Self` cannot collide with a user's type variable.** A type variable is
// lowercase by lexer rule (an uppercase name is a concrete type), so no program can
// declare one called `Self`, and `Self` is a reserved word besides.
//
// # What is *not* checked here
//
// Whether the body satisfies the method's declared effect bound. A default is checked for
// purity like any other body, by the purity pass, against the bound the trait declares —
// see checker/purity.go's collectMethodImpls, which now gathers defaults too.

// selfVar is the type variable a default method's `Self` is checked as. Unforgeable by a
// user program, since a type variable is lowercase-leading.
const selfVar = "Self"

// checkTraitDefaultMethods type-checks every trait default-method body in the program,
// once each, with Self abstract.
//
// It runs after impls are collected — dispatchViaGenericBound publishes a candidate per
// implementing type, and a candidate set gathered before the impls were known would be
// empty, so a default calling another trait method would type-check and then fail to
// lower.
func (tc *TypeChecker) checkTraitDefaultMethods(program *ast.Program) {
	for _, stmt := range program.Statements {
		trait, ok := stmt.(*ast.TraitDeclStmt)
		if !ok {
			continue
		}
		for i := range trait.Methods {
			tm := &trait.Methods[i]
			if tm.DefaultMethod == nil || tm.Signature == nil {
				continue
			}
			tc.checkOneDefaultMethod(trait, tm)
		}
	}
}

// checkOneDefaultMethod checks a single default body against the trait's declared
// signature, with Self bound to the trait (and its supertraits).
func (tc *TypeChecker) checkOneDefaultMethod(trait *ast.TraitDeclStmt, tm *ast.TraitMethod) {
	self := types.GenericType{Name: selfVar}

	// The bound is what makes `self.other()` resolve at all, and it is closed over
	// supertraits by the same helper a `where` clause uses — a default body may call a
	// supertrait's method, and refusing that would make `trait B: A` mean less inside A's
	// own defaults than at any call site.
	oldBounds := tc.genericBounds
	tc.genericBounds = map[string][]string{}
	for k, v := range oldBounds {
		tc.genericBounds[k] = v
	}
	tc.genericBounds[selfVar] = tc.closeOverSupertraits([]string{trait.Name}, trait.GetLocation())
	defer func() { tc.genericBounds = oldBounds }()

	// The trait's own type parameters stay as they are written: a default body sees
	// `Get<e>`'s `e` as the variable it is, since there is no impl here to bind it to.
	prevTrait := tc.currentDefaultTrait
	tc.currentDefaultTrait = trait.Name
	defer func() { tc.currentDefaultTrait = prevTrait }()

	sig := substituteSelf(tm.Signature, self)

	// checkTraitImplMethodBody is the same work an impl's method needs — parameters bound
	// from the signature, the return type checked against the body, `?` resolving through
	// the enclosing return. Reusing it is what keeps a default and an override from being
	// checked by two rules that can disagree.
	// **In the trait's own module's scope**, which is the thing a setup pass does not get
	// for free. The per-statement loop wraps every top-level statement in `checkInModule`;
	// this pass runs before that loop, so `tc.scope` was the *global* scope — which holds
	// only what modules export. A bare reference to one of the module's own top-level
	// names therefore resolved to nothing, and the miss was silent: the "undefined
	// function" arm is guarded by a visibility check that answers "found but private" for
	// a name the global scope cannot see, so the call was abandoned with no diagnostic and
	// no recorded instantiation, and the build failed later as `call to unknown function`.
	//
	// A single-module program hid it completely — with no prelude the global scope holds
	// the program's own declarations, so every reproduction small enough to paste worked.
	// The same shape as hazard 13's module header, and the same lesson.
	if scope := tc.moduleScopeOf(trait); scope != nil {
		prevScope := tc.scope
		tc.scope = scope
		defer func() { tc.scope = prevScope }()
	}
	tc.checkTraitImplMethodBody(tm.Name.GetName(), *tm.DefaultImpl(), sig)
}

// publishDefaultBodyCandidates publishes, for every bound call *inside* a default body,
// the impl that call resolves to at this concrete receiver.
//
// It is publishImplBodyCandidates for a default: that one is driven by the impl's own
// `where` constraints, and a default has exactly one bound to walk — `Self`, at the trait
// that declares it. Without it the body type-checks (the bound is abstract) and then fails
// to lower, because the candidate table would hold only what boundCandidatesByType keys by
// the impl's *declared* target — `Box2<t>` — while the specialization asks about
// `Box2<i64>`.
//
// The trait name alone is passed; publishCandidatesAt closes it over supertraits, so a
// default calling a supertrait's method publishes too.
func (tc *TypeChecker) publishDefaultBodyCandidates(trait *ast.TraitDeclStmt, tm *ast.TraitMethod, concrete types.Type) {
	if tm.DefaultMethod == nil || tm.DefaultMethod.Body == nil || concrete == nil {
		return
	}
	// A default whose body reaches another default on the same type re-enters here; the
	// guard is publishImplBodyCandidates's, for the same reason — without it the
	// typechecker spins, which reads as a frozen editor rather than a crash.
	key := fmt.Sprintf("default|%p|%s|%s", trait, tm.Name.Key(), concrete.String())
	if tc.publishing == nil {
		tc.publishing = map[string]bool{}
	}
	if tc.publishing[key] {
		return
	}
	tc.publishing[key] = true
	defer delete(tc.publishing, key)
	tc.publishCandidatesAt(&ast.LambdaExpr{Body: tm.DefaultMethod.Body}, trait.Name, concrete)
}
