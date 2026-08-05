package collector_ctx

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/cst"
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
	// DeclareOverload offers a top-level declaration to an existing same-named one as
	// a receiver-keyed overload, reporting whether it was accepted and, if not, why.
	// It is asked *before* sequential rebinding, since two same-named module-level
	// functions are the case rebinding would otherwise silently collapse into one.
	DeclareOverload(*ast.VarDeclStmt) (bool, string)
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
	Source []byte
	// File is the path Source was read from, stamped onto every location this Ctx
	// produces. Empty for a single-file compile, where naming the file would be
	// noise; with modules it is what tells two diagnostics at the same line and
	// column apart. Setting it here — at the one place real source locations are
	// built — is what gives every later pass's diagnostics the right file for free.
	File string
	// Module is the module path File belongs to, recorded alongside each top-level
	// declaration so a later pass can tell which module owns a name.
	Module     string
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
		File:      ctx.File,
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

// AddErrorCoded is AddError with an explicit diagnostic code. Most collector errors are
// internal "this shape should not reach here" reports with no code; a rule the *user* can
// violate needs one, so it can be looked up and suppressed like any other.
func (ctx *Ctx) AddErrorCoded(node *sitter.Node, sev diag.Severity, code string, format string, args ...any) {
	*ctx.errors = append(*ctx.errors, diag.Diagnostic{
		Code:     code,
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

// RangeBound reports whether a range's `start`/`end` field is a bound that was
// actually written.
//
// Absent is not only nil. Where the grammar requires a bound, tree-sitter's error
// recovery can *insert* one to keep parsing — `range(..)` yields a zero-width
// `decimal_int` sitting on the `)` — and a nil check alone reads that as a bound
// of value zero. Both the missing flag and the empty span are tested because the
// recovery does not always set the former.
func RangeBound(node *sitter.Node) bool {
	return node != nil && !node.IsMissing() && node.EndByte() > node.StartByte()
}

// rangeExpressionForm is the `form` string the *iterated* range passes, and the only one a
// descending operator is legal in. A constant rather than a literal at the call site
// because the check below turns on it: a typo would silently permit a descending pattern.
const rangeExpressionForm = "range expression"

// ascendingOperator is the operator a refused descending one is repaired to, so collection
// continues with a well-formed node (hazard 3). `>` → `<`, `>=` → `<=`.
func ascendingOperator(op string) string {
	if op == ">" {
		return "<"
	}
	return "<="
}

// ascendingRange renders the ascending spelling of the same *set*, for the diagnostic —
// bounds swapped as well as the operator, since `5..>1` covers 2 through 5 and its
// ascending twin is `1..<5`.
//
// Built from the `start`/`end` **fields** rather than by splicing the node's source at the
// operator. A constraint's node text is the whole `range(0..<=100)` wrapper, so splicing it
// produced `0)..<=range(100` — a suggestion that is not merely ugly but unparseable, which
// is worse than none. Reading the bounds gets the same answer at all three sites.
func (ctx *Ctx) ascendingRange(node *sitter.Node, op string) string {
	start, end := cst.Field(node, "start"), cst.Field(node, "end")
	if !RangeBound(start) || !RangeBound(end) {
		return ""
	}
	return ctx.NodeText(end) + ".." + ascendingOperator(op) + ctx.NodeText(start)
}

// RangeEndOperator reads a range's `end_operator` field, enforcing the one rule
// the three range grammars share: a range with an end bound must say whether that
// bound is included (`lyra-E032`). `form` names the construct for the message
// ("range pattern", "range expression", "range constraint").
//
// The operator is optional in all three grammars and required here, deliberately.
// Every reader of the collected operator tests `== "<"`, so an omitted one fell
// through to *inclusive* — `0..9` silently meant `0..<=9`, and the extra value is
// the boundary the exhaustiveness checker and the emitted comparison disagree on.
// A diagnostic naming both fixes beats a syntax error pointing at whichever token
// failed to shift, which is the same trade lyra-E029 made for modifier order.
//
// Returns "=" after reporting, so the caller still builds a well-formed node
// (hazard 3: a diagnostic *and* a usable node, never a nil one). An open-ended
// range — no end bound at all — has no operator to write and is not this error.
func (ctx *Ctx) RangeEndOperator(node *sitter.Node, form string) string {
	if opNode := cst.Field(node, "end_operator"); opNode != nil {
		op := ctx.NodeText(opNode)
		// A descending range is an *iteration* order, and only an expression is iterated.
		// A pattern and a constraint are **sets**, where `5..>1` describes exactly the
		// members `1..<5` does — so the operator is not wrong about a detail, it is
		// asking for something the construct cannot have. The grammar accepts all four
		// spellings everywhere (one node kind, per the 08/01 unification) and the
		// restriction lives here, where the message can name the ascending form to write
		// instead.
		if types.RangeDescends(op) && form != rangeExpressionForm {
			suggestion := ""
			if ascending := ctx.ascendingRange(node, op); ascending != "" {
				suggestion = fmt.Sprintf(" — write it ascending, as `%s`", ascending)
			}
			ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeDescendingRangeNotIterated,
				"a %s cannot count downwards: `%s` describes a set, and a set has no direction%s",
				form, ctx.NodeText(node), suggestion)
			return ascendingOperator(op)
		}
		return op
	}
	if !RangeBound(cst.Field(node, "end")) {
		return ""
	}
	// Build both suggestions from the source itself by splicing at the `..`, so
	// they are right for every form the notation takes — open-start (`..9`),
	// signed (`-128..-1`), and stepped (`0..10:2`).
	text := ctx.NodeText(node)
	ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeMissingRangeEndOperator,
		"%s `%s` does not say whether its end bound is included; write `%s` for inclusive or `%s` for exclusive",
		form, text,
		strings.Replace(text, "..", "..<=", 1),
		strings.Replace(text, "..", "..<", 1))
	return "<="
}

// MustField retrieves a required child-by-field-name node.
// If the field is missing, it records a consistent location-aware error and returns false.
func (ctx *Ctx) MustField(node *sitter.Node, fieldName string) (*sitter.Node, bool) {
	field := cst.Field(node, fieldName)
	if field == nil {
		ctx.AddError(node, diag.SeverityError, "%s is missing %q field", node.Kind(), fieldName)
		return nil, false
	}
	return field, true
}
