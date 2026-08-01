package llvm

import "testing"

// A binding that shadows an outer one is scoped: when its scope ends, the outer
// binding is back.
//
// It was not. `l.locals` was one flat name→slot map for a whole function, so a
// shadowing binding permanently clobbered the outer one — `let n = 100; { let n =
// 5 }; n` read 5 — and every construct that binds a name for the duration of a
// sub-tree (a block's `let`, a loop variable, a match arm's pattern) leaked that
// binding into everything after it. Silently, and with a wrong *value* rather than
// an error, since the typechecker resolves these names correctly and only codegen
// disagreed. Each case below returns the outer value plus the inner one, so a
// leaked binding shows up as a wrong answer rather than a crash.
func TestExec_ShadowedBindingsAreScoped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The reported shape: a nested block.
			"a nested block",
			`let main = () -> u8 => {
			   let n = 100
			   let inner = { let n = 5; n }
			   u8(n + inner)
			 }`,
			105,
		},
		{
			"an if branch",
			`let main = () -> u8 => {
			   let n = 100
			   let r = if n > 50 { let n = 5; n } else { 0 }
			   u8(n + r)
			 }`,
			105,
		},
		{
			// A loop body-local shadowing an outer binding. Only reachable at all
			// since loop-body locals started resolving.
			"a loop body-local",
			`let main = () -> u8 => {
			   let n = 100
			   var seen = 0
			   for var i = 0; i < 2; i += 1 {
			     let n = 5
			     seen = seen + n
			   }
			   u8(seen + n - 100)
			 }`,
			10, // 5 + 5, with the outer n still 100
		},
		{
			// The C-style loop *counter* shadowing an outer binding: the counter
			// belongs to the loop, not to what follows it.
			"a loop counter",
			`let main = () -> u8 => {
			   let x = 100
			   var total = 0
			   for var x = 0; x < 3; x += 1 {
			     total = total + x
			   }
			   u8(total + x)
			 }`,
			103, // 0+1+2, then the outer x is still 100
		},
		{
			// The same for a for-in loop variable.
			"a for-in loop variable",
			`let main = () -> u8 => {
			   let i = 100
			   var total = 0
			   for i in 0..<3 {
			     total = total + i
			   }
			   u8(total + i)
			 }`,
			103,
		},
		{
			// A match arm's pattern binding is scoped to that arm.
			"a match arm binding",
			`data Maybe = Some(i64) | None
			 let main = () -> u8 => {
			   let v = 100
			   let r = match Some(5) {
			     Some(v) => v,
			     None => 0,
			   }
			   u8(v + r)
			 }`,
			105,
		},
		{
			// An identifier catch-all binds the whole scrutinee — also arm-scoped.
			"a scalar match catch-all binding",
			`let main = () -> u8 => {
			   let y = 100
			   let r = match 5 {
			     0 => 0,
			     y => y,
			   }
			   u8(y + r)
			 }`,
			105,
		},
		{
			// One arm's binding must not be visible to the next: both arms bind `v`,
			// and the second arm is the one taken.
			"a binding does not leak between arms",
			`data Pair = Left(i64) | Right(i64)
			 let pick = (p: Pair) -> i64 => match p {
			   Left(v) => v,
			   Right(v) => v * 2,
			 }
			 let main = () -> u8 => u8(pick(Right(5)))`,
			10,
		},
		{
			// The sharp version of the same rule: the *second* arm reads an outer `v`
			// that the *first* arm's pattern shadows. Without a per-arm reset the
			// second arm reads the first arm's slot — which on this path was never
			// stored to, so the value is whatever was on the stack (measured: 6).
			"an arm reads an outer binding an earlier arm shadowed",
			`data Pair = Left(i64) | Right(i64)
			 let main = () -> u8 => {
			   let v = 100
			   let r = match Right(5) {
			     Left(v) => v,
			     Right(x) => v + x,
			   }
			   u8(r)
			 }`,
			105,
		},
		{
			// Nested shadowing, two levels deep, each restored in turn.
			"nested shadowing two levels deep",
			`let main = () -> u8 => {
			   let n = 100
			   let mid = {
			     let n = 20
			     let deep = { let n = 3; n }
			     n + deep
			   }
			   u8(n + mid - 100)
			 }`,
			23, // 20 + 3; the outer n is still 100
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

// Scoping the *names* must not disturb the ownership bookkeeping, which tracks
// slots rather than names: two managed values of the same name in nested scopes
// are two distinct allocations, each released exactly once.
func TestExec_ShadowedManagedBindings(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `let main = () -> u8 => {
	   let s = "a" ++ "b"
	   let inner = { let s = "c" ++ "d"; s == "cd" }
	   if s == "ab" && inner { 7 } else { 1 }
	 }`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d; want 7", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 7 {
		t.Errorf("ASan run exited %d; want 7", got)
	}
	assertNoConservationLeak(t, src)
}
