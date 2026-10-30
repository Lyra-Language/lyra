package driver

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// eraseQualifiedNames rewrites every constructor, struct literal and pattern name written
// through a namespace import — `shapes.Circle(2)`, `shapes.Dot`, `shapes.Pt { x: 1 }`,
// `shapes.Square { side } =>` — to the bare name, and records each node's location with the
// module the qualifier named (symbols/qualified.go), before any pass reads the program.
// A name it refuses for want of a namespace or a member stays dotted and is recorded as
// reported, so the typechecker's "undefined" does not follow it. It returns its
// diagnostics, and per file the namespaces it erased, which count those
// imports as used (lyra-W004).
//
// **A desugaring, as the constructor aliases are**, for their reason: a constructor is
// matched by its own name in a dozen places, and a dotted name honoured at one and not
// another is a program that checks and then miscompiles. The record is what keeps the
// qualifier's meaning — `shapes.Circle` beside this module's own `Circle` is shapes' —
// where the bare name alone would take the nearest.
//
// A type written through a namespace (`p: shapes.Pt`) is not erased: it resolves dotted.
func eraseQualifiedNames(program *ast.Program, st *symbols.SymbolTable) ([]diag.Diagnostic, map[string]map[string]bool) {
	if program == nil || st == nil {
		return nil, nil
	}
	var diags []diag.Diagnostic
	used := map[string]map[string]bool{}
	erase := func(name, written *string, loc ast.Location) {
		namespace, member := *written, *name
		if namespace == "" {
			var ok bool
			if namespace, member, ok = symbols.SplitQualified(*name); !ok {
				return
			}
		}
		module := st.ModuleOfFile[loc.File]
		path, ok := st.NamespaceIn(module, namespace)
		if !ok {
			diags = append(diags, qualifiedError(loc, fmt.Sprintf(
				"%s.%s: no namespace import named %q here — `import %s` reaches its types and constructors as `%s.%s`",
				namespace, member, namespace, namespace, namespace, member)))
			st.RecordQualifiedFailure(loc)
			return
		}
		if used[loc.File] == nil {
			used[loc.File] = map[string]bool{}
		}
		used[loc.File][namespace] = true
		// A constructor first: `data Marker = Marker` declares both, and the construction is
		// the constructor's.
		var isPublic bool
		if decl, ok := st.DataTypeWithConstructorIn(path, member); ok {
			isPublic = decl.IsPublic
		} else if decl, ok := st.LookupTypeIn(path, member); ok {
			isPublic = decl.IsPublic
		} else {
			diags = append(diags, qualifiedError(loc, fmt.Sprintf(
				"module %q has no type or constructor %q", path, member)))
			st.RecordQualifiedFailure(loc)
			return
		}
		// A private one is refused and then erased like any other: it resolves, so the
		// refusal is the only diagnostic it costs.
		if !isPublic {
			diags = append(diags, qualifiedError(loc, fmt.Sprintf(
				"%s.%s is private to module %q; mark its declaration `pub` to reach it from here",
				namespace, member, path)))
		}
		// The qualifier is kept on the node as well as recorded: the AST outlives this
		// analysis (the driver's collect cache shares it with the next), the record does not.
		*name, *written = member, namespace
		st.RecordQualified(loc, path, member)
	}
	erasePatterns := func(node ast.AstNode) {
		for _, p := range ast.PatternsOf(node) {
			ast.WalkPattern(p, func(q ast.Pattern) bool {
				if dp, ok := q.(*ast.DataPattern); ok {
					erase(&dp.Name, &dp.Namespace, dp.GetLocation())
				}
				return true
			})
		}
	}
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			erasePatterns(s)
			return true
		}, func(e ast.Expression) bool {
			switch ex := e.(type) {
			case *ast.DataConstructorExpr:
				erase(&ex.Constructor, &ex.Namespace, ex.GetLocation())
			case *ast.TupleLiteralExpr:
				// An applied constructor, `shapes.Circle(2)`, and a named tuple or newtype.
				erase(&ex.Name, &ex.Namespace, ex.GetLocation())
			case *ast.StructInstanceExpr:
				erase(&ex.Name, &ex.Namespace, ex.GetLocation())
			}
			erasePatterns(e)
			return true
		})
	}
	return diags, used
}

func qualifiedError(loc ast.Location, message string) diag.Diagnostic {
	return diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityError,
		Code:     diag.CodeTypeError,
		Message:  message,
	}
}
