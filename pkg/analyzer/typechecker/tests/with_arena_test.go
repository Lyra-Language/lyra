package typechecker_test

import "testing"

// `with` (arena allocation) is refused — lyra-E050, 08/13.
//
// The feature was designed early (grammar, collector, a reserved runtime shim,
// the PinnedRC box sentinel) and never implemented: nothing lowered the
// statement and, because checkNode had no WithStmt arm, nothing type-checked the
// arena expression either. What made it worth refusing rather than leaving inert
// is that the purity pass *discharged* every allocation lexically inside a `with`
// body, so the statement's only observable effect was to switch `noalloc` off —
// see TestNoAlloc_ArenaDoesNotDischarge in pkg/analyzer/checker.

func TestTypeCheck_WithArena_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  with a = 1024 {
    println("inside")
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"arena allocation is not implemented, so `with` cannot be used yet — allocate normally (a `shared` value is reference-counted)")
}

// The handle is optional in the grammar (`with <expr> { … }`), and that form is
// refused identically — the diagnostic is about the statement, not the binding.
func TestTypeCheck_WithArena_NoHandleRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  with 1024 {
    println("inside")
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"arena allocation is not implemented, so `with` cannot be used yet — allocate normally (a `shared` value is reference-counted)")
}

// The body is checked, so a `with` block does not hide the mistakes inside it
// behind one error. This needed `WithStmt.Body` to become a `*BlockExpr` (08/13):
// held by value, its address was not the pointer the collector recorded a scope
// for, so the body could not be checked without reporting every name declared
// inside it as undefined — and so it was not checked at all.
//
// **That silence was load-bearing.** Allocation is a use-site property the
// typechecker *records*, so an unchecked body is one whose `shared` constructions
// `noalloc` cannot see — the second layer of the same hole, underneath the arena
// discharge. See TestNoAlloc_ArenaDoesNotDischarge in pkg/analyzer/checker.
func TestTypeCheck_WithArena_BodyStillChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  with a = 1024 {
    let x: i64 = "not an int"
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"arena allocation is not implemented, so `with` cannot be used yet — allocate normally (a `shared` value is reference-counted)",
		"x: cannot assign string to i64")
}

// …and checking it introduces no false "undefined" for the body's own names, the
// failure mode that kept the body unchecked. The regression pin for the pointer
// change: `n` is declared in the block, `a` is the handle one scope up.
func TestTypeCheck_WithArena_NoFalseUndefinedInBody(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  with a = 1024 {
    let n: i64 = 5
    println("${n}")
    println("${a}")
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"arena allocation is not implemented, so `with` cannot be used yet — allocate normally (a `shared` value is reference-counted)")
}

// `Arena.new(...)` — the canonical spelling every doc and the old discharge test
// used — is doubly unreachable, and neither half was visible until the arena
// expression started being inferred. There is no `Arena` type declared anywhere
// (so this is E035's undefined-name case), and even if there were, Lyra has no
// type-namespaced associated functions (E035's first case, 08/06). The phantom's
// own documented syntax could not have worked; nothing said so because nothing
// looked.
func TestTypeCheck_WithArena_CanonicalSpellingAlsoUnspellable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  with a = Arena.new(1024) {
    println("inside")
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"arena allocation is not implemented, so `with` cannot be used yet — allocate normally (a `shared` value is reference-counted)",
		`undefined constructor or type "Arena"`)
}
