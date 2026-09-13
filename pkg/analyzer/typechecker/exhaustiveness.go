package typechecker

import (
	"slices"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Exhaustiveness for a match on a *tuple*, as a pattern matrix rather than a per-arm test.
//
// The per-arm test this replaces (aggregateMatchIsExhaustive) asked only "is any one arm
// irrefutable?", which cannot see coverage spread across arms:
//
//	match (self, predicate) {
//	  (Some v, pred) => …,
//	  (None, _)      => …,
//	}
//
// Every value is covered — column 0 is a `Maybe` and both its constructors appear, column 1
// is a binding in both arms — but no single arm is irrefutable, so it warned. **That is the
// shape every multi-clause function desugars to** (typechecker/multi_clause.go), so the
// prelude's own combinators each drew a false lyra-E009; a warning that fires on correct
// code is worse than no warning, because it trains the reader to ignore the class.
//
// Checking columns *independently* would be unsound in the other direction, which is why
// this is a matrix and not a loop over columns: `(Some v, None)` beside `(None, Some x)`
// covers both constructors in both columns while leaving `(Some, Some)` unmatched.
//
// The algorithm is Maranget's: to decide whether a wildcard row is redundant, specialize the
// matrix by each constructor of column 0 and recurse, so coverage is only ever concluded
// from a set of rows that agree on every column to its left.
//
// **Uninterpretable rows are dropped rather than assumed to cover.** Every conservative
// choice here shrinks the matrix, which can only make the answer "not exhaustive" — the
// direction that warns about correct code, never the one that stays silent about a match
// that can trap at runtime.

// tupleMatchIsExhaustive reports whether arms cover every value of tuple type tt.
func (tc *TypeChecker) tupleMatchIsExhaustive(arms []ast.MatchArm, tt types.TupleType, loc ast.Location) bool {
	width := len(tt.Elements)
	if width == 0 {
		return len(unguardedArms(arms)) > 0
	}
	var rows [][]ast.Pattern
	for _, arm := range arms {
		if arm.Guard != nil {
			continue // a guard may fail, so the arm covers nothing on its own
		}
		if row, ok := tupleRow(arm.Pattern, width); ok {
			rows = append(rows, row)
		}
	}
	return tc.matrixIsExhaustive(rows, tt.Elements, loc)
}

// tupleRow splits one arm pattern into its columns. An irrefutable pattern binds the whole
// tuple and so covers every column; a tuple pattern of the right width supplies them
// directly. Anything else is a pattern the typechecker has already rejected against a tuple
// scrutinee (checkTupleMatchArm), and contributes no row.
func tupleRow(pat ast.Pattern, width int) ([]ast.Pattern, bool) {
	switch p := pat.(type) {
	case *ast.WildcardPattern, *ast.IdentifierPattern:
		return make([]ast.Pattern, width), true // nil == wildcard
	case *ast.BindingPattern:
		return tupleRow(p.Pattern, width)
	case *ast.TuplePattern:
		positions, fits := ast.MatchPositions(p.Elements, width)
		if !fits {
			return nil, false
		}
		return positions.Columns, true
	}
	return nil, false
}

// matrixIsExhaustive reports whether rows cover every value of the product colTypes.
func (tc *TypeChecker) matrixIsExhaustive(rows [][]ast.Pattern, colTypes []types.Type, loc ast.Location) bool {
	if len(colTypes) == 0 {
		// No columns left to distinguish values: any surviving row matches.
		return len(rows) > 0
	}
	ctors, finite := tc.constructorSet(colTypes[0], loc)
	// **Specialize only when column 0 names every constructor**; otherwise the rows that
	// bind it whole are all that can cover a constructor none of them names, which is
	// the default matrix. Beyond being Maranget's rule, this is what terminates on a
	// recursive type: specializing a wildcard row of `List` by `Cons` hands back another
	// `List` column, forever.
	if finite && !headsNameEvery(rows, ctors) {
		finite = false
	}
	if !finite {
		// A column whose values cannot be enumerated (an integer, a string, a nested
		// aggregate) is covered only by rows that bind it whole. Testing one — `(0, …)`
		// — leaves the rest of that column's values to some other row, which is exactly
		// what the default matrix keeps.
		return tc.matrixIsExhaustive(defaultMatrix(rows), colTypes[1:], loc)
	}
	for _, ctor := range ctors {
		sub, subTypes := specialize(rows, colTypes, ctor)
		if !tc.matrixIsExhaustive(sub, subTypes, loc) {
			return false
		}
	}
	return true
}

// constructor is one alternative of a column's type: a data constructor, or one of bool's
// two values. fields are the columns it expands into (none, for a bool or a nullary
// constructor).
type constructor struct {
	name   string
	fields []types.Type
	// fieldNames is set for a struct, whose single constructor's fields a pattern names
	// rather than lists; kind says which of the aggregate shapes this is.
	fieldNames []string
	kind       constructorKind
}

type constructorKind int

const (
	dataOrBoolConstructor constructorKind = iota
	tupleConstructor
	structConstructor
)

// headsNameEvery reports whether some row's column-0 pattern is headed by each constructor.
func headsNameEvery(rows [][]ast.Pattern, ctors []constructor) bool {
	for _, c := range ctors {
		named := false
		for _, row := range rows {
			if headNames(row[0], c) {
				named = true
				break
			}
		}
		if !named {
			return false
		}
	}
	return true
}

// headNames reports whether pat is headed by ctor rather than binding the column whole.
func headNames(pat ast.Pattern, ctor constructor) bool {
	switch p := ast.UnwrapBinding(pat).(type) {
	case *ast.DataPattern:
		return ctor.kind == dataOrBoolConstructor && p.Name == ctor.name
	case *ast.LiteralPattern:
		s, isStr := p.Value.(string)
		return ctor.kind == dataOrBoolConstructor && isStr && s == ctor.name
	case *ast.TuplePattern:
		return ctor.kind == tupleConstructor
	case *ast.StructPattern:
		return ctor.kind == structConstructor
	}
	return false
}

// constructorSet enumerates a type's alternatives, reporting false when they cannot be
// enumerated — an integer, a string, a rune, an array, or a type that does not resolve.
//
// A tuple or struct is **one** constructor whose fields are its elements. They were left
// unexpanded while only a tuple match used the matrix; since 09/13 a `data` match does
// too, and `Some((a, None))` beside `Some((a, Some(x)))` is a nested tuple the language's
// own code can write.
func (tc *TypeChecker) constructorSet(t types.Type, loc ast.Location) ([]constructor, bool) {
	if types.IsBoolean(t) {
		return []constructor{{name: "true"}, {name: "false"}}, true
	}
	if dt, ok := tc.resolveToDataType(t, loc); ok {
		out := make([]constructor, 0, len(dt.Constructors))
		for _, c := range dt.Constructors {
			out = append(out, constructor{name: c.Name, fields: c.FieldTypes()})
		}
		return out, len(out) > 0
	}
	resolved := tc.resolveGenericAggregate(tc.resolveType(t, loc), loc)
	if tt, ok := resolved.(types.TupleType); ok {
		return []constructor{{name: "()", fields: tt.Elements, kind: tupleConstructor}}, true
	}
	if fields, ok := structFields(resolved); ok {
		c := constructor{name: "{}", kind: structConstructor}
		for _, f := range fields {
			c.fields = append(c.fields, f.Type)
			c.fieldNames = append(c.fieldNames, f.Name)
		}
		return []constructor{c}, true
	}
	return nil, false
}

// specialize keeps the rows that can match ctor in column 0, replacing that column with the
// constructor's own fields. A row binding the column whole contributes wildcards for them.
func specialize(rows [][]ast.Pattern, colTypes []types.Type, ctor constructor) ([][]ast.Pattern, []types.Type) {
	subTypes := make([]types.Type, 0, len(ctor.fields)+len(colTypes)-1)
	subTypes = append(subTypes, ctor.fields...)
	subTypes = append(subTypes, colTypes[1:]...)

	var out [][]ast.Pattern
	for _, row := range rows {
		sub, ok := specializeHead(row[0], ctor)
		if !ok {
			continue
		}
		// A fresh row: sub may be a pattern's own Elements slice, and appending to that
		// would write the remaining columns into the AST's backing array.
		next := make([]ast.Pattern, 0, len(sub)+len(row)-1)
		next = append(next, sub...)
		next = append(next, row[1:]...)
		out = append(out, next)
	}
	return out, subTypes
}

// specializeHead expands one row's column-0 pattern into the constructor's fields, or
// reports false when the pattern cannot match that constructor.
func specializeHead(pat ast.Pattern, ctor constructor) ([]ast.Pattern, bool) {
	switch p := pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern, *ast.RestPattern:
		return make([]ast.Pattern, len(ctor.fields)), true
	case *ast.BindingPattern:
		return specializeHead(p.Pattern, ctor)
	case *ast.DataPattern:
		if ctor.kind != dataOrBoolConstructor || p.Name != ctor.name {
			return nil, false
		}
		return payloadColumns(p.Pattern, len(ctor.fields))
	case *ast.TuplePattern:
		if ctor.kind != tupleConstructor {
			return nil, false
		}
		positions, fits := ast.MatchPositions(p.Elements, len(ctor.fields))
		return positions.Columns, fits
	case *ast.StructPattern:
		if ctor.kind != structConstructor {
			return nil, false
		}
		cols := make([]ast.Pattern, len(ctor.fields))
		for _, f := range p.Fields {
			if i := slices.Index(ctor.fieldNames, f.Name); i >= 0 {
				cols[i] = f.Pattern // a shorthand field is nil, which binds the column whole
			}
		}
		return cols, true
	case *ast.LiteralPattern:
		// bool's two values are spelled as literals rather than constructors.
		if s, isStr := p.Value.(string); isStr && s == ctor.name {
			return nil, true
		}
		return nil, false
	}
	return nil, false
}

// payloadColumns splits a data pattern's payload into one column per constructor field. The
// collector wraps a multi-field payload in a tuple pattern, mirroring FieldTypes' unwrapping
// on the type side; a payload bound whole expands to wildcards.
func payloadColumns(payload ast.Pattern, arity int) ([]ast.Pattern, bool) {
	switch arity {
	case 0:
		return nil, true
	case 1:
		return []ast.Pattern{unparenthesize(payload)}, true
	}
	switch p := payload.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		return make([]ast.Pattern, arity), true
	case *ast.BindingPattern:
		return payloadColumns(p.Pattern, arity)
	case *ast.TuplePattern:
		positions, fits := ast.MatchPositions(p.Elements, arity)
		if !fits {
			return nil, false
		}
		return positions.Columns, true
	}
	return nil, false
}

// unparenthesize strips the one-element tuple pattern that parentheses around a
// single-field payload collect as: `Some (Some x)` gives DataPattern{Some, TuplePattern{
// DataPattern{Some, …}}} where the unparenthesized `Some None` gives the inner pattern
// directly. Both spell the same thing, and only the type side unwraps it for free
// (FieldTypes does the matching unwrap on an anonymous one-element tuple).
func unparenthesize(pat ast.Pattern) ast.Pattern {
	for {
		tuple, ok := pat.(*ast.TuplePattern)
		if !ok || len(tuple.Elements) != 1 {
			return pat
		}
		pat = tuple.Elements[0]
	}
}

// defaultMatrix keeps the rows that bind column 0 whole — the only ones that say anything
// about a value no other row's test matched — with that column dropped.
func defaultMatrix(rows [][]ast.Pattern) [][]ast.Pattern {
	var out [][]ast.Pattern
	for _, row := range rows {
		if patternIsIrrefutable(row[0]) {
			out = append(out, row[1:])
		}
	}
	return out
}

// unguardedArms are the arms that cover on their own.
func unguardedArms(arms []ast.MatchArm) []ast.MatchArm {
	var out []ast.MatchArm
	for _, arm := range arms {
		if arm.Guard == nil {
			out = append(out, arm)
		}
	}
	return out
}

// dataMatchCoverage splits the constructors of dt a match fails to cover into the ones no
// arm names (missing) and the ones an arm names for only some payloads (partial).
//
// **The second group did not exist until 09/13**: coverage was a set of constructor names,
// so `match m { Some(0) => …, None => … }` on a `Maybe<i64>` was exhaustive to the checker
// and trapped on `Some(5)` at run time. Each constructor is now the matrix specialized by
// it, which asks of its payload columns what the tuple check asks of a tuple's.
func (tc *TypeChecker) dataMatchCoverage(arms []ast.MatchArm, dt types.DataType, loc ast.Location) (missing, partial []string) {
	var rows [][]ast.Pattern
	for _, arm := range arms {
		if arm.Guard == nil {
			rows = append(rows, []ast.Pattern{arm.Pattern})
		}
	}
	ctors, _ := tc.constructorSet(dt, loc)
	for _, c := range ctors {
		sub, subTypes := specialize(rows, []types.Type{dt}, c)
		switch {
		case len(sub) == 0:
			missing = append(missing, c.name)
		case !tc.matrixIsExhaustive(sub, subTypes, loc):
			partial = append(partial, c.name)
		}
	}
	return missing, partial
}

// dataCoverageMessage is lyra-E009's text for a `data` match: the constructors no arm names,
// then those named for only some payloads, with the arm that would close each.
func dataCoverageMessage(dt types.DataType, missing, partial []string) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing constructors: "+strings.Join(missing, ", "))
	}
	for _, name := range partial {
		parts = append(parts, "not every payload of "+name+" is matched (add `"+name+" _ => …`)")
	}
	return "match on " + dt.Name + " is not exhaustive: " + strings.Join(parts, "; ")
}
