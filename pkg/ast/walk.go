package ast

// WalkStmt visits stmt and, if onStmt(stmt) returns true, recurses into all
// child statements and expressions using the same callbacks.
// Passing nil for onStmt or onExpr is treated as always returning true.
func WalkStmt(stmt Statement, onStmt func(Statement) bool, onExpr func(Expression) bool) {
	if stmt == nil {
		return
	}
	if onStmt != nil && !onStmt(stmt) {
		return
	}
	walkStmtChildren(stmt, onStmt, onExpr)
}

// WalkExpr visits expr and, if onExpr(expr) returns true, recurses into all
// child expressions and statements using the same callbacks.
// Passing nil for onStmt or onExpr is treated as always returning true.
func WalkExpr(expr Expression, onStmt func(Statement) bool, onExpr func(Expression) bool) {
	if expr == nil {
		return
	}
	if onExpr != nil && !onExpr(expr) {
		return
	}
	walkExprChildren(expr, onStmt, onExpr)
}

func walkStmtChildren(stmt Statement, onStmt func(Statement) bool, onExpr func(Expression) bool) {
	switch s := stmt.(type) {
	case *VarDeclStmt:
		WalkExpr(s.Value, onStmt, onExpr)
	case *DestructuringDeclStmt:
		WalkExpr(s.Value, onStmt, onExpr)
	case *ExpressionStmt:
		WalkExpr(s.Expression, onStmt, onExpr)
	case *VarReassignmentStmt:
		WalkExpr(s.Value, onStmt, onExpr)
	case *DerefAssignmentStmt:
		WalkExpr(s.Target.Operand, onStmt, onExpr)
		WalkExpr(s.Value, onStmt, onExpr)
	case *LValueAssignmentStmt:
		WalkExpr(s.Target, onStmt, onExpr)
		WalkExpr(s.Value, onStmt, onExpr)
	case *ReturnStmt:
		WalkExpr(s.Value, onStmt, onExpr)
	case *BreakStmt:
		WalkExpr(s.Value, onStmt, onExpr)
	case *WithStmt:
		WalkExpr(s.Arena, onStmt, onExpr)
		for _, inner := range s.Body.Statements {
			WalkStmt(inner, onStmt, onExpr)
		}
	case *IfDestructuringStmt:
		WalkExpr(s.DestructuringStatement.Value, onStmt, onExpr)
		for _, inner := range s.Then.Statements {
			WalkStmt(inner, onStmt, onExpr)
		}
		if s.Else != nil {
			for _, inner := range s.Else.Statements {
				WalkStmt(inner, onStmt, onExpr)
			}
		}
	case *ElseDestructuringStmt:
		WalkExpr(s.DestructuringStatement.Value, onStmt, onExpr)
		for _, inner := range s.Else.Statements {
			WalkStmt(inner, onStmt, onExpr)
		}
	case *TraitImplStmt:
		for i := range s.Methods {
			WalkExpr(s.Methods[i].Clause.Body, onStmt, onExpr)
		}
	case *TraitDeclStmt:
		for i := range s.Methods {
			if s.Methods[i].DefaultMethod != nil {
				WalkExpr(s.Methods[i].DefaultMethod.Body, onStmt, onExpr)
			}
		}
		// TypeDeclStmt, ImportStmt, ContinueStmt, ModuleDeclStmt: no expression children.
	}
}

func walkExprChildren(expr Expression, onStmt func(Statement) bool, onExpr func(Expression) bool) {
	switch e := expr.(type) {
	case *BlockExpr:
		for _, stmt := range e.Statements {
			WalkStmt(stmt, onStmt, onExpr)
		}
	case *IfExpr:
		WalkExpr(e.Condition, onStmt, onExpr)
		WalkExpr(e.Then, onStmt, onExpr)
		WalkExpr(e.Else, onStmt, onExpr)
	case *MatchExpr:
		WalkExpr(e.Scrutinee, onStmt, onExpr)
		for _, arm := range e.MatchArms {
			if arm.Guard != nil {
				WalkExpr(arm.Guard.Condition, onStmt, onExpr)
			}
			WalkExpr(arm.Body, onStmt, onExpr)
		}
	case *LambdaExpr:
		for i := range e.Parameters {
			WalkExpr(e.Parameters[i].DefaultValue, onStmt, onExpr)
		}
		WalkExpr(e.Body, onStmt, onExpr)
		for _, clause := range e.LambdaClauses {
			WalkExpr(clause.Body, onStmt, onExpr)
		}
	case *ForLoopExpr:
		if e.Init != nil {
			WalkExpr(e.Init.Value, onStmt, onExpr)
		}
		if e.Condition != nil {
			WalkExpr(*e.Condition, onStmt, onExpr)
		}
		if e.Post != nil {
			WalkExpr(*e.Post, onStmt, onExpr)
		}
		for _, stmt := range e.Body.Statements {
			WalkStmt(stmt, onStmt, onExpr)
		}
	case *ForInLoopExpr:
		WalkExpr(e.Iterable, onStmt, onExpr)
		for _, stmt := range e.Body.Statements {
			WalkStmt(stmt, onStmt, onExpr)
		}
	case *FunctionCallExpr:
		WalkExpr(e.Function, onStmt, onExpr)
		for _, arg := range e.Arguments {
			WalkExpr(arg, onStmt, onExpr)
		}
	case *MemberExpr:
		WalkExpr(e.Object, onStmt, onExpr)
	case *IndexExpr:
		WalkExpr(e.Object, onStmt, onExpr)
		WalkExpr(e.Index, onStmt, onExpr)
	case *TryExpr:
		WalkExpr(e.Operand, onStmt, onExpr)
	case *MathBinaryOpExpr:
		WalkExpr(e.Left, onStmt, onExpr)
		WalkExpr(e.Right, onStmt, onExpr)
	case *MathAssignOpExpr:
		WalkExpr(&e.Left, onStmt, onExpr)
		WalkExpr(e.Right, onStmt, onExpr)
	case *BooleanBinaryOpExpr:
		WalkExpr(e.Left, onStmt, onExpr)
		WalkExpr(e.Right, onStmt, onExpr)
	case *NotBooleanExpr:
		WalkExpr(e.Expression, onStmt, onExpr)
	case *NegationExpr:
		WalkExpr(e.Operand, onStmt, onExpr)
	case *AwaitExpr:
		WalkExpr(e.Operand, onStmt, onExpr)
	case *AddressOfExpr:
		WalkExpr(e.Operand, onStmt, onExpr)
	case *DerefExpr:
		WalkExpr(e.Operand, onStmt, onExpr)
	case *ArrayLiteralExpr:
		for _, el := range e.Elements {
			WalkExpr(el, onStmt, onExpr)
		}
	case *TupleLiteralExpr:
		for _, el := range e.Elements {
			WalkExpr(el, onStmt, onExpr)
		}
	case *InterpolatedStringExpr:
		for _, seg := range e.Segments {
			WalkExpr(seg, onStmt, onExpr)
		}
	case *StructInstanceExpr:
		if e.BaseStruct != nil {
			WalkExpr(e.BaseStruct, onStmt, onExpr)
		}
		for _, f := range e.Fields {
			WalkExpr(f.Value, onStmt, onExpr)
		}
	case *AnonymousStructInstanceExpr:
		if e.BaseStruct != nil {
			WalkExpr(e.BaseStruct, onStmt, onExpr)
		}
		for _, f := range e.Fields {
			WalkExpr(f.Value, onStmt, onExpr)
		}
	case *ArrayCompExpr:
		for _, gen := range e.Generators {
			WalkExpr(gen.Value, onStmt, onExpr)
		}
		for _, g := range e.Guards {
			WalkExpr(g, onStmt, onExpr)
		}
		WalkExpr(e.Result, onStmt, onExpr)
	case *RangeExpr:
		WalkExpr(e.Start, onStmt, onExpr)
		WalkExpr(e.End, onStmt, onExpr)
		WalkExpr(e.Step, onStmt, onExpr)
	case *NullCoalescingExpr:
		WalkExpr(e.Optional, onStmt, onExpr)
		WalkExpr(e.Default, onStmt, onExpr)
	case *StringConcatExpr:
		WalkExpr(e.Left, onStmt, onExpr)
		WalkExpr(e.Right, onStmt, onExpr)
	case *ArrayRepeatExpr:
		WalkExpr(e.Value, onStmt, onExpr)
		WalkExpr(e.Count, onStmt, onExpr)
	case *ComposeExpr:
		WalkExpr(e.Left, onStmt, onExpr)
		WalkExpr(e.Right, onStmt, onExpr)
	case *YieldExpr:
		WalkExpr(e.Value, onStmt, onExpr)
	case *YieldFromExpr:
		WalkExpr(e.Generator, onStmt, onExpr)
	case *UnsafeBlockExpr:
		for _, stmt := range e.Body.Statements {
			WalkStmt(stmt, onStmt, onExpr)
		}
	case *GivenExpr:
		for _, bstmt := range e.Bindings {
			WalkStmt(bstmt, onStmt, onExpr)
		}
		WalkExpr(e.Body, onStmt, onExpr)
	case *DataConstructorExpr:
		WalkExpr(e.Value, onStmt, onExpr)
	case *GuardExpr:
		WalkExpr(e.Condition, onStmt, onExpr)
		// Leaf nodes: IdentifierExpr, SpreadExpr, all literal types, SizeofExpr — no children.
	}
}
