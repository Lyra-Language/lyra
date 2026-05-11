package ast

type BreakStmt struct {
	AstBase
	Label string // Optional label for breaking out of labeled loops
}

func (b *BreakStmt) statementNode() {}

func (b *BreakStmt) GetName() string {
	if b.Label == "" {
		return "break"
	}
	return "break " + b.Label
}
