package llvm

import (
	"strings"
	"testing"
)

// Float literals, arithmetic, and comparisons lower to real IR. A float can't
// reach the u8 exit code directly (there's no float→int conversion), so every
// case funnels the float through a comparison (→ bool → int) — which also
// exercises the fcmp path. buildAndRun compiles with clang and runs, so an invalid
// float instruction or a wrong predicate shows up as a rejected build or the wrong
// exit code.
func TestExec_FloatArithmetic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// A bare f64 literal, compared.
		{
			"literal compare",
			`let main = () -> u8 => {
			   let x: f64 = 2.5
			   if x > 2.0 { 1 } else { 0 }
			 }`,
			1,
		},
		// Addition (fadd), bracketed by a two-sided comparison so an off result fails.
		{
			"add",
			`let main = () -> u8 => {
			   let x: f64 = 1.5 + 1.5
			   if x > 2.9 && x < 3.1 { 42 } else { 0 }
			 }`,
			42,
		},
		// Division (fdiv): 10.0 / 4.0 == 2.5.
		{
			"div",
			`let main = () -> u8 => {
			   let x: f64 = 10.0 / 4.0
			   if x > 2.4 && x < 2.6 { 25 } else { 0 }
			 }`,
			25,
		},
		// Subtraction and multiplication together: (5.0 - 1.0) * 2.0 == 8.0.
		{
			"sub and mul",
			`let main = () -> u8 => {
			   let x: f64 = (5.0 - 1.0) * 2.0
			   if x > 7.9 && x < 8.1 { 8 } else { 0 }
			 }`,
			8,
		},
		// f32 width (LLVM `float`, not `double`): 1.0 + 0.5 == 1.5.
		{
			"f32 add",
			`let main = () -> u8 => {
			   let x: f32 = 1.0 + 0.5
			   if x > 1.4 && x < 1.6 { 3 } else { 0 }
			 }`,
			3,
		},
		// Negation via subtraction, compared as negative.
		{
			"negative value",
			`let main = () -> u8 => {
			   let x: f64 = 0.0 - 3.5
			   if x < 0.0 { 7 } else { 0 }
			 }`,
			7,
		},
		// Truncated modulo `%` (frem, C fmod): 7.5 % 2.0 == 1.5.
		{
			"mod",
			`let main = () -> u8 => {
			   let x: f64 = 7.5 % 2.0
			   if x > 1.4 && x < 1.6 { 15 } else { 0 }
			 }`,
			15,
		},
		// Floored remainder `%%`: (-7.5) %% 2.0 == 0.5 (sign follows the divisor),
		// distinct from `%`'s -1.5. The dividend is built by subtraction.
		{
			"floored remainder",
			`let main = () -> u8 => {
			   let a: f64 = 0.0 - 7.5
			   let x: f64 = a %% 2.0
			   if x > 0.4 && x < 0.6 { 5 } else { 0 }
			 }`,
			5,
		},
		// Compound assignment on a float var (fadd via `+=`).
		{
			"compound assign",
			`let main = () -> u8 => {
			   var x: f64 = 1.0
			   x += 2.5
			   if x > 3.4 && x < 3.6 { 9 } else { 0 }
			 }`,
			9,
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

// Numeric conversions involving float: int→float (sitofp/uitofp, from the
// source's signedness) and float→float widening (fpext). float→int via the
// `i64(x)`-style conversion call is still a typecheck error — the explicit
// escape hatch is the `.floor()`/`.ceil()`/`.round()` builtin methods
// (TestExec_FloatRounding), a different call shape entirely.
func TestExec_FloatConversions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Signed int → float, then divided: f64(7) / 2.0 == 3.5.
		{
			"signed int to float",
			`let main = () -> u8 => {
			   let n: i64 = 7
			   let x: f64 = f64(n) / 2.0
			   if x > 3.4 && x < 3.6 { 35 } else { 0 }
			 }`,
			35,
		},
		// Unsigned int → float (uitofp): f64(200) stays 200.0.
		{
			"unsigned int to float",
			`let main = () -> u8 => {
			   let n: u8 = 200
			   let x: f64 = f64(n)
			   if x > 199.9 && x < 200.1 { 20 } else { 0 }
			 }`,
			20,
		},
		// float→float widening (fpext): f64(1.5f32) == 1.5.
		{
			"f32 to f64 widening",
			`let main = () -> u8 => {
			   let a: f32 = 1.5
			   let b: f64 = f64(a)
			   if b > 1.4 && b < 1.6 { 15 } else { 0 }
			 }`,
			15,
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

// Explicit float→int rounding via the .floor()/.ceil()/.round() builtin
// methods (builtins.go's floatRoundingOps): each lowers to the matching
// llvm.<op>.<width> intrinsic, then fptosi to the builtin's fixed i64 return
// type — the escape hatch the numeric-conversion typecheck error points to.
func TestExec_FloatRounding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"floor positive", `let main = () -> u8 => {
			   let x: f64 = 3.7
			   u8(x.floor())
			 }`, 3},
		{"floor negative", `let main = () -> u8 => {
			   let x: f64 = -3.2
			   let n: i64 = x.floor()
			   if n == -4 { 1 } else { 0 }
			 }`, 1},
		{"ceil positive", `let main = () -> u8 => {
			   let x: f64 = 3.2
			   u8(x.ceil())
			 }`, 4},
		{"ceil negative", `let main = () -> u8 => {
			   let x: f64 = -3.7
			   let n: i64 = x.ceil()
			   if n == -3 { 1 } else { 0 }
			 }`, 1},
		{"round half away from zero", `let main = () -> u8 => {
			   let x: f64 = 3.5
			   u8(x.round())
			 }`, 4},
		{"round down", `let main = () -> u8 => {
			   let x: f64 = 3.4
			   u8(x.round())
			 }`, 3},
		{"round on f32", `let main = () -> u8 => {
			   let x: f32 = 2.5
			   u8(x.round())
			 }`, 3},
		{"narrow via explicit int conversion", `let main = () -> u8 => {
			   let x: f64 = 200.9
			   u8(x.floor())
			 }`, 200},
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

// A float parameter and a float return value both lower, so a float helper
// function composes: dbl(2.5) == 5.0.
func TestExec_FloatFunction(t *testing.T) {
	t.Parallel()
	src := `let dbl = (x: f64) -> f64 => x * 2.0
	 let main = () -> u8 => {
	   let y: f64 = dbl(2.5)
	   if y > 4.9 && y < 5.1 { 50 } else { 0 }
	 }`
	if got := buildAndRun(t, src); got != 50 {
		t.Errorf("float function: exited %d; want 50", got)
	}
}

// A float scrutinee lowers to the same if-else ladder as an integer scalar match,
// but with `fcmp` tests: an ordered `oeq` for a literal arm, and a two-sided
// ordered range check for a range arm. A float match always needs a wildcard (the
// reals can't be enumerated); an identifier catch-all binds the float value.
func TestExec_FloatMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Literal arm hit (fcmp oeq).
		{
			"literal hit",
			`let f = (x: f64) -> u8 => match x {
			   1.5 => 1,
			   2.5 => 2,
			   _ => 0,
			 }
			 let main = () -> u8 => f(2.5)`,
			2,
		},
		// No literal matches → the wildcard arm.
		{
			"wildcard fallthrough",
			`let f = (x: f64) -> u8 => match x {
			   1.5 => 1,
			   2.5 => 2,
			   _ => 0,
			 }
			 let main = () -> u8 => f(9.5)`,
			0,
		},
		// Range arms (half-open): 1.5 falls in [1.0, 2.0).
		{
			"range arm",
			`let f = (x: f64) -> u8 => match x {
			   0.0..<1.0 => 1,
			   1.0..<2.0 => 2,
			   _ => 3,
			 }
			 let main = () -> u8 => f(1.5)`,
			2,
		},
		// An identifier catch-all binds the float, usable in a comparison.
		{
			"identifier catch-all binds the float",
			`let f = (x: f64) -> u8 => match x {
			   0.0 => 100,
			   other => if other > 5.0 { 7 } else { 8 },
			 }
			 let main = () -> u8 => f(9.0)`,
			7,
		},
		// f32 scrutinee (LLVM `float`).
		{
			"f32 literal",
			`let f = (x: f32) -> u8 => match x {
			   1.5 => 15,
			   _ => 0,
			 }
			 let main = () -> u8 => f(1.5)`,
			15,
		},
		// A guard on a float match arm (data/scalar guard machinery reused).
		{
			"float match guard",
			`let f = (x: f64) -> u8 => match x {
			   n if n > 3.0 => 1,
			   _ => 2,
			 }
			 let main = () -> u8 => f(5.0)`,
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

// TestEmit_FloatMatchIR pins the float-match ladder: `fcmp` tests and a `br i1`
// per arm, no `switch` (a float match has no switch form).
func TestEmit_FloatMatchIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let f = (x: f64) -> u8 => match x {
	   1.5 => 1,
	   _ => 0,
	 }
	 let main = () -> u8 => f(1.5)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fcmp", "br i1"} {
		if !strings.Contains(got, want) {
			t.Errorf("float-match IR missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "switch") {
		t.Errorf("a float match should lower to an fcmp ladder, not a switch:\n%s", got)
	}
}

// TestEmit_FloatIR pins the float instruction selection: a `double` literal, an
// `fadd`, and an `fcmp` (not their integer counterparts).
func TestEmit_FloatIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let main = () -> u8 => {
	   let x: f64 = 1.5 + 1.5
	   if x > 2.9 { 1 } else { 0 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fadd", "fcmp", "double"} {
		if !strings.Contains(got, want) {
			t.Errorf("float IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_FloatRoundingIR pins the instruction selection for a rounding
// builtin: a call to the matched llvm.<op>.<width> intrinsic followed by an
// fptosi to the fixed i64 return type — not, say, a manual truncation.
func TestEmit_FloatRoundingIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let main = () -> u8 => {
	   let x: f64 = 3.7
	   u8(x.floor())
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"call double @llvm.floor.f64", "fptosi"} {
		if !strings.Contains(got, want) {
			t.Errorf("rounding IR missing %q:\n%s", want, got)
		}
	}
}
