package driver_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// copiedBeforeMut is each lyra-W032 the program draws, as `line:copied>callee`.
func copiedBeforeMut(t *testing.T, body string) []string {
	t.Helper()
	res := analyzeModules(t, map[string]string{"main.lyra": `module main
struct St { n: i64, at: Pt, tag: string }
struct Pt { x: i64, y: i64 }
struct Pair { a: St, b: i64 }
data Held = Held(St, i64)
let bump = (s: mut St) -> i64 => {
  s.n += 1
  s.n
}
let nudge = (p: mut Pt) -> i64 => {
  p.x += 1
  p.x
}
let keep = pure (a: St, b: i64) -> i64 => a.n + b
let look = pure (a: ref St, b: i64) -> i64 => a.n + b
impl St {
  let grow = (self: mut) -> i64 => {
    self.n += 10
    self.n
  }
}
pub let main = () -> u8 => {
  var s = St { n: 0, at: Pt { x: 0, y: 0 }, tag: "" }
` + body + `
  0
}
`})
	for _, d := range res.Errors() {
		t.Errorf("unexpected error: %s", d.Message)
	}
	quoted := regexp.MustCompile(`^"([^"]+)" is copied here, and (\S+) changes`)
	var got []string
	for _, d := range res.Diagnostics {
		if d.Code != diag.CodeCopiedBeforeMut {
			continue
		}
		m := quoted.FindStringSubmatch(d.Message)
		if m == nil {
			t.Fatalf("unexpected message: %s", d.Message)
		}
		got = append(got, fmt.Sprintf("%d:%s>%s", d.Location.StartLine-19, m[1], m[2]))
	}
	return got
}

// An operand that is a place on its own, copied before a later operand of the same list
// changes it through a `mut` argument, is said so where it is written (lyra-W032): Vega's
// `(s, proceed(s, then))` returned the state from before the call (11/02).
func TestCopiedBeforeMut_IsReportedAtTheCopy(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"a tuple", "  let r = (s, bump(s))", "s>bump"},
		{"a call's arguments", "  let r = keep(s, bump(s))", "s>bump"},
		{"a field read before the whole is written", "  let r = (s.n, bump(s))", "s.n>bump"},
		{"the whole read before a field is written", "  let r = (s, nudge(s.at))", "s>nudge"},
		{"a method's `mut self`", "  let r = (s, s.grow())", "s>grow"},
		{"an array", "  let r = [s, Pair { a: s, b: bump(s) }.a]", "s>bump"},
		{"a struct literal's fields as written", "  let r = Pair { a: s, b: bump(s) }", "s>bump"},
		{"a constructor", "  let r = Held(s, bump(s))", "s>bump"},
		{"a write nested in a later operand", "  let r = (s, keep(St { n: 1, at: s.at, tag: \"\" }, bump(s)))", "s>bump"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := copiedBeforeMut(t, c.body+"\n  let _ = r")
			found := false
			for _, g := range got {
				if strings.HasSuffix(g, c.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("want a W032 for %s, got %v", c.want, got)
			}
		})
	}
}

// Nothing is copied, or nothing the copy holds is written: the read comes after the call, a
// `ref` argument lends the storage and sees the write, two fields part, or the earlier
// operand is a value computed from the place. (A write in a lambda, `(s, () => bump(s))`, is
// not here: passing a capture to a `mut` parameter is lyra-E024.)
func TestCopiedBeforeMut_IsQuietWhereNothingIsLost(t *testing.T) {
	cases := []struct{ name, body string }{
		{"the read after the call", "  let r = (bump(s), s)"},
		{"a ref argument", "  let r = look(s, bump(s))"},
		{"disjoint fields", "  let r = (s.n, nudge(s.at))"},
		{"a computed value", "  let r = (s.n + 1, bump(s))"},
		{"a call bound first", "  let w = bump(s)\n  let r = (s, w)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := copiedBeforeMut(t, c.body+"\n  let _ = r"); len(got) > 0 {
				t.Errorf("want no W032, got %v", got)
			}
		})
	}
}
