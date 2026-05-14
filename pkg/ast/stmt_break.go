package ast

type BreakStmt struct {
	AstBase
	Label string     // Optional label for breaking out of labeled loops
	Value Expression // Optional value to return from the loop
}

func (b *BreakStmt) statementNode() {}

func (b *BreakStmt) GetName() string {
	if b.Label == "" {
		return "break"
	}
	return "break " + b.Label
}
