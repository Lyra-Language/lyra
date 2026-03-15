package collctx

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type ErrorSeverity int

const (
	SeverityError ErrorSeverity = iota
	SeverityWarning
	SeverityInfo
)

type CollectorError struct {
	Message  string
	Location ast.Location
	Severity ErrorSeverity
}

func (e CollectorError) Error() string {
	return fmt.Sprintf("%s: %s", &e.Location, e.Message)
}

// Ctx carries the shared mutable state and cross-package callbacks that
// collector subpackages need without importing the root collector package.
type Ctx struct {
	Source []byte
	Errors *[]error

	// CollectExpr dispatches to the expression collector.
	CollectExpr func(*sitter.Node) ast.Expression
	// ParseType parses a type annotation node into a types.Type.
	ParseType func(*sitter.Node) types.Type
	// RegisterType registers a type declaration in the symbol table.
	RegisterType func(*ast.TypeDeclStmt) error
	// CollectGenericParams collects generic type parameter names from a generic_parameters node.
	CollectGenericParams func(*sitter.Node) []string
}

func (ctx *Ctx) NodeText(node *sitter.Node) string {
	return string(ctx.Source[node.StartByte():node.EndByte()])
}

func (ctx *Ctx) NodeLocation(node *sitter.Node) ast.Location {
	start := node.StartPosition()
	end := node.EndPosition()
	return ast.Location{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}

func (ctx *Ctx) AddError(node *sitter.Node, sev ErrorSeverity, format string, args ...any) {
	*ctx.Errors = append(*ctx.Errors, CollectorError{
		Message:  fmt.Sprintf(format, args...),
		Location: ctx.NodeLocation(node),
		Severity: sev,
	})
}

func (ctx *Ctx) AppendError(err error) {
	*ctx.Errors = append(*ctx.Errors, err)
}
