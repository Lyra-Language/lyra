package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// errorsOf is the error diagnostics of a one-file program, code and message.
func errorsOf(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, d := range analyzeTree(t, map[string]string{"main.lyra": src}).Diagnostics {
		if d.Severity == diag.SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// A constructor with a payload, written bare, is a function (eraseConstructorValues), so
// it is no longer a value of its data type: `let g: Ev = Width` was *accepted* until 10/19,
// a constructor with no payload that only the backend refused. A nullary constructor is
// still the value it always was.
func TestConstructorValue_IsNotAValueOfItsType(t *testing.T) {
	errs := errorsOf(t, `data Ev = Width(i64) | Quit
let main = () -> void => {
  let g: Ev = Width
  let q: Ev = Quit
}
`)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "cannot assign (i64) -> Ev to Ev") {
		t.Fatalf("want one error assigning (i64) -> Ev to Ev, got %v", errs)
	}
}

// A generic constructor no context types is refused once, as the construction it stands
// for is (lyra-E073) — not as the undefined parameters of a lambda the program never wrote.
func TestConstructorValue_UntypableGenericIsRefusedOnce(t *testing.T) {
	for name, src := range map[string]string{
		"a local": `data Box<t> = Wrap(t) | Empty
let main = () -> void => {
  let w = Wrap
}
`,
		"a top-level binding": `data Box<t> = Wrap(t) | Empty
let wrap = Wrap
let main = () -> void => {}
`,
	} {
		t.Run(name, func(t *testing.T) {
			errs := errorsOf(t, src)
			if len(errs) != 1 || errs[0].Code != diag.CodeUninferableType ||
				!strings.Contains(errs[0].Message, "cannot tell what `Wrap` takes as a function") {
				t.Fatalf("want one lyra-E073 about Wrap, got %v", errs)
			}
		})
	}
}
