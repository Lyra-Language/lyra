package ast

import (
	"fmt"
)

type ImportStmt struct {
	AstBase
	Path []ModuleName
	Alias string
	Members []ImportMember
}

func (i *ImportStmt) statementNode() {}

func (i *ImportStmt) GetName() string { return "import" }

func (i *ImportStmt) Print(indent string) {
	fmt.Printf("%sImportStmt {\n", indent)
	fmt.Printf("%s\tPath: {\n", indent)
	for _, name := range i.Path {
		name.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	if i.Alias != "" {
		fmt.Printf("%s\tAlias: %s\n", indent, i.Alias)
	}
	if len(i.Members) > 0 {
		fmt.Printf("%s\tMembers: {\n", indent)
		for _, member := range i.Members {
			member.Print(indent + "\t\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type ImportMember struct {
	Name string
	Alias string
}

func (i *ImportMember) Print(indent string) {
	if i.Alias != "" {
		fmt.Printf("%sImportMember(name: %s, alias: %s)\n", indent, i.Name, i.Alias)
	} else {
		fmt.Printf("%sImportMember(name: %s)\n", indent, i.Name)
	}
}