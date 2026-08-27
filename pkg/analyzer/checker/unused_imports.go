package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CheckUnusedImports walks all top-level ImportStmt nodes and warns for any
// imported name (member alias/name, or module alias) that never appears as an
// identifier reference anywhere in the program.
//
// `used` carries what this check cannot see for itself (typechecker.ImportedModulesUsed);
// its zero value is fine for a caller with no typechecker pass. Two spellings reach an
// import without writing the name it bound, and the syntactic test below is blind to both:
// a **UFCS call** writes no module name — `m.map(f)`, not `maybe.map(m, f)` — and a
// **trait-method or operator dispatch** writes no trait name — `(7).tag()`, not
// `Tag::tag(7)`. The import is exactly what permitted each, so without this the warning
// tells you to delete an import the program needs.
func CheckUnusedImports(program *ast.Program, used ImportUse) []diag.Diagnostic {
	// Collected **per file**, because `program` spans every unit the import graph pulled
	// in: an imported module's own source mentions its own types constantly, so a
	// program-wide set answers "is this name used anywhere" when the question is "is it
	// used *here*". Invisible while only identifiers were counted — a declaration's own
	// name is not one — and immediate once type positions were, since `std.math`'s impl
	// methods take `self: Complex<t>` and every unused `import std.math.{ Complex }`
	// stopped warning.
	refsByFile := collectRefsByFile(program)

	var warnings []diag.Diagnostic
	for _, node := range program.Statements {
		stmt, ok := node.(*ast.ImportStmt)
		if !ok {
			continue
		}
		loc := stmt.GetLocation()
		refs := refsByFile[loc.File]
		// A module this file called into method-style is used, whatever its name does or
		// does not appear in the source. Checked before the name-based tests below, which
		// cannot see such a use at all.
		if used.Modules[loc.File][modulePath(stmt)] {
			continue
		}
		dispatched := used.Names[loc.File]

		switch {
		case len(stmt.Members) > 0:
			// Named member imports: `import foo.{ a, b as c }`
			for _, m := range stmt.Members {
				effective := m.Name
				if m.Alias != "" {
					effective = m.Alias
				}
				if strings.HasPrefix(effective, "_") {
					continue
				}
				// A trait this file dispatched to is used. Keyed on `m.Name`, the
				// **declared** name, not the effective one: the trait knows what it is
				// called, not what this import chose to call it, so `import lib.{ Tag as
				// T }` matches here exactly as the unaliased form does.
				if dispatched[m.Name] {
					continue
				}
				if !refs[effective] {
					warnings = append(warnings, diag.Diagnostic{
						Location: m.Location,
						Severity: diag.SeverityWarning,
						Code:     diag.CodeUnusedImport,
						Message:  fmt.Sprintf("imported name %q is never used", effective),
						Tags:     []diag.Tag{diag.TagUnnecessary},
					})
				}
			}

		case stmt.Alias != "":
			// Module alias import: `import foo.bar as baz`
			if strings.HasPrefix(stmt.Alias, "_") {
				continue
			}
			if !refs[stmt.Alias] {
				warnings = append(warnings, diag.Diagnostic{
					Location: loc,
					Severity: diag.SeverityWarning,
					Code:     diag.CodeUnusedImport,
					Message:  fmt.Sprintf("imported alias %q is never used", stmt.Alias),
					Tags:     []diag.Tag{diag.TagUnnecessary},
				})
			}

		default:
			// Plain import: `import foo.bar` — bound name is last path component.
			if len(stmt.Path) == 0 {
				continue
			}
			name := stmt.Path[len(stmt.Path)-1].Name
			if strings.HasPrefix(name, "_") {
				continue
			}
			if !refs[name] {
				warnings = append(warnings, diag.Diagnostic{
					Location: loc,
					Severity: diag.SeverityWarning,
					Code:     diag.CodeUnusedImport,
					Message:  fmt.Sprintf("imported module %q is never used", name),
					Tags:     []diag.Tag{diag.TagUnnecessary},
				})
			}
		}
	}
	return warnings
}

// ImportUse is what the unused-import check cannot determine from the syntax, gathered by
// the typechecker (ImportedModulesUsed).
//
// **Two granularities, because the two resolutions differ in what they can name.** A UFCS
// call resolves to a function the member list admitted, and the call site names neither
// the module nor — after desugaring — anything the import wrote, so the whole import is
// what has to be spared: `Modules` is file → module path. A dispatch does have a name to
// point at, the trait's, so `Names` is file → declared trait name and only that member is
// spared. Sparing the whole import there would silence its other members, which turns one
// wrong warning into several missing ones.
type ImportUse struct {
	// Modules maps a file to the module paths it reached through a UFCS call.
	Modules map[string]map[string]bool
	// Names maps a file to the *declared* names of traits it reached by dispatch.
	Names map[string]map[string]bool
}

// modulePath renders an import's dotted module path ("util.math"), the form the
// typechecker records a resolution-reached module under.
func modulePath(stmt *ast.ImportStmt) string {
	parts := make([]string, len(stmt.Path))
	for i, p := range stmt.Path {
		parts[i] = p.Name
	}
	return strings.Join(parts, ".")
}

// collectRefsByFile returns, per source file, the set of all names referenced in it — as an
// identifier, as a struct literal's type, or **in a type position**.
//
// The last two were missing until 08/14, and between them they are how an imported *type*
// is used: `Complex { re: … }` names it as a literal, `(c: Complex<f64>)` and
// `-> Complex<f64>` name it in a signature, and neither is an IdentifierExpr — a type is
// not an expression at all, so the expression walk could not see it however far it
// descended. So `import std.math.{ Complex }` warned as unused in a program that fails to
// compile without it (`undefined struct type "Complex"`), which is precisely the failure
// the UFCS note above describes: advice to delete an import the program needs.
//
// Over-collecting is the safe direction here and is deliberate. Every name in every type
// counts as a reference, so an import can only ever be reported unused when the name
// genuinely appears nowhere — a false *absence* is a warning nobody can act on correctly,
// while a false presence is only a warning not shown.
func collectRefsByFile(program *ast.Program) map[string]map[string]bool {
	byFile := make(map[string]map[string]bool)
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		file := stmt.GetLocation().File
		refs, seen := byFile[file]
		if !seen {
			refs = make(map[string]bool)
			byFile[file] = refs
		}
		noteType := func(t types.Type) {
			if t != nil {
				types.CollectTypeNames(t, refs)
			}
		}
		noteSignature := func(sig *types.LambdaType) {
			if sig == nil {
				return
			}
			for _, p := range sig.Parameters {
				noteType(p.Type)
			}
			noteType(sig.ReturnType.Type)
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			switch st := s.(type) {
			case *ast.VarDeclStmt:
				noteType(st.Type)
			case *ast.ExternDeclStmt:
				// An extern's signature is a `*types.LambdaType` on the declaration,
				// not a LambdaExpr in the tree, so the expression walk below never
				// reaches it. That is the whole of an extern's type surface — it has
				// no body — so without this the only way to *use* an imported type at
				// the boundary warned as unused: `import std.ffi.{ CLong }` beside
				// `unsafe extern pure labs: (CLong) -> CLong` advised deleting the
				// import the program cannot compile without.
				noteSignature(st.Signature)
			case *ast.TraitDeclStmt:
				// The same shape one declaration kind over: a trait's method
				// *signatures* are LambdaTypes too, and a trait is where an imported
				// type is most likely to be named without any body mentioning it. A
				// default method's body is a LambdaClause the walk does reach.
				for _, m := range st.Methods {
					noteSignature(m.Signature)
				}
			case *ast.TypeDeclStmt:
				// A declaration's *members* are what mention other types;
				// CollectTypeNames stops at a nominal head, which is right for a use
				// (`Pair` mentions `Pair`) and wrong here, where the head is the thing
				// being declared and the fields are the references.
				switch dt := st.Type.(type) {
				case types.NamedStructType:
					for _, f := range dt.Fields {
						noteType(f.Type)
					}
				case types.DataType:
					for _, ctor := range dt.Constructors {
						for _, p := range ctor.Params {
							noteType(p)
						}
					}
				default:
					noteType(st.Type)
				}
			}
			return true
		}, func(e ast.Expression) bool {
			switch ex := e.(type) {
			case *ast.IdentifierExpr:
				refs[ex.Name] = true
			case *ast.StructInstanceExpr:
				refs[ex.Name] = true
			case *ast.LambdaExpr:
				for _, p := range ex.Parameters {
					noteType(p.Type)
				}
				noteType(ex.ReturnType.Type)
			}
			return true
		})
	}
	return byFile
}
