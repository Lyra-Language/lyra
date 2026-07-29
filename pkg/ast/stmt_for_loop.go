package ast

// Both loop forms hold their body as a **pointer**, not a value, so the
// *BlockExpr identity the collector recorded in the ScopeTable survives into the
// AST — the same reason IfDestructuringStmt.Then/Else are pointers.
//
// A value field copies the block, and the copy has a different address than the
// one the scope was keyed on. Every consumer that recovers a block's scope by
// pointer (the typechecker's enterScope above all) then missed, and enterScope's
// miss path is silent: it runs the body in the *enclosing* scope. The visible
// effect was that a `let` declared inside any loop body was invisible there
// ("undefined identifier"), because the collector had defined it in the body's
// own block scope, which nothing could reach.
type ForLoopExpr struct {
	ExprBase
	Label     string
	Init      *VarDeclStmt
	Condition *Expression
	Post      *Expression
	Body      *BlockExpr
}

func (t *ForLoopExpr) GetName() string { return "for_loop" }

type ForInLoopExpr struct {
	ExprBase
	Label    string
	Key      string
	Value    string
	Iterable Expression
	Body     *BlockExpr
}

func (t *ForInLoopExpr) GetName() string { return "for_in_loop" }
