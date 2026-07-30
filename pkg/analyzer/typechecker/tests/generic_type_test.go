package typechecker_test

import "testing"

// A generic type's construction evaluates to *that instantiation*, so an annotation
// naming a different one is rejected — and the message names both, which it could not
// before: ParameterizedType.String() dropped its type arguments, rendering the
// mismatch as the nonsense "cannot assign Box to Box".
func TestGenericType_ArgumentMismatchRejected(t *testing.T) {
	source := `
struct Box<t> { value: t }
let f = () -> i64 => {
    let b: Box<string> = Box { value: 5 }
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "b: cannot assign Box<i64> to Box<string>")
}

// The instantiation is what a field read resolves against, so the field has the
// argument's type rather than the declaration's type variable. Reading it as the
// variable was the original symptom ("cannot convert t to u8").
func TestGenericType_FieldReadUsesTypeArgument(t *testing.T) {
	source := `
struct Box<t> { value: t }
let f = () -> i64 => {
    let b = Box { value: 5 }
    b.value
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// A wrong argument against a parameter typed at a specific instantiation.
func TestGenericType_ArgumentAgainstInstantiatedParam(t *testing.T) {
	source := `
struct Box<t> { value: t }
let get = (b: Box<i64>) -> i64 => b.value
let f = () -> i64 => get(Box { value: "s" })`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "get: argument 1 (b): cannot assign Box<string> to Box<i64>")
}

// A generic `data` type's argument is solved from the constructor's payload, so
// `Some(5)` is a `Maybe<i64>` and mismatches a `Maybe<string>` annotation.
func TestGenericType_DataConstructorSolvesArgument(t *testing.T) {
	source := `
data Maybe<t> = None | Some(t)
let f = () -> i64 => {
    let m: Maybe<string> = Some(5)
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "m: cannot assign Maybe<i64> to Maybe<string>")
}
