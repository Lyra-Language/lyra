package printer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type Printer struct {
}

func NewPrinter() *Printer {
	return &Printer{}
}

func (p *Printer) Print(node *sitter.Node) {
	cursor := node.Walk()
	p.printNode(cursor)
}

func (p *Printer) printNode(cursor *sitter.TreeCursor) {
	currentNode := cursor.Node()
	depth := cursor.Depth()
	fmt.Println(strings.Repeat("  ", int(depth)), currentNode.Kind())

	// print all children of the current node
	if cursor.GotoFirstChild() {
		for {
			p.printNode(cursor)
			if !cursor.GotoNextSibling() {
				break
			}
		}
		cursor.GotoParent()
	}
}
