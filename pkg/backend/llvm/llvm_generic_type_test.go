package llvm

import (
	"strings"
	"testing"
)

// Generic *types*, end to end: `struct Box<t>` and `data Maybe<t>` construct, read,
// match, and lower — one emitted LLVM type per distinct instantiation.
//
// Unlike generic functions, whose instantiations the typechecker collects into a
// table, a generic type is materialized *lazily* by lowerType (generic_types.go).
// There is no single syntactic site that "uses" a type: `Box<i64>` can arrive as a
// construction, a parameter, a return, a field of another type, an array element, or
// a type argument of another generic — and all of those already funnel through
// lowerType, so materializing what it is handed cannot fall out of sync with what the
// program actually uses.
//
// Before this, a generic type was unusable from both ends: the typechecker solved the
// substitution at a construction and then discarded it (so a field read returned the
// type *variable* and an annotated binding reported "cannot assign Box to Box"), and
// the backend tried to lay out the declaration itself and failed on "unknown type: t".
func TestExec_GenericTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The type argument is inferred from the field value.
			"generic struct, inferred",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b = Box { value: 5 }
			   u8(b.value)
			 }`,
			5,
		},
		{
			"generic struct, annotated",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b: Box<i64> = Box { value: 7 }
			   u8(b.value)
			 }`,
			7,
		},
		{
			"generic struct, turbofish",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b = Box::<u8> { value: 5 }
			   b.value
			 }`,
			5,
		},
		{
			// Two instantiations of one declaration coexist as two distinct LLVM types.
			"two instantiations coexist",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let a = Box { value: 30 }
			   let b = Box { value: true }
			   if b.value { u8(a.value) } else { 0 }
			 }`,
			30,
		},
		{
			// A generic instantiated at another instantiation of itself — the nested
			// type argument has to be substituted before the name is mangled, or both
			// levels would collide on one layout.
			"nested Box<Box<i64>>",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let inner = Box { value: 9 }
			   let outer = Box { value: inner }
			   u8(outer.value.value)
			 }`,
			9,
		},
		{
			"two type parameters",
			`struct Pair<a, b> { first: a, second: b }
			 let main = () -> u8 => {
			   let p = Pair { first: 40, second: u8(2) }
			   u8(p.first) + p.second
			 }`,
			42,
		},
		{
			"through a parameter and a return",
			`struct Box<t> { value: t }
			 let get = (b: Box<i64>) -> i64 => b.value
			 let mk = () -> Box<i64> => Box { value: 11 }
			 let main = () -> u8 => u8(get(mk()))`,
			11,
		},
		{
			"interior assignment to a generic field",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   var b = Box { value: 1 }
			   b.value = 3
			   u8(b.value)
			 }`,
			3,
		},
		{
			// A generic `data` type: the argument is solved from the constructor's
			// payload, the same unifier a generic call uses.
			"generic data, inferred",
			`data Maybe<t> = None | Some(t)
			 let main = () -> u8 => {
			   let m = Some(5)
			   match m { Some(x) => u8(x), None => 0 }
			 }`,
			5,
		},
		{
			// A nullary constructor pins nothing, so the annotation supplies the
			// argument.
			"generic data, nullary needs context",
			`data Maybe<t> = None | Some(t)
			 let main = () -> u8 => {
			   let m: Maybe<i64> = None
			   match m { Some(x) => u8(x), None => 9 }
			 }`,
			9,
		},
		{
			"generic data through a call",
			`data Maybe<t> = None | Some(t)
			 let unwrap = (m: Maybe<i64>) -> i64 => match m { Some(x) => x, None => 0 }
			 let main = () -> u8 => u8(unwrap(Some(12)))`,
			12,
		},
		{
			// A generic *named tuple* — positional, so its parameters are solved from
			// the supplied elements, the same unification a data constructor drives.
			"generic named tuple",
			`tuple Pair<t>(t, t)
			 let main = () -> u8 => {
			   let p = Pair(3, 4)
			   u8(p.0 + p.1)
			 }`,
			7,
		},
		{
			// The case the lazy declare-then-define split exists for: a recursive
			// generic type. Laying out the tail re-enters lowerType for the *same*
			// instantiation, which must find the placeholder rather than recurse
			// forever. (The cycle is broken by `shared`, per lyra-E014.)
			"recursive generic list",
			`data List<t> = Nil | Cons(t, shared List<t>)
			 let sum = (xs: shared List<i64>) -> i64 => match xs {
			   Cons(h, rest) => h + sum(rest),
			   Nil => 0,
			 }
			 let main = () -> u8 => {
			   let l: shared List<i64> = Cons(1, Cons(2, Cons(3, Nil)))
			   u8(sum(l))
			 }`,
			6,
		},
	}
	clang := lookClang(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A generic type at a *managed* type argument. Managed-ness is a property of the type
// argument, not of the declaration, so every ownership decision has to be made against
// the substituted type — the generic-function lesson (per-instantiation ownership
// analysis) in its type-level form.
func TestExec_GenericTypesManaged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"Box<string> field read",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b = Box { value: "ab" ++ "cd" }
			   if b.value == "abcd" { 2 } else { 1 }
			 }`,
			2,
		},
		{
			// A copy that is *not* a last use, so the string genuinely needs a second
			// reference. This is the shape that was a double free.
			"Box<string> copied, both used",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b = Box { value: "ab" ++ "cd" }
			   let c = b
			   if b.value == "abcd" { if c.value == "abcd" { 2 } else { 1 } } else { 1 }
			 }`,
			2,
		},
		{
			"Maybe<string> matched",
			`data Maybe<t> = None | Some(t)
			 let main = () -> u8 => {
			   let m = Some("x" ++ "y")
			   match m { Some(s) => if s == "xy" { 3 } else { 1 }, None => 0 }
			 }`,
			3,
		},
		{
			"Maybe<string> copied, both used",
			`data Maybe<t> = None | Some(t)
			 let main = () -> u8 => {
			   let m = Some("x" ++ "y")
			   let n = m
			   match m { Some(s) => match n { Some(u) => if s == u { 3 } else { 1 }, None => 0 }, None => 0 }
			 }`,
			3,
		},
		{
			"recursive List<string>",
			`data List<t> = Nil | Cons(t, shared List<t>)
			 let count = (xs: shared List<string>) -> i64 => match xs {
			   Cons(h, rest) => 1 + count(rest),
			   Nil => 0,
			 }
			 let main = () -> u8 => {
			   let l: shared List<string> = Cons("a" ++ "1", Cons("b" ++ "2", Nil))
			   u8(count(l))
			 }`,
			2,
		},
		{
			"Box over an array of strings",
			`struct Box<t> { value: t }
			 let main = () -> u8 => {
			   let b = Box { value: ["p" ++ "q", "r" ++ "s"] }
			   u8(b.value.len())
			 }`,
			2,
		},
	}
	clang := lookClang(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// The double free, pinned as a *count* rather than a run.
//
// A generic type at a managed argument must make exactly the same ownership decisions
// as the equivalent concrete declaration — same retains, same drops. It did not: the
// ownership pass (which decides where a +1 is minted) read the raw ParameterizedType
// and judged `Box<string>` to own nothing, because the declaration's field type is the
// variable `t`; meanwhile the backend (which decides where a reference is released)
// reads types through recordedType, which normalizes an instantiation to its
// substituted struct, so it framed and deep-released *both* bindings. Drop twice, dup
// never — one allocation freed twice.
//
// Counting is the detector because macOS ASan did not report it: it does not see leaks
// there and, measured earlier in this work, misses genuine double frees too. Comparing
// against the concrete declaration rather than asserting absolute numbers also means
// this test keeps its meaning as the ownership model gets more precise — what matters
// is that generic and concrete cannot diverge.
func TestEmit_GenericManagedMatchesConcrete(t *testing.T) {
	t.Parallel()
	const generic = `struct Box<t> { value: t }
	 let main = () -> u8 => {
	   let b = Box { value: "ab" ++ "cd" }
	   let c = b
	   if b.value == "abcd" { if c.value == "abcd" { 2 } else { 1 } } else { 1 }
	 }`
	const concrete = `struct Box { value: string }
	 let main = () -> u8 => {
	   let b = Box { value: "ab" ++ "cd" }
	   let c = b
	   if b.value == "abcd" { if c.value == "abcd" { 2 } else { 1 } } else { 1 }
	 }`

	genIR, err := emitSource(t, generic)
	if err != nil {
		t.Fatal(err)
	}
	conIR, err := emitSource(t, concrete)
	if err != nil {
		t.Fatal(err)
	}

	// The glue is named after the type, so count calls by role rather than by symbol.
	count := func(ir, role string) int {
		n := 0
		for _, line := range strings.Split(ir, "\n") {
			if strings.Contains(line, "call") && strings.Contains(line, "@lyra_"+role+"_") {
				n++
			}
		}
		return n
	}
	for _, role := range []string{"retain", "drop"} {
		got, want := count(genIR, role), count(conIR, role)
		if got != want {
			t.Errorf("generic Box<string> emits %d %s-glue call(s), concrete Box emits %d — "+
				"the ownership pass and the backend must agree about a managed type argument, "+
				"or one allocation is freed twice (or never)\n\ngeneric IR:\n%s", got, role, want, genIR)
		}
	}
	if count(genIR, "retain") == 0 {
		t.Errorf("expected a retain for the non-last-use copy; got none:\n%s", genIR)
	}
}

// Each instantiation is its own LLVM type. Two instantiations sharing one layout would
// be a miscompile, not an inefficiency — reading an i1 field as i64, say.
func TestEmit_GenericTypeIsMonomorphized(t *testing.T) {
	t.Parallel()
	src := `struct Box<t> { value: t }
	 let main = () -> u8 => {
	   let a = Box { value: 30 }
	   let b = Box { value: true }
	   if b.value { u8(a.value) } else { 0 }
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"%Box$i64 = type { i64 }", "%Box$boolean = type { i1 }"} {
		if !strings.Contains(ir, want) {
			t.Errorf("expected %q in the emitted IR:\n%s", want, ir)
		}
	}
	// The bare declaration has no layout and must not be emitted under its own name.
	if strings.Contains(ir, "%Box = type") {
		t.Errorf("the generic declaration itself was emitted as a type; only instantiations should be:\n%s", ir)
	}
}

// An uninstantiated generic type costs nothing — the same property an uninstantiated
// generic function has, and the reason lowering is driven by use rather than by
// declaration.
func TestEmit_UninstantiatedGenericTypeEmitsNothing(t *testing.T) {
	t.Parallel()
	src := `struct Unused<t> { value: t }
	 let main = () -> u8 => 0`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "Unused") {
		t.Errorf("an uninstantiated generic type reached the module:\n%s", ir)
	}
}
