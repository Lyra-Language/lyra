// Package driver is the single entry point to Lyra's front-end pipeline. It runs
// parse → collect → standalone checker passes → typecheck → purity on a source
// unit and returns the typed program together with every table a later stage
// (e.g. codegen) needs, plus the normalized diagnostics from every pass.
//
// It exists so the pipeline has one definition that any front-end consumer
// shares. The compiler (cmd/lyrac) calls Analyze; the LSP server (cmd/lyra-lsp)
// still runs its own copy of the same passes and should be migrated onto Analyze
// next (its inline pipeline is byte-for-byte mirrored here). Anything that wants
// a typed program — the backend included — starts here.
package driver

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Result is the full output of the front-end pipeline for one source unit. On a
// fatal parse failure Program and the tables are nil and Diagnostics holds only
// the parse error; otherwise every field is populated (Diagnostics may still
// contain errors from a later pass).
type Result struct {
	Program     *ast.Program
	SymbolTable *symbols.SymbolTable
	ScopeTable  *symbols.ScopeTable
	TypeTable   *typetable.TypeTable
	MethodTable *typetable.MethodTable
	Ownership   *ownership.Table
	Captures    *captures.Table      // each lambda's free variables (its closure environment)
	RangeSafety *checker.SafetyTable // overflow ops the backend may leave unchecked
	Diagnostics []diag.Diagnostic
}

// HasErrors reports whether any diagnostic is error-severity. A compiler should
// stop before codegen when this is true; a language server publishes anyway.
func (r *Result) HasErrors() bool {
	for i := range r.Diagnostics {
		if r.Diagnostics[i].Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only the error-severity diagnostics, preserving order.
func (r *Result) Errors() []diag.Diagnostic {
	var out []diag.Diagnostic
	for i := range r.Diagnostics {
		if r.Diagnostics[i].Severity == diag.SeverityError {
			out = append(out, r.Diagnostics[i])
		}
	}
	return out
}

// Analyze runs the complete front-end pipeline on source and returns the typed
// program, its tables, and every diagnostic produced, normalized to
// diag.Diagnostic. Diagnostics are appended in the same order the passes run
// (parse → collect → pre-typecheck checks → purity → post-typecheck checks →
// type errors), so callers get a stable ordering.
func Analyze(source []byte) *Result {
	res := &Result{}

	tree, err := parser.Parse(string(source))
	if tree == nil && err == nil {
		err = fmt.Errorf("parser returned nil tree")
	}
	if err != nil {
		res.err(ast.Location{}, "", fmt.Sprintf("parse error: %v", err))
		return res
	}
	root := tree.RootNode()

	// CST-level ERROR/MISSING nodes (tree-sitter embeds parse errors as nodes).
	res.Diagnostics = append(res.Diagnostics, collectParseErrors(root, source)...)

	// Collect: CST → AST + symbol/scope tables.
	c := collector.NewCollector(source)
	program, symTable, scopeTable, collectorErrors := c.Collect(root)
	res.Program, res.SymbolTable, res.ScopeTable = program, symTable, scopeTable
	for _, rawErr := range collectorErrors {
		if ce, ok := rawErr.(diag.Diagnostic); ok {
			res.Diagnostics = append(res.Diagnostics, ce)
		} else {
			res.err(ast.Location{}, "", rawErr.Error())
		}
	}

	// Standalone AST checker passes that run before typechecking. Each of these
	// reports only errors (never warnings) in the current pipeline.
	for _, e := range checker.CheckUseBeforeDeclaration(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckReturnOutsideFunction(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckBreakContinueOutsideLoop(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckAwaitOutsideAsync(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckTryOutsideResult(program, symTable) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckYieldOutsideGenerator(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckUnsafeOutsideUnsafe(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckRecursiveTypes(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckEffectBounds(program) {
		res.err(e.Location, e.Code, e.Message)
	}

	// Typecheck: AST → TypeTable (+ MethodTable for dispatch resolutions).
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	typeErrors := tc.Check(program)
	res.TypeTable = tt
	res.MethodTable = tc.MethodTable()

	// Purity must run after typechecking — it consumes the resolved MethodTable.
	for _, e := range checker.CheckPurity(program, scopeTable, tt, tc.MethodTable()) {
		res.err(e.Location, e.Code, e.Message)
	}

	// Use-after-move also runs after typechecking: it needs the TypeTable to tell
	// which values are managed (only those are actually consumed by an `own`
	// parameter).
	res.Diagnostics = append(res.Diagnostics, checker.CheckUseAfterMove(program, symTable, tt)...)

	// Value-range analysis (integer overflow / constant comparisons) also runs
	// after typechecking — it reads the TypeTable for each expression's width and
	// signedness. It also returns the safety table the backend uses to elide
	// provably-unnecessary overflow traps.
	rangeDiags, rangeSafety := checker.CheckIntegerRanges(program, tt)
	res.Diagnostics = append(res.Diagnostics, rangeDiags...)
	res.RangeSafety = rangeSafety

	// Ownership analysis (retain/release-temp decisions for managed values) runs
	// after typechecking — it reads the TypeTable to identify managed types. It
	// produces no diagnostics; the backend consumes the table.
	res.Ownership = ownership.Analyze(program, symTable, tt)

	// Capture analysis: each lambda's free variables, which the backend copies
	// into a closure environment. It reads the TypeTable for each captured
	// binding's type, so it too runs after typechecking.
	res.Captures = captures.Analyze(program, symTable, tt)

	// Writing to a captured binding cannot reach the enclosing one (captures are
	// by value), so it is rejected rather than silently dropped.
	res.Diagnostics = append(res.Diagnostics, checker.CheckCapturedAssignment(program, res.Captures)...)

	// Post-typecheck checker passes that already return diag.Diagnostic (they
	// carry their own severity and Unnecessary/Deprecated tags).
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnreachableCode(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnusedVariables(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnusedImports(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnusedParameters(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckTypeNames(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckInertBorrowModifiers(program)...)

	// Shadowing is a warning and carries the prior declaration as related info.
	for _, sw := range checker.CheckShadowing(program) {
		d := diag.Diagnostic{
			Severity: diag.SeverityWarning,
			Code:     sw.Code,
			Location: sw.Location,
			Message:  sw.Message,
		}
		if sw.OriginalLocation.StartLine > 0 {
			d.RelatedInformation = []diag.RelatedInformation{{
				Location: sw.OriginalLocation,
				Message:  "previously declared here",
			}}
		}
		res.Diagnostics = append(res.Diagnostics, d)
	}

	// Type errors are appended last (matching the previous LSP ordering). They
	// carry their own severity, tags, and related information.
	for _, te := range typeErrors {
		sev := diag.SeverityError
		if te.Severity == typechecker.SeverityWarning {
			sev = diag.SeverityWarning
		}
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{
			Severity:           sev,
			Code:               te.Code,
			Location:           te.Location,
			Message:            te.Message,
			Tags:               te.Tags,
			RelatedInformation: te.RelatedInformation,
		})
	}

	return res
}

// err appends an error-severity diagnostic.
func (r *Result) err(loc ast.Location, code, msg string) {
	r.Diagnostics = append(r.Diagnostics, diag.Diagnostic{
		Severity: diag.SeverityError,
		Code:     code,
		Location: loc,
		Message:  msg,
	})
}

// collectParseErrors walks the tree-sitter CST for ERROR and MISSING nodes and
// returns them as diagnostics. Tree-sitter embeds parse errors as named nodes
// rather than failing the parse, so this is the only way to surface them.
// Positions are converted from tree-sitter's 0-based rows/columns to Lyra's
// 1-based ast.Location convention so downstream mapping treats them like any
// other diagnostic.
func collectParseErrors(root *sitter.Node, source []byte) []diag.Diagnostic {
	if !root.HasError() {
		return nil
	}
	var diags []diag.Diagnostic
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node.IsMissing() {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.SeverityError,
				Location: nodeLocation(node),
				Message:  fmt.Sprintf("missing %s", node.Kind()),
			})
			return
		}
		if node.IsError() {
			text := node.Utf8Text(source)
			msg := "syntax error"
			if len(text) > 0 && len(text) <= 40 {
				msg = fmt.Sprintf("syntax error: unexpected %q", text)
			}
			diags = append(diags, diag.Diagnostic{
				Severity: diag.SeverityError,
				Location: nodeLocation(node),
				Message:  msg,
			})
			return
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			if child := node.Child(i); child != nil && child.HasError() {
				walk(child)
			}
		}
	}
	walk(root)
	return diags
}

// nodeLocation converts a tree-sitter node's 0-based span to a 1-based
// ast.Location.
func nodeLocation(node *sitter.Node) ast.Location {
	start := node.StartPosition()
	end := node.EndPosition()
	return ast.Location{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}
