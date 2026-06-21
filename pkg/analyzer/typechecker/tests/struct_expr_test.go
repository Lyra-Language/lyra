package typechecker_test

import (
	"testing"
)

func TestTypeCheck_StructLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { name: "Alice", age: 30 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_OneInvalidField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { name: "Alice", age: "30" }
	`, false)
	assertErrorsAre(t, res, "Person.age: cannot assign string to i64")
}

func TestTypeCheck_StructLiteral_TwoInvalidFields_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { name: 30, age: "Alice" }
	`, false)
	assertErrorsAre(t, res, "Person.name: cannot assign integer literal to string", "Person.age: cannot assign string to i64")
}

func TestTypeCheck_StructLiteralWithDefault_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64 = 0,
		}
		let s = Person { name: "Alice" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteralWithAllDefaults_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string = "",
			age: i64 = 0,
		}
		let s = Person {}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteralWithDefault_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64 = 0,
		}
		let s = Person { name: 32 }
	`, false)
	assertErrorsAre(t, res, "Person.name: cannot assign integer literal to string")
}

func TestTypeCheck_StructLiteralWithExpression_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let oldAge = 30
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { name: "Alice", age: oldAge + 1 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_Shorthand_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { "Alice", 30 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_Shorthand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: i64,
		}
		let s = Person { 30, "Alice" }
	`, false)
	assertErrorsAre(t, res, "Person.name: cannot assign integer literal to string", "Person.age: cannot assign string to i64")
}

func TestTypeCheck_StructLiteral_GenericArgs_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point<n> { x: n, y: n }
		let int_point = Point::<i32> { x: 10, y: 20 }
		let float_point = Point::<f64> { x: 10.5, y: 20.5 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_GenericArgs_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point<n> { x: n, y: n }
		let int_point = Point::<f64> { x: true, y: "20" }
		let float_point = Point::<i32> { x: 10.5, y: 20.5 }
	`, false)
	assertErrorsAre(t, res,
		"Point.x: cannot assign boolean to f64",
		"Point.y: cannot assign string to f64",
		"Point.x: cannot assign float literal to i32",
		"Point.y: cannot assign float literal to i32",
	)
}

// Generic arguments are optional: when the turbofish is omitted, the type
// parameters are inferred from the field values.
func TestTypeCheck_StructLiteral_GenericArgs_Inferred_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point<n> { x: n, y: n }
		let int_point = Point { x: 10, y: 20 }
		let float_point = Point { x: 10.5, y: 20.5 }
	`, false)
	assertNoErrors(t, res)
}

// Inference fixes the parameter from the first field, so a later field of a
// different type is still rejected.
func TestTypeCheck_StructLiteral_GenericArgs_Inferred_Inconsistent_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point<n> { x: n, y: n }
		let p = Point { x: 10, y: "twenty" }
	`, false)
	assertErrorsAre(t, res, "Point.y: cannot assign string to i64")
}

// An explicit but wrong-arity turbofish is still an error (inference only kicks
// in when the arguments are omitted entirely).
func TestTypeCheck_StructLiteral_GenericArgs_WrongCount_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point<n> { x: n, y: n }
		let p = Point::<i32, f64> { x: 1, y: 2 }
	`, false)
	assertErrorsAre(t, res, "Point: expected 1 generic arguments, got 2")
}

func TestTypeCheck_NamedStructMissingField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person { name: string, age: i64, sex: string }
		let person = Person { name: "Alice", age: 30 }
	`, false)
	assertErrorsAre(t, res, "Person: missing field \"sex\"")
}

func TestTypeCheck_NamedStructUpdate_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person { name: string, age: i64, sex: string = "male" }
		let person = Person { name: "Alice", age: 30 }
		let updated = Person { person | age: 31, sex: "female" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NamedStructUpdateUnknownField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person { name: string, age: i64 }
		let person = Person { name: "Alice", age: 30 }
		let updated = Person { person | age: 31, sex: "female" }
	`, false)
	assertErrorsAre(t, res,
		"Person: unknown field \"sex\"",
	)
}

func TestTypeCheck_NamedStructUpdateWrongType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person { name: string, age: i64 }
		let person = Person { name: "Alice", age: 30 }
		let updated = Person { person | age: "31" }
	`, false)
	assertErrorsAre(t, res,
		"Person.age: cannot assign string to i64",
	)
}

func TestTypeCheck_AnonymousStructLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let person = { name: "Alice", age: 30 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_AnonymousStructUpdate_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let person = { name: "Alice", age: 30 }
		let updated = { person | age: 31, sex: "female" }
	`, false)
	assertNoErrors(t, res)
}

// ── Member expression callees ────────────────────────────────────────────────

func TestCall_MemberExpr_Valid_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Callable {
			fn: (i64) -> i64,
		}
		let c = Callable { fn: (n: i64) -> i64 => n * 2 }
		let r: i64 = c.fn(5)
	`, false)
	assertNoErrors(t, res)
}

func TestCall_MemberExpr_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Callable {
			fn: (i64) -> i64,
		}
		let c = Callable { fn: (n: i64) -> i64 => n * 2 }
		c.fn()
	`, false)
	assertErrorsAre(t, res, `fn: expected 1 argument(s), got 0`)
}

func TestCall_MemberExpr_WrongArgType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Callable {
			fn: (i64) -> i64,
		}
		let c = Callable { fn: (n: i64) -> i64 => n * 2 }
		c.fn(true)
	`, false)
	assertErrorsAre(t, res, `fn: argument 1: cannot assign boolean to i64`)
}

func TestCall_MemberExpr_NonCallableField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
		}
		let p = Person { name: "Alice" }
		p.name()
	`, false)
	assertErrorsAre(t, res, `member "name" is not callable (type string)`)
}

func TestCall_MemberExpr_OnUnknownStruct_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x = unknown.field()
	`, false)
	assertErrorsAre(t, res, `undefined identifier "unknown"`)
}
