package typechecker_test

import "testing"

// Tests for the "unknown type" diagnostic emitted by resolveType when an
// UnresolvedType name has no matching declaration in symTable.Types.
//
// resolveType is called from two places:
//   - checkVarDecl  — when a variable carries a type annotation
//   - effectiveType — when resolving the type for a reassignment or math-assign
//
// In both cases the error fires in addition to any secondary assignability error
// that follows, so most scenarios produce two errors per bad annotation.

// ── let / var declaration with an unknown annotation ─────────────────────────

func TestUnknownType_LetDecl_UnknownAnnotation(t *testing.T) {
	// "unknown type" fires first; then the assignability check also fails
	// because the resolved type is still UnresolvedType (returned as-is).
	res := parseCollectAndCheck(t, `let x: Frob = 5`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`x: cannot assign integer literal to Frob`,
	)
}

func TestUnknownType_VarDecl_UnknownAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `var x: Frob = 5`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`x: cannot assign integer literal to Frob`,
	)
}

func TestUnknownType_LetDecl_StringValue(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: MyString = "hello"`, false)
	assertErrorsAre(t, res,
		`unknown type "MyString"`,
		`x: cannot assign string to MyString`,
	)
}

func TestUnknownType_LetDecl_BoolValue(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: MyBool = true`, false)
	assertErrorsAre(t, res,
		`unknown type "MyBool"`,
		`x: cannot assign boolean to MyBool`,
	)
}

func TestUnknownType_LetDecl_FloatValue(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: Real = 3.14`, false)
	assertErrorsAre(t, res,
		`unknown type "Real"`,
		`x: cannot assign float literal to Real`,
	)
}

// ── multiple unknown types in the same program ────────────────────────────────

func TestUnknownType_TwoDecls_TwoDistinctUnknownTypes(t *testing.T) {
	// Each declaration fires independently.
	res := parseCollectAndCheck(t, `
		let x: Foo = 1
		let y: Bar = 2
	`, false)
	assertErrorsAre(t, res,
		`unknown type "Foo"`,
		`x: cannot assign integer literal to Foo`,
		`unknown type "Bar"`,
		`y: cannot assign integer literal to Bar`,
	)
}

func TestUnknownType_TwoDecls_SameUnknownType(t *testing.T) {
	// Using the same undefined type name twice produces independent errors.
	res := parseCollectAndCheck(t, `
		let x: Frob = 1
		let y: Frob = 2
	`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`x: cannot assign integer literal to Frob`,
		`y: cannot assign integer literal to Frob`,
	)
}

// ── reassignment of a var with an unknown annotation ─────────────────────────
//
// resolveType is called a second time by effectiveType inside
// checkVarReassignment, so the error fires once per site.

func TestUnknownType_VarReassignment_ErrorAtBothSites(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: Frob = 5
		x = 10
	`, false)
	// Declaration site (checkVarDecl):
	//   - unknown type "Frob"
	//   - x: cannot assign integer literal to Frob
	// Reassignment site (checkVarReassignment → effectiveType):
	//   - unknown type "Frob"
	//   - x: cannot assign integer literal to Frob
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`x: cannot assign integer literal to Frob`,
		`x: cannot assign integer literal to Frob`,
	)
}

// ── math-assign on a var with an unknown annotation ───────────────────────────

func TestUnknownType_MathAssign_ErrorAtBothSites(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: Frob = 5
		x += 1
	`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`x: cannot assign integer literal to Frob`,
		`x: cannot assign integer literal to Frob`,
	)
}

// ── known types do not trigger the error ─────────────────────────────────────

func TestUnknownType_KnownPrimitive_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = 42`, false)
	assertNoErrors(t, res)
}

func TestUnknownType_KnownStruct_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point { x: i64, y: i64 }
		let p: Point = Point { x: 1, y: 2 }
	`, false)
	assertNoErrors(t, res)
}

func TestUnknownType_KnownDataType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Color = Red | Green | Blue
		let c = Red
	`, false)
	assertNoErrors(t, res)
}

// ── function parameter type annotations ──────────────────────────────────────
//
// withParamScope now resolves every parameter annotation via resolveType, so
// an unknown type name in a parameter position emits the same error as an
// unknown type in a variable annotation.

func TestUnknownType_FunctionParam_WithReturnType(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (x: Frob) -> i64 => 42`, false)
	assertErrorsAre(t, res, `unknown type "Frob"`)
}

func TestUnknownType_FunctionParam_NoReturnType(t *testing.T) {
	// withParamScope is now always entered, even when the function has no
	// declared return type, so the error still fires.
	res := parseCollectAndCheck(t, `let f = (x: Frob) => 42`, false)
	assertErrorsAre(t, res, `unknown type "Frob"`)
}

func TestUnknownType_FunctionParam_OneUnknownOneKnown(t *testing.T) {
	// Only the unknown type emits an error; the known primitive is silent.
	res := parseCollectAndCheck(t, `let f = (x: Frob, y: i64) -> i64 => 42`, false)
	assertErrorsAre(t, res, `unknown type "Frob"`)
}

func TestUnknownType_FunctionParam_TwoUnknown(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (x: Frob, y: Bar) -> i64 => 42`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`unknown type "Bar"`,
	)
}

func TestUnknownType_FunctionParam_KnownStructType_NoError(t *testing.T) {
	// A declared struct type in a parameter annotation resolves correctly.
	res := parseCollectAndCheck(t, `
		struct Point { x: i64, y: i64 }
		let origin = (p: Point) -> i64 => 0
	`, false)
	assertNoErrors(t, res)
}

func TestUnknownType_FunctionParam_KnownPrimitives_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let add = (a: i64, b: i64) -> i64 => a + b`, false)
	assertNoErrors(t, res)
}

func TestUnknownType_FunctionParam_CallSite_CompoundErrors(t *testing.T) {
	// The declaration emits "unknown type"; the call site then emits a
	// separate argument-type error because the param type is still unresolved.
	res := parseCollectAndCheck(t, `
		let f = (x: Frob) -> i64 => 42
		f(5)
	`, false)
	assertErrorsAre(t, res,
		`unknown type "Frob"`,
		`f: argument 1 (x): cannot assign integer literal to Frob`,
	)
}

func TestKnownUserType_FunctionParam_CallSite_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		newtype Degree = i64 where range(0..<360)
		let f = (x: Degree) -> i64 => 42
		f(90)
	`, false)
	assertNoErrors(t, res)
}

// ── current behaviour: unknown return type gives a mismatch, not this error ──
//
// Lambda return types are also not passed through resolveType.  When the body
// type cannot be assigned to the (still-unresolved) return type, a return-type
// mismatch is reported instead.

func TestUnknownType_FunctionReturnType_MismatchNotUnknownType(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (x: i64) -> Frob => 42`, false)
	assertErrorsAre(t, res,
		`f: return type mismatch: expected Frob, got integer literal`,
	)
}
