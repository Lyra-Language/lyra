package types

import "fmt"

type Type interface {
	typeNode()
	IsNumericType() bool
	GetName() string
	Print(indent string)
}

// UnresolvedType represents a type reference that hasn't been resolved yet
type UnresolvedType struct {
	Name string // e.g., "Tree", "Point", "Maybe"
}

func (UnresolvedType) typeNode()             {}
func (u UnresolvedType) IsNumericType() bool { return false }
func (u UnresolvedType) GetName() string     { return u.Name }
func (u UnresolvedType) Print(indent string) { fmt.Printf("%s%s\n", indent, u.Name) }
