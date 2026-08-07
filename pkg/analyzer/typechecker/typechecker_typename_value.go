package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// reportUnresolvedConstructor diagnoses a PascalCase name used in *value*
// position that is not a data constructor — `Rng.seeded(42)`, `Point.make(1)`,
// `Nonexistent.make(1)`, or a bare `let x = Rng`.
//
// The lexer splits the cases for us: a PascalCase name in expression position is
// always a `user_defined_type_name`, which the collector turns into a
// DataConstructorExpr on the assumption that it names a nullary constructor
// (`None`, `Red`). When it does not, the assumption is simply wrong, and until
// 08/06 inferExprType answered nil and said nothing — so every one of the forms
// above type-checked clean and then died in the backend as
// `llvm: unsupported method call "seeded"`. That is hazard 5 inverted: the
// backend was refusing a form the front end had never looked at, rather than one
// it deliberately accepted.
//
// **Reported here rather than at the member-call site**, which is where the
// symptom showed up, because the same silence reaches value position through
// several routes — a call (`Rng.seeded(42)`), a plain member access
// (`Rng.field`), and a bare mention (`let x = Rng`) — and the receiver is at
// fault in all three. One diagnostic at the source beats one per consumer, which
// is hazard 8's lesson; the message is phrased about the *name*, so it reads
// correctly wherever the name appears.
//
// Three cases, because the fix differs:
//
//   - a declared type — Lyra has no type-namespaced associated functions, so
//     `Rng.seeded(…)` is not a call at all and the free function is what to
//     write. This is why the prelude's constructors are bare (`rng_seeded`)
//     rather than namespaced;
//   - a declared trait — `Trait::method(…)` *is* a spelling the language has,
//     and `.` is a plausible way to reach for it, so the message names it;
//   - nothing at all — an undefined name, reported as one.
func (tc *TypeChecker) reportUnresolvedConstructor(e *ast.DataConstructorExpr) {
	name := e.Constructor
	loc := e.GetLocation()

	if _, ok := tc.symTable.LookupTypeFrom(name, loc); ok {
		tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
			"%s is a type, not a value; Lyra has no associated functions, so there is no %s.something(...) — call the free function directly",
			name, name)
		return
	}
	if _, ok := tc.symTable.LookupTraitFrom(name, loc); ok {
		tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
			"%s is a trait, not a value; to call one of its methods write %s::method(receiver, ...)",
			name, name)
		return
	}
	tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
		"undefined constructor or type %q", name)
}
