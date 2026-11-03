package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// reportUnresolvedConstructor diagnoses a PascalCase name used in *value*
// position that is not a data constructor — `Nonexistent.make(1)`, or a bare `let x = Rng`.
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
//   - a declared type — a type is no value, and a `Type.name` that names an associated
//     function never reaches here (associated.go), nor one naming none: that has a
//     message of its own. What is left is a mention, `let x = Rng`;
//   - a declared trait — `Trait::method(…)` *is* a spelling the language has,
//     and `.` is a plausible way to reach for it, so the message names it;
//   - nothing at all — an undefined name, reported as one.
func (tc *TypeChecker) reportUnresolvedConstructor(e *ast.DataConstructorExpr) {
	name := e.Constructor
	loc := e.GetLocation()
	if tc.symTable.QualifiedFailed(loc) {
		return
	}

	if _, ok := tc.symTable.LookupTypeFrom(name, loc); ok {
		tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
			"%s is a type, not a value; write %s { … } or %s(…) to build one, or %s.function(…) to call one it declares in an `impl` block",
			name, name, name, name)
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
