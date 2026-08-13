package regex

import (
	"fmt"
	"strings"
	"testing"
)

// The compiled table must agree with IsMatch on every input — that is the whole
// claim, since the two are the compile-time and run-time rungs of one check and a
// value passing one while failing the other is worse than either.
//
// So this is a **differential** test rather than a set of hand-written
// expectations: hand-written cases would test my reading of the pattern, while
// these test the thing that must actually hold. The engine is the reference.

var matcherPatterns = []string{
	`[0-9]+`,
	`^[0-9]+$`,
	`^[a-z][a-z0-9-]*$`,
	`[0-9a-fA-F]+`,
	`^#[0-9a-f]{6}$`,
	`a*`,
	`a+b`,
	`(ab)+`,
	`a|bc|def`,
	`^$`,
	`.`,
	`.*`,
	`^.*$`,
	`[^0-9]+`,
	`\d{3}-\d{4}`,
	`^[A-Z][a-z]+( [A-Z][a-z]+)*$`,
	`x`,
	``,
	`(a|b)*abb`,
	`^\s*$`,
}

var matcherInputs = []string{
	"", "0", "7", "42", "abc", "a", "b", "ab", "abb", "aabb", "x", "xy",
	"123", "1a2", "0123456789", "hello", "Hello World", "hello-world",
	"#ff00aa", "#FF00AA", "#ff00a", "deadbeef", "123-4567", "12-345",
	"   ", "\t", " a ",
	// Newlines are the interesting case: MultiLine is on by default, so `^`/`$`
	// fire at every '\n', and a trailing newline is handled differently from an
	// interior one (IsMatch omits the trailing beginning-of-line).
	"\n", "\n\n", "a\n", "\na", "a\nb", "1\n2", "abc\n", "\nabc", "a\n\nb",
	"12\n34\n", "\n12", "x\ny\nz",
}

func TestMatcher_AgreesWithIsMatch(t *testing.T) {
	for _, pat := range matcherPatterns {
		re, err := Compile(pat)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pat, err)
		}
		m, err := re.Matcher(4096)
		if err != nil {
			t.Fatalf("Matcher(%q): %v", pat, err)
		}
		for _, in := range matcherInputs {
			want, err := re.IsMatch([]byte(in))
			if err != nil {
				t.Fatalf("IsMatch(%q, %q): %v", pat, in, err)
			}
			if got := m.Match([]byte(in)); got != want {
				t.Errorf("pattern %q, input %q: table says %v, engine says %v", pat, in, got, want)
			}
		}
	}
}

// The same agreement over generated inputs, which reach byte sequences the curated
// list above does not — including every single byte and every two-byte pair drawn
// from a small but adversarial alphabet.
func TestMatcher_AgreesWithIsMatch_Generated(t *testing.T) {
	alphabet := []byte("ab0-#\n \t")
	var inputs [][]byte
	for _, b := range alphabet {
		inputs = append(inputs, []byte{b})
		for _, c := range alphabet {
			inputs = append(inputs, []byte{b, c})
			for _, d := range alphabet {
				inputs = append(inputs, []byte{b, c, d})
			}
		}
	}
	for _, pat := range matcherPatterns {
		re, err := Compile(pat)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pat, err)
		}
		m, err := re.Matcher(4096)
		if err != nil {
			t.Fatalf("Matcher(%q): %v", pat, err)
		}
		for _, in := range inputs {
			want, err := re.IsMatch(in)
			if err != nil {
				t.Fatalf("IsMatch(%q, %q): %v", pat, in, err)
			}
			if got := m.Match(in); got != want {
				t.Errorf("pattern %q, input %q: table says %v, engine says %v", pat, in, got, want)
			}
		}
	}
}

// Every byte value, not just printable ones — the table has 256 columns and all of
// them must be right.
func TestMatcher_AllByteValues(t *testing.T) {
	re, err := Compile(`^[\x00-\xff]$`)
	if err != nil {
		t.Skipf("pattern unsupported: %v", err)
	}
	m, err := re.Matcher(4096)
	if err != nil {
		t.Fatalf("Matcher: %v", err)
	}
	for b := 0; b < 256; b++ {
		in := []byte{byte(b)}
		want, err := re.IsMatch(in)
		if err != nil {
			t.Fatalf("IsMatch(%v): %v", b, err)
		}
		if got := m.Match(in); got != want {
			t.Errorf("byte %d: table says %v, engine says %v", b, got, want)
		}
	}
}

// A lookbehind is refused rather than approximated: its gate depends on text
// *preceding* the input, which a flat byte table cannot represent.
func TestMatcher_RefusesLookbehind(t *testing.T) {
	re, err := Compile(`(?<=a)b`)
	if err != nil {
		t.Skipf("lookbehind unsupported by the parser: %v", err)
	}
	if _, err := re.Matcher(4096); err == nil {
		t.Error("expected a lookbehind pattern to be refused")
	} else if !strings.Contains(err.Error(), "lookbehind") {
		t.Errorf("expected the refusal to name the lookbehind, got: %v", err)
	}
}

// A pattern whose DFA exceeds the cap is refused rather than emitting a table
// larger than the program using it.
func TestMatcher_RefusesOversizedDFA(t *testing.T) {
	re, err := Compile(`^[0-9]{40}$`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := re.Matcher(4); err == nil {
		t.Error("expected an oversized DFA to be refused")
	}
}

// The dense renumbering keeps dead at 0, so generated code can compare against one
// constant, and every transition stays inside the table.
func TestMatcher_TableIsWellFormed(t *testing.T) {
	for _, pat := range matcherPatterns {
		m, err := CompileMatcher(pat, 4096)
		if err != nil {
			t.Fatalf("CompileMatcher(%q): %v", pat, err)
		}
		if len(m.Trans) != m.NumStates*256 {
			t.Errorf("pattern %q: table is %d entries, want %d", pat, len(m.Trans), m.NumStates*256)
		}
		if len(m.NewlineLast) != m.NumStates || len(m.AcceptFinal) != m.NumStates {
			t.Errorf("pattern %q: per-state arrays are the wrong length", pat)
		}
		if m.AcceptFinal[DeadState] {
			t.Errorf("pattern %q: the dead state must never accept", pat)
		}
		check := func(what string, v int32) {
			if v < 0 || int(v) >= m.NumStates {
				t.Errorf("pattern %q: %s leaves the table (%d of %d)", pat, what, v, m.NumStates)
			}
		}
		check("start", m.Start)
		for i, v := range m.Trans {
			check(fmt.Sprintf("Trans[%d]", i), v)
		}
		for i, v := range m.NewlineLast {
			check(fmt.Sprintf("NewlineLast[%d]", i), v)
		}
		// Dead is absorbing: no byte leads out of it.
		for b := 0; b < 256; b++ {
			if m.Trans[int(DeadState)*256+b] != DeadState {
				t.Errorf("pattern %q: byte %d escapes the dead state", pat, b)
			}
		}
	}
}
