package llvm

import (
	"strings"
	"testing"
)

// An array comprehension inside a closure.
//
// The captures pass computes a lambda's free variables as "names read, minus names bound",
// and it had no case for `ArrayCompExpr` — so a comprehension's generator binder was never
// recorded as bound, and the enclosing lambda tried to capture its own binder. The backend
// refused outright: *"captured binding \"n\" is not in scope where the closure is created"*.
// A comprehension simply could not appear inside a closure.
//
// It is the same bug the `ForLoopExpr` arm beside it exists to fix, whose comment describes
// a loop counter reading as a capture — hazard 8's "when adding an expression kind, grep for
// the kind it is a variant of". A comprehension binds like a `for-in` and was missed.
//
// The third case is the one that keeps the fix honest: `factor` is a genuine capture from
// the enclosing scope and must still be captured. Binding the generator names is not licence
// to stop capturing.
func TestExec_ComprehensionInsideAClosure(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
let main = () -> void => {
  let xs: []i64 = [1, 2, 3]
  let ys: []i64 = [10, 20]
  let factor = 100
  let f = () -> i64 => {
    let a = [n in xs | n * 2]
    let b = [n in xs, m in ys | n + m]
    let c = [n in xs | n > 1 | n * factor]
    a[0] + b[0] + c[0]
  }
  println(f())
}
`)
	// a[0] = 2, b[0] = 1+10 = 11, c[0] = 2*100 = 200.
	if code != 0 || strings.TrimSpace(out) != "213" {
		t.Errorf("exit %d, output %q; want \"213\" — a comprehension's binder is bound by the "+
			"comprehension, and an outer name it reads is still a capture", code, strings.TrimSpace(out))
	}
}
