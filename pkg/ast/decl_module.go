package ast

type ModuleDeclStmt struct {
	AstBase
	Path []ModuleName
}

func (m *ModuleDeclStmt) statementNode() {}

func (m *ModuleDeclStmt) GetName() string { return "module" }

type ModuleName struct {
	Name string
}
