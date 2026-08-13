// Package driver is the single entry point to Lyra's front-end pipeline. It runs
// parse → collect → standalone checker passes → typecheck → purity on a source
// unit and returns the typed program together with every table a later stage
// (e.g. codegen) needs, plus the normalized diagnostics from every pass.
//
// It exists so the pipeline has one definition that any front-end consumer
// shares. Both user-facing tools resolve an import graph and then call
// AnalyzeUnits — the compiler (cmd/lyrac) and the LSP server (cmd/lyra-lsp) —
// while Analyze serves a caller holding a lone snippet. Anything that wants a
// typed program, the backend included, starts here.
package driver

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/modules"
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
	// Instantiations are the generic specializations the program uses (each call
	// site's solved type variables); the backend emits one function per distinct set.
	Instantiations *typetable.InstantiationTable
	Ownership      *ownership.Table
	// OwnershipBySpec holds a *separate* ownership table per generic specialization,
	// keyed by the instantiation's Key(). A generic body's decisions turn on whether
	// its values are reference-counted, which is a property of the type *argument* —
	// so it is analyzed once per instantiation, and the backend consults the table for
	// the specialization it is lowering. The tables cannot be merged: they are keyed
	// by AST node, and the same node carries different annotations per instantiation.
	OwnershipBySpec map[string]*ownership.Table
	// OwnershipByMethod is the same thing for **trait-impl method bodies**, keyed by
	// typetable.Resolution.SpecKey(). It is separate from OwnershipBySpec only because
	// the two are keyed differently; the reason both exist is identical.
	//
	// Until 08/03 there was no ownership analysis for a method body at all — generic or
	// not — so a method holding a managed value emitted no retains and no releases.
	OwnershipByMethod map[string]*ownership.Table
	Captures          *captures.Table      // each lambda's free variables (its closure environment)
	RangeSafety       *checker.SafetyTable // overflow ops the backend may leave unchecked
	Diagnostics       []diag.Diagnostic
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
	tree, err := parser.Parse(string(source))
	if tree == nil && err == nil {
		err = fmt.Errorf("parser returned nil tree")
	}
	if err != nil {
		res := &Result{}
		res.err(ast.Location{}, "", fmt.Sprintf("parse error: %v", err))
		return res
	}
	return AnalyzeUnits([]modules.Unit{{Source: source, Tree: tree, Root: tree.RootNode()}})
}

// AnalyzeUnits runs the pipeline over several already-resolved source units — the
// multi-module entry point, with Analyze as the single-unit case.
//
// The units are collected into **one** program with one symbol table rather than
// analyzed separately and linked. For a single-package, one-process build that is the
// right shape: every pass after collection (typecheck, ownership, captures, the
// checkers) reaches across the whole program and the backend emits one LLVM module, so
// splitting the front end into independently-analyzed units would mean teaching all of
// them to work across unit boundaries for no benefit yet. Separate compilation is a
// later concern, and the one that would justify that cost.
//
// Each unit's diagnostics are stamped with its file, since a line and column alone
// cannot say which file they came from once there is more than one.
func AnalyzeUnits(units []modules.Unit) *Result {
	res := &Result{}
	if len(units) == 0 {
		res.err(ast.Location{}, "", "no source units to analyze")
		return res
	}

	c := collector.NewCollector(units[0].Source)
	// Named before any file is walked: type registration happens *during* the walk, so
	// a prelude module set later would leave every type registered before it unable to
	// tell a prelude name from a user one.
	c.SetPreludeModule(preludeOf(units))
	// Likewise before the walk: a declaration that takes a name an imported module
	// exports is keyed apart from it, and a type is keyed as it is registered.
	c.SetImports(modules.ImportGraph(units))
	for _, u := range units {
		before := len(res.Diagnostics)
		res.Diagnostics = append(res.Diagnostics, collectParseErrors(u.Root, u.Source)...)
		stampFile(res.Diagnostics[before:], u.File)
		c.AddFile(u.Root, u.Source, u.File, u.Path)
	}
	program, symTable, scopeTable, collectorErrors := c.Finish()
	res.Program, res.SymbolTable, res.ScopeTable = program, symTable, scopeTable
	res.Diagnostics = append(res.Diagnostics, shadowWarnings(symTable)...)
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
	// checker.CheckUnsafeOutsideUnsafe (lyra-E011) is deliberately **not** run:
	// raw pointers and `unsafe` blocks are unimplemented and refused at the
	// expression instead (lyra-E051, 08/13). Its policy — a raw-pointer op or a
	// call to an `unsafe` function needs an enclosing `unsafe` block or function,
	// which does not leak across a lambda boundary — is correct and stays tested
	// against the function directly; what it cannot do while pointers do not work
	// is give advice, because it told the reader to write an `unsafe` block that
	// was itself an unknown expression. Wire it back in the day E051 comes out.
	for _, e := range checker.CheckRecursiveTypes(program) {
		res.err(e.Location, e.Code, e.Message)
	}
	for _, e := range checker.CheckEffectBounds(program) {
		res.err(e.Location, e.Code, e.Message)
	}

	// Generic parameter lists, reconciled against the signature they belong to.
	// Appended rather than run through res.err because, alone among the
	// pre-typecheck passes, it reports both severities: an undeclared type
	// variable is an error, a declared-but-unmentioned one a warning. It runs
	// here, before typechecking, because reporting at the declaration is the
	// point — the failure it closes is a diagnostic that lands at a call site or
	// in the backend instead.
	res.Diagnostics = append(res.Diagnostics, checker.CheckGenericParams(program)...)

	// Typecheck: AST → TypeTable (+ MethodTable for dispatch resolutions).
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	typeErrors := tc.Check(program)
	res.TypeTable = tt
	res.MethodTable = tc.MethodTable()
	res.Instantiations = tc.Instantiations()

	// Capture analysis: each lambda's free variables, which the backend copies
	// into a closure environment. It reads the TypeTable for each captured
	// binding's type, so it runs after typechecking — and *before* purity as of
	// 08/12, because whether constructing a nested lambda allocates is exactly
	// whether it captures (a capturing closure heap-boxes its environment; a
	// capture-free one shares a pinned static), and `noalloc` has to charge that.
	res.Captures = captures.Analyze(program, symTable, tt)

	// Purity must run after typechecking — it consumes the resolved MethodTable —
	// and after captures, which is how it knows a closure construction's cost.
	for _, e := range checker.CheckPurity(program, scopeTable, tt, tc.MethodTable(), res.Captures) {
		res.err(e.Location, e.Code, e.Message)
	}

	// Use-after-move also runs after typechecking: it needs the TypeTable to tell
	// which values are managed (only those are actually consumed by an `own`
	// parameter).
	res.Diagnostics = append(res.Diagnostics, checker.CheckUseAfterMove(program, symTable, tt, res.MethodTable)...)

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
	res.Ownership = ownership.Analyze(program, symTable, tt, res.MethodTable)
	// Close the instantiation set *before* the per-specialization ownership pass below,
	// not after: a specialization discovered later would have no table of its own and
	// would fall back to the program-wide one, which is analyzed generically — where a
	// type variable is not managed, so a `t = string` body would emit neither retains nor
	// releases. See instantiations.go.
	res.Diagnostics = append(res.Diagnostics, closeInstantiations(res)...)
	// …and once more per generic instantiation, with that instantiation's type
	// arguments substituted (see OwnershipBySpec).
	//
	// Over `Concrete()` rather than `All()`: a template — a call inside a generic body,
	// whose bindings are the enclosing body's own type variables — is not a
	// specialization and is never emitted, so analyzing it would key a table under a name
	// nothing looks up, at types that are not real.
	res.OwnershipBySpec = map[string]*ownership.Table{}
	for _, inst := range res.Instantiations.Concrete() {
		res.OwnershipBySpec[inst.Key()] = ownership.AnalyzeLambda(inst.Func, symTable, tt, inst.Subst, res.MethodTable)
	}
	// …and once per trait-method specialization. `Analyze` above walks top-level
	// declarations, which impl methods are not — they hang off a TraitImplStmt — so
	// before this a method body was analyzed by nothing at all, and a `string` inside one
	// was neither retained nor released. A method is analyzed the same way a generic
	// function is, per binding set: whether a value is reference-counted is a property of
	// the type *argument*, so the answer at `t = string` is not the answer at `t = i64`.
	res.OwnershipByMethod = map[string]*ownership.Table{}
	for _, r := range res.MethodTable.Specializations() {
		lam, err := r.Lambda()
		if err != nil {
			// A method with no declared signature cannot be given one here. The
			// backend reports it when the call is lowered, with the location.
			continue
		}
		res.OwnershipByMethod[r.SpecKey()] = ownership.AnalyzeLambda(lam, symTable, tt, r.Bindings, res.MethodTable)
	}

	// Writing to a captured binding cannot reach the enclosing one (captures are
	// by value), so it is rejected rather than silently dropped. (The captures
	// table itself is built earlier, before purity — see above.)
	res.Diagnostics = append(res.Diagnostics, checker.CheckCapturedAssignment(program, res.Captures)...)

	// Post-typecheck checker passes that already return diag.Diagnostic (they
	// carry their own severity and Unnecessary/Deprecated tags).
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnreachableCode(program)...)
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnusedVariables(program)...)
	// The UFCS map comes from the typechecker: a method-style call into another module
	// never writes that module's name, so the syntactic check cannot see the use.
	res.Diagnostics = append(res.Diagnostics, checker.CheckUnusedImports(program, tc.UFCSModules())...)
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

// stampFile records which file a batch of diagnostics came from. Only diagnostics
// that do not already name one are stamped, so a resolver diagnostic (which knows its
// own file, and may be about a *different* file than the one being analyzed) keeps it.
func stampFile(diags []diag.Diagnostic, file string) {
	if file == "" {
		return
	}
	for i := range diags {
		if diags[i].File == "" {
			diags[i].File = file
		}
	}
}

// preludeOf reports which unit, if any, is the prelude. The driver takes it from the
// units it was handed rather than from a constant, so a caller that resolved without a
// prelude gets no prelude behaviour — the two cannot disagree.
func preludeOf(units []modules.Unit) string {
	for _, u := range units {
		if u.Path == modules.PreludeModule {
			return u.Path
		}
	}
	return ""
}

// shadowWarnings turns each user declaration that took a name reaching it from elsewhere
// into a warning. A warning rather than an error, for both sources: the prelude is
// implicitly in scope, so rejecting a clash would make every name it exports permanently
// unusable and adding one later would break programs that never mentioned it — and an
// *imported* name is the same bargain made deliberately, so punishing it harder than the
// implicit case is backwards.
//
// The two differ only in what the message can offer. The prelude has no qualifier to
// point at, being reachable precisely because nothing names it; an imported module does,
// so the warning names the spelling that still reaches the shadowed declaration.
func shadowWarnings(symTable *symbols.SymbolTable) []diag.Diagnostic {
	if symTable == nil {
		return nil
	}
	out := make([]diag.Diagnostic, 0, len(symTable.Shadowed))
	for _, s := range symTable.Shadowed {
		d := diag.Diagnostic{
			Location: s.Loc,
			Severity: diag.SeverityWarning,
			Code:     diag.CodePreludeShadowed,
			Message: fmt.Sprintf(
				"%s shadows the prelude's %s — this declaration wins; rename it if that was not intended",
				s.Name, s.Name),
		}
		if s.Source != "" {
			d.Code = diag.CodeImportShadowed
			d.Message = fmt.Sprintf(
				"%s shadows the %s imported from module %q — this declaration wins;"+
					" reach the imported one as `%s.%s`",
				s.Name, s.Name, s.Source, lastSegment(s.Source), s.Name)
		}
		out = append(out, d)
	}
	return out
}

// lastSegment is the namespace a plain `import a.b` binds — the name the shadowing
// module writes to reach past its own declaration. An `as` alias would rename it, and
// this does not chase that: the hint is a spelling to try, and getting it from the
// import list would mean threading the referencing file through a warning built from a
// declaration.
func lastSegment(module string) string {
	if idx := strings.LastIndex(module, "."); idx >= 0 {
		return module[idx+1:]
	}
	return module
}
