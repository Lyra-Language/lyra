package llvm

import (
	"github.com/llir/llvm/ir"
)

// Dominator information over a *finished* function, used to decide whether a value
// produced in one block is live at another.
//
// **Why this exists, and why it runs at the end.** A `break`, a `continue` and a `?`
// all leave a statement without reaching the flush that would release its pending
// temporaries, so the jump has to release them itself. It may only release the ones
// that are actually live where it stands: an SSA value is defined in exactly one
// block, so it is available at the jump precisely when its defining block dominates
// the jump's block. Releasing one that is *not* dominated frees a value the taken
// path never produced — a double free, which is the failure direction that matters
// here (the alternative, skipping it, only leaks).
//
// The dominator relation is a property of the *whole* CFG, and the CFG does not
// exist yet while a jump is being lowered: later blocks can still add edges. So the
// releases are recorded when the jump is lowered and emitted once the function body
// is complete (`resolveExitReleases`), against a CFG that can no longer change.
// Computing it early against a partial CFG could report a dominance that a later
// edge invalidates, which is exactly the unsound direction.
//
// Inserting into an already-terminated block is safe and is what makes the deferral
// work: llir keeps a block's `Insts` and its `Term` in separate fields and prints
// the instructions first, so appending lands *before* the jump rather than after it.
// Every release this emits is straight-line (a store and a call — see deepRelease),
// so it cannot need a block of its own.

// domTree answers "does block a dominate block b?" for one function.
type domTree struct {
	// idx gives each block its index; doms[i] is the index of i's immediate
	// dominator, or -1 for the entry block.
	idx  map[*ir.Block]int
	doms []int
}

// newDomTree computes the dominator tree of fn with the classic iterative
// algorithm (Cooper/Harvey/Kennedy). Functions here are small — a handful to a few
// dozen blocks — so the simple fixpoint is the right trade against a Lengauer-Tarjan
// implementation nobody would want to maintain.
func newDomTree(fn *ir.Func) *domTree {
	blocks := fn.Blocks
	if len(blocks) == 0 {
		return &domTree{idx: map[*ir.Block]int{}}
	}
	idx := make(map[*ir.Block]int, len(blocks))
	for i, b := range blocks {
		idx[b] = i
	}
	// Predecessors, derived from each block's terminator successors. A block llir
	// has not sealed yet has no terminator and so contributes no edges; by the time
	// this runs the body is complete, and any such block is unreachable anyway.
	preds := make([][]int, len(blocks))
	for i, b := range blocks {
		if b.Term == nil {
			continue
		}
		for _, s := range b.Term.Succs() {
			if j, ok := idx[s]; ok {
				preds[j] = append(preds[j], i)
			}
		}
	}
	// blocks[0] is the entry block, and llir's function layout guarantees that.
	// The traversal order is the block order, which for a body built front-to-back
	// is close enough to reverse postorder that the fixpoint settles in a pass or
	// two; correctness does not depend on the order, only speed.
	doms := make([]int, len(blocks))
	for i := range doms {
		doms[i] = -1
	}
	doms[0] = 0
	for changed := true; changed; {
		changed = false
		for i := 1; i < len(blocks); i++ {
			newIdom := -1
			for _, p := range preds[i] {
				if doms[p] == -1 {
					continue // not yet reachable from entry
				}
				if newIdom == -1 {
					newIdom = p
					continue
				}
				newIdom = intersect(doms, p, newIdom)
			}
			if newIdom != -1 && doms[i] != newIdom {
				doms[i] = newIdom
				changed = true
			}
		}
	}
	return &domTree{idx: idx, doms: doms}
}

// intersect walks two nodes up the dominator tree until they meet — the standard
// helper of the iterative algorithm, relying on the index order being a topological
// approximation.
func intersect(doms []int, a, b int) int {
	for a != b {
		for a > b {
			if doms[a] == a || doms[a] == -1 {
				return b
			}
			a = doms[a]
		}
		for b > a {
			if doms[b] == b || doms[b] == -1 {
				return a
			}
			b = doms[b]
		}
	}
	return a
}

// dominates reports whether a dominates b (reflexively: a block dominates itself).
// An unknown block — one from another function, or one never reached from entry —
// answers false, which is the conservative direction: the caller skips the release
// and leaks rather than freeing a value that may not exist on this path.
func (d *domTree) dominates(a, b *ir.Block) bool {
	ai, ok := d.idx[a]
	if !ok {
		return false
	}
	bi, ok := d.idx[b]
	if !ok {
		return false
	}
	if d.doms[bi] == -1 {
		return false // b is unreachable from entry; claim nothing about it
	}
	for {
		if ai == bi {
			return true
		}
		if bi == 0 {
			return false // walked to entry without meeting a
		}
		next := d.doms[bi]
		if next == bi || next == -1 {
			return false
		}
		bi = next
	}
}
