package collector_test

import (
	"strings"
	"testing"
)

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
unsafe extern take: (n: i32, ..., n2: i32) -> i32
`)
	assertCollectorErrorContains(t, errs, "`...` must be the last parameter")
}

// **An `extern` parameter needs a name; a plain function type's must not have one.**
//
// The argument is that an extern is a *declaration* standing in for a C prototype, not a
// type: it substitutes for a `let`, and `let f = (a: i64) -> i64` names its parameters.
// Writing the extern as a bare type list was the anomaly. It matters most here because the
// boundary is where a positional mistake links cleanly and computes garbage — `bsearch:
// (^u8, ^u8, u64, u64, …)` gives a reader no way to tell the key from the base — and the
// information exists, in the C header being transcribed.
//
// The name is documentation the compiler cannot check: nothing compares it to the header,
// so a *wrong* name is as silent as none. What it buys is a transcription a reader can
// verify by eye, and `argument 2 (destLen)` instead of `argument 2 (arg1)`.
func TestExternParamName_IsRequired(t *testing.T) {
	errs := parseAndCollectErrors(t, `
unsafe extern pure labs: (i64) -> i64
`)
	assertCollectorErrorContains(t, errs, "an `extern` parameter needs a name: write `name: i64`")
}

func TestExternParamName_IsAcceptedWhenWritten(t *testing.T) {
	errs := parseAndCollectErrors(t, `
unsafe extern det compress: (dest: ^mut u8, destLen: ^mut u64, source: ^u8) -> i32
`)
	for _, e := range errs {
		if strings.Contains(e.Error(), "lyra-E067") || strings.Contains(e.Error(), "needs a name") {
			t.Errorf("unexpected E067: %v", e)
		}
	}
}

// A plain function *type* describes a shape, whose parameters have nothing to be named —
// refused the other way, so the two spellings cannot drift into meaning the same thing.
func TestExternParamName_IsRefusedOnAPlainFunctionType(t *testing.T) {
	errs := parseAndCollectErrors(t, `
let f: (n: i64) -> i64 = g
`)
	assertCollectorErrorContains(t, errs, "has no name to give")
}

// A **callback's own** signature is a type, so its parameters stay unnamed even inside an
// extern — the rule follows the construct rather than the enclosing declaration.
func TestExternParamName_ACallbacksOwnParametersStayUnnamed(t *testing.T) {
	errs := parseAndCollectErrors(t, `
unsafe extern qsort: (base: ^mut u8, count: u64, size: u64, cmp: (^u8, ^u8) -> i32) -> void
`)
	for _, e := range errs {
		if strings.Contains(e.Error(), "needs a name") || strings.Contains(e.Error(), "has no name to give") {
			t.Errorf("unexpected E067: %v", e)
		}
	}
}
