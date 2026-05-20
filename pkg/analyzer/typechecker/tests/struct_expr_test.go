package typechecker_test

import (
	"testing"
)

func TestTypeCheck_StructLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int,
		}
		let s = Person { name: "Alice", age: 30 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_OneInvalidField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int,
		}
		let s = Person { name: "Alice", age: "30" }
	`, false)
	assertErrorsAre(t, res, "Person.age: cannot assign string to int")
}

func TestTypeCheck_StructLiteral_TwoInvalidFields_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int,
		}
		let s = Person { name: 30, age: "Alice" }
	`, false)
	assertErrorsAre(t, res, "Person.name: cannot assign integer literal to string", "Person.age: cannot assign string to int")
}

func TestTypeCheck_StructLiteralWithDefault_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int = 0,
		}
		let s = Person { name: "Alice" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteralWithAllDefaults_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string = "",
			age: int = 0,
		}
		let s = Person {}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteralWithDefault_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int = 0,
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
			age: oldAge + 1,
		}
		let s = Person { name: "Alice" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_Shorthand_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int,
		}
		let s = Person { "Alice", 30 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructLiteral_Shorthand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Person {
			name: string,
			age: int,
		}
		let s = Person { 30, "Alice" }
	`, false)
	assertErrorsAre(t, res, "Person.name: cannot assign integer literal to string", "Person.age: cannot assign string to int")
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

// func TestTypeCheck_AnonymousStructLiteral_Ok(t *testing.T) {
// 	res := parseCollectAndCheck(t, `
// 		let person = { name: "Alice", age: 30 }
// 	`, false)
// 	assertNoErrors(t, res)
// }
