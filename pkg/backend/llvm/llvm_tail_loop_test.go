package llvm

import "testing"

// A `for { … }` with no `break` as the last statement of a non-void body: the front end
// types it `never`, and the backend seals its unreachable exit block instead of refusing
// the function with "block has no value". Exits are the `return`s inside it.
func TestExec_TailInfiniteLoop(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"return from inside", `let find = (n: u8) -> u8 => {
		   var i: u8 = 0
		   for {
		     i += 1
		     if i >= n { return i * 2 }
		   }
		 }
		 let main = () -> u8 => find(21)`, 42},
		{"nested loop's own break does not leave the outer", `let f = (n: u8) -> u8 => {
		   var i: u8 = 0
		   var rounds: u8 = 0
		   for {
		     rounds += 1
		     for i < n { i += 1; break }
		     if i >= n { return rounds }
		   }
		 }
		 let main = () -> u8 => f(5)`, 5},
		{"as main's own body", `let main = () -> u8 => {
		   var i: u8 = 0
		   for {
		     i += 3
		     if i > 10 { return i }
		   }
		 }`, 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Fatalf("exit code = %d, want %d", got, c.want)
			}
		})
	}
}
