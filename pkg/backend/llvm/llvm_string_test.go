package llvm

import (
	"strings"
	"testing"
)

// A string lowers to an immutable fat pointer { i8* data, i64 len } (bytes in a
// private global constant). A string can't reach the u8 exit code directly, so
// every case funnels it through equality/match (→ bool → int). buildAndRun
// compiles with clang and runs, so a wrong length/memcmp shows up as the wrong
// exit code.
func TestExec_StringEquality(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Equal strings compare equal.
		{
			"equal via param",
			`let eq = (a: string, b: string) -> bool => a == b
			 let main = () -> u8 => if eq("hello", "hello") { 1 } else { 0 }`,
			1,
		},
		// Same length, different bytes.
		{
			"different bytes",
			`let eq = (a: string, b: string) -> bool => a == b
			 let main = () -> u8 => if eq("cat", "dog") { 1 } else { 0 }`,
			0,
		},
		// Different lengths (the length check rejects before/around memcmp).
		{
			"different lengths",
			`let eq = (a: string, b: string) -> bool => a == b
			 let main = () -> u8 => if eq("ab", "abc") { 1 } else { 0 }`,
			0,
		},
		// A prefix is not equal to the longer string (min-length memcmp + len check).
		{
			"prefix not equal",
			`let eq = (a: string, b: string) -> bool => a == b
			 let main = () -> u8 => if eq("abc", "ab") { 1 } else { 0 }`,
			0,
		},
		// `!=` negates.
		{
			"not-equal operator",
			`let ne = (a: string, b: string) -> bool => a != b
			 let main = () -> u8 => if ne("x", "y") { 5 } else { 0 }`,
			5,
		},
		// Empty strings are equal (n = 0 memcmp).
		{
			"empty strings equal",
			`let eq = (a: string, b: string) -> bool => a == b
			 let main = () -> u8 => if eq("", "") { 7 } else { 0 }`,
			7,
		},
		// A `let`-bound string round-trips through alloca/store/load.
		{
			"let binding",
			`let main = () -> u8 => {
			   let s: string = "lyra"
			   if s == "lyra" { 3 } else { 0 }
			 }`,
			3,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A string parameter and a string return value both lower (the fat pointer is
// passed/returned by value), so a string helper composes.
func TestExec_StringFunction(t *testing.T) {
	t.Parallel()
	src := `let greet = () -> string => "hello"
	 let main = () -> u8 => if greet() == "hello" { 11 } else { 0 }`
	if got := buildAndRun(t, src); got != 11 {
		t.Errorf("string function: exited %d; want 11", got)
	}
}

// A string scrutinee matches via the shared scalar ladder, each literal arm a
// byte-equality test; an identifier catch-all binds the whole string.
func TestExec_StringMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// A literal arm is taken.
		{
			"literal arm",
			`let f = (s: string) -> u8 => match s {
			   "yes" => 1,
			   "no"  => 2,
			   _     => 0,
			 }
			 let main = () -> u8 => f("no")`,
			2,
		},
		// The wildcard is taken when no literal matches.
		{
			"wildcard fallthrough",
			`let f = (s: string) -> u8 => match s {
			   "yes" => 1,
			   "no"  => 2,
			   _     => 0,
			 }
			 let main = () -> u8 => f("maybe")`,
			0,
		},
		// An identifier catch-all binds the string, usable in the arm body.
		{
			"identifier catch-all binds the string",
			`let f = (s: string) -> u8 => match s {
			   "" => 0,
			   other => if other == "hi" { 4 } else { 5 },
			 }
			 let main = () -> u8 => f("hi")`,
			4,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// Concatenation heap-allocates a new string (lyra_rc_alloc + memcpy) and returns
// a fat pointer into the box's payload — observed here by comparing the result to
// a literal via ==, and by matching on it. Covers empty operands (memcpy n=0), a
// concat-of-concats chain, and concatenation of runtime (parameter) strings whose
// bytes don't live in a constant global.
func TestExec_StringConcat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"two literals",
			`let main = () -> u8 => {
			   let s: string = "foo" ++ "bar"
			   if s == "foobar" { 1 } else { 0 }
			 }`,
			1,
		},
		{
			"empty left operand",
			`let main = () -> u8 => {
			   let s: string = "" ++ "x"
			   if s == "x" { 1 } else { 0 }
			 }`,
			1,
		},
		{
			"empty right operand",
			`let main = () -> u8 => {
			   let s: string = "x" ++ ""
			   if s == "x" { 1 } else { 0 }
			 }`,
			1,
		},
		{
			"both empty",
			`let main = () -> u8 => {
			   let s: string = "" ++ ""
			   if s == "" { 1 } else { 0 }
			 }`,
			1,
		},
		{
			"chain of concats (left-associated)",
			`let main = () -> u8 => {
			   let s: string = "a" ++ "b" ++ "c" ++ "d"
			   if s == "abcd" { 1 } else { 0 }
			 }`,
			1,
		},
		{
			"concat of runtime (parameter) strings",
			`let join = (a: string, b: string) -> string => a ++ b
			 let main = () -> u8 => if join("hel", "lo") == "hello" { 1 } else { 0 }`,
			1,
		},
		{
			"a heap string matches",
			`let main = () -> u8 => {
			   let s: string = "yes" ++ "!"
			   match s {
			     "yes!" => 7,
			     _      => 0,
			   }
			 }`,
			7,
		},
		{
			"length of the concatenated string is the sum",
			`let main = () -> u8 => {
			   let s: string = "abc" ++ "de"
			   if s == "abcde" && s != "abcd" { 1 } else { 0 }
			 }`,
			1,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_StringConcatIR pins the lowering: a call to the emitted allocator and
// two memcpys building the result, plus the runtime being defined (not just
// declared) inside the module.
func TestEmit_StringConcatIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let main = () -> u8 => {
	   let s: string = "foo" ++ "bar"
	   if s == "foobar" { 1 } else { 0 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define i8* @lyra_rc_alloc(", // runtime is defined in-module, not an extern
		"call i8* @lyra_rc_alloc(",   // concatenation allocates
		"@memcpy",                    // and copies both halves
	} {
		if !strings.Contains(got, want) {
			t.Errorf("concat IR missing %q:\n%s", want, got)
		}
	}
}

// A program that never allocates carries no runtime — it's emitted lazily.
func TestEmit_NoRuntimeWhenUnused(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, "let main = () -> u8 => 42\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "lyra_rc_alloc") {
		t.Errorf("a non-allocating program should not emit the RC runtime:\n%s", got)
	}
}

// TestEmit_StringLiteralIsPinnedBox pins the literal representation: a static
// { i64, [N x i8] } box with a PinnedRC (-1) header, so retain/release no-op on
// it and the ownership model can treat every string value uniformly. `data`
// points past the header (index 0,1,0 → box+8), no runtime allocation.
func TestEmit_StringLiteralIsPinnedBox(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let main = () -> u8 => if "hi" == "hi" { 1 } else { 0 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`private constant { i64, [2 x i8] } { i64 -1,`, // pinned box in static memory
		`i32 0, i32 1, i32 0`,                          // data points past the rc header
	} {
		if !strings.Contains(got, want) {
			t.Errorf("literal-box IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_StringIR pins the representation: a private constant for the bytes,
// the { i8*, i64 } fat pointer, and a memcmp call for equality.
func TestEmit_StringIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let eq = (a: string, b: string) -> bool => a == b
	 let main = () -> u8 => if eq("hi", "hi") { 1 } else { 0 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"private", "@memcmp", "{ i8*, i64 }"} {
		if !strings.Contains(got, want) {
			t.Errorf("string IR missing %q:\n%s", want, got)
		}
	}
}

// Escaped string patterns are deferred (they need Lyra-specific unescaping) and
// must error loudly. Concatenation and interpolation are no longer here — they
// lower now (TestExec_StringConcat, TestExec_StringInterpolation).
func TestEmit_StringDeferred(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			"escaped pattern",
			`let f = (s: string) -> u8 => match s {
			   "a\tb" => 1,
			   _      => 0,
			 }
			 let main = () -> u8 => f("x")`,
			"escaped string patterns",
		},
	}
	for _, c := range cases {
		_, err := emitSource(t, c.src)
		if err == nil {
			t.Errorf("%s: expected a not-implemented error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: expected error containing %q, got: %v", c.name, c.wantErr, err)
		}
	}
}
