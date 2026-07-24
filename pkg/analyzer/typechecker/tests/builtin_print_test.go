package typechecker_test

import "testing"

// print/println are compiler-provided builtin free functions: (string) -> void.
// They resolve without a declaration, but only when no user binding shadows the
// name (scope resolution wins first).

func TestTypeCheck_Print_StringArg_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let main = () -> u8 => {
    print("hello")
    println("world")
    0
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Print_VoidMainBody_Ok(t *testing.T) {
	// A void single-expression body is now inferred, so the print call is
	// validated there too (previously such a body was left unchecked).
	res := parseCollectAndCheck(t, `
  let main = () -> void => println("hi")
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Print_ConcatArg_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let greet = (name: string) -> void => println("Hi, " ++ name)
  let main = () -> void => greet("Lyra")
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Print_NonStringArg_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let main = () -> u8 => {
    print(5)
    0
  }
	`, false)
	assertErrorsAre(t, res, "print: argument 1: cannot assign integer literal to string")
}

func TestTypeCheck_Print_WrongArity_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let main = () -> u8 => {
    print("a", "b")
    0
  }
	`, false)
	assertErrorsAre(t, res, "print: expected 1 argument(s), got 2")
}

func TestTypeCheck_Print_UserBindingShadows_Ok(t *testing.T) {
	// A user function named `print` shadows the builtin: this call resolves to
	// the user's (u8) -> u8, so a u8 argument is fine and a string would error.
	res := parseCollectAndCheck(t, `
  let print = (n: u8) -> u8 => n
  let main = () -> u8 => print(7)
	`, false)
	assertNoErrors(t, res)
}
