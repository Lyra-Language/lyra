package printer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type Printer struct {
	source []byte
}

func NewPrinter(source []byte) *Printer {
	return &Printer{source: source}
}

func (p *Printer) Print(node *sitter.Node) {
	fmt.Println("Printing tree...")
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

func (p *Printer) nodeText(node *sitter.Node) string {
	return string(p.source[node.StartByte():node.EndByte()])
}
