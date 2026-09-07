package llvm

import "testing"

// A destructuring `let`'s names are owners, not borrows of a temporary. Each case binds
// managed values built at run time (a literal string is immortal and would hide a double
// release), then releases every other reference to them before reading through the
// names. Under ASan, so a use-after-free or a double release fails rather than passing
// by luck.
func TestExec_DestructuringLetOwnsItsNames(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
	}{
		{"tuple literal of arrays", `let main = () -> u8 => {
		   var a: []i64 = [1, 2]
		   var b: []i64 = [3, 4]
		   let (x, y) = (b, a)
		   a = []
		   b = []
		   if x[0] == 3 && y[0] == 1 { 0 } else { 1 }
		 }`},
		{"tuple from a call", `let pair = (p: []i64, q: []i64) -> ([]i64, []i64) => (p, q)
		 let main = () -> u8 => {
		   var a: []i64 = [1, 2]
		   var b: []i64 = [3, 4]
		   let (x, y) = pair(b, a)
		   a = []
		   b = []
		   if x[0] == 3 && y[0] == 1 { 0 } else { 1 }
		 }`},
		{"runtime strings", `let main = () -> u8 => {
		   var a = "le" ++ "ft"
		   var b = "ri" ++ "ght"
		   let (x, y) = (b, a)
		   a = ""
		   b = ""
		   if x == "right" && y == "left" { 0 } else { 1 }
		 }`},
		{"struct pattern", `struct Pair { xs: []i64, name: string }
		 let main = () -> u8 => {
		   var xs: []i64 = [7, 8]
		   let { xs: got, name } = Pair { xs: xs, name: "n" ++ "m" }
		   xs = []
		   if got[1] == 8 && name == "nm" { 0 } else { 1 }
		 }`},
		{"let-else", `let main = () -> u8 => {
		   var a: []i64 = [1, 2]
		   var b: []i64 = [3, 4]
		   let (x, y) = (b, a) else { return 9 }
		   a = []
		   b = []
		   if x[0] == 3 && y[0] == 1 { 0 } else { 1 }
		 }`},
		{"swap two array bindings", `let main = () -> u8 => {
		   var a: []i64 = [1, 2]
		   var b: []i64 = [3, 4]
		   (a, b) = (b, a)
		   (a, b) = (b, a)
		   (a, b) = (b, a)
		   if a[0] == 3 && b[0] == 1 && a.len() == 2 { 0 } else { 1 }
		 }`},
		{"bound name moved on", `let keep = (xs: own []i64) -> i64 => xs[0]
		 let main = () -> u8 => {
		   var a: []i64 = [5, 6]
		   var b: []i64 = [7, 8]
		   let (x, y) = (b, a)
		   a = []
		   b = []
		   let s = keep(x) + y[0]
		   if s == 12 { 0 } else { 1 }
		 }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != 0 {
				t.Fatalf("exit code = %d, want 0", got)
			}
		})
	}
}
