package collector_ctx

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

// Collector is the recursive dispatch surface that subpackages need from
// [Ctx] without importing the root collector package. The concrete
// *collector.Collector implements this interface and is embedded on [Ctx].
type Collector interface {
	CollectExpr(*sitter.Node) ast.Expression
	CollectStatement(*sitter.Node) ast.Statement
	ParseDestructuringPattern(*sitter.Node) ast.Pattern
	CollectPattern(*sitter.Node) ast.Pattern
	ParseType(*sitter.Node) types.Type
	ParseLambdaType(*sitter.Node) *types.LambdaType
	RegisterType(*ast.TypeDeclStmt) error
	RegisterFunction(string, *ast.LambdaExpr) error
	RegisterVariable(*ast.VarDeclStmt) error
	RegisterParameter(*ast.Parameter) error
	PushFunctionScope()
	PushBlockScope()
	PushLoopScope()
	PopScope()
	CollectGenericParams(*sitter.Node) []ast.GenericParam
	MergeWhereConstraints([]ast.GenericParam, *sitter.Node) []ast.GenericParam
	CollectBounds(*sitter.Node) []string
}

// Ctx carries shared mutable state (source text, error sink) plus a [Collector]
// for expression / statement / type / symbol-table dispatch.
type Ctx struct {
	Source []byte
	errors *[]error
	Collector
}

// NewCtx constructs a [Ctx] for use by the root collector. The error slice pointer
// must remain valid for the lifetime of collection (AppendError / AddError append into it).
func NewCtx(source []byte, coll Collector, errs *[]error) *Ctx {
	return &Ctx{
		Source:    source,
		errors:    errs,
		Collector: coll,
	}
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
	*ctx.errors = append(*ctx.errors, CollectorError{
		Message:  fmt.Sprintf(format, args...),
		Location: ctx.NodeLocation(node),
		Severity: sev,
	})
}

// MustField retrieves a required child-by-field-name node.
// If the field is missing, it records a consistent location-aware error and returns false.
func (ctx *Ctx) MustField(node *sitter.Node, fieldName string) (*sitter.Node, bool) {
	field := node.ChildByFieldName(fieldName)
	if field == nil {
		ctx.AddError(node, SeverityError, "%s is missing %q field", node.Kind(), fieldName)
		return nil, false
	}
	return field, true
}
