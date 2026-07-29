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
			c.rewriteCtorStmt(s)
		}
	}
}

// rewriteCtorExpr returns e with any constructor/named-tuple expression inside it
// (or e itself) reclassified. Children are rewritten first, then e itself: a bare
// nullary-constructor name becomes a DataConstructorExpr, an applied
// constructor/named-tuple call becomes a named TupleLiteralExpr.
func (c *Collector) rewriteCtorExpr(e ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	c.rewriteCtorChildren(e)
	switch n := e.(type) {
	case *ast.IdentifierExpr:
		// A bare uppercase name that is a *nullary* constructor and not shadowed by a
		// value binding denotes that constructor (an applied-only constructor needs
		// call syntax, so a bare use of it is left to surface as undefined).
		if n.IsConst && c.isNullaryConstructor(n.Name) && !c.shadowedByValue(n.Name) {
			return &ast.DataConstructorExpr{ExprBase: n.ExprBase, Constructor: n.Name}
		}
	case *ast.FunctionCallExpr:
		if id, ok := n.Function.(*ast.IdentifierExpr); ok &&
			id.IsConst && c.isConstructorOrNamedTuple(id.Name) && !c.shadowedByValue(id.Name) {
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

// rewriteCtorStmt rewrites the expressions a statement holds. It mirrors
// ast.walkStmtChildren, but reassigns each expression slot (a visitor can't).
func (c *Collector) rewriteCtorStmt(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.DestructuringDeclStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.ExpressionStmt:
		s.Expression = c.rewriteCtorExpr(s.Expression)
	case *ast.VarReassignmentStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.DerefAssignmentStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.LValueAssignmentStmt:
		s.Target = c.rewriteCtorExpr(s.Target)
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.ReturnStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.BreakStmt:
		s.Value = c.rewriteCtorExpr(s.Value)
	case *ast.WithStmt:
		s.Arena = c.rewriteCtorExpr(s.Arena)
		c.rewriteCtorBlock(&s.Body)
	case *ast.IfDestructuringStmt:
		s.DestructuringStatement.Value = c.rewriteCtorExpr(s.DestructuringStatement.Value)
		c.rewriteCtorBlock(s.Then)
		c.rewriteCtorBlock(s.Else) // nil-safe
	case *ast.ElseDestructuringStmt:
		s.DestructuringStatement.Value = c.rewriteCtorExpr(s.DestructuringStatement.Value)
		c.rewriteCtorBlock(s.Else)
	case *ast.TraitImplStmt:
		for i := range s.Methods {
			s.Methods[i].Clause.Body = c.rewriteCtorExpr(s.Methods[i].Clause.Body)
		}
	case *ast.TraitDeclStmt:
		for i := range s.Methods {
			if s.Methods[i].DefaultMethod != nil {
				s.Methods[i].DefaultMethod.Body = c.rewriteCtorExpr(s.Methods[i].DefaultMethod.Body)
			}
		}
	}
}

// rewriteCtorChildren rewrites the expressions/statements a *container* expression
// holds. It mirrors ast.walkExprChildren, reassigning each slot in place.
func (c *Collector) rewriteCtorChildren(expr ast.Expression) {
	switch e := expr.(type) {
	case *ast.BlockExpr:
		c.rewriteCtorBlock(e)
	case *ast.IfExpr:
		e.Condition = c.rewriteCtorExpr(e.Condition)
		e.Then = c.rewriteCtorExpr(e.Then)
		e.Else = c.rewriteCtorExpr(e.Else)
	case *ast.MatchExpr:
		e.Scrutinee = c.rewriteCtorExpr(e.Scrutinee)
		for i := range e.MatchArms {
			if e.MatchArms[i].Guard != nil {
				e.MatchArms[i].Guard.Condition = c.rewriteCtorExpr(e.MatchArms[i].Guard.Condition)
			}
			e.MatchArms[i].Body = c.rewriteCtorExpr(e.MatchArms[i].Body)
		}
	case *ast.LambdaExpr:
		for i := range e.Parameters {
			e.Parameters[i].DefaultValue = c.rewriteCtorExpr(e.Parameters[i].DefaultValue)
		}
		e.Body = c.rewriteCtorExpr(e.Body)
		for i := range e.LambdaClauses {
			e.LambdaClauses[i].Body = c.rewriteCtorExpr(e.LambdaClauses[i].Body)
		}
	case *ast.ForLoopExpr:
		if e.Init != nil {
			e.Init.Value = c.rewriteCtorExpr(e.Init.Value)
		}
		if e.Condition != nil {
			*e.Condition = c.rewriteCtorExpr(*e.Condition)
		}
		if e.Post != nil {
			*e.Post = c.rewriteCtorExpr(*e.Post)
		}
		c.rewriteCtorBlock(e.Body)
	case *ast.ForInLoopExpr:
		e.Iterable = c.rewriteCtorExpr(e.Iterable)
		c.rewriteCtorBlock(e.Body)
	case *ast.FunctionCallExpr:
		e.Function = c.rewriteCtorExpr(e.Function)
		for i := range e.Arguments {
			e.Arguments[i] = c.rewriteCtorExpr(e.Arguments[i])
		}
	case *ast.MemberExpr:
		e.Object = c.rewriteCtorExpr(e.Object)
	case *ast.IndexExpr:
		e.Object = c.rewriteCtorExpr(e.Object)
		e.Index = c.rewriteCtorExpr(e.Index)
	case *ast.TryExpr:
		e.Operand = c.rewriteCtorExpr(e.Operand)
	case *ast.MathBinaryOpExpr:
		e.Left = c.rewriteCtorExpr(e.Left)
		e.Right = c.rewriteCtorExpr(e.Right)
	case *ast.MathAssignOpExpr:
		e.Right = c.rewriteCtorExpr(e.Right)
	case *ast.BooleanBinaryOpExpr:
		e.Left = c.rewriteCtorExpr(e.Left)
		e.Right = c.rewriteCtorExpr(e.Right)
	case *ast.NotBooleanExpr:
		e.Expression = c.rewriteCtorExpr(e.Expression)
	case *ast.NegationExpr:
		e.Operand = c.rewriteCtorExpr(e.Operand)
	case *ast.AwaitExpr:
		e.Operand = c.rewriteCtorExpr(e.Operand)
	case *ast.AddressOfExpr:
		e.Operand = c.rewriteCtorExpr(e.Operand)
	case *ast.DerefExpr:
		e.Operand = c.rewriteCtorExpr(e.Operand)
	case *ast.ArrayLiteralExpr:
		for i := range e.Elements {
			e.Elements[i] = c.rewriteCtorExpr(e.Elements[i])
		}
	case *ast.TupleLiteralExpr:
		for i := range e.Elements {
			e.Elements[i] = c.rewriteCtorExpr(e.Elements[i])
		}
	case *ast.InterpolatedStringExpr:
		for i := range e.Segments {
			e.Segments[i] = c.rewriteCtorExpr(e.Segments[i])
		}
	case *ast.StructInstanceExpr:
		// BaseStruct is a bare *IdentifierExpr (the record-update target); it can't be
		// a constructor, so it needs no rewrite.
		for i := range e.Fields {
			e.Fields[i].Value = c.rewriteCtorExpr(e.Fields[i].Value)
		}
	case *ast.AnonymousStructInstanceExpr:
		// BaseStruct is a bare *IdentifierExpr (the record-update target); it can't be
		// a constructor, so it needs no rewrite.
		for i := range e.Fields {
			e.Fields[i].Value = c.rewriteCtorExpr(e.Fields[i].Value)
		}
	case *ast.ArrayCompExpr:
		for i := range e.Generators {
			e.Generators[i].Value = c.rewriteCtorExpr(e.Generators[i].Value)
		}
		for i := range e.Guards {
			e.Guards[i] = c.rewriteCtorExpr(e.Guards[i])
		}
		e.Result = c.rewriteCtorExpr(e.Result)
	case *ast.RangeExpr:
		e.Start = c.rewriteCtorExpr(e.Start)
		e.End = c.rewriteCtorExpr(e.End)
		e.Step = c.rewriteCtorExpr(e.Step)
	case *ast.NullCoalescingExpr:
		e.Optional = c.rewriteCtorExpr(e.Optional)
		e.Default = c.rewriteCtorExpr(e.Default)
	case *ast.StringConcatExpr:
		e.Left = c.rewriteCtorExpr(e.Left)
		e.Right = c.rewriteCtorExpr(e.Right)
	case *ast.ArrayRepeatExpr:
		e.Value = c.rewriteCtorExpr(e.Value)
		e.Count = c.rewriteCtorExpr(e.Count)
	case *ast.ComposeExpr:
		e.Left = c.rewriteCtorExpr(e.Left)
		e.Right = c.rewriteCtorExpr(e.Right)
	case *ast.YieldExpr:
		e.Value = c.rewriteCtorExpr(e.Value)
	case *ast.YieldFromExpr:
		e.Generator = c.rewriteCtorExpr(e.Generator)
	case *ast.UnsafeBlockExpr:
		c.rewriteCtorBlock(&e.Body)
	case *ast.DataConstructorExpr:
		e.Value = c.rewriteCtorExpr(e.Value)
	case *ast.GuardExpr:
		e.Condition = c.rewriteCtorExpr(e.Condition)
	}
}

func (c *Collector) rewriteCtorBlock(b *ast.BlockExpr) {
	if b == nil {
		return
	}
	for i := range b.Statements {
		c.rewriteCtorStmt(b.Statements[i])
	}
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
	if decl, ok := c.table.Types[name]; ok {
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
// the only binding form an uppercase name can take) in the global scope, in which
// case that binding — not a same-named constructor — is what the name refers to.
func (c *Collector) shadowedByValue(name string) bool {
	if sym, ok := c.table.GlobalScope.Lookup(name); ok {
		if _, isVar := sym.(*ast.VarDeclStmt); isVar {
			return true
		}
	}
	return false
}
