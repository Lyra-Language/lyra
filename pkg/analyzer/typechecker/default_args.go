package typechecker

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Default parameter values.
//
//	let fib = (n: i64, a: i64 = 0, b: i64 = 1) -> i64 { … }
//	fib(10)   // a and b come from the declaration
//
// The grammar, collector and arity check already understood defaults — what was missing is
// that the *call site* never received them, so the backend saw a call with fewer arguments
// than the function had parameters and refused the function outright.
//
// They are filled in here, in the front end, by appending the declaration's default
// expressions to the call's argument list before anything else looks at it. The rest of the
// pipeline then sees a call that passes every argument explicitly: the arguments are
// type-checked against their parameters like any other, the widths propagate, and the
// backend needs no notion of defaults at all — the same reasoning as the multi-clause
// desugaring and contextual lambdas.
//
// **Filling is idempotent**, which matters because the typechecker revisits nodes: after the
// first pass the argument count equals the parameter count, so a second pass adds nothing.
//
// One deliberate sharing: the appended expression is the *same AST node* as the declaration's
// default, not a copy. Two call sites that both omit an argument therefore share it. That is
// sound for everything keyed by node — the recorded type is the parameter's type at every
// site, and the width propagated is the same — because a default is evaluated against the
// parameter's declared type and cannot vary by caller. Cloning would need a deep AST copy,
// which this compiler deliberately avoids everywhere else (see the monomorphizer's note on
// substitution over cloning).

// applyDefaultArguments appends the callee's default expressions for any trailing arguments
// the call omits. It stops at the first parameter with no default, leaving the arity check to
// report a genuinely missing argument.
func (tc *TypeChecker) applyDefaultArguments(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) {
	if lambda == nil || call == nil {
		return
	}
	for i := len(call.Arguments); i < len(lambda.Parameters); i++ {
		def := lambda.Parameters[i].DefaultValue
		if def == nil {
			return
		}
		tc.typeTable.MarkDefault(def)
		call.Arguments = append(call.Arguments, def)
	}
}

// inDeclaringModule installs the scope a filled-in default is read in — its declaring
// module's, with no caller's parameters — and answers the function restoring the caller's.
//
// **A default is the declaration's, wherever it is filled in.** `(by: i64 = SECRET)` or
// `zone: Zone = THIRDS` names the declaring module's own constants, which a caller in
// another module neither imports nor may see (a private one); and a name the caller has
// in scope must not capture it. Inferred in the caller's scope, such a default was
// "undefined identifier" at every use outside its module (10/01, std.genesis.camera's
// `Camera.zone`).
func (tc *TypeChecker) inDeclaringModule(def ast.Expression) func() {
	scope := tc.moduleScopeOf(def)
	if scope == nil {
		return func() {}
	}
	oldScope, oldParams := tc.scope, tc.paramTypes
	tc.scope, tc.paramTypes = scope, nil
	return func() { tc.scope, tc.paramTypes = oldScope, oldParams }
}

// checkDefaultsAreTrailing reports a parameter with a default value followed by one without.
//
// Positional arguments fill left to right, so `(a: i64 = 1, b: i64)` cannot be called with
// one argument in any useful way: the value would bind to `a`, and `b` — which has no default
// — would be left unfilled. Worse than useless, it was silently *accepted*: the arity check
// counts required parameters rather than checking their order, so `f(5)` bound 5 to `a` and
// then had nothing for `b`, which is a call the programmer clearly did not mean.
//
// Every language with positional defaults imposes this rule for the same reason (Python, C++,
// TypeScript). Named arguments would lift it; Lyra has none.
func (tc *TypeChecker) checkDefaultsAreTrailing(funcName string, lambda *ast.LambdaExpr) {
	seenDefault := false
	for i := range lambda.Parameters {
		p := &lambda.Parameters[i]
		if p.DefaultValue != nil {
			seenDefault = true
			continue
		}
		if seenDefault {
			tc.addError(p.GetLocation(), SeverityError,
				"%s: parameter %q has no default but follows one that does; "+
					"parameters with defaults must come last, since arguments fill left to right",
				funcName, p.Pattern.GetName())
			return
		}
	}
}

// applyDefaultFields fills a struct literal's omitted fields with the declaration's
// default expressions — the struct analogue of applyDefaultArguments above, and
// deliberately the same shape: the fill happens in the front end, so the rest of the
// pipeline sees a literal that supplies every field explicitly and the backend needs no
// notion of defaults at all.
//
// It closed a gap the *grammar* was hiding. `Person {}` did not parse (a literal body
// required at least one field), which read as "defaults are unusable when every field
// has one" — but the real gap was one rung down and wider: no default was ever filled,
// so `Person { name: "x" }` with a defaulted `age` also failed, in the backend, with
// `field "age" has no value (default values not implemented yet)`. The todo entry's own
// suggested workaround — name a field you wanted the default for — did not work either.
//
// Three rules, each mirroring the argument side:
//
//   - **Idempotent.** The typechecker revisits nodes; after the first pass every
//     defaulted field is present, so a second pass adds nothing.
//   - **Record-update syntax is skipped.** `Player { base | hp: 3 }` takes its missing
//     fields from the *base*, so filling defaults there would overwrite what the base
//     supplies with the declaration's default — the one case where "missing" does not
//     mean "wanted the default".
//   - **Each literal gets its own copy of the default** where nothing in it binds a name
//     (ast.CopyBindingFree) — a leaf (10/27), and since 10/30 any tree of one: `[None]`,
//     `(None, 0)`, a struct literal, a constructor applied. A generic struct's default is
//     typed per instantiation — `shown: Maybe<Laid<e>> = None` is a `Maybe<Laid<i64>>` in
//     one literal and a `Maybe<Laid<string>>` in the next — and one node records one type,
//     so a shared one was "cannot assign Maybe<Laid<i64>> to Maybe<Laid<string>>".
//   - **A default that binds a name is the declaration's own node**, shared by every
//     literal: a lambda's parameters and a block's locals are registered against their
//     nodes and typed by location, so a copy would share those with the original. Shared,
//     it is typed by the first instantiation that fills it, and a literal at another one of
//     a field whose type mentions the struct's parameters is refused by name (sharedDefault)
//     rather than as a mismatch inside the default. Typing one per instantiation would
//     need it lifted into a generic function the monomorphizer specializes (todo.md).
func (tc *TypeChecker) applyDefaultFields(expr *ast.StructInstanceExpr, structType types.NamedStructType, decl *ast.TypeDeclStmt, typeSubst map[string]types.Type) {
	if expr == nil || expr.BaseStruct != nil {
		return
	}
	supplied := make(map[string]bool, len(expr.Fields))
	for idx, f := range expr.Fields {
		name := f.Name
		if name == "" && idx < len(structType.Fields) {
			// A shorthand field (`{ x, y }`) carries no name of its own and stands for
			// the declared field at its position.
			name = structType.Fields[idx].Name
		}
		supplied[name] = true
	}
	for _, declared := range structType.Fields {
		if supplied[declared.Name] || declared.DefaultValue == nil {
			continue
		}
		value, ok := declared.DefaultValue.(ast.Expression)
		if !ok {
			continue // not an expression: the declaration's own diagnostic to report
		}
		if copied, ok := ast.CopyBindingFree(value); ok && !tc.holdsScope(copied) {
			value = copied
		} else if !tc.sharedDefaultFits(expr, declared, value, decl, typeSubst) {
			continue
		}
		tc.typeTable.MarkDefault(value)
		expr.Fields = append(expr.Fields, ast.StructField{
			Name:  declared.Name,
			Value: value,
		})
	}
}

// holdsScope reports whether the collector registered a scope under any node of expr — the
// guard beside CopyBindingFree's own test, which a node kind that binds and is not yet in
// its list would otherwise slip past.
func (tc *TypeChecker) holdsScope(expr ast.Expression) bool {
	held := false
	ast.WalkExpr(expr, nil, func(e ast.Expression) bool {
		if _, ok := tc.scopeTable.Get(e); ok {
			held = true
		}
		return !held
	})
	return held
}

// sharedDefault is the instantiation a shared default was first typed at, and where.
type sharedDefault struct {
	instantiation string
	at            ast.Location
}

// sharedDefaultFits reports whether literal expr may use the shared default for field: yes
// unless the field's type mentions the struct's parameters and an earlier literal typed the
// default at other arguments, which is refused here, once, naming why. Unsolved arguments
// are not compared — the literal's own check reports what is missing.
func (tc *TypeChecker) sharedDefaultFits(expr *ast.StructInstanceExpr, field types.StructField, def ast.Expression, decl *ast.TypeDeclStmt, typeSubst map[string]types.Type) bool {
	if decl == nil || len(decl.GenericParams) == 0 {
		return true
	}
	mentioned := map[string]bool{}
	types.CollectTypeVars(field.Type, mentioned)
	var args []string
	depends := false
	for _, p := range decl.GenericParams {
		depends = depends || mentioned[p.Name]
		arg := typeSubst[p.Name]
		if arg == nil {
			return true
		}
		args = append(args, types.Source(arg))
	}
	if !depends {
		return true
	}
	inst := expr.Name + "<" + strings.Join(args, ", ") + ">"
	if tc.sharedDefaults == nil {
		tc.sharedDefaults = map[ast.Expression]sharedDefault{}
	}
	first, seen := tc.sharedDefaults[def]
	if !seen {
		tc.sharedDefaults[def] = sharedDefault{instantiation: inst, at: expr.GetLocation()}
		return true
	}
	if first.instantiation == inst {
		return true
	}
	tc.addError(expr.GetLocation(), SeverityError,
		"%s: the default for %q binds names (a lambda, a block or a match), so it is one expression every literal shares, typed for %s at %s — it cannot also be %s's; write %q in this literal",
		expr.Name, field.Name, first.instantiation, first.at.Pretty(), inst, field.Name)
	return false
}
