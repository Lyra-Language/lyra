package ast

type ImportStmt struct {
	AstBase
	Path []ModuleName
	Alias string
	Members []ImportMember
}

func (i *ImportStmt) statementNode() {}

func (i *ImportStmt) GetName() string { return "import" }

type ImportMember struct {
	Name     string
	Alias    string
	Location Location
}
