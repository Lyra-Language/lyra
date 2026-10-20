package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// fillConstructorLambda types what no context typed in a lambda the driver made from a
// constructor written as a function value (`let make = Width`; pkg/driver's
// eraseConstructorValues). A context fills such a lambda as it fills any literal, first —
// `xs.map(Some)` takes `map`'s `t` — so this fills only the blanks still standing: each
// parameter from its payload field, and the return from the data type, where neither
// mentions the type's parameters. They are the annotations the hand-written lambda would
// need, and the declaration states them.
//
// A generic constructor no context reached cannot be typed — `let wrap = Some` says nothing
// of what `Some` holds — and is refused once, as the construction `Some(x)` with nothing
// to solve its parameter is (lyra-E073), rather than as an undefined name inside a lambda
// the program never wrote.
func (tc *TypeChecker) fillConstructorLambda(lambda *ast.LambdaExpr) {
	if lambda.ConstructorValue == nil {
		return
	}
	name := lambda.ConstructorValue.Constructor
	decl, dt, ok := tc.declaringDataType(name, lambda.GetLocation())
	if !ok {
		return
	}
	var fields []types.Type
	for _, c := range dt.Constructors {
		if c.Name == name {
			fields = c.FieldTypes()
		}
	}
	if len(fields) != len(lambda.Parameters) {
		return
	}
	unsolved := false
	for i := range lambda.Parameters {
		if lambda.Parameters[i].Type != nil {
			continue
		}
		if !mentionsNoTypeVar(fields[i]) {
			unsolved = true
			continue
		}
		lambda.Parameters[i].Type = tc.resolveType(fields[i], lambda.GetLocation())
	}
	if lambda.ReturnType.Type == nil && decl != nil && len(decl.GenericParams) == 0 {
		lambda.ReturnType.Type = dt
	}
	if unsolved && !tc.constructorValueReported[lambda] {
		if tc.constructorValueReported == nil {
			tc.constructorValueReported = map[*ast.LambdaExpr]bool{}
		}
		tc.constructorValueReported[lambda] = true
		tc.addErrorCode(lambda.GetLocation(), SeverityError, diag.CodeUninferableType,
			"cannot tell what `%s` takes as a function: nothing here solves %s's type parameter. "+
				"Give it a context that says — an annotation (`let f: (i64) -> %s<i64> = %s`) or a "+
				"parameter of a function type — or write the lambda (`(x: i64) => %s(x)`)",
			name, dt.Name, dt.Name, name, name)
	}
}
