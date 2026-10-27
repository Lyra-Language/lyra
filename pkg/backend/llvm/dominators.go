package llvm

import (
	"sort"

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
	// root is the index the tree is rooted at: the entry block for a finished
	// function, a statement's start block for newDomTreeFrom.
	root int
	// enter and leave number each block as a depth-first walk of the tree first and
	// last visits it, so `a` dominates `b` exactly when b's interval nests in a's —
	// answered at once, where walking b's dominators up to a cost the tree's depth. In
	// a long function that walk, asked of every block for each temporary produced on a
	// branch (regionExits), was minutes of a build (`ui.app`'s host, 10/27). -1 for a
	// block the root does not reach.
	enter, leave []int
}

// number gives each block reached from the root its interval ([domTree.enter],
// [domTree.leave]), walking the tree from the root without recursion.
func (d *domTree) number() {
	n := len(d.doms)
	d.enter = make([]int, n)
	d.leave = make([]int, n)
	children := make([][]int, n)
	for i := range n {
		d.enter[i], d.leave[i] = -1, -1
		if i != d.root && d.doms[i] >= 0 && d.doms[i] != i {
			children[d.doms[i]] = append(children[d.doms[i]], i)
		}
	}
	if d.root < 0 || d.root >= n {
		return
	}
	clock := 0
	type frame struct{ node, next int }
	stack := []frame{{d.root, 0}}
	d.enter[d.root] = clock
	clock++
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(children[top.node]) {
			child := children[top.node][top.next]
			top.next++
			d.enter[child] = clock
			clock++
			stack = append(stack, frame{child, 0})
			continue
		}
		d.leave[top.node] = clock
		clock++
		stack = stack[:len(stack)-1]
	}
}

// newDomTree computes the dominator tree of fn with the classic iterative
// algorithm (Cooper/Harvey/Kennedy). Functions here are small — a handful to a few
// dozen blocks — so the simple fixpoint is the right trade against a Lengauer-Tarjan
// implementation nobody would want to maintain.
func newDomTree(fn *ir.Func) *domTree {
	if len(fn.Blocks) == 0 {
		return &domTree{idx: map[*ir.Block]int{}}
	}
	return newDomTreeFrom(fn, fn.Blocks[0])
}

// newDomTreeFrom computes dominance over the part of fn reachable from root, as if
// root were the entry. **This is the form to use while the function is still being
// built.** Mid-lowering, some block *enclosing* the current statement can still lack
// its terminator — a match whose dispatch is emitted after its arms, a loop header
// sealed after its body — so from the real entry the statement's blocks look
// unreachable, and dominates answers false for all of them. For flushStmtTemps that
// answer is not the safe direction: it means "release where it was produced", which
// frees a temporary a later operand of the same statement still reads (the glTF
// viewer's `"…${name}…" ++ "${x.floor()}"` read its left operand after the float guard
// released it). A statement is single-entry at its start block, and by the time it is
// flushed every block inside it is sealed, so the region rooted there is complete and
// its dominance is the whole CFG's.
func newDomTreeFrom(fn *ir.Func, root *ir.Block) *domTree {
	blocks := fn.Blocks
	idx := make(map[*ir.Block]int, len(blocks))
	for i, b := range blocks {
		idx[b] = i
	}
	r, ok := idx[root]
	if !ok {
		return &domTree{idx: map[*ir.Block]int{}}
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
	// The root stands in for the entry (blocks[0] for a whole function — llir's layout
	// guarantees that). **Cooper, Harvey and Kennedy's algorithm needs the postorder of a
	// walk from the root**, both to visit blocks in reverse postorder and for intersect to
	// tell which of two blocks is nearer the root. It took the block order for both until
	// 10/27 — close to reverse postorder for a body built front to back, but not for every
	// CFG, and where it was not an immediate dominator came out too high: a block reported as
	// not dominating one it does, which here is a temporary released early
	// (TestDomTreeAgreesWithItsDefinition).
	post := postorder(r, blocks, idx)
	order := make([]int, 0, len(blocks))
	for i := range blocks {
		if post[i] >= 0 {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(x, y int) bool { return post[order[x]] > post[order[y]] })
	doms := make([]int, len(blocks))
	for i := range doms {
		doms[i] = -1
	}
	doms[r] = r
	for changed := true; changed; {
		changed = false
		for _, i := range order {
			if i == r {
				continue
			}
			newIdom := -1
			for _, p := range preds[i] {
				if doms[p] == -1 {
					continue // not yet reached from the root
				}
				if newIdom == -1 {
					newIdom = p
					continue
				}
				newIdom = intersect(doms, post, p, newIdom)
			}
			if newIdom != -1 && doms[i] != newIdom {
				doms[i] = newIdom
				changed = true
			}
		}
	}
	d := &domTree{idx: idx, doms: doms, root: r}
	d.number()
	return d
}

// postorder numbers each block reached from root by when a depth-first walk along the
// terminators' successors finishes it — the root last; -1 for a block not reached.
func postorder(root int, blocks []*ir.Block, idx map[*ir.Block]int) []int {
	post := make([]int, len(blocks))
	for i := range post {
		post[i] = -1
	}
	seen := make([]bool, len(blocks))
	type frame struct {
		node  int
		succs []*ir.Block
		next  int
	}
	succsOf := func(i int) []*ir.Block {
		if blocks[i].Term == nil {
			return nil
		}
		return blocks[i].Term.Succs()
	}
	clock := 0
	seen[root] = true
	stack := []frame{{root, succsOf(root), 0}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(top.succs) {
			s := top.succs[top.next]
			top.next++
			if j, ok := idx[s]; ok && !seen[j] {
				seen[j] = true
				stack = append(stack, frame{j, succsOf(j), 0})
			}
			continue
		}
		post[top.node] = clock
		clock++
		stack = stack[:len(stack)-1]
	}
	return post
}

// intersect walks two blocks up the dominator tree until they meet, the one finished earlier
// in the walk ([postorder]) — farther from the root — stepping up first: the standard helper
// of the iterative algorithm.
func intersect(doms, post []int, a, b int) int {
	for a != b {
		for post[a] < post[b] {
			a = doms[a]
		}
		for post[b] < post[a] {
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
	if d.doms[bi] == -1 || d.enter[ai] < 0 || d.enter[bi] < 0 {
		return false // b is unreachable from entry; claim nothing about it
	}
	return d.enter[ai] <= d.enter[bi] && d.leave[bi] <= d.leave[ai]
}
