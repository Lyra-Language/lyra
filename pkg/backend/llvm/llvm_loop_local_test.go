package llvm

import "testing"

// A `let`/`var` declared inside a loop body is visible there.
//
// It was not, for either loop form: the collector puts body-locals in a child
// block scope keyed on the body block, and both loop nodes held that block **by
// value**, so the copy had a different address than the scope was keyed on. The
// typechecker's enterScope missed — and a miss is silent, running the body in the
// enclosing scope — so every body-local read as "undefined identifier". Making
// the bodies pointers is the fix; these run the result.
func TestExec_LoopBodyLocals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"C-style loop",
			`let main = () -> u8 => {
			   var total = 0
			   for var i = 0; i < 3; i += 1 {
			     let doubled = i * 2
			     total = total + doubled
			   }
			   u8(total)
			 }`,
			6, // 0 + 2 + 4
		},
		{
			"for-in over an array",
			`let main = () -> u8 => {
			   var total = 0
			   for x in [1, 2, 3] {
			     let doubled = x * 2
			     total = total + doubled
			   }
			   u8(total)
			 }`,
			12, // 2 + 4 + 6
		},
		{
			"for-in over a range",
			`let main = () -> u8 => {
			   var total = 0
			   for i in 0..<4 {
			     let squared = i * i
			     total = total + squared
			   }
			   u8(total)
			 }`,
			14, // 0 + 1 + 4 + 9
		},
		{
			// A `var` too, reassigned within the same iteration.
			"a mutable body-local",
			`let main = () -> u8 => {
			   var total = 0
			   for var i = 0; i < 3; i += 1 {
			     var step = i
			     step = step + 1
			     total = total + step
			   }
			   u8(total)
			 }`,
			6, // 1 + 2 + 3
		},
		{
			// A body-local reading an outer binding: the body's scope chains to the
			// enclosing one, so both are visible.
			"a body-local reading an outer binding",
			`let main = () -> u8 => {
			   let base = 10
			   var total = 0
			   for var i = 0; i < 3; i += 1 {
			     let scaled = base + i
			     total = total + scaled
			   }
			   u8(total)
			 }`,
			33, // 10 + 11 + 12
		},
		{
			// The shape closures could not take before: a closure bound inside a loop
			// body, capturing the loop counter.
			"a closure bound in a loop body",
			`let call = (f: (i64) -> i64) -> i64 => f(10)
			 let main = () -> u8 => {
			   var total = 0
			   for var i = 0; i < 3; i += 1 {
			     let f = (x: i64) -> i64 => x + i
			     total = total + call(f)
			   }
			   u8(total)
			 }`,
			33, // 10 + 11 + 12
		},
		{
			// A one-armed `if` as the loop body's last statement. A loop body has no
			// value, but it was being checked as though its final statement were one,
			// so this was rejected with "`if` used as a value must have an `else`
			// branch" — a separate defect the same work uncovered.
			"a one-armed if as the last statement",
			`let main = () -> u8 => {
			   var n = 0
			   for var i = 0; i < 3; i += 1 {
			     if i > 1 { n = n + 1 }
			   }
			   u8(n)
			 }`,
			1,
		},
		{
			// Nested loops, each with its own body-local.
			"nested loops with their own locals",
			`let main = () -> u8 => {
			   var total = 0
			   for var i = 0; i < 2; i += 1 {
			     let outer = i + 1
			     for var j = 0; j < 2; j += 1 {
			       let inner = j + 1
			       total = total + outer * inner
			     }
			   }
			   u8(total)
			 }`,
			9, // (1*1 + 1*2) + (2*1 + 2*2)
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A *managed* body-local is released each iteration, not once at the end and not
// never: the binding is scoped to the block, so its frame is the body's.
func TestExec_LoopBodyManagedLocal(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `let main = () -> u8 => {
	   var n = 0
	   for var i = 0; i < 3; i += 1 {
	     let s = "a" ++ "b"
	     if s == "ab" { n = n + 1 }
	   }
	   u8(n)
	 }`
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("ASan run exited %d; want 3", got)
	}
	// One allocation site and one release site, both inside the loop — so each
	// iteration's string is freed on that iteration. A release hoisted out of the
	// loop would free only the last one.
	assertNoConservationLeak(t, src)
}
