package collector_test

import "testing"

// The C variadic marker's *placement* rules, which are the collector's because the grammar
// deliberately admits more than the language means: `...` parses wherever a function type
// is written, since giving the extern its own signature rule would mean a second copy of
// `lambda_type` free to drift from the first — for a message the collector gives better
// than a syntax error pointing at whichever token failed to shift. This is that trade being
// paid.

// **The marker belongs to an extern and nowhere else** — the diagnostic the grammar's
// permissiveness is paid for. `...` parses wherever a function type is written, because
// giving the extern its own signature rule would mean a second copy of `lambda_type` free
// to drift from the first, for a message the collector gives better.
func TestVariadic_TheMarkerIsRefusedOutsideAnExtern(t *testing.T) {
	errs := parseAndCollectErrors(t, `
let f: (i32, ...) -> i32 = g
let g = pure (n: i32) -> i32 => n
`)
	assertCollectorErrorContains(t, errs, "belongs only to an `extern` signature")
}

// C needs a named parameter before `...`, and every real variadic API has one — a format
// string, a count, a sentinel.
func TestVariadic_NeedsANamedParameterBeforeTheMarker(t *testing.T) {
	errs := parseAndCollectErrors(t, `
unsafe extern take: (...) -> i32
`)
	assertCollectorErrorContains(t, errs, "at least one named parameter before `...`")
}

// Everything after `...` in a C signature is variadic, so a named parameter cannot follow
// it: that is the list written in the wrong order, not a shape the ABI has.
func TestVariadic_TheMarkerMustBeLast(t *testing.T) {
	errs := parseAndCollectErrors(t, `
unsafe extern take: (i32, ..., i32) -> i32
`)
	assertCollectorErrorContains(t, errs, "`...` must be the last parameter")
}
