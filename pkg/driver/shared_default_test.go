package driver_test

import (
	"strings"
	"testing"
)

// A lambda default of a generic struct is shared by every literal, so it is typed by the first
// instantiation filling it; a literal at another is refused once, at the literal, naming the
// field and the first instantiation — not as "cannot assign (string) -> Maybe<i64> to
// (string) -> Maybe<string>" inside a default the literal never wrote (10/30).
func TestSharedDefault_AtASecondInstantiationIsRefusedByName(t *testing.T) {
	res := analyzeModules(t, map[string]string{"main.lyra": `module main
struct Spec<e> {
  label: string,
  on_edit: (string) -> Maybe<e> = (_: string) => None,
}
let main = () -> u8 => {
  let a = Spec::<i64> { label: "a" }
  let b = Spec::<string> { label: "b" }
  let c = Spec::<string> { label: "c", on_edit: (_: string) => None }
  0
}
`})
	errs := res.Errors()
	if len(errs) != 1 {
		t.Fatalf("want one error, got %d: %v", len(errs), errs)
	}
	msg := errs[0].Message
	for _, want := range []string{`"on_edit"`, "Spec<i64>", "Spec<string>", "binds names"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %s: %s", want, msg)
		}
	}
	if errs[0].Location.StartLine != 8 {
		t.Errorf("reported at line %d, want 8 (b's literal)", errs[0].Location.StartLine)
	}
}
