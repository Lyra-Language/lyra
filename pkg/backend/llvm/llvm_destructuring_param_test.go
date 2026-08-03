package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A **destructuring parameter** — `let sum = ((a, b): (i64, i64)) -> i64 => a + b` —
// is the fourth destructuring form, and the last one that did not lower. It is the
// irrefutable one: a parameter has no failure path (there is no `else`, and a function
// cannot decline to be called), so it goes through the same patternMatcher the other
// three do and refuses a pattern that can fail.
//
// It reaches every shape of function at once, because they all bind parameters through
// bindParameters: a plain function, a generic specialization, a lifted closure, and a
// trait-impl method (whose clause patterns *are* its parameters, via typetable.Resolution.Lambda).
var destructuringParamCases = []struct {
	name string
	src  string
	want int
}{
	{
		"tuple parameter",
		`let sum = ((a, b): (i64, i64)) -> i64 => a + b
		 let main = () -> u8 => u8(sum((3, 4)))`,
		7,
	},
	{
		"struct parameter",
		`struct Pt { x: i64, y: i64 }
		 let area = ({ x, y }: Pt) -> i64 => x * y
		 let main = () -> u8 => u8(area(Pt { x: 3, y: 4 }))`,
		12,
	},
	{
		// Nested aggregates recurse through aggPatternBind, exactly as a nested
		// sub-pattern in a match arm does.
		"nested tuple parameter",
		`let f = (((a, b), c): ((i64, i64), i64)) -> i64 => a + b + c
		 let main = () -> u8 => u8(f(((1, 2), 3)))`,
		6,
	},
	{
		// A wildcard element binds nothing and imposes no test, so the pattern is
		// still irrefutable.
		"wildcard element",
		`let fst = ((a, _): (i64, i64)) -> i64 => a * 2
		 let main = () -> u8 => u8(fst((5, 99)))`,
		10,
	},
	{
		"a struct field renamed by its sub-pattern",
		`struct Pt { x: i64, y: i64 }
		 let f = ({ x: a, y: b }: Pt) -> i64 => a - b
		 let main = () -> u8 => u8(f(Pt { x: 9, y: 2 }))`,
		7,
	},
	{
		// Mixed with ordinary parameters, and not in first position — the ir.Param
		// index must still line up.
		"one destructured parameter among plain ones",
		`let f = (n: i64, (a, b): (i64, i64), m: i64) -> i64 => n + a + b + m
		 let main = () -> u8 => u8(f(1, (2, 3), 4))`,
		10,
	},
	{
		// A `shared` parameter arrives as a pointer to its ref-counted box, which
		// patternMatcher unboxes before the pattern sees it — the same path a
		// `shared` match scrutinee takes.
		"shared struct parameter",
		`struct Pt { x: i64, y: i64 }
		 let area = ({ x, y }: shared Pt) -> i64 => x * y
		 let main = () -> u8 => {
		   let p: shared Pt = Pt { x: 3, y: 5 }
		   u8(area(p))
		 }`,
		15,
	},
	{
		// `ref` is a read-only borrow arriving as a pointer, so the value is loaded
		// and destructured from there. The bound names are copies either way, which
		// is why this is sound where `mut` is refused.
		"ref struct parameter",
		`struct Pt { x: i64, y: i64 }
		 let area = ({ x, y }: ref Pt) -> i64 => x * y
		 let main = () -> u8 => {
		   let p = Pt { x: 2, y: 5 }
		   u8(area(p))
		 }`,
		10,
	},
	{
		// A generic function has no non-generic form to emit, so this only works if
		// the specialization path binds parameters the same way (it shares
		// defineFunctionInto).
		"generic function specialization",
		`let fst = ((a, b): (t, u)) -> t => a
		 let main = () -> u8 => u8(fst((7, 9)))`,
		7,
	},
	{
		// A lambda *value* is lifted to a closure whose slot 0 is the environment,
		// so its parameters are offset by one.
		"lambda value passed as an argument",
		`let apply = (g: ((i64, i64)) -> i64, p: (i64, i64)) -> i64 => g(p)
		 let main = () -> u8 => u8(apply(((a, b): (i64, i64)) -> i64 => a * b, (3, 4)))`,
		12,
	},
	{
		"closure with a capture and a destructured parameter",
		`let mk = (n: i64) -> ((i64, i64)) -> i64 => ((a, b): (i64, i64)) -> i64 => a + b + n
		 let main = () -> u8 => {
		   let f = mk(1)
		   u8(f((3, 4)))
		 }`,
		8,
	},
	{
		// A trait-impl method's clause patterns are its parameters, and the impl
		// writes no annotation — the trait's signature supplies the type the pattern
		// is walked against (checkTraitImplMethodBody).
		"trait-impl method receiver",
		`struct Pt { x: i64, y: i64 }
		 trait Summable { total: (Self) -> i64 }
		 impl Summable for Pt { total = ({ x, y }) => x + y }
		 let main = () -> u8 => {
		   let p = Pt { x: 3, y: 4 }
		   u8(p.total())
		 }`,
		7,
	},
	{
		"trait-impl method argument",
		`struct Pt { x: i64, y: i64 }
		 trait Shift { by: (Self, (i64, i64)) -> i64 }
		 impl Shift for Pt { by = (self, (dx, dy)) => self.x + dx + self.y + dy }
		 let main = () -> u8 => {
		   let p = Pt { x: 1, y: 2 }
		   u8(p.by((3, 4)))
		 }`,
		10,
	},
	{
		// A managed field bound out of a *borrowed* parameter is a borrow: the caller
		// still owns the string, so the callee must not release it — reading `p.name`
		// after the call is what would fault if it did.
		"managed field of a borrowed parameter",
		`struct Person { name: string, age: i64 }
		 let show = ({ name, age }: Person) -> i64 => {
		   print(name)
		   age
		 }
		 let main = () -> u8 => {
		   let p = Person { name: "Ada", age: 36 }
		   let n = show(p)
		   print(p.name)
		   u8(n)
		 }`,
		36,
	},
	{
		// An `own` parameter transfers the caller's +1, so the callee owes the
		// release — of the *whole* aggregate, since the pattern need not name every
		// managed field (`age` is bound, `name` is not, and `name` still must be
		// freed).
		"own parameter releases the fields its pattern does not name",
		`struct Person { name: string, age: i64 }
		 let take = ({ age }: own Person) -> i64 => age
		 let main = () -> u8 => u8(take(Person { name: "Ada", age: 36 }))`,
		36,
	},
	{
		// The other half: a managed field bound out of an `own` parameter and then
		// returned outlives the aggregate's release, so it must be retained.
		"managed field escapes an own parameter",
		`struct Person { name: string, age: i64 }
		 let name_of = ({ name, age }: own Person) -> string => name
		 let main = () -> u8 => {
		   let n = name_of(Person { name: "Ada", age: 36 })
		   print(n)
		   u8(0)
		 }`,
		0,
	},
}

func TestExec_DestructuringParameters(t *testing.T) {
	t.Parallel()
	for _, c := range destructuringParamCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestExec_DestructuringParametersASan runs the same cases under AddressSanitizer.
// The ones that matter here are the managed-field cases: a bound name is a *copy* of
// a field out of a value someone else owns, so releasing it per-name would double-free
// (borrowed parameter), and not releasing the aggregate at all would leak an `own` one.
func TestExec_DestructuringParametersASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range destructuringParamCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_DestructuringParameterRefused pins the two forms that do *not* lower, since
// both would be silently wrong rather than visibly broken.
func TestEmit_DestructuringParameterRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The typechecker admits a value-testing sub-pattern in a parameter, and
			// there is nowhere for the failing path to go — no `else`, no next arm.
			"a refutable parameter pattern",
			`let f = ((1, b): (i64, i64)) -> i64 => b
			 let main = () -> u8 => u8(f((1, 2)))`,
			"parameter pattern must match every value of its type",
		},
		{
			// `mut` is a mutable borrow: the bindings would be copies, so a write
			// could not reach the caller. That is the whole content of `mut`, so
			// lowering it would be a borrow that silently is not one.
			"a mut parameter destructured",
			`struct Pt { x: i64, y: i64 }
			 let f = ({ x, y }: mut Pt) -> i64 => x + y
			 let main = () -> u8 => {
			   let mut p = Pt { x: 1, y: 2 }
			   u8(f(p))
			 }`,
			"`mut` parameter cannot be destructured",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := emitSource(t, c.src)
			if err == nil {
				t.Fatalf("%s: expected a lowering error, got none", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
			}
		})
	}
}

// TestEmit_OwnDestructuredParamIR pins the refcount shape behind the two `own` cases
// above: the aggregate is released exactly once (one release covering every managed
// field, not one per bound name), and a field that escapes is retained on its way out.
func TestEmit_OwnDestructuredParamIR(t *testing.T) {
	t.Parallel()
	count := func(src, needle string) int {
		got, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return strings.Count(got, needle)
	}

	// `name` is never bound, and is still freed: the release is of the whole Person.
	unnamed := `struct Person { name: string, age: i64 }
	 let take = ({ age }: own Person) -> i64 => age
	 let main = () -> u8 => u8(take(Person { name: "Ada", age: 36 }))`
	if n := count(unnamed, "call void @lyra_rc_release"); n != 1 {
		t.Errorf("own parameter: want exactly 1 release, got %d", n)
	}

	// The escaping field outlives that release, so it carries a +1 of its own.
	escaping := `struct Person { name: string, age: i64 }
	 let name_of = ({ name, age }: own Person) -> string => name
	 let main = () -> u8 => {
	   let n = name_of(Person { name: "Ada", age: 36 })
	   print(n)
	   u8(0)
	 }`
	if n := count(escaping, "call void @lyra_rc_retain"); n < 1 {
		t.Errorf("escaping field: want at least 1 retain, got %d", n)
	}
}
