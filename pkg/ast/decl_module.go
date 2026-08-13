package ast

type ModuleDeclStmt struct {
	AstBase
	Path []ModuleName
	// Doc is the `//!` block at the top of the file, nil if there is none.
	//
	// It is `//!` rather than a `///` above the `module` line because a module is a
	// file **or a directory**: a directory module has no single header to sit above,
	// and its several files each want to say something about the module they join.
	// Docs from every file of a multi-file module are concatenated in file order.
	Doc *Doc
}

func (m *ModuleDeclStmt) statementNode() {}

func (m *ModuleDeclStmt) GetName() string { return "module" }

type ModuleName struct {
	Name string
}
