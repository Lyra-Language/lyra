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
	if dt, ok := decl.Type.(types.DataType); ok {
		return c.deriveOrdImplForData(decl, dt)
	}
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		c.addDeriveError(decl.NameLocation,
			"`@derive(Ord)` is implemented for structs and `data` types; %q is neither — write `impl Ord for %s` by hand",
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

// deriveOrdImplForData builds `impl Ord for <data type>`: **by constructor declaration
// order first, then by payload**, which is the ordering `data` types get everywhere
// that derives one.
//
// The language has no way to read a variant's tag, so the comparison is written as a
// match over the pair. It is **3n arms, not n-squared**, which is what made this worth
// building rather than leaving to hand-written impls: for each constructor in order,
//
//	(Ci(a…), Ci(b…)) => <lexicographic compare of the payloads>
//	(Ci(_…), _)      => Less        // self is the earlier variant
//	(_, Ci(_…))      => Greater     // other is the earlier variant
//
// and once those three are past, no later arm can see Ci on either side — so the next
// constructor's three are reached only by values that are neither. The last constructor
// needs only the first arm: everything else has already been decided.
//
// **Wildcards are arity-matched** (`Rect(_, _)`, not `Rect _`). A single wildcard
// standing for a multi-field payload parses and type-checks and then fails to lower —
// `payload pattern for "Rect" not implemented yet` — a pre-existing gap this synthesis
// simply steps around rather than trips over (todo.md).
func (c *Collector) deriveOrdImplForData(decl *ast.TypeDeclStmt, dt types.DataType) *ast.TraitImplStmt {
	if len(dt.Constructors) == 0 {
		c.addDeriveError(decl.NameLocation,
			"`@derive(Ord)` on %q has no constructors to order by", decl.Name)
		return nil
	}
	loc := decl.GetLocation()
	var arms []ast.MatchArm
	for i, ctor := range dt.Constructors {
		fields := ctor.FieldTypes()
		arms = append(arms, ast.MatchArm{
			Pattern: pairPattern(ctorPattern(ctor.Name, bindings("__a", len(fields)), loc),
				ctorPattern(ctor.Name, bindings("__b", len(fields)), loc), loc),
			Body: c.deriveOrdPayloadBody(fields, 0, loc),
		})
		if i == len(dt.Constructors)-1 {
			// Nothing is left that could reach a "self is earlier" / "other is earlier"
			// arm: every other constructor has already been decided on both sides.
			break
		}
		anyCtor := ctorPattern(ctor.Name, wildcards(len(fields), loc), loc)
		arms = append(arms,
			ast.MatchArm{
				Pattern: pairPattern(anyCtor, &ast.WildcardPattern{PatternBase: patBase(loc)}, loc),
				Body:    ctorExpr("Less", loc),
			},
			ast.MatchArm{
				Pattern: pairPattern(&ast.WildcardPattern{PatternBase: patBase(loc)},
					ctorPattern(ctor.Name, wildcards(len(fields), loc), loc), loc),
				Body: ctorExpr("Greater", loc),
			})
	}

	self := &ast.IdentifierPattern{PatternBase: patBase(loc), Name: "self"}
	other := &ast.IdentifierPattern{PatternBase: patBase(loc), Name: "other"}
	return &ast.TraitImplStmt{
		AstBase:   ast.AstBase{Location: loc},
		TraitName: deriveOrd,
		Type:      types.UnresolvedType{Name: decl.Name},
		Methods: []ast.TraitMethodImpl{{
			Name: ast.MethodName{Kind: ast.MethodNameKindIdentifier, Value: "compare"},
			Clause: ast.LambdaClause{
				AstBase:  ast.AstBase{Location: loc},
				Patterns: []ast.Pattern{self, other},
				Body: &ast.MatchExpr{
					ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
					Scrutinee: &ast.TupleLiteralExpr{
						ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
						Elements: []ast.Expression{
							identExpr("self", loc), identExpr("other", loc),
						},
					},
					MatchArms: arms,
				},
			},
		}},
	}
}

// deriveOrdPayloadBody compares the bound payload fields of one variant, lexicographically
// — the same shape the struct derive uses, over `__a0…`/`__b0…` instead of field names.
// A nullary variant is `Equal`: matching tags with no payload is all there is to compare.
func (c *Collector) deriveOrdPayloadBody(fields []types.Type, i int, loc ast.Location) ast.Expression {
	if len(fields) == 0 {
		return ctorExpr("Equal", loc)
	}
	cmp := &ast.BooleanBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     identExpr(fmt.Sprintf("__a%d", i), loc),
		Operator: ast.BooleanBinaryOpSpaceship,
		Right:    identExpr(fmt.Sprintf("__b%d", i), loc),
	}
	if i == len(fields)-1 {
		return cmp
	}
	const carry = "__ord"
	return &ast.MatchExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Scrutinee: cmp,
		MatchArms: []ast.MatchArm{
			{
				Pattern: &ast.DataPattern{PatternBase: patBase(loc), Name: "Equal"},
				Body:    c.deriveOrdPayloadBody(fields, i+1, loc),
			},
			{
				Pattern: &ast.IdentifierPattern{PatternBase: patBase(loc), Name: carry},
				Body:    identExpr(carry, loc),
			},
		},
	}
}

func patBase(loc ast.Location) ast.PatternBase {
	return ast.PatternBase{AstBase: ast.AstBase{Location: loc}}
}

func identExpr(name string, loc ast.Location) ast.Expression {
	return &ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}, Name: name}
}

// ctorExpr builds a nullary constructor value (`Less`, `Equal`, `Greater`).
func ctorExpr(name string, loc ast.Location) ast.Expression {
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: name,
	}
}

// ctorPattern builds `Name` for a nullary constructor and `Name(p0, …)` otherwise. The
// payload is always a tuple pattern, whatever the arity — an arity-1 `C(x)` collects and
// lowers exactly as the bare `C x` does, so one shape covers both.
func ctorPattern(name string, payload []ast.Pattern, loc ast.Location) ast.Pattern {
	p := &ast.DataPattern{PatternBase: patBase(loc), Name: name}
	if len(payload) > 0 {
		p.Pattern = &ast.TuplePattern{PatternBase: patBase(loc), Elements: payload}
	}
	return p
}

func pairPattern(a, b ast.Pattern, loc ast.Location) ast.Pattern {
	return &ast.TuplePattern{PatternBase: patBase(loc), Elements: []ast.Pattern{a, b}}
}

func bindings(prefix string, n int) []ast.Pattern {
	out := make([]ast.Pattern, n)
	for i := range out {
		out[i] = &ast.IdentifierPattern{Name: fmt.Sprintf("%s%d", prefix, i)}
	}
	return out
}

func wildcards(n int, loc ast.Location) []ast.Pattern {
	out := make([]ast.Pattern, n)
	for i := range out {
		out[i] = &ast.WildcardPattern{PatternBase: patBase(loc)}
	}
	return out
}
