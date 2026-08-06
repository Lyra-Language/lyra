// Package captures computes, for every lambda in a program, the bindings its
// body reads from an enclosing scope — its free variables.
//
// The AST records nothing about captures: a lambda is just a body expression,
// and the names in it are resolved lexically by whoever is walking. That is fine
// for a *top-level* function, whose body can only reach parameters and globals,
// but a nested lambda is a closure — it must carry the enclosing values it reads,
// because it may outlive the frame they live in. This pass is what makes those
// values explicit, so the backend can copy them into a closure environment.
//
// Captures are **by value**. Each captured binding is copied into the closure's
// environment when the closure is *created*, so a closure that escapes its
// defining frame stays valid — the alternative (capturing the frame slot by
// reference) is a dangling pointer the moment the frame returns, and Lyra has no
// escape analysis to tell the two cases apart. The visible consequence is that
// assigning to a captured binding inside a lambda cannot affect the enclosing
// binding, which is why that is rejected outright (lyra-E024) rather than
// silently doing nothing.
package captures

import (
	"cmp"
	"slices"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// primitiveTypeNames are the type keywords that can appear where an identifier
// read otherwise would: a conversion `u8(x)` parses as a call on the bare name
// `u8`. None of them can ever name a binding, so they are never captures.
var primitiveTypeNames = []types.PrimitiveTypeName{
	types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
	types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128,
	types.Float16, types.Float32, types.Float64,
	types.Boolean, types.String, types.Rune,
}

// Capture is one binding a lambda body reads from an enclosing scope.
type Capture struct {
	Name string
	Type types.Type // the recorded type of a read of it inside the body
}

// Table maps each lambda to its captures, in a stable order (by name) so the
// environment layout an emitted closure uses is deterministic.
type Table struct {
	byLambda map[*ast.LambdaExpr][]Capture
}

// Of returns the lambda's captures, or nil for a lambda that captures nothing
// (and for a nil table, so a caller without this pass sees "no captures" rather
// than crashing).
func (t *Table) Of(fn *ast.LambdaExpr) []Capture {
	if t == nil {
		return nil
	}
	return t.byLambda[fn]
}

// Analyze walks every lambda in the program and records its captures.
//
// The rule is deliberately simple: a name read anywhere inside the lambda that
// is not bound anywhere inside it, and is not a global (a top-level function,
// const, or type), is a capture. Both halves are computed **flow-insensitively**
// — every binder in the body contributes, regardless of where it sits — which is
// sound here because a read of an outer binding that is *later* shadowed by an
// inner declaration of the same name is already a use-before-declaration error
// (checker.CheckUseBeforeDeclaration), so no valid program distinguishes the two
// readings.
//
// Both failure directions are loud rather than silent, which is what makes that
// simplicity safe. Capturing a name that was really local costs a wasted copy at
// worst — the body's own binding shadows it — and errors in the backend if no
// enclosing binding of that name exists. Missing a genuine capture errors too:
// the lifted body function starts with an empty local set, so a name that is
// neither a parameter, a capture, nor a global has nowhere to come from.
func Analyze(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable) *Table {
	a := &analyzer{
		table:   &Table{byLambda: map[*ast.LambdaExpr][]Capture{}},
		globals: globalNames(program, symTable),
		tt:      tt,
	}
	for _, node := range program.Statements {
		switch n := node.(type) {
		case ast.Statement:
			ast.WalkStmt(n, a.onStmt, a.onExpr)
		case ast.Expression:
			ast.WalkExpr(n, a.onStmt, a.onExpr)
		}
	}
	return a.table
}

type analyzer struct {
	table   *Table
	globals map[string]bool
	tt      *typetable.TypeTable
}

func (a *analyzer) onStmt(ast.Statement) bool { return true }

func (a *analyzer) onExpr(expr ast.Expression) bool {
	fn, ok := expr.(*ast.LambdaExpr)
	if !ok {
		return true
	}
	// Recurse first (the walk continues into the body below), then record this
	// lambda's own set. A nested lambda's free names show up in this lambda's read
	// set too — the read walk does not stop at a lambda boundary — so an inner
	// closure's capture is transitively captured by every enclosing lambda that
	// does not bind it, which is exactly what the environment chain needs.
	a.table.byLambda[fn] = a.capturesOf(fn)
	return true
}

// capturesOf computes one lambda's captures: names read inside it, minus names
// bound inside it, minus globals.
func (a *analyzer) capturesOf(fn *ast.LambdaExpr) []Capture {
	bound := map[string]bool{}
	for _, p := range fn.Parameters {
		addPatternNames(bound, p.Pattern)
	}
	for _, clause := range fn.LambdaClauses {
		for _, p := range clause.Patterns {
			addPatternNames(bound, p)
		}
	}
	reads := map[string]ast.Expression{}
	collect(bodyOf(fn), bound, reads)

	var out []Capture
	for name, read := range reads {
		if bound[name] || a.globals[name] {
			continue
		}
		var t types.Type
		if a.tt != nil {
			t, _ = a.tt.Get(read)
		}
		out = append(out, Capture{Name: name, Type: t})
	}
	slices.SortFunc(out, func(x, y Capture) int { return cmp.Compare(x.Name, y.Name) })
	return out
}

// bodyOf returns the expressions that make up a lambda's body — the single body
// for the ordinary form, or every clause body for the multi-clause form.
func bodyOf(fn *ast.LambdaExpr) []ast.Expression {
	if len(fn.LambdaClauses) == 0 {
		return []ast.Expression{fn.Body}
	}
	out := make([]ast.Expression, 0, len(fn.LambdaClauses))
	for _, c := range fn.LambdaClauses {
		out = append(out, c.Body)
	}
	return out
}

// collect walks the body accumulating every identifier *read* into reads and
// every name a binder introduces into bound. It descends into nested lambdas so
// their free names reach the enclosing lambda's read set; their parameters land
// in bound for the same reason a local does — a read of that name can only occur
// where it is bound.
func collect(body []ast.Expression, bound map[string]bool, reads map[string]ast.Expression) {
	onExpr := func(e ast.Expression) bool {
		switch v := e.(type) {
		case *ast.IdentifierExpr:
			if _, seen := reads[v.Name]; !seen {
				reads[v.Name] = v
			}
		case *ast.LambdaExpr:
			for _, p := range v.Parameters {
				addPatternNames(bound, p.Pattern)
			}
			for _, c := range v.LambdaClauses {
				for _, p := range c.Patterns {
					addPatternNames(bound, p)
				}
			}
		case *ast.MatchExpr:
			for _, arm := range v.MatchArms {
				addPatternNames(bound, arm.Pattern)
			}
		case *ast.ForInLoopExpr:
			// The collector puts the first name in Key and the second in Value; the
			// single-variable form leaves Value empty.
			if v.Key != "" {
				bound[v.Key] = true
			}
			if v.Value != "" {
				bound[v.Value] = true
			}
		case *ast.ForLoopExpr:
			// A C-style loop's init declaration is reached by the generic walker only
			// as an *expression* (it walks Init.Value), never as the statement it is —
			// so the counter has to be bound here, or `for var i = 0; …; i += 1` inside
			// a lambda reads as a capture and its `i += 1` as a write to one.
			if v.Init != nil {
				bound[v.Init.Name] = true
			}
		}
		return true
	}
	onStmt := func(s ast.Statement) bool {
		switch v := s.(type) {
		case *ast.VarDeclStmt:
			bound[v.Name] = true
		case *ast.DestructuringDeclStmt:
			addPatternNames(bound, v.Pattern)
		case *ast.IfDestructuringStmt:
			addPatternNames(bound, v.DestructuringStatement.Pattern)
		case *ast.ElseDestructuringStmt:
			addPatternNames(bound, v.DestructuringStatement.Pattern)
		case *ast.WithStmt:
			bound[v.Name] = true // the arena handle
		}
		return true
	}
	for _, e := range body {
		ast.WalkExpr(e, onStmt, onExpr)
	}
}

// addPatternNames adds every binding name a pattern introduces. A literal,
// range, regex, or wildcard pattern binds nothing; the composite forms recurse.
func addPatternNames(bound map[string]bool, p ast.Pattern) {
	switch v := p.(type) {
	case *ast.IdentifierPattern:
		bound[v.Name] = true
	case *ast.BindingPattern:
		bound[v.Name] = true
		addPatternNames(bound, v.Pattern)
	case *ast.TuplePattern:
		for _, e := range v.Elements {
			addPatternNames(bound, e)
		}
	case *ast.ArrayPattern:
		for _, e := range v.Elements {
			addPatternNames(bound, e)
		}
	case *ast.RestPattern:
		if v.Identifier != "" {
			bound[v.Identifier] = true
		}
	case *ast.StructPattern:
		for i := range v.Fields {
			f := v.Fields[i]
			if f.Pattern != nil {
				addPatternNames(bound, f.Pattern)
				continue
			}
			bound[f.Name] = true // shorthand `{ x }` binds the field name
		}
	case *ast.DataPattern:
		addPatternNames(bound, v.Pattern)
	}
}

// globalNames is the set of names a lambda body can reach without capturing:
// every top-level binding (functions and consts alike) and every declared type,
// including a `data` type's constructors, which appear in expression position as
// plain identifiers — plus the primitive type keywords, since a conversion
// (`u8(x)`) is a call whose callee is a bare identifier naming a type.
func globalNames(program *ast.Program, symTable *symbols.SymbolTable) map[string]bool {
	out := map[string]bool{}
	for _, name := range primitiveTypeNames {
		out[string(name)] = true
	}
	// The compiler-provided free functions are reachable everywhere, so calling one
	// from a closure body is not a capture. (A user binding of the same name
	// shadows the builtin for the typechecker; treating it as global here would
	// only mean *not* capturing it, which surfaces as a loud unbound-identifier
	// error in the backend rather than wrong code.)
	for _, name := range []string{"print", "println", "read_line"} {
		out[name] = true
	}
	for _, stmt := range program.Statements {
		switch v := stmt.(type) {
		case *ast.VarDeclStmt:
			out[v.Name] = true
		case *ast.TypeDeclStmt:
			out[v.Name] = true
		}
	}
	if symTable != nil {
		// Ranged for the declarations, not the keys: Types is keyed by declKey, so a
		// private type is filed under `<module>::<name>` — recording that as a declared
		// name would enter a string no source can write, while missing the real one.
		for _, decl := range symTable.Types {
			if decl == nil {
				continue
			}
			name := decl.Name
			out[name] = true
			if dt, ok := decl.Type.(types.DataType); ok {
				for _, ctor := range dt.Constructors {
					out[ctor.Name] = true
				}
			}
		}
	}
	return out
}
