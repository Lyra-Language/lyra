package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// reclassifyConstructorExprs makes an all-caps / single-capital data constructor
// or named-tuple usable in *expression* position (`data Dir = N | S | E | W;
// let d = N`, `FOO(3)`, `POINT(1, 2)`). Such a name lexes as a `const_identifier`
// (the token reserved for constants, `/[A-Z][A-Z0-9_]*/`), so a bare use collects
// to an `IdentifierExpr` and an applied use to a `FunctionCallExpr` — not the
// `DataConstructorExpr` / named `TupleLiteralExpr` a PascalCase constructor
// produces (`user_defined_type_name` needs a lowercase letter to be unambiguous).
// The typechecker then reports a misleading "undefined identifier".
//
// This post-pass — like reclassifyStructPatterns, run after walkProgram has
// registered every type, so a forward-referenced constructor still resolves —
// rewrites those two forms into the exact nodes PascalCase yields, so all
// downstream passes handle them identically with no special-casing. A real
// value binding of the same name (a `const N`) shadows the constructor: the
// rewrite is skipped when the name is declared as a value, matching normal scope
// resolution and leaving existing constant code untouched. (Pattern position
// already resolves these constructors, so only expressions need this.)
func (c *Collector) reclassifyConstructorExprs() {
	for _, stmt := range c.ast.Statements {
		if s, ok := stmt.(ast.Statement); ok {
			ast.RewriteStmt(s, c.reclassifyCtorExpr)
		}
	}
}

// reclassifyCtorExpr is the rewrite applied to every expression in the program: a bare
// nullary-constructor name becomes a DataConstructorExpr, an applied constructor or
// named-tuple call becomes a named TupleLiteralExpr, and everything else is returned
// unchanged. ast.RewriteStmt rewrites children first, so each node here already has its
// final operands.
//
// **This used to be a hand-copy of ast.walkExprChildren**, ~200 lines reassigning each
// slot in place, and it had fallen three node kinds behind the walker it mirrored:
// `TupleIndexExpr`, `BitwiseNotExpr` and a deref assignment's target were never
// descended into, so `~FLAG`, `PAIR(1, 2).0` and `p^ = N` kept the const-identifier
// spelling and surfaced as "undefined identifier". ast.RewriteStmt is that traversal
// once, checked against the canonical walker by pkg/ast's exhaustiveness test.
//
// The one thing the shared rewriter cannot do is replace a slot typed *IdentifierExpr —
// a record-update base and a compound assignment's left side. Neither can be a
// constructor (the first names a struct value to copy, the second a mutable binding), so
// leaving them alone is what this pass wanted anyway.
func (c *Collector) reclassifyCtorExpr(e ast.Expression) ast.Expression {
	switch n := e.(type) {
	case *ast.IdentifierExpr:
		// A bare uppercase name that is a *nullary* constructor and not shadowed by a
		// value binding denotes that constructor (an applied-only constructor needs
		// call syntax, so a bare use of it is left to surface as undefined).
		if n.IsConst && c.isNullaryConstructor(n.Name) && !c.shadowedByValue(n.Name, n.GetLocation()) {
			return &ast.DataConstructorExpr{ExprBase: n.ExprBase, Constructor: n.Name}
		}
	case *ast.FunctionCallExpr:
		if id, ok := n.Function.(*ast.IdentifierExpr); ok &&
			id.IsConst && c.isConstructorOrNamedTuple(id.Name) && !c.shadowedByValue(id.Name, id.GetLocation()) {
			// `FOO(3)` / `POINT(1, 2)` — the applied-constructor form is a named tuple
			// literal (the same node PascalCase `Some(42)` / `Point(3, 4)` produces).
			return &ast.TupleLiteralExpr{
				ExprBase:         n.ExprBase,
				Name:             id.Name,
				Elements:         n.Arguments,
				GenericArguments: n.GenericArguments,
			}
		}
	}
	return e
}

// isNullaryConstructor reports whether name is a payload-free constructor of a
// declared data type (the only constructor form a bare name denotes).
func (c *Collector) isNullaryConstructor(name string) bool {
	for _, decl := range c.table.Types {
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, ctor := range dt.Constructors {
			if ctor.Name == name {
				return len(ctor.FieldTypes()) == 0
			}
		}
	}
	return false
}

// isConstructorOrNamedTuple reports whether name is a data constructor (any arity)
// or a declared named tuple — the applied form `name(args)` denotes either.
func (c *Collector) isConstructorOrNamedTuple(name string) bool {
	if decl, ok := c.table.LookupType(name); ok {
		if tt, ok := decl.Type.(types.TupleType); ok && tt.Name == name {
			return true
		}
	}
	for _, decl := range c.table.Types {
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, ctor := range dt.Constructors {
			if ctor.Name == name {
				return true
			}
		}
	}
	return false
}

// shadowedByValue reports whether name is declared as a value binding (a `const`,
// the only binding form an uppercase name can take) as the file at loc sees it, in
// which case that binding — not a same-named constructor — is what the name refers to.
//
// The lookup starts from the referencing file's own module scope rather than from the
// global one, because that is where a top-level declaration lands; the chain out to the
// prelude and global scopes then covers a constant another module exported.
func (c *Collector) shadowedByValue(name string, loc ast.Location) bool {
	scope := c.table.ModuleScopeFor(c.table.ModuleOfFile[loc.File])
	if scope == nil {
		return false
	}
	if sym, ok := scope.Lookup(name); ok {
		if _, isVar := sym.(*ast.VarDeclStmt); isVar {
			return true
		}
	}
	return false
}
