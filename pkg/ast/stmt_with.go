package ast

// WithStmt is `with <name> = <arena> { … }` — arena allocation, which is not
// implemented (lyra-E050 refuses it).
//
// Body is a **pointer**, as every other statement holding a block is
// (IfDestructuringStmt.Then, etc.). It was a by-value BlockExpr until 08/13, and
// that was load-bearing in the wrong direction: the ScopeTable is keyed by node
// pointer, so `&w.Body` — the address of the struct's own copy — never matched
// the `*BlockExpr` the collector recorded a scope for. The typechecker could not
// check the body without reporting every name declared inside it as undefined,
// so it did not check it at all, so a `shared` construction in a `with` body was
// never even type-recorded — which is the second reason `noalloc` did not see it,
// underneath the arena discharge that was the first.
type WithStmt struct {
	AstBase
	Name  string
	Arena Expression
	Body  *BlockExpr
}

func (w *WithStmt) statementNode()  {}
func (w *WithStmt) GetName() string { return "with_statement" }
