package driver

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// eraseConstructorValues rewrites every constructor that carries a payload but is written
// without one — `xs.map(Width)`, `on_change: Width`, `let make = Pair` — into the lambda
// that applies it: `(p0) => Width(p0)`, a parameter per payload field. A nullary
// constructor (`Quit`, `None`) is a value and is left alone.
//
// **A desugaring, as an aliased constructor is** (constructor_alias.go), and for the same
// reason: a lambda is understood by every pass already — captures, purity, ownership and
// the backend's closures — where a constructor that is also a function would be a new case
// in each, and one missed is a program that checks and then miscompiles. Until 10/19 such
// a constructor was refused as a value of its data type ("cannot assign Ev to (i64) -> Ev"),
// and assigned to its data type it was *accepted*, a constructor with no payload that only
// the backend refused. Rust and Haskell treat it as a function, and lui had to write
// `(value: i64) -> Ev => Width(value)` in its place.
//
// The parameters are left blank, so the context types them as it types any lambda literal
// (`map`'s `t`, a field's `(i64) -> e`); one no context reaches is filled from the
// constructor's declaration by the typechecker, which is why the lambda keeps the
// constructor (LambdaExpr.ConstructorValue) — the editor features answer from it too. Runs
// after eraseConstructorAliases, so the name is the declared one.
func eraseConstructorValues(program *ast.Program, st *symbols.SymbolTable) {
	if st == nil {
		return
	}
	rewrite := func(e ast.Expression) ast.Expression {
		dc, ok := e.(*ast.DataConstructorExpr)
		if !ok || dc.Value != nil {
			return e
		}
		fields := payloadArity(st, dc.Constructor, dc.GetLocation())
		if fields == 0 {
			return e
		}
		return constructorLambda(dc, fields)
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.RewriteStmt(stmt, rewrite)
		}
	}
}

// payloadArity is how many payload fields the constructor named at loc declares, 0 when
// the name is not a constructor or carries none. A lone `void` field is none, as `Ok()`
// spells it (DataTypeConstructor.FieldTypes).
func payloadArity(st *symbols.SymbolTable, name string, loc ast.Location) int {
	decl, ok := st.DeclaringDataType(name, loc)
	if !ok {
		return 0
	}
	dt, ok := decl.Type.(types.DataType)
	if !ok {
		return 0
	}
	for _, c := range dt.Constructors {
		if c.Name == name {
			return len(c.FieldTypes())
		}
	}
	return 0
}

// constructorLambda is `(p0, …) => Ctor(p0, …)` at the constructor's own span. The
// parameter names cannot collide with a name the program binds: `__` begins none a
// program may write.
func constructorLambda(dc *ast.DataConstructorExpr, fields int) *ast.LambdaExpr {
	at := ast.AstBase{Location: dc.GetLocation()}
	lambda := &ast.LambdaExpr{ExprBase: ast.ExprBase{AstBase: at}, ConstructorValue: dc}
	apply := &ast.TupleLiteralExpr{ExprBase: ast.ExprBase{AstBase: at}, Name: dc.Constructor}
	for i := range fields {
		name := fmt.Sprintf("__%s%d", dc.Constructor, i)
		lambda.Parameters = append(lambda.Parameters, ast.Parameter{
			AstBase: at,
			Pattern: &ast.IdentifierPattern{PatternBase: ast.PatternBase{AstBase: at}, Name: name},
		})
		apply.Elements = append(apply.Elements, &ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: at}, Name: name})
	}
	lambda.Body = apply
	return lambda
}
