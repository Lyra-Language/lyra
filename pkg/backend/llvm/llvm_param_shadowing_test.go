package llvm

import "testing"

// A `let`/`var`/`for` binding shadows a parameter or pattern binding of the same name from
// its declaration on. The backend binds names as it reaches them, so it always meant this;
// the typechecker read the parameter first, which refused a shadow of another type and —
// where the types agreed — typed the use against the wrong binding. These run the program,
// so they pin that the shadow's *value* is the one used, including where the types are the
// same and only the value tells the bindings apart.
func TestExec_ParamShadowing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"let of the same type",
			`let f = pure (n: i64) -> i64 => {
			   let n = n * 10
			   n + 1
			 }
			 let main = () -> u8 => u8(f(4))`,
			41,
		},
		{
			"let of another type",
			`struct Box { side: i64 }
			 let g = pure (c: Box) -> i64 => {
			   let c = c.side + 1
			   c * 2
			 }
			 let main = () -> u8 => u8(g(Box { side: 1 }))`,
			4,
		},
		{
			"var reassigned",
			`let f = pure (n: i64) -> i64 => {
			   var n = n * 10
			   n = n + 1
			   n += 1
			   n
			 }
			 let main = () -> u8 => u8(f(4))`,
			42,
		},
		{
			"for-in variable of the same type, the parameter again after the loop",
			`let f = pure (c: i64, xs: []i64) -> i64 => {
			   var sum = 0
			   for c in xs { sum += c }
			   sum + c
			 }
			 let main = () -> u8 => u8(f(100, [1, 2, 3]))`,
			106,
		},
		{
			"for-in variable of another type",
			`struct Box { side: i64 }
			 struct Pt { x: i64 }
			 let f = pure (c: Box, items: []Pt) -> i64 => {
			   var sum = 0
			   for c in items { sum += c.x }
			   sum + c.side
			 }
			 let main = () -> u8 => u8(f(Box { side: 100 }, [Pt { x: 5 }, Pt { x: 7 }]))`,
			112,
		},
		{
			"nested block, the parameter again after it",
			`let f = pure (n: i64) -> i64 => {
			   var total = 0
			   {
			     let n = n * 10
			     total += n
			   }
			   total + n
			 }
			 let main = () -> u8 => u8(f(3))`,
			33,
		},
		{
			"use before the shadowing let is the parameter",
			`let f = pure (n: i64) -> i64 => {
			   let a = n + 1
			   let n = 50
			   a + n
			 }
			 let main = () -> u8 => u8(f(3))`,
			54,
		},
		{
			"closure captures the shadow",
			`let f = (n: i64) -> i64 => {
			   let n = n * 2
			   let k = () -> i64 => n + 1
			   k()
			 }
			 let main = () -> u8 => u8(f(5))`,
			11,
		},
		{
			"let shadows a pattern binding",
			`data Opt = Has(i64) | Nothing
			 let f = pure (o: Opt) -> i64 => match o {
			   Has(v) => {
			     let v = v * 3
			     v
			   },
			   Nothing => 0,
			 }
			 let main = () -> u8 => u8(f(Has(7)))`,
			21,
		},
		{
			// The scope holds only the latest of a sequentially rebound name; the use
			// between the two declarations means the first (it failed to compile before).
			"use between two sequential rebindings",
			`let h = pure (n: i64) -> i64 => {
			   let x = n + 1
			   let y = x * 2
			   let x = y + 100
			   x
			 }
			 let main = () -> u8 => u8(h(1))`,
			104,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exit %d, want %d", got, c.want)
			}
		})
	}
}

// Managed values, under ASan: a string parameter shadowed by a string `let`, by a number,
// and by a loop over strings — each binding owned or borrowed as its own declaration says,
// whatever the parameter is.
func TestExec_ParamShadowing_ManagedUnderASan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"string let of the same type",
			`let f = (s: string) -> i64 => {
			   let s = "${s}!!"
			   s.len()
			 }
			 let main = () -> u8 => u8(f("abc${"de"}"))`,
			7,
		},
		{
			"number let over a string parameter",
			`let f = (s: string) -> i64 => {
			   let s = s.len()
			   s * 2
			 }
			 let main = () -> u8 => u8(f("abc${"de"}"))`,
			10,
		},
		{
			"for-in over strings shadowing a string parameter",
			`let f = (s: string, xs: []string) -> i64 => {
			   var total = 0
			   for s in xs { total += s.len() }
			   total * 10 + s.len()
			 }
			 let main = () -> u8 => u8(f("ab${"c"}", ["x${"y"}", "z"]))`,
			33,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, c.src); got != c.want {
				t.Errorf("under ASan: exit %d, want %d", got, c.want)
			}
		})
	}
}

// Perceus reuse keys on names. An `own` parameter `xs` made *every* binding named `xs`
// look owned, so an arm's `xs` — a borrow out of `outer`, which the caller still holds —
// was reclaimed by the inner match and written over in place: `outer` came back changed
// and its cell was freed twice. A parameter whose name the body rebinds is not offered for
// reuse.
func TestExec_OwnParamRebindingIsNotReused(t *testing.T) {
	t.Parallel()
	src := `data List = Nil | Cons(i64, shared List)
	 let f = (xs: own shared List, outer: shared List) -> shared List => match outer {
	   Nil => xs,
	   Cons(h, xs) => match xs {
	     Nil => Nil,
	     Cons(g, t) => Cons(g + h, t)
	   }
	 }
	 let sum = (xs: shared List) -> i64 => match xs {
	   Nil => 0,
	   Cons(h, t) => h + sum(t)
	 }
	 let main = () -> u8 => {
	   let outer: shared List = Cons(1, Cons(10, Nil))
	   let none: shared List = Nil
	   let r: shared List = f(none, outer)
	   u8(sum(outer) * 10 + sum(r))
	 }`
	if got := buildAndRunASanWithPrelude(t, src); got != 121 {
		t.Errorf("under ASan: exit %d, want 121 (outer intact: 11, result: 11)", got)
	}
}
