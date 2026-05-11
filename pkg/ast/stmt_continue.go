package ast

type ContinueStmt struct {
	AstBase
	Label string // Optional label for labeled continue
}

func (s *ContinueStmt) statementNode() {}

func (b *ContinueStmt) GetName() string {
	if b.Label == "" {
		return "continue"
	}
	return "continue " + b.Label
}
