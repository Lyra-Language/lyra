package llvm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/regex"
)

// Regex matching at run time (08/13), which is what lets a `pattern(...)`
// constrained newtype be built from a value the compiler cannot read.
//
// A pattern never needs compiling at run time — a constraint's pattern is part of a
// type — so `pkg/regex` runs at compile time and only its *answer* ships: two
// constant tables and a shared driver loop. That is why this exists without the
// runtime gaining a regex engine, which `lyra-E052` and `lyra-E054` had both
// recorded the absence of.
//
// **These tests check the emitted code against the engine**, not against
// expectations. pkg/regex/matcher_test.go already checks the flattened table against
// IsMatch; this checks that the IR actually implements the table, which is the half
// a Go test cannot reach. Together they close the loop: engine → table → emitted
// loop, each step compared to the one before.

// regexProbe builds a program that constructs a pattern-constrained newtype from a
// value the compiler cannot fold, so the run either completes (matched) or trips the
// constraint trap (did not match).
func regexProbe(pattern, input string) string {
	return fmt.Sprintf(`newtype Probe = string where pattern(r"%s")
let mk = (s: string) -> Probe => Probe(s)
let main = () -> u8 => {
  let v = %s ++ ""
  println(string(mk(v)))
  0
}
`, pattern, quoteLyra(input))
}

// quoteLyra renders a Go string as a Lyra string literal. Only the escapes these
// inputs need are handled, which is deliberate — a general escaper here would be
// untested machinery in a test.
func quoteLyra(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestExec_RegexMatchAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		inputs  []string
	}{
		{`^[0-9]+$`, []string{"123", "0", "", "abc", "12a", "1 2", "12\n"}},
		{`^[a-z][a-z0-9-]*$`, []string{"my-post-1", "a", "A", "1abc", "", "a b"}},
		{`^#[0-9a-f]{6}$`, []string{"#ff00aa", "#FF00AA", "#ff00a", "#ff00aaa", "ff00aa"}},
		{`[0-9]+`, []string{"abc123", "abc", "1", ""}},
		{`^$`, []string{"", "a", "\n"}},
		{`^a+b$`, []string{"ab", "aaab", "b", "abb", "ba"}},
	}
	for _, c := range cases {
		for _, in := range c.inputs {
			t.Run(fmt.Sprintf("%s/%q", c.pattern, in), func(t *testing.T) {
				t.Parallel()
				re, err := regex.Compile(c.pattern)
				if err != nil {
					t.Fatalf("compiling %q: %v", c.pattern, err)
				}
				want, err := re.IsMatch([]byte(in))
				if err != nil {
					t.Fatalf("IsMatch: %v", err)
				}
				_, code := buildAndRunPanic(t, regexProbe(c.pattern, in))
				matched := code == 0
				if matched != want {
					t.Errorf("pattern %q, input %q: compiled program says matched=%v, engine says %v",
						c.pattern, in, matched, want)
				}
			})
		}
	}
}

// The trap is the ordinary constraint trap, so a violated pattern reports the same
// way a violated range does and exits with the same code.
func TestExec_RegexMismatchTrapsLikeAnyConstraint(t *testing.T) {
	t.Parallel()
	stderr, code := buildAndRunPanic(t, regexProbe(`^[0-9]+$`, "nope"))
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
	if !strings.Contains(stderr, "violates its newtype's constraint") {
		t.Errorf("stderr = %q; want the constraint trap message", stderr)
	}
}

// **A literal costs nothing.** It was matched at compile time, so no table, no
// driver and no call are emitted — the same rule the numeric constraints follow, and
// the reason adding runtime matching did not make constrained newtypes expensive.
func TestEmit_LiteralPatternEmitsNoMatcher(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `newtype Digits = string where pattern(r"^[0-9]+$")
let main = () -> void => {
  let d: Digits = "123"
  println(string(d))
}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"lyra_regex_match", ".re.trans", ".re.acc"} {
		if strings.Contains(ir, unwanted) {
			t.Errorf("a literal-checked pattern emitted %q:\n%s", unwanted, ir)
		}
	}
}

// One table per distinct pattern, however many places use it — the tables are the
// large part of this feature, so sharing them is what keeps it affordable.
func TestEmit_OnePatternEmitsOneTable(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `newtype Digits = string where pattern(r"^[0-9]+$")
let a = (s: string) -> Digits => Digits(s)
let b = (s: string) -> Digits => Digits(s)
let main = () -> void => {
  println(string(a("1" ++ "")))
  println(string(b("2" ++ "")))
}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(ir, ".re.trans."); got == 0 {
		t.Fatalf("expected a transition table to be emitted:\n%s", ir)
	}
	if got := strings.Count(ir, "@.re.trans.0 = "); got != 1 {
		t.Errorf("expected exactly one transition table definition, got %d", got)
	}
	if strings.Contains(ir, "@.re.trans.1 = ") {
		t.Error("the same pattern emitted a second table")
	}
}
