package checker

import (
	"fmt"
	"slices"
	"strconv"

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
// exclusivity by place (`checkExclusiveMutableBorrow`) already answers for the place itself,
// and two names for one array among them are `lyra-E097`'s (secondName). A local sharing a
// global's box, grown by a callee through the global, is `lyra-E098`'s (writtenGlobal), which
// asks what the callee may write.
//
// **A function returning a sequence is refused any array or `shared` value's storage**
// (`lyra-E092`), the global's or a local's: `for v in each(ys[0]) { ys = [] }`. Its body runs
// between the consumer's iterations, so the consumer does what a global's other functions can.
// By place and not by what the consumer writes, since a closure or a second name would get past
// that. On 11/03 no `gen` in the workspace took `ref` or `mut` at all, so it cost nothing.
func CheckGlobalBorrows(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable, mt *typetable.MethodTable) []diag.Diagnostic {
	c := &globalBorrows{
		symTable: symTable,
		tt:       tt,
		mt:       mt,
		callees:  &useAfterMove{symTable: symTable, tt: tt, mt: mt},
	}
	c.writes = newGlobalWrites(c, program)
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
	// writes is what each function may write of the mutable globals (lyra-E098).
	writes *globalWrites
	out    []diag.Diagnostic
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
	lam := c.callees.resolveCallee(e)
	toSeq := lam != nil && (lam.IsGenerator || types.IsSeq(lam.ReturnType.Type))
	var lent []lentArg
	offset := 0
	if receiver != nil {
		offset = 1
		if len(modes) > 0 {
			c.argument(receiver, modes[0], 1, toSeq)
			lent = append(lent, lentArg{receiver, modes[0], 1})
		}
	}
	for i, arg := range e.Arguments {
		if i+offset < len(modes) {
			c.argument(arg, modes[i+offset], i+offset+1, toSeq)
			lent = append(lent, lentArg{arg, modes[i+offset], i + offset + 1})
		}
	}
	c.secondName(lent)
	c.writtenGlobal(e, lent)
}

// writtenGlobal refuses storage inside a box lent to a call that may write a mutable global
// able to hold that box (`lyra-E098`): `var zs = g; via(zs[0])`, where `via` pushes onto `g`.
// The two names are one array, so the push frees the buffer `zs[0]` points into. No walk of
// the caller shows it, and a refusal of every call that writes a global would take in calls
// lending what no global can be, so the callee's summary is asked by type (globalWrites,
// ownership.CanHold), as lyra-E097 asks the `mut` beside it. Storage a global's own path
// reaches is lyra-E091's already.
func (c *globalBorrows) writtenGlobal(e *ast.FunctionCallExpr, args []lentArg) {
	if c.writes == nil {
		return
	}
	var written []*ast.VarDeclStmt
	asked := false
	for _, a := range args {
		if !c.lends(a) || ownership.RootedAtMutableGlobal(a.expr, c.symTable, c.bound) {
			continue
		}
		box, reached := ownership.BoxReached(a.expr, c.tt, c.symTable)
		if !reached {
			continue
		}
		if !asked {
			written = c.writes.of(c.calleeBodies(e, true))
			asked = true
		}
		for _, decl := range written {
			t := c.globalType(decl)
			if t == nil || !ownership.CanHold(t, box, c.symTable, decl.GetLocation()) {
				continue
			}
			c.out = append(c.out, diag.Diagnostic{
				Location: a.expr.GetLocation(),
				Severity: diag.SeverityError,
				Code:     diag.CodeWrittenGlobalBorrow,
				Message: fmt.Sprintf(
					"argument %d lends storage inside an array or `shared` value, and this call can write the mutable global %q, which can hold that same array under another name (after `var zs = %s`, both are one array). Growing or replacing it during the call would free what argument %d points at. Copy argument %d into a local and pass that",
					a.position, decl.Name, decl.Name, a.position, a.position),
			})
			return
		}
	}
}

// globalType is a global's type as checked: its annotation, else its value's.
func (c *globalBorrows) globalType(decl *ast.VarDeclStmt) types.Type {
	if decl.Type != nil {
		return decl.Type
	}
	if t, ok := c.tt.Get(decl.Value); ok {
		return t
	}
	return nil
}

// lentArg is one argument of a call with the mode of the parameter it fills.
type lentArg struct {
	expr     ast.Expression
	mode     types.TypeModifier
	position int
}

// secondName refuses storage inside a box lent beside a `mut` argument that can hold a box
// (`lyra-E097`): `via(ys[0], zs)` after `var zs = ys`, where `via` grows `zs` and frees the
// buffer `ys[0]` points into. Exclusivity by place can't see it, as `ys` and `zs` are two
// names for one box, so the pair is refused when the `mut` argument's type can hold the box the
// lent storage is in — a `mut Fonts` beside `w.host`, with `w` a `shared Running`, can't.
// What's lent is a by-reference argument: a retain can't save it, as growth frees the buffer
// and keeps the box. A bare one holding a managed value is a value, which the ownership pass
// holds for the call instead (holdBareBesideMut).
//
// A pair one path of which contains the other is the typechecker's (`f(ys[0], ys)`, by place),
// and a global's storage is lyra-E091's already.
func (c *globalBorrows) secondName(args []lentArg) {
	for _, a := range args {
		if !c.lends(a) || ownership.RootedAtMutableGlobal(a.expr, c.symTable, c.bound) {
			continue
		}
		box, reached := ownership.BoxReached(a.expr, c.tt, c.symTable)
		if !reached {
			continue
		}
		for _, b := range args {
			if b.position == a.position || b.mode != types.Mut || !c.canHold(b.expr, box) ||
				pathContains(b.expr, a.expr) {
				continue
			}
			c.out = append(c.out, diag.Diagnostic{
				Location: a.expr.GetLocation(),
				Severity: diag.SeverityError,
				Code:     diag.CodeSecondNameBorrow,
				Message: fmt.Sprintf(
					"argument %d lends storage inside an array or `shared` value, and argument %d is `mut` and can hold that same array under another name (after `var zs = ys`, both are one array). Growing or rewriting it in the callee would free what argument %d points at. Copy argument %d into a local and pass that",
					a.position, b.position, a.position, a.position),
			})
			return
		}
	}
}

// lends reports whether the callee is lent this argument's storage, by pointer.
func (c *globalBorrows) lends(a lentArg) bool {
	return c.byReference(a.expr, a.mode)
}

// canHold reports whether the argument's recorded type can hold a box of type box: whether it
// can be another name for it (ownership.CanHold).
func (c *globalBorrows) canHold(e ast.Expression, box types.Type) bool {
	t, recorded := c.tt.Get(e)
	return recorded && ownership.CanHold(t, box, c.symTable, e.GetLocation())
}

// pathContains reports whether the place outer is inner or one of the places inner's path
// passes through (`ys` contains `ys[0]`, `s.items` contains `s.items[i].name`), compared as
// written; an index is any index, as exclusivity by place treats it.
func pathContains(outer, inner ast.Expression) bool {
	want := placeSteps(outer)
	if want == nil {
		return false
	}
	for x, ok := inner, true; ok; x, ok = ast.PlaceObject(x) {
		if got := placeSteps(x); got != nil && slices.Equal(got, want) {
			return true
		}
	}
	return false
}

// placeSteps spells a place path root first, or nil for what isn't one.
func placeSteps(e ast.Expression) []string {
	var reversed []string
	for {
		switch x := e.(type) {
		case *ast.IdentifierExpr:
			steps := append([]string{x.Name}, make([]string, 0, len(reversed))...)
			for i := len(reversed) - 1; i >= 0; i-- {
				steps = append(steps, reversed[i])
			}
			return steps
		case *ast.MemberExpr:
			reversed = append(reversed, "."+x.Property.Name)
		case *ast.TupleIndexExpr:
			reversed = append(reversed, "."+strconv.Itoa(x.Index))
		case *ast.IndexExpr:
			reversed = append(reversed, "[]")
		default:
			return nil
		}
		object, ok := ast.PlaceObject(e)
		if !ok {
			return nil
		}
		e = object
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

func (c *globalBorrows) argument(arg ast.Expression, mode types.TypeModifier, position int, toSeq bool) {
	if !c.byReference(arg, mode) || !ownership.ReachedThroughBox(arg, c.tt, c.symTable) {
		return
	}
	switch {
	case ownership.RootedAtMutableGlobal(arg, c.symTable, c.bound):
		c.out = append(c.out, diag.Diagnostic{
			Location: arg.GetLocation(),
			Severity: diag.SeverityError,
			Code:     diag.CodeGlobalBorrow,
			Message: fmt.Sprintf(
				"argument %d is passed by `%s` into an array or `shared` value a mutable global holds, and any function can reassign that global or grow the array while the callee holds the borrow, which would free the storage under it. Copy it into a local, pass that, and assign it back",
				position, mode),
		})
	case toSeq:
		c.out = append(c.out, diag.Diagnostic{
			Location: arg.GetLocation(),
			Severity: diag.SeverityError,
			Code:     diag.CodeSeqBorrow,
			Message: fmt.Sprintf(
				"argument %d is passed by `%s` into an array or `shared` value, for a function returning a sequence. Its body runs between the consumer's iterations, and the consumer can reassign that array or grow it, which frees the storage under it. Copy it into a local and pass that, or take it by value",
				position, mode),
		})
	}
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
