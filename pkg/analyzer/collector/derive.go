package collector

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `@derive(Ord)` — the structural ordering, synthesized as an ordinary impl.
//
// **It builds an `ast.TraitImplStmt` and appends it to the program**, rather than
// teaching dispatch about derives. Everything downstream then treats a derived impl
// exactly as a hand-written one: the typechecker checks the body (so deriving on a
// field that is not orderable is an ordinary error, at the field, with no new
// diagnostic), the coherence check refuses a derive beside a hand-written
// `impl Ord` (lyra-E037, for free), and the backend lowers it through the path that
// already exists. That is the same erasure `@derive`'s neighbours use — juxtaposition,
// bare match-arm jumps, UFCS — and it is why this file has no counterpart anywhere
// else in the compiler.
//
// The body is the lexicographic comparison, in declaration order:
//
//	compare = (self, other) => match self.a <=> other.a {
//	  Equal => match self.b <=> other.b { Equal => self.c <=> other.c, c => c },
//	  c => c,
//	}
//
// **Declaration order is the ordering**, which is a real commitment: reordering a
// struct's fields changes how its values sort. That is why the ordering is opt-in
// through an attribute rather than automatic — the alternative is a type that silently
// acquires an order nobody chose, and a reordering that silently changes it. Rust
// makes the same trade for the same reason.

// deriveOrd is the trait name `@derive(Ord)` synthesizes an impl for.
const deriveOrd = "Ord"

// synthesizeDerives turns each `@derive(...)` on a type declaration into a real impl
// block. Runs in Finish, after every type is registered, so a field whose type is
// declared later still resolves.
func (c *Collector) synthesizeDerives() {
	for _, stmt := range c.ast.Statements {
		decl, ok := stmt.(*ast.TypeDeclStmt)
		if !ok || len(decl.Derives) == 0 {
			continue
		}
		for _, trait := range decl.Derives {
			if trait != deriveOrd {
				// A **warning**, not an error: the derive is not wrong, it is a no-op —
				// the trait it names does not exist yet. Saying so beats silence (a
				// `@derive(Show)` that compiles and does nothing is the phantom-builtin
				// shape this compiler keeps digging out) without refusing a program over
				// a feature that has not landed. It becomes moot when the trait does.
				c.addDeriveDiagnostic(decl.NameLocation, diag.SeverityWarning, diag.CodeInertDerive,
					"`@derive(%s)` does nothing: only `@derive(Ord)` is implemented, so %q gets no %s",
					trait, decl.Name, trait)
				continue
			}
			impl := c.deriveOrdImpl(decl)
			if impl == nil {
				continue
			}
			c.ast.Statements = append(c.ast.Statements, impl)
		}
	}
}

// deriveOrdImpl builds the `impl Ord for <decl>` block, or reports why it cannot.
func (c *Collector) deriveOrdImpl(decl *ast.TypeDeclStmt) *ast.TraitImplStmt {
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		// A `data` type's derived ordering is by constructor order and then payload,
		// and the language has no way to read a tag — so the synthesis would be an
		// N-squared match over both scrutinees. Worth doing, not worth guessing at
		// here; refused with the fix rather than silently skipped (todo.md).
		c.addDeriveError(decl.NameLocation,
			"`@derive(Ord)` is only implemented for structs; %q is not one — write `impl Ord for %s` by hand",
			decl.Name, decl.Name)
		return nil
	}
	if len(st.Fields) == 0 {
		// Every value is equal to every other. Legal, and almost certainly a mistake,
		// so it is refused rather than silently ordering nothing.
		c.addDeriveError(decl.NameLocation,
			"`@derive(Ord)` on %q has no fields to order by", decl.Name)
		return nil
	}

	loc := decl.GetLocation()
	body := c.deriveOrdBody(st.Fields, 0, loc)
	self := &ast.IdentifierPattern{PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}}, Name: "self"}
	other := &ast.IdentifierPattern{PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}}, Name: "other"}
	return &ast.TraitImplStmt{
		AstBase:   ast.AstBase{Location: loc},
		TraitName: deriveOrd,
		Type:      types.UnresolvedType{Name: decl.Name},
		Methods: []ast.TraitMethodImpl{{
			Name: ast.MethodName{Kind: ast.MethodNameKindIdentifier, Value: "compare"},
			Clause: ast.LambdaClause{
				AstBase:  ast.AstBase{Location: loc},
				Patterns: []ast.Pattern{self, other},
				Body:     body,
			},
		}},
	}
}

// deriveOrdBody builds the comparison for fields[i:] — the i-th field's `<=>`, with
// the remaining fields as the `Equal` arm and the comparison itself as the fallthrough.
//
// The last field is its comparison alone: there is nothing left to break a tie with,
// so wrapping it in a match that returns the same value either way would be dead
// weight in the emitted code.
func (c *Collector) deriveOrdBody(fields []types.StructField, i int, loc ast.Location) ast.Expression {
	cmp := &ast.BooleanBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     fieldOf("self", fields[i].Name, loc),
		Operator: ast.BooleanBinaryOpSpaceship,
		Right:    fieldOf("other", fields[i].Name, loc),
	}
	if i == len(fields)-1 {
		return cmp
	}
	// `c => c` binds whatever the comparison answered and hands it straight back, so
	// a decided ordering short-circuits the remaining fields.
	const carry = "__ord"
	return &ast.MatchExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Scrutinee: cmp,
		MatchArms: []ast.MatchArm{
			{
				Pattern: &ast.DataPattern{
					PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
					Name:        "Equal",
				},
				Body: c.deriveOrdBody(fields, i+1, loc),
			},
			{
				Pattern: &ast.IdentifierPattern{
					PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
					Name:        carry,
				},
				Body: &ast.IdentifierExpr{
					ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
					Name:     carry,
				},
			},
		},
	}
}

// fieldOf builds `<binding>.<field>`.
func fieldOf(binding, field string, loc ast.Location) ast.Expression {
	return &ast.MemberExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object: &ast.IdentifierExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Name:     binding,
		},
		Property: ast.IdentifierExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Name:     field,
		},
	}
}

// addDeriveError reports against the *type's name* rather than a CST node: the
// synthesis runs in Finish, long after the nodes are gone, and the name is where a
// reader looks for the attribute anyway.
func (c *Collector) addDeriveError(loc ast.Location, format string, args ...any) {
	c.addDeriveDiagnostic(loc, diag.SeverityError, diag.CodeMalformedDerive, format, args...)
}

// addDeriveDiagnostic is addDeriveError at a chosen severity: a derive naming a trait
// that does not exist yet is a no-op worth reporting, not a program to refuse.
func (c *Collector) addDeriveDiagnostic(loc ast.Location, sev diag.Severity, code, format string, args ...any) {
	c.errors = append(c.errors, diag.Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Location: loc,
		Severity: sev,
		Code:     code,
	})
}
