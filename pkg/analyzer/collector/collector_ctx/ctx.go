package collector_ctx

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Collector is the recursive dispatch surface that subpackages need from
// [Ctx] without importing the root collector package. The concrete
// *collector.Collector implements this interface and is embedded on [Ctx].
type Collector interface {
	// LookupCurrentScope returns the symbol registered under name in the
	// innermost scope only (does not walk parent scopes). Used by
	// declaration collectors to detect same-scope re-declarations before
	// attempting to register, so they can emit a precise error message.
	LookupCurrentScope(name string) (ast.Named, bool)
	CollectExpr(*sitter.Node) ast.Expression
	CollectStatement(*sitter.Node) ast.Statement
	ParseDestructuringPattern(*sitter.Node) ast.Pattern
	CollectPattern(*sitter.Node) ast.Pattern
	ParseType(*sitter.Node) types.Type
	ParseLambdaType(*sitter.Node) *types.LambdaType
	RegisterType(*ast.TypeDeclStmt) error
	RegisterTrait(*ast.TraitDeclStmt) error
	RegisterFunction(string, *ast.LambdaExpr) error
	RegisterVariable(*ast.VarDeclStmt) error
	// RedefineVariable replaces an existing same-scope binding, used to allow
	// same-scope sequential rebinding (e.g. `let x = parse(x)`) so that later
	// references resolve to the newest declaration.
	RedefineVariable(*ast.VarDeclStmt)
	// RegisterDestructuredName binds one name from a destructuring declaration
	// (e.g. `x` in `let (x, y) = pair`) into the current scope, mapped to the
	// owning declaration so its mutability is recoverable.
	RegisterDestructuredName(string, *ast.DestructuringDeclStmt)
	RegisterParameter(*ast.Parameter) error
	PushFunctionScope() *symbols.Scope
	PushBlockScope() *symbols.Scope
	PushLoopScope() *symbols.Scope
	PopScope()
	RecordScope(node ast.AstNode, scope *symbols.Scope)
	CollectGenericParams(*sitter.Node) []ast.GenericParam
	MergeWhereConstraints([]ast.GenericParam, *sitter.Node) []ast.GenericParam
	CollectBounds(*sitter.Node) []string
}

// Ctx carries shared mutable state (source text, error sink) plus a [Collector]
// for expression / statement / type / symbol-table dispatch.
type Ctx struct {
	Source     []byte
	errors     *[]error
	ScopeTable *symbols.ScopeTable
	Collector
}

// NewCtx constructs a [Ctx] for use by the root collector. The error slice pointer
// must remain valid for the lifetime of collection (AppendError / AddError append into it).
func NewCtx(source []byte, coll Collector, errs *[]error) *Ctx {
	return &Ctx{
		Source:     source,
		errors:     errs,
		ScopeTable: symbols.NewScopeTable(),
		Collector:  coll,
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

func (ctx *Ctx) AddError(node *sitter.Node, sev diag.Severity, format string, args ...any) {
	*ctx.errors = append(*ctx.errors, diag.Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Location: ctx.NodeLocation(node),
		Severity: sev,
	})
}

func (ctx *Ctx) AddErrorRelated(node *sitter.Node, sev diag.Severity, related []diag.RelatedInformation, format string, args ...any) {
	*ctx.errors = append(*ctx.errors, diag.Diagnostic{
		Message:            fmt.Sprintf(format, args...),
		Location:           ctx.NodeLocation(node),
		Severity:           sev,
		RelatedInformation: related,
	})
}

// MustField retrieves a required child-by-field-name node.
// If the field is missing, it records a consistent location-aware error and returns false.
func (ctx *Ctx) MustField(node *sitter.Node, fieldName string) (*sitter.Node, bool) {
	field := node.ChildByFieldName(fieldName)
	if field == nil {
		ctx.AddError(node, diag.SeverityError, "%s is missing %q field", node.Kind(), fieldName)
		return nil, false
	}
	return field, true
}
