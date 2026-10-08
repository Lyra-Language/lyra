package collector

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectInherentImpl expands `impl Person { let say_hi = (self) => … }` into the top-level
// declarations it is sugar for, `let say_hi = (self: Person) => …`, one per member.
//
// **The block is erased here.** A method in Lyra is a top-level function whose first
// parameter is `self` — callable as `p.say_hi()` or `say_hi(p)`, overloaded by receiver,
// public by `pub`, a candidate wherever its module is reachable — and the block changes
// none of that, so nothing past the collector needs to know it was written. The type is
// filled into `self` by the declaration collector (declarations/inherent_member.go), which
// also refuses what is not a method.
//
// Each member is documented by its own `///`, as a top-level `let` is.
func (c *Collector) collectInherentImpl(node *sitter.Node) []ast.Statement {
	typeNode := cst.Field(node, "type")
	if typeNode == nil {
		return nil
	}
	target := &collector_ctx.InherentTarget{
		Type: c.parseType(typeNode),
		Text: c.ctx.NodeText(typeNode),
	}
	if whereNode := cst.Field(node, "generic_parameter_constraints"); whereNode != nil {
		target.Bounds = c.collectInherentBounds(whereNode, target)
	}
	members := cst.Field(node, "members")
	if members == nil {
		return nil
	}
	var out []ast.Statement
	for i := uint(0); i < members.NamedChildCount(); i++ {
		member := members.NamedChild(i)
		if member.Kind() != "declaration" {
			continue // a comment
		}
		// The pattern arm: a destructuring, or a SCREAMING_CASE name (which lexes as a
		// constant and so never reaches the identifier arm). Neither is a method.
		if pattern := cst.Field(member, "pattern"); pattern != nil {
			c.ctx.AddErrorCoded(pattern, diag.SeverityError, diag.CodeInherentMember,
				"`%s` in `impl %s` is not a method; declare it outside the block", c.ctx.NodeText(pattern), target.Text)
			continue
		}
		c.ctx.InherentTarget = target
		stmt := c.CollectStatement(member)
		c.ctx.InherentTarget = nil
		if stmt == nil {
			continue
		}
		c.attachDoc(member, stmt)
		out = append(out, stmt)
	}
	return out
}

// refuseNestedInherentImpl reports an `impl` block below the top level. Its members would
// be locals, and a local is never a method.
func (c *Collector) refuseNestedInherentImpl(node *sitter.Node) {
	text := "Type"
	if t := cst.Field(node, "type"); t != nil {
		text = c.ctx.NodeText(t)
	}
	c.ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeInherentMember,
		"`impl %s { … }` must be at the top level: its members are top-level methods", text)
}

// collectInherentBounds reads `impl t where t: Ord, u: Show { … }`: the bounds a block puts
// on its target's variables, which every member takes as if it had written them in its own
// `where`. A bound on a variable the target does not mention is refused (lyra-E031): no
// member's `self` would carry it, so it would bound nothing.
func (c *Collector) collectInherentBounds(whereNode *sitter.Node, target *collector_ctx.InherentTarget) []ast.GenericParam {
	vars := map[string]bool{}
	types.CollectTypeVars(target.Type, vars)
	var bounds []ast.GenericParam
	index := map[string]int{}
	for i := uint(0); i < whereNode.NamedChildCount(); i++ {
		child := whereNode.NamedChild(i)
		if child.Kind() != "generic_parameter_constraint" {
			continue
		}
		nameNode := cst.Field(child, "generic_type")
		boundsNode := cst.Field(child, "generic_bounds")
		if nameNode == nil || boundsNode == nil {
			continue
		}
		name := c.ctx.NodeText(nameNode)
		if !vars[name] {
			c.ctx.AddErrorCoded(nameNode, diag.SeverityError, diag.CodeUndeclaredTypeVariable,
				"`%s` is not a type variable of `impl %s`, so this bound applies to nothing; bound it on the member that uses it",
				name, target.Text)
			continue
		}
		at, seen := index[name]
		if !seen {
			at = len(bounds)
			index[name] = at
			bounds = append(bounds, ast.GenericParam{Name: name, Location: c.ctx.NodeLocation(child)})
		}
		bounds[at].Constraints = append(bounds[at].Constraints, c.CollectBounds(boundsNode)...)
	}
	return bounds
}
