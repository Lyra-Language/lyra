package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckGlobalBorrows refuses lending a callee storage inside a box a mutable global holds
// (`lyra-E091`): `add(g[0])`, `read(b.xs[i])`, `g[0].update()`.
//
// **Every function can reach a global**, so while the callee holds the pointer, it or anything
// it calls may reassign the global or grow the array, and that frees the storage under it. A
// hold on the box wouldn't save it: growing frees the buffer and keeps the box. Copying in and
// out would, at a cost on every call and with a place that reads stale during it, for a shape no
// program in this workspace wrote (672 by-reference place arguments on 11/03, none rooted at a
// global). So it is refused, whether or not this callee does either: the question is what a
// signature promises, and an answer from the callee's body would change with its next edit.
//
// What stays legal is storage a box isn't holding, which nothing can free: the whole global, a
// field of a stack struct (`b.n`), an element of a fixed-size array. So does a builtin's
// receiver (`g[0].push(1)`), which runs no code of the program's, and a C function, which can't
// reach a Lyra global save through a callback it was handed.
//
// The local twin isn't refused: a callee reaches a local only through its arguments, where
// exclusivity by place (`checkExclusiveMutableBorrow`) already answers for the place itself.
// Two names for one array, `f(ys[0], zs)` after `var zs = ys`, stays open (todo.md).
func CheckGlobalBorrows(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable, mt *typetable.MethodTable) []diag.Diagnostic {
	c := &globalBorrows{
		symTable: symTable,
		tt:       tt,
		mt:       mt,
		callees:  &useAfterMove{symTable: symTable, tt: tt, mt: mt},
	}
	for _, node := range program.Statements {
		switch s := node.(type) {
		case *ast.TraitImplStmt:
			for i := range s.Methods {
				clause := &s.Methods[i].Clause
				c.function(&ast.LambdaExpr{Body: clause.Body}, patternNames(clause.Patterns))
			}
		case *ast.TraitDeclStmt:
			for i := range s.Methods {
				if d := s.Methods[i].DefaultMethod; d != nil {
					c.function(&ast.LambdaExpr{Body: d.Body}, patternNames(d.Patterns))
				}
			}
		case ast.Statement:
			ast.WalkStmt(s, nil, c.visit)
		}
	}
	return c.out
}

type globalBorrows struct {
	symTable *symbols.SymbolTable
	tt       *typetable.TypeTable
	mt       *typetable.MethodTable
	// callees answers what a call calls, as the use-after-move pass does.
	callees *useAfterMove
	// bound is, per enclosing function, every name it binds: a name there isn't a global.
	bound []map[string]bool
	out   []diag.Diagnostic
}

func patternNames(patterns []ast.Pattern) map[string]bool {
	names := map[string]bool{}
	for _, p := range patterns {
		for _, name := range patternBoundNames(p) {
			names[name] = true
		}
	}
	return names
}

// function walks a function's body with the names it binds in scope.
func (c *globalBorrows) function(lam *ast.LambdaExpr, params map[string]bool) {
	binds := captures.BodyBinders(lam)
	for name := range params {
		binds[name] = true
	}
	for _, p := range lam.Parameters {
		binds[p.GetName()] = true
	}
	c.bound = append(c.bound, binds)
	defer func() { c.bound = c.bound[:len(c.bound)-1] }()
	ast.WalkExprChildren(lam, nil, c.visit)
}

func (c *globalBorrows) visit(e ast.Expression) bool {
	switch e := e.(type) {
	case *ast.LambdaExpr:
		c.function(e, nil)
		return false
	case *ast.FunctionCallExpr:
		c.call(e)
	}
	return true
}

// call checks each argument the callee takes by reference.
func (c *globalBorrows) call(e *ast.FunctionCallExpr) {
	modes, receiver := c.parameterModes(e)
	offset := 0
	if receiver != nil {
		offset = 1
		if len(modes) > 0 {
			c.argument(e, receiver, modes[0], 1)
		}
	}
	for i, arg := range e.Arguments {
		if i+offset < len(modes) {
			c.argument(e, arg, modes[i+offset], i+offset+1)
		}
	}
}

// parameterModes answers the borrow mode of each parameter and, for a `.`-call, the receiver
// that fills the first: nothing for a builtin or a C function, which aren't asked about.
func (c *globalBorrows) parameterModes(e *ast.FunctionCallExpr) ([]types.TypeModifier, ast.Expression) {
	if lam := c.callees.resolveCallee(e); lam != nil && lam.Body == nil && len(lam.LambdaClauses) == 0 {
		return nil, nil
	}
	if params, receiver := c.callees.calleeParams(e); params != nil {
		modes := make([]types.TypeModifier, len(params))
		for i := range params {
			modes[i] = params[i].TypeModifier
		}
		return modes, receiver
	}
	// A call dispatched through a `where` bound has no resolution, and every candidate impl
	// is held to the trait's signature.
	ref, ok := c.mt.GetBound(e)
	if !ok || c.symTable == nil {
		return nil, nil
	}
	trait, found := c.symTable.LookupTraitFrom(ref.Trait, e.GetLocation())
	member, isDot := e.Function.(*ast.MemberExpr)
	if !found || trait == nil || !isDot {
		return nil, nil
	}
	for i := range trait.Methods {
		if trait.Methods[i].Name.Value == ref.Method && trait.Methods[i].Signature != nil {
			params := trait.Methods[i].Signature.Parameters
			modes := make([]types.TypeModifier, len(params))
			for j := range params {
				modes[j] = params[j].Borrow
			}
			return modes, member.Object
		}
	}
	return nil, nil
}

func (c *globalBorrows) argument(call *ast.FunctionCallExpr, arg ast.Expression, mode types.TypeModifier, position int) {
	if !c.byReference(arg, mode) ||
		!ownership.RootedAtMutableGlobal(arg, c.symTable, c.bound) ||
		!ownership.ReachedThroughBox(arg, c.tt, c.symTable) {
		return
	}
	c.out = append(c.out, diag.Diagnostic{
		Location: arg.GetLocation(),
		Severity: diag.SeverityError,
		Code:     diag.CodeGlobalBorrow,
		Message: fmt.Sprintf(
			"argument %d is passed by `%s` into an array or `shared` value a mutable global holds, and any function can reassign that global or grow the array while the callee holds the borrow, which would free the storage under it. Copy it into a local, pass that, and assign it back",
			position, mode),
	})
}

// byReference reports whether this argument goes by pointer: `mut` always, `ref` unless what it
// lends is a scalar. The argument's recorded type answers that for a generic parameter too.
func (c *globalBorrows) byReference(arg ast.Expression, mode types.TypeModifier) bool {
	t, recorded := c.tt.Get(arg)
	if !recorded {
		return mode == types.Mut
	}
	return types.IsByRefParam(mode, types.StripNewtype(t))
}
