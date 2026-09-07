package llvm

import "testing"

// `(p0, p1) = rhs` evaluates rhs to a tuple first and then writes the places left to
// right, so a swap needs no temporary. Each case checks the order of evaluation the
// desugaring promises, on a different kind of place.
func TestExec_TupleAssignment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"swap two bindings", `let main = () -> u8 => {
		   var a: u8 = 3
		   var b: u8 = 40
		   (a, b) = (b, a)
		   a * 2 + b   // 83 if swapped, 46 if not
		 }`, 83},
		{"swap two elements", `let main = () -> u8 => {
		   var xs: []u8 = [1, 2, 3]
		   (xs[0], xs[2]) = (xs[2], xs[0])
		   xs[0] * 10 + xs[2]
		 }`, 31},
		{"struct fields, three places rotated", `struct Pt { x: u8, y: u8, z: u8 }
		 let main = () -> u8 => {
		   var p = Pt { x: 1, y: 2, z: 3 }
		   (p.x, p.y, p.z) = (p.y, p.z, p.x)
		   p.x * 100 + p.y * 10 + p.z
		 }`, 231},
		{"pair from a call", `let divmod = pure (n: u8, d: u8) -> (u8, u8) => (n / d, n %% d)
		 let main = () -> u8 => {
		   var q: u8 = 0
		   var r: u8 = 0
		   (q, r) = divmod(17, 5)
		   q * 10 + r
		 }`, 32},
		{"through raw pointers", `let main = () -> u8 => {
		   var n: u8 = 4
		   var m: u8 = 7
		   unsafe {
		     let pn = &mut n
		     let pm = &mut m
		     (pn^, pm^) = (pm^, pn^)
		   }
		   n * 10 + m
		 }`, 74},
		{"index expressions evaluated once each, after the right side", `var calls: u8 = 0
		 let idx = (i: i64) -> i64 => { calls += 1; i }
		 let main = () -> u8 => {
		   var xs: []u8 = [5, 6]
		   (xs[idx(0)], xs[idx(1)]) = (xs[1], xs[0])
		   calls * 100 + xs[0] * 5 + xs[1]   // 2 calls, xs is [6, 5]
		 }`, 235},
		{"last statement of a void function", `let swap_first = (self: mut []u8) -> void => {
		   (self[0], self[1]) = (self[1], self[0])
		 }
		 let main = () -> u8 => {
		   var xs: []u8 = [9, 1]
		   xs.swap_first()
		   xs[0]
		 }`, 1},
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

// Managed values move through the swap without a leak or a double free: the tuple
// takes ownership of both, and each slot releases what it held. Run under ASan.
func TestExec_TupleAssignment_ManagedSwap(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
	   var a = "left"
	   var b = "right"
	   (a, b) = (b, a)
	   var xs: []string = ["x", "y", "z"]
	   (xs[0], xs[2]) = (xs[2], xs[0])
	   if a == "right" && b == "left" && xs[0] == "z" && xs[2] == "x" { 0 } else { 1 }
	 }`
	if got := buildAndRunASan(t, lookClang(t), src); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}
