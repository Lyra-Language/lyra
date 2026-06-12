package typechecker_test

import "testing"

// ── return-type checking ────────────────────────────────────────────────────

// Single-expression bodies

func TestTypeCheck_Fn_ReturnType_ExprBody_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `let add = (a: i64, b: i64) -> i64 => a + b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_ReturnType_ExprBody_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `let bad = (x: i64) -> string => x * 2`, false)
	assertErrorsAre(t, res, "bad: return type mismatch: expected string, got i64")
}

func TestTypeCheck_Fn_ReturnType_StringConcat_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `let greet = (name: string) -> string => "Hello, " ++ name`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_ReturnType_StringConcat_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `let bad = (x: i64) -> i64 => "prefix" ++ "suffix"`, false)
	assertErrorsAre(t, res, "bad: return type mismatch: expected i64, got string")
}

func TestTypeCheck_Fn_ReturnType_NoAnnotation_NoError(t *testing.T) {
	// No declared return type: body is never checked.
	res := parseCollectAndCheck(t, `let f = (x: i64) => x * 2`, false)
	assertNoErrors(t, res)
}

// Block bodies – explicit return statement

func TestTypeCheck_Fn_ReturnType_BlockReturn_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let double = (x: i64) -> i64 => {
			return x * 2
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_ReturnType_BlockReturn_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let bad = (x: i64) -> string => {
			return x * 2
		}
	`, false)
	assertErrorsAre(t, res, "bad: return type mismatch: expected string, got i64")
}

// Block bodies – implicit last-expression return

func TestTypeCheck_Fn_ReturnType_BlockImplicit_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let double = (x: i64) -> i64 => {
			x * 2
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_ReturnType_BlockImplicit_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let bad = (x: i64) -> string => {
			x * 2
		}
	`, false)
	assertErrorsAre(t, res, "bad: return type mismatch: expected string, got i64")
}

// ── call-site argument count ────────────────────────────────────────────────

func TestTypeCheck_Call_ArgCount_Correct(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		add(1, 2)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgCount_TooFew(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		add(1)
	`, false)
	assertErrorsAre(t, res, "add: expected 2 argument(s), got 1")
}

func TestTypeCheck_Call_ArgCount_TooMany(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		add(1, 2, 3)
	`, false)
	assertErrorsAre(t, res, "add: expected 2 argument(s), got 3")
}

func TestTypeCheck_Call_ArgCount_ZeroParams_Correct(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let answer = () -> i64 => 42
		answer()
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgCount_ZeroParams_TooMany(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let answer = () -> i64 => 42
		answer(1)
	`, false)
	assertErrorsAre(t, res, "answer: expected 0 argument(s), got 1")
}

func TestTypeCheck_Call_ArgCount_WithDefault_Correct_AllProvided(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let greet = (name: string, prefix: string = "Hello") -> string => prefix ++ " " ++ name
		greet("Alice", "Hi")
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgCount_WithDefault_Correct_OnlyRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let greet = (name: string, prefix: string = "Hello") -> string => prefix ++ " " ++ name
		greet("Alice")
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgCount_WithDefault_TooMany(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let greet = (name: string, prefix: string = "Hello") -> string => prefix ++ " " ++ name
		greet("Alice", "Hi", "extra")
	`, false)
	assertErrorsAre(t, res, "greet: expected 1 to 2 argument(s), got 3")
}

// ── call-site argument types ─────────────────────────────────────────────────

func TestTypeCheck_Call_ArgType_IntLiterals_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		add(1, 2)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgType_StringLiteral_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let greet = (name: string) -> string => "Hello, " ++ name
		greet("Alice")
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ArgType_Mismatch_BoolToInt(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		let flag: bool = true
		add(flag, 2)
	`, false)
	assertErrorsAre(t, res, "add: argument 1 (a): cannot assign boolean to i64")
}

func TestTypeCheck_Call_ArgType_Mismatch_IntToString(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let greet = (name: string) -> string => "Hello, " ++ name
		let n: i64 = 42
		greet(n)
	`, false)
	assertErrorsAre(t, res, "greet: argument 1 (name): cannot assign i64 to string")
}

// ── void return-type enforcement ───────────────────────────────────────────

func TestTypeCheck_Fn_Void_EmptyBlock_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let noop = () -> void => {}`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_Void_BareReturn_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let noop = () -> void => {
			return
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Fn_Void_ReturnValue_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let bad = () -> void => {
			return 42
		}
	`, false)
	assertErrorsAre(t, res, "bad: void function must not return a value")
}

func TestTypeCheck_Fn_Void_ReturnString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let bad = (name: string) -> void => {
			return name
		}
	`, false)
	assertErrorsAre(t, res, "bad: void function must not return a value")
}

func TestTypeCheck_Fn_Void_MultipleReturnValues_MultipleErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let bad = (x: i64) -> void => {
			return x * 2
			return x * 3
		}
	`, false)
	assertErrorsAre(t, res, "bad: void function must not return a value", "bad: void function must not return a value")
}

func TestTypeCheck_Fn_Void_ExprBody_NoError(t *testing.T) {
	// Single-expression body on a void function: the value is discarded.
	res := parseCollectAndCheck(t, `let log = (msg: string) -> void => msg`, false)
	assertNoErrors(t, res)
}

// ── return-type propagation ──────────────────────────────────────────────────

func TestTypeCheck_Call_ReturnType_Propagates_Match(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		let x: i64 = add(1, 2)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Call_ReturnType_Propagates_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		let x: string = add(1, 2)
	`, false)
	assertErrorsAre(t, res, "x: cannot assign i64 to string")
}
