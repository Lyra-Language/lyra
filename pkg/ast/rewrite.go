package ast

// RewriteStmt applies rewrite to every expression stmt holds, replacing each slot with
// what rewrite returns, and recurses through the statements and expressions beneath it.
//
// **This is the writing half of walk.go**, and it exists for the same reason WalkStmt
// does: a pass that *replaces* nodes rather than reading them cannot use a visitor, so
// before this it hand-copied walkStmtChildren — and the copy fell behind by three node
// kinds without anything failing. rewriteStmtChildren and rewriteExprChildren are
// registered as mirrors in exhaustive_test.go, so they now fall behind at the edit.
//
// Rewriting is **post-order**: the children of a node are rewritten before rewrite is
// applied to the node itself, so a callback that rebuilds a node sees its final children.
// rewrite is never called with nil.
func RewriteStmt(stmt Statement, rewrite func(Expression) Expression) {
	if stmt == nil {
		return
	}
	rewriteStmtChildren(stmt, rewrite)
}

// RewriteExpr rewrites everything inside expr, then applies rewrite to expr itself, and
// returns the replacement — which the caller must store back into whatever slot held expr.
// Returns nil for a nil expr, so it is safe on optional slots.
func RewriteExpr(expr Expression, rewrite func(Expression) Expression) Expression {
	if expr == nil {
		return nil
	}
	rewriteExprChildren(expr, rewrite)
	return rewrite(expr)
}

// rewriteBlock rewrites a block's statements. Nil-safe: the optional blocks
// (IfDestructuringStmt.Else, a WithStmt with no body) come through here.
func rewriteBlock(b *BlockExpr, rewrite func(Expression) Expression) {
	if b == nil {
		return
	}
	for i := range b.Statements {
		RewriteStmt(b.Statements[i], rewrite)
	}
}

// rewriteIdentSlot rewrites a slot whose static type is *IdentifierExpr rather than
// Expression, storing the result back only if it is still an identifier.
//
// **The narrow slots are the one place a rewrite can be dropped**, and there are three:
// MathAssignOpExpr.Left and the two BaseStruct fields (a record-update target). Each is
// a binding's *name*, not an arbitrary expression — the grammar admits nothing else
// there — so a callback that wants to turn one into some other node kind is asking for
// something the AST cannot represent, and silently keeping the identifier is the only
// available answer. Callers that care must check these slots themselves.
func rewriteIdentSlot(slot **IdentifierExpr, rewrite func(Expression) Expression) {
	if *slot == nil {
		return
	}
	if out, ok := RewriteExpr(*slot, rewrite).(*IdentifierExpr); ok {
		*slot = out
	}
}

func rewriteStmtChildren(stmt Statement, rewrite func(Expression) Expression) {
	switch s := stmt.(type) {
	case *VarDeclStmt:
		s.Value = RewriteExpr(s.Value, rewrite)
	case *DestructuringDeclStmt:
		s.Value = RewriteExpr(s.Value, rewrite)
	case *ExpressionStmt:
		s.Expression = RewriteExpr(s.Expression, rewrite)
	case *VarReassignmentStmt:
		s.Value = RewriteExpr(s.Value, rewrite)
	case *DerefAssignmentStmt:
		s.Target.Operand = RewriteExpr(s.Target.Operand, rewrite)
		s.Value = RewriteExpr(s.Value, rewrite)
	case *LValueAssignmentStmt:
		s.Target = RewriteExpr(s.Target, rewrite)
		s.Value = RewriteExpr(s.Value, rewrite)
	case *ReturnStmt:
		s.Value = RewriteExpr(s.Value, rewrite)
	case *BreakStmt:
		s.Value = RewriteExpr(s.Value, rewrite)
	case *WithStmt:
		s.Arena = RewriteExpr(s.Arena, rewrite)
		rewriteBlock(s.Body, rewrite)
	case *IfDestructuringStmt:
		s.DestructuringStatement.Value = RewriteExpr(s.DestructuringStatement.Value, rewrite)
		rewriteBlock(s.Then, rewrite)
		rewriteBlock(s.Else, rewrite)
	case *ElseDestructuringStmt:
		s.DestructuringStatement.Value = RewriteExpr(s.DestructuringStatement.Value, rewrite)
		rewriteBlock(s.Else, rewrite)
	case *TraitImplStmt:
		for i := range s.Methods {
			s.Methods[i].Clause.Body = RewriteExpr(s.Methods[i].Clause.Body, rewrite)
		}
	case *TraitDeclStmt:
		for i := range s.Methods {
			if s.Methods[i].DefaultMethod != nil {
				s.Methods[i].DefaultMethod.Body = RewriteExpr(s.Methods[i].DefaultMethod.Body, rewrite)
			}
		}
		// TypeDeclStmt, ImportStmt, ContinueStmt, ModuleDeclStmt: no expression children.
	}
}

func rewriteExprChildren(expr Expression, rewrite func(Expression) Expression) {
	switch e := expr.(type) {
	case *BlockExpr:
		rewriteBlock(e, rewrite)
	case *IfExpr:
		e.Condition = RewriteExpr(e.Condition, rewrite)
		e.Then = RewriteExpr(e.Then, rewrite)
		e.Else = RewriteExpr(e.Else, rewrite)
	case *MatchExpr:
		e.Scrutinee = RewriteExpr(e.Scrutinee, rewrite)
		for i := range e.MatchArms {
			if e.MatchArms[i].Guard != nil {
				e.MatchArms[i].Guard.Condition = RewriteExpr(e.MatchArms[i].Guard.Condition, rewrite)
			}
			e.MatchArms[i].Body = RewriteExpr(e.MatchArms[i].Body, rewrite)
		}
	case *LambdaExpr:
		for i := range e.Parameters {
			e.Parameters[i].DefaultValue = RewriteExpr(e.Parameters[i].DefaultValue, rewrite)
		}
		e.Body = RewriteExpr(e.Body, rewrite)
		for i := range e.LambdaClauses {
			e.LambdaClauses[i].Body = RewriteExpr(e.LambdaClauses[i].Body, rewrite)
		}
	case *ForLoopExpr:
		if e.Init != nil {
			e.Init.Value = RewriteExpr(e.Init.Value, rewrite)
		}
		if e.Condition != nil {
			*e.Condition = RewriteExpr(*e.Condition, rewrite)
		}
		if e.Post != nil {
			*e.Post = RewriteExpr(*e.Post, rewrite)
		}
		rewriteBlock(e.Body, rewrite)
	case *ForInLoopExpr:
		e.Iterable = RewriteExpr(e.Iterable, rewrite)
		rewriteBlock(e.Body, rewrite)
	case *FunctionCallExpr:
		e.Function = RewriteExpr(e.Function, rewrite)
		for i := range e.Arguments {
			e.Arguments[i] = RewriteExpr(e.Arguments[i], rewrite)
		}
	case *MemberExpr:
		e.Object = RewriteExpr(e.Object, rewrite)
	case *TupleIndexExpr:
		e.Object = RewriteExpr(e.Object, rewrite)
	case *IndexExpr:
		e.Object = RewriteExpr(e.Object, rewrite)
		e.Index = RewriteExpr(e.Index, rewrite)
	case *TryExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *MathBinaryOpExpr:
		e.Left = RewriteExpr(e.Left, rewrite)
		e.Right = RewriteExpr(e.Right, rewrite)
	case *MathAssignOpExpr:
		// Left is an IdentifierExpr *value*, so it is rewritten through its address
		// and stored back only as an identifier — see rewriteIdentSlot.
		if out, ok := RewriteExpr(&e.Left, rewrite).(*IdentifierExpr); ok {
			e.Left = *out
		}
		e.Right = RewriteExpr(e.Right, rewrite)
	case *BooleanBinaryOpExpr:
		e.Left = RewriteExpr(e.Left, rewrite)
		e.Right = RewriteExpr(e.Right, rewrite)
	case *NotBooleanExpr:
		e.Expression = RewriteExpr(e.Expression, rewrite)
	case *NegationExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *BitwiseNotExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *AwaitExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *AddressOfExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *DerefExpr:
		e.Operand = RewriteExpr(e.Operand, rewrite)
	case *ArrayLiteralExpr:
		for i := range e.Elements {
			e.Elements[i] = RewriteExpr(e.Elements[i], rewrite)
		}
	case *TupleLiteralExpr:
		for i := range e.Elements {
			e.Elements[i] = RewriteExpr(e.Elements[i], rewrite)
		}
	case *InterpolatedStringExpr:
		for i := range e.Segments {
			e.Segments[i] = RewriteExpr(e.Segments[i], rewrite)
		}
	case *StructInstanceExpr:
		rewriteIdentSlot(&e.BaseStruct, rewrite)
		for i := range e.Fields {
			e.Fields[i].Value = RewriteExpr(e.Fields[i].Value, rewrite)
		}
	case *AnonymousStructInstanceExpr:
		rewriteIdentSlot(&e.BaseStruct, rewrite)
		for i := range e.Fields {
			e.Fields[i].Value = RewriteExpr(e.Fields[i].Value, rewrite)
		}
	case *ArrayCompExpr:
		for i := range e.Generators {
			e.Generators[i].Value = RewriteExpr(e.Generators[i].Value, rewrite)
		}
		for i := range e.Guards {
			e.Guards[i] = RewriteExpr(e.Guards[i], rewrite)
		}
		e.Result = RewriteExpr(e.Result, rewrite)
	case *RangeExpr:
		e.Start = RewriteExpr(e.Start, rewrite)
		e.End = RewriteExpr(e.End, rewrite)
		e.Step = RewriteExpr(e.Step, rewrite)
	case *NullCoalescingExpr:
		e.Optional = RewriteExpr(e.Optional, rewrite)
		e.Default = RewriteExpr(e.Default, rewrite)
	case *StringConcatExpr:
		e.Left = RewriteExpr(e.Left, rewrite)
		e.Right = RewriteExpr(e.Right, rewrite)
	case *ArrayRepeatExpr:
		e.Value = RewriteExpr(e.Value, rewrite)
		e.Count = RewriteExpr(e.Count, rewrite)
	case *ComposeExpr:
		e.Left = RewriteExpr(e.Left, rewrite)
		e.Right = RewriteExpr(e.Right, rewrite)
	case *YieldExpr:
		e.Value = RewriteExpr(e.Value, rewrite)
	case *YieldFromExpr:
		e.Generator = RewriteExpr(e.Generator, rewrite)
	case *UnsafeBlockExpr:
		rewriteBlock(e.Body, rewrite)
	case *DataConstructorExpr:
		e.Value = RewriteExpr(e.Value, rewrite)
	case *GuardExpr:
		e.Condition = RewriteExpr(e.Condition, rewrite)
	case *SpreadExpr:
		e.Value = RewriteExpr(e.Value, rewrite)
		// Leaf nodes: IdentifierExpr, all literal types, SizeofExpr — no children.
	}
}
