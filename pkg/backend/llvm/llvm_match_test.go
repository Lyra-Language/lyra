package llvm

import (
	"strings"
	"testing"
)

// `match` on a data value lowers to a tag switch: store the scrutinee, load its
// tag, switch to a block per arm, and (for a data pattern) reinterpret the
// payload blob as that variant's payload struct and bind the fields. The arms
// feed a merge phi, so a match is a value. This is what finally makes a
// constructed data value observable end to end — construct, match, extract a
// field, return it — so these are all buildAndRun.
func TestExec_DataMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Enum: the tag alone selects the arm; arm literals take the return width.
		{
			"enum",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 1,
			   Green => 2,
			   Blue => 3,
			 }
			 let main = () -> u8 => f(Green)`,
			2,
		},
		// Single payload, bare binding `Some x`: the payload field is extracted
		// and returned.
		{
			"single payload binding",
			`data Maybe = None | Some(u8)
			 let f = (m: Maybe) -> u8 => match m {
			   None => 0,
			   Some x => x,
			 }
			 let main = () -> u8 => f(Some(42))`,
			42,
		},
		// Multi-field payload `Rect(w, h)`: both fields bind and feed a computation.
		{
			"multi-field payload",
			`data Shape = Circle(u8) | Rect(u8, u8)
			 let f = (s: Shape) -> u8 => match s {
			   Circle(r) => r,
			   Rect(w, h) => w + h,
			 }
			 let main = () -> u8 => f(Rect(20, 22))`,
			42,
		},
		// Wildcard catch-all as the switch default.
		{
			"wildcard catch-all",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 7,
			   _ => 0,
			 }
			 let main = () -> u8 => f(Blue)`,
			0,
		},
		// Identifier catch-all binds the whole scrutinee (unused here) and is the
		// default arm.
		{
			"identifier catch-all",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 1,
			   other => 5,
			 }
			 let main = () -> u8 => f(Blue)`,
			5,
		},
		// Construction and match composed inline: Some(9) is built and immediately
		// matched, x binds to 9.
		{
			"construct then match inline",
			`data Maybe = None | Some(u8)
			 let main = () -> u8 => match Some(9) {
			   None => 0,
			   Some x => x + 1,
			 }`,
			10,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_DataMatchIR pins the match shape: a switch on the loaded tag, and an
// extractvalue reading a bound payload field.
func TestEmit_DataMatchIR(t *testing.T) {
	got, err := emitSource(t, `data Maybe = None | Some(u8)
	 let f = (m: Maybe) -> u8 => match m {
	   None => 0,
	   Some x => x,
	 }
	 let main = () -> u8 => f(Some(7))`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"switch",      // the tag dispatch
		"extractvalue", // reading the bound payload field
	} {
		if !strings.Contains(got, want) {
			t.Errorf("match IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_MatchGuard_Deferred: an arm guard (`if x > 0`) isn't lowered yet, so
// it errors loudly rather than silently dropping the condition.
func TestEmit_MatchGuard_Deferred(t *testing.T) {
	src := `data Maybe = None | Some(u8)
	 let f = (m: Maybe) -> u8 => match m {
	   Some x if x > 0 => x,
	   _ => 0,
	 }
	 let main = () -> u8 => f(Some(5))`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: match guards are not implemented yet")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Errorf("expected a guard error, got: %v", err)
	}
}

// TestEmit_MatchNonData_Deferred: a non-data scrutinee (here an integer) isn't
// lowered yet — only data-type matches are — so it errors loudly.
func TestEmit_MatchNonData_Deferred(t *testing.T) {
	src := `let f = (n: u8) -> u8 => match n {
	   0 => 1,
	   _ => 2,
	 }
	 let main = () -> u8 => f(0)`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: non-data-type match is not implemented yet")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected a not-implemented error, got: %v", err)
	}
}
