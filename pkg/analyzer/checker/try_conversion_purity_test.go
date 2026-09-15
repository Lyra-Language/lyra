package checker_test

import "testing"

// A `?` converting its error through `impl From<…>` runs that impl's `from` on the failure
// path, so its effects are the site's: a `pure` function propagating through an impure
// conversion is refused, and a pure conversion passes.
func TestPurity_TryConversionEffect(t *testing.T) {
	const base = `
data Result<t, e> = Ok(t) | Err(e)
trait From<s> { from: (s) -> Self }
data Low = Oops
data High = Wrapped(Low)
let low = pure () -> Result<i64, Low> => Err(Oops)
`
	if errs := checkPurity(t, base+`
impl From<Low> for High { from = (e) => { println("converting"); Wrapped(e) } }
let high = pure () -> Result<i64, High> => Ok(low()?)`); len(errs) == 0 {
		t.Fatalf("expected the impure conversion to be refused in a pure function, got none")
	} else if errs[0].Message != "pure function propagates an error with `?` through a non-pure From conversion" {
		t.Fatalf("unexpected message: %q", errs[0].Message)
	}
	if errs := checkPurity(t, base+`
impl From<Low> for High { from = (e) => Wrapped(e) }
let high = pure () -> Result<i64, High> => Ok(low()?)`); len(errs) != 0 {
		t.Fatalf("a pure conversion should pass, got %v", errs)
	}
}
