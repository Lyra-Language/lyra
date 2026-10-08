package declarations

import (
	"slices"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// applyInherentReceiver makes one member of `impl Type { … }` the top-level method it
// stands for: `let say_hi = (self) => …` becomes `let say_hi = (self: Type) => …`, and
// `(self: mut)` becomes `(self: mut Type)`. It returns the member's generic list, extended
// with the target's type variables when the member wrote a list of its own.
//
// It runs inside the declaration collector, before the binding is registered, because
// receiver-keyed overloading reads the `self` type at registration. Everything after the
// collector sees an ordinary declaration — the block leaves no trace in the AST.
//
// What cannot be desugared into a method is refused (lyra-E089) rather than hoisted as
// written: a `var`, a non-function, and a function with no `self` would each still compile
// as a top-level declaration, so a block that quietly admitted them would hold things that
// are not methods of the type it names.
func applyInherentReceiver(
	node, nameNode *sitter.Node,
	kind ast.BindingKind,
	isMut bool,
	value ast.Expression,
	generics []ast.GenericParam,
	target *collector_ctx.InherentTarget,
	ctx *collector_ctx.Ctx,
) []ast.GenericParam {
	name := ctx.NodeText(nameNode)
	if kind != ast.BindingLet || isMut {
		ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeInherentMember,
			"`%s` in `impl %s` must be a plain `let`: a method is a function binding, and the block declares nothing else",
			name, target.Text)
		return generics
	}
	lambda, ok := value.(*ast.LambdaExpr)
	if !ok {
		ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeInherentMember,
			"`%s` in `impl %s` is not a function; declare it outside the block", name, target.Text)
		return generics
	}
	if len(lambda.Parameters) == 0 || lambda.Parameters[0].GetName() != "self" {
		ctx.AddErrorCoded(nameNode, diag.SeverityError, diag.CodeInherentMember,
			"`%s` in `impl %s` takes no `self`, so it is not a method of %s; declare it outside the block (a constructor is a bare function, e.g. `let new_thing = …`)",
			name, target.Text, target.Text)
		return generics
	}
	self := &lambda.Parameters[0]
	if self.Type != nil {
		ctx.AddErrorCoded(nameNode, diag.SeverityError, diag.CodeInherentMember,
			"`self` in `impl %s` takes its type from the block; write `self`, `self: mut` or `self: ref`",
			target.Text)
		return generics
	}
	// The parameter is registered in the lambda's scope by pointer into this slice, so the
	// scope sees the type too.
	self.Type = target.Type

	// `impl Box<t>` has no written list, its variables being lexical as a trait impl's are.
	// A member with no list of its own needs none; one that writes a list makes it
	// authoritative (lyra-E031), so the target's variables are appended — after the
	// member's own, so a turbofish binds what the member wrote, in the order it wrote it.
	if len(generics) > 0 {
		for _, v := range types.AppendTypeVars(target.Type, nil) {
			if !slices.ContainsFunc(generics, func(g ast.GenericParam) bool { return g.Name == v }) {
				generics = append(generics, ast.GenericParam{Name: v, Location: ctx.NodeLocation(nameNode)})
			}
		}
	}
	if len(target.Bounds) == 0 {
		return generics
	}
	// The block's `where` reaches a body only through the member's list, which is what puts
	// a bound in scope — so a member that wrote none is given the one it implies: every
	// variable its signature mentions, in the order written (`self`'s first). Without all
	// of them the list would be authoritative and refuse the rest.
	if len(generics) == 0 {
		var names []string
		for i := range lambda.Parameters {
			names = types.AppendTypeVars(lambda.Parameters[i].Type, names)
		}
		names = types.AppendTypeVars(lambda.ReturnType.Type, names)
		for _, v := range names {
			generics = append(generics, ast.GenericParam{Name: v, Location: ctx.NodeLocation(nameNode)})
		}
	}
	for _, b := range target.Bounds {
		at := slices.IndexFunc(generics, func(g ast.GenericParam) bool { return g.Name == b.Name })
		if at < 0 {
			continue // the target's variables are all in the list by now
		}
		for _, trait := range b.Constraints {
			if !slices.Contains(generics[at].Constraints, trait) {
				generics[at].Constraints = append(generics[at].Constraints, trait)
			}
		}
	}
	return generics
}
