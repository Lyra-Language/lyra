package llvm

import "testing"

// A binding with no initializer — a `for` variable over functions, a destructured name — is
// called through its declared type, as an indirect call. It failed to type-check as "its
// definition depends on itself" until 10/02.
func TestExec_CallingFunctionValuesWithoutAnInitializer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"for variable",
			`let one = () -> i64 => 1
			 let ten = () -> i64 => 10
			 let run = (fs: []() -> i64) -> i64 => {
			   var t = 0
			   for g in fs { t += g() }
			   t
			 }
			 let main = () -> u8 => u8(run([one, ten, ten]))`,
			21,
		},
		{
			"destructured name",
			`let one = () -> i64 => 1
			 let two = (n: i64) -> i64 => n * 2
			 let main = () -> u8 => {
			   let (f, g) = (one, two)
			   u8(g(f() + 20))
			 }`,
			42,
		},
		{
			"closures in a loop",
			`let adder = (k: i64) -> (i64) -> i64 => (n: i64) -> i64 => n + k
			 let main = () -> u8 => {
			   var t = 0
			   for add in [adder(1), adder(2)] { t = add(t) }
			   u8(t)
			 }`,
			3,
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

// The same through ASan with captured strings, so the loop's borrowed element is called
// without being released.
func TestExec_CallingAFunctionValuedLoopVariableUnderASan(t *testing.T) {
	t.Parallel()
	src := `let suffix = (s: string) -> (string) -> string => (x: string) -> string => "${x}${s}"
	 let main = () -> u8 => {
	   var out = "a"
	   for f in [suffix("b${"c"}"), suffix("d")] { out = f(out) }
	   u8(out.len())
	 }`
	if got := buildAndRunASanWithPrelude(t, src); got != 4 {
		t.Errorf("under ASan: exit %d, want 4", got)
	}
}
