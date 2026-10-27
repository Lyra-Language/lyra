package llvm

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
)

// `break` and `continue` leave every statement between the jump and the loop without
// reaching its flush, so the pending temporaries of those statements have to be
// released at the jump. They were not: `loop { if ("a" ++ "b") == "ab" { break } }`
// leaked the concatenation, confirmed at 18 bytes with LeakSanitizer on Linux (macOS
// has no LSan, which is why these are structural tests plus an ASan run).
//
// The jump cannot decide this on its own, because "is this temporary live where I
// stand?" is a dominance question and the CFG is still being built. So the jump
// records the obligation and `resolveExitReleases` settles it once the function is
// complete, against a dominator tree that can no longer change (dominators.go).

// A single allocation in the loop body must be released on **both** ways out of the
// loop body — the `break` and the fall-through — so two release sites. One means the
// break path leaks it, which is the bug.
func TestEmit_BreakReleasesPendingTemp(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var i = 0
  loop {
    if ("a" ++ "b") == "ab" { break }
    i = i + 1
  }
  u8(i)
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	body := ir[strings.Index(ir, "define i32 @main"):]
	body = body[:strings.Index(body, "\n}")]
	if got := strings.Count(body, "call void @lyra_rc_release"); got != 2 {
		t.Errorf("@main has %d release sites, want 2 (the break path and the fall-through); "+
			"1 means break leaks the pending temporary\n%s", got, body)
	}
}

// The behavioural half, over the shapes that differ in how the jump relates to the
// producing block. Each is checked for the right answer and under ASan, which is
// what would catch the opposite error — releasing a temporary the taken path never
// produced, a double free rather than a leak.
func TestExec_ExitReleases(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The reported leak: the temporary is produced in the loop body and the
			// break sits in a branch of it.
			"break with a temporary in the condition",
			`let main = () -> u8 => {
			   var i = 0
			   loop { if ("a" ++ "b") == "ab" { break }
			     i = i + 1 }
			   u8(i)
			 }`,
			0,
		},
		{
			// `continue` skips the same flushes `break` does, and runs many times
			// rather than once — so a leak here compounds per iteration.
			"continue with a pending temporary",
			`let main = () -> u8 => {
			   var i = 0
			   while i < 3 { i = i + 1
			     if ("p" ++ "q") == "pq" { continue } else { i = i + 10 } }
			   u8(i)
			 }`,
			3,
		},
		{
			// A labeled break leaves two loops at once, so it must release the
			// temporaries of both bodies' statements, not just the inner one's.
			"labeled break out of nested loops",
			`let main = () -> u8 => {
			   var i = 0
			   outer: loop {
			     loop { if ("m" ++ "n") == "mn" { break outer } else { i = i + 1 } } }
			   u8(i + 3)
			 }`,
			3,
		},
		{
			// The other direction, and the one a too-eager fix breaks: the temporary
			// belongs to a statement *enclosing* the whole loop, so that statement's
			// own flush still runs after the loop exits. Releasing it at the break as
			// well would be a double free — this is what loopCtx.tempBase prevents.
			"a temporary from a statement enclosing the loop",
			`let use = (s: string, n: i64) -> i64 => n
			 let main = () -> u8 => {
			   let r = use("a" ++ "b", { var k = 0
			     loop { break }
			     5 })
			   u8(r)
			 }`,
			5,
		},
		{
			// A temporary produced *after* the loop, to pin that the jump's recorded
			// obligation does not reach forward into later statements.
			"a temporary after the loop is unaffected",
			`let main = () -> u8 => {
			   var i = 0
			   loop { if i > 0 { break } else { i = i + 1 } }
			   if ("y" ++ "z") == "yz" { i = i + 4 } else { i = i + 9 }
			   u8(i)
			 }`,
			5,
		},
	}
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

// The dominator tree itself, on a hand-built CFG — the exit-release path only ever
// asks it about shapes the lowerer happens to produce, and the one answer that must
// never be wrong (a false "dominates" is a double free) deserves a direct test.
//
//	entry → a → c        entry dominates everything; a and b dominate only
//	      ↘ b ↗          themselves and nothing past the merge, because c is
//	                     reachable without either.
func TestDomTree(t *testing.T) {
	t.Parallel()
	fn := ir.NewFunc("f", lltypes.Void)
	entry := fn.NewBlock("entry")
	a := fn.NewBlock("a")
	b := fn.NewBlock("b")
	c := fn.NewBlock("c")
	entry.NewCondBr(constant.True, a, b)
	a.NewBr(c)
	b.NewBr(c)
	c.NewRet(nil)

	dt := newDomTree(fn)
	cases := []struct {
		from, to *ir.Block
		want     bool
		why      string
	}{
		{entry, entry, true, "a block dominates itself"},
		{entry, a, true, "entry dominates a branch arm"},
		{entry, c, true, "entry dominates the merge"},
		{a, c, false, "an arm does not dominate the merge — c is reachable via b"},
		{b, c, false, "the other arm likewise"},
		{a, b, false, "sibling arms do not dominate each other"},
		{c, entry, false, "dominance does not run backwards"},
	}
	for _, k := range cases {
		if got := dt.dominates(k.from, k.to); got != k.want {
			t.Errorf("dominates(%s, %s) = %v, want %v — %s",
				k.from.LocalName, k.to.LocalName, got, k.want, k.why)
		}
	}
	// A block from another function is unknown, and unknown must answer false: the
	// caller reads a false as "skip the release", which leaks rather than double-frees.
	other := ir.NewFunc("g", lltypes.Void).NewBlock("x")
	if dt.dominates(other, c) || dt.dominates(entry, other) {
		t.Error("a block outside the function must not be reported as dominating or dominated")
	}
}

// Dominance answered by the tree's numbering (domTree.number) against its definition, on
// random CFGs with loops, unreachable blocks and roots other than the entry: `a` dominates `b`
// when `b` is reachable from the root and every path there passes `a` — `b` is unreachable
// with `a` taken out. The numbering replaced a walk up the tree (10/27), and a false "dominates"
// is a double free.
func TestDomTreeAgreesWithItsDefinition(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(27))
	for round := 0; round < 300; round++ {
		n := 2 + rng.Intn(14)
		fn := ir.NewFunc("f", lltypes.Void)
		blocks := make([]*ir.Block, n)
		for i := range blocks {
			blocks[i] = fn.NewBlock("")
		}
		succs := make([][]int, n)
		for i, b := range blocks {
			switch rng.Intn(4) {
			case 0:
				b.NewRet(nil)
			case 1:
				j := rng.Intn(n)
				succs[i] = []int{j}
				b.NewBr(blocks[j])
			default:
				j, k := rng.Intn(n), rng.Intn(n)
				succs[i] = []int{j, k}
				b.NewCondBr(constant.True, blocks[j], blocks[k])
			}
		}
		root := 0
		if round%3 == 0 {
			root = rng.Intn(n)
		}
		// Whether `to` is reachable from the root without passing `without` (-1 for none).
		reaches := func(to, without int) bool {
			if root == without {
				return false
			}
			seen := make([]bool, n)
			stack := []int{root}
			seen[root] = true
			for len(stack) > 0 {
				x := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if x == to {
					return true
				}
				for _, y := range succs[x] {
					if y != without && !seen[y] {
						seen[y] = true
						stack = append(stack, y)
					}
				}
			}
			return false
		}
		dt := newDomTreeFrom(fn, blocks[root])
		for a := 0; a < n; a++ {
			for b := 0; b < n; b++ {
				want := reaches(b, -1) && (a == b || !reaches(b, a))
				if got := dt.dominates(blocks[a], blocks[b]); got != want {
					t.Fatalf("round %d (root %d, edges %v): dominates(%d, %d) = %v, want %v",
						round, root, succs, a, b, got, want)
				}
			}
		}
	}
}
