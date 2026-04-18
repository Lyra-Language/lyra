package ast

import "fmt"

type ModuleDeclStmt struct {
	AstBase
	Path []ModuleName
}

func (m *ModuleDeclStmt) statementNode() {}

func (m *ModuleDeclStmt) GetName() string { return "module" }

func (m *ModuleDeclStmt) Print(indent string) {
	fmt.Printf("%sModuleDeclStmt {\n", indent)
	for _, name := range m.Path {
		name.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}

type ModuleName struct {
	Name string
}

func (m *ModuleName) Print(indent string) {
	fmt.Printf("%sModuleName(%s)\n", indent, m.Name)
}