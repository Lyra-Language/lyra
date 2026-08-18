package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckArrayRepeatAliasing warns when `[v; n]` fills its slots with a value the
// program can mutate through the reference they all share (`lyra-W019`).
//
// `[[' '; WIDTH]; HEIGHT]` is the shape it exists for: one row, referenced HEIGHT
// times, so every `grid[py][px] = c` writes the same place and every row prints
// identically. The semantics are right — `[v; n]` evaluates v once, and that is what
// makes `[expensive(); 1000]` one call — so the fix is a diagnostic, not a change to
// the literal. What made it worth a code of its own is that the failure is *silent*:
// a uniform image reads as bad arithmetic, and in examples/mandelbrot.lyra it
// outlived the two genuine arithmetic bugs it was hiding behind.
//
// # It runs after typechecking, and it has to
//
// The element's type at *inference* time is not the type it is lowered at. Under a
// `[][]rune` annotation the inner `[' '; WIDTH]` infers as the fixed `[WIDTH]rune` —
// which shares nothing, being copied per slot — and only propagation widens it to
// the heap-boxed `[]rune` that does. So a check inside inferArrayRepeatType would
// have cleared the exact program that motivated it. This reads the TypeTable, which
// holds the settled type the backend will lower, and is a standalone pass for that
// reason rather than an arm of the typechecker.
//
// # What it does not fire on
//
// A count that folds to 0 or 1 has no second slot to share with, so there is nothing
// to observe. A count only known at run time is assumed to be plural: `[buf; n]` with
// n from a window resize is the case the warning most wants to reach, and refusing to
// guess would silence it exactly where the author cannot see the number either.
//
// The element predicate — ownership.SharesMutableState — is deliberately narrower
// than "managed", and the gap is `["hi"; 3]`: a string is a shared box and immutable,
// so it aliases unobservably. Warning on managed values would fire mostly on correct
// code, which is the way a warning stops being read.
func CheckArrayRepeatAliasing(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable) []diag.Diagnostic {
	if tt == nil {
		return nil
	}
	var out []diag.Diagnostic
	onExpr := func(e ast.Expression) bool {
		repeat, ok := e.(*ast.ArrayRepeatExpr)
		if !ok {
			return true
		}
		if d := repeatAliasDiagnostic(repeat, symTable, tt); d != nil {
			out = append(out, *d)
		}
		return true
	}
	for _, s := range program.Statements {
		if stmt, ok := s.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, onExpr)
		}
	}
	return out
}

// repeatAliasDiagnostic returns the warning for one repeat literal, or nil.
func repeatAliasDiagnostic(repeat *ast.ArrayRepeatExpr, symTable *symbols.SymbolTable, tt *typetable.TypeTable) *diag.Diagnostic {
	// A folded count of 0 or 1 fills no second slot. An unfoldable one is plural by
	// assumption — see the doc comment.
	if n, folded := ast.FoldIntExpr(repeat.Count); folded && n < 2 {
		return nil
	}
	recorded, ok := tt.Get(repeat)
	if !ok {
		return nil
	}
	elem := elementType(recorded)
	if elem == nil {
		return nil
	}
	loc := repeat.GetLocation()
	path, shares := ownership.SharedMutablePath(elem, symTable, loc, nil)
	if !shares {
		return nil
	}
	// Where the sharing is behind a struct field, name it. "the same Row" leaves the
	// reader to work out what is wrong with a struct of two numbers and a list; naming
	// `cells` points at the field that does it.
	via := ""
	if len(path) > 0 {
		via = fmt.Sprintf(" — and so the same `%s` inside it", strings.Join(path, "."))
	}
	return &diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeSharedRepeatElement,
		Message: fmt.Sprintf(
			"every slot of this repeat holds the *same* %s, not a copy of it%s: `[v; n]` evaluates its value "+
				"once, so a write through one slot is visible through all of them. Build a fresh value per "+
				"slot — a `for` loop with `push` — if they are meant to be independent",
			renderElement(elem), via),
	}
}

// elementType is the element of whichever array flavor the literal settled on. A
// repeat records `[n]T` in fixed position and `[]T` in dynamic position, and the
// question is about T either way.
func elementType(t types.Type) types.Type {
	switch v := types.StripNewtype(t).(type) {
	case types.StaticArrayType:
		return v.ElementType
	case types.DynamicArrayType:
		return v.ElementType
	}
	return nil
}

// renderElement names the element type the way a Lyra program spells it, for the
// cases that can reach here.
//
// The compiler's own rendering is written for diagnostics that are already discussing
// types (`DynamicArray<rune>`), and this message is discussing a *value* the author
// wrote — so it has to read as the annotation they would write. Deliberately partial:
// only a type that shares mutable state reaches this, which in practice is `[]T` and
// the named aggregates, whose String() is already their source spelling. Anything
// else falls through, exactly as docgen's fuller typeName does.
func renderElement(t types.Type) string {
	switch v := types.StripNewtype(t).(type) {
	case types.DynamicArrayType:
		return "[]" + renderElement(v.ElementType)
	case types.StaticArrayType:
		return fmt.Sprintf("[%d]%s", v.Size, renderElement(v.ElementType))
	case types.TupleType:
		parts := make([]string, len(v.Elements))
		for i, e := range v.Elements {
			parts[i] = renderElement(e)
		}
		inner := "(" + strings.Join(parts, ", ") + ")"
		if types.IsAnonymousTupleName(v.Name) {
			return inner
		}
		return v.Name + inner
	case types.PrimitiveType:
		// `boolean` and `integer literal` are diagnostic spellings, not Lyra ones.
		switch v.Name {
		case types.Boolean:
			return "bool"
		case types.UntypedInt, types.UntypedSignedInt:
			return "i64"
		case types.UntypedFloat:
			return "f64"
		}
		return string(v.Name)
	case types.ParameterizedType:
		if len(v.TypeArguments) == 0 {
			return v.Name
		}
		parts := make([]string, len(v.TypeArguments))
		for i, a := range v.TypeArguments {
			parts[i] = renderElement(a)
		}
		return v.Name + "<" + strings.Join(parts, ", ") + ">"
	}
	return t.String()
}
