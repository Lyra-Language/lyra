package llvm

import (
	"fmt"
	"slices"
	"sort"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"
)

// A **path-sensitive** conservation check over the emitted IR: does every
// allocation reach a release on every path that can leave its function?
//
// Why this exists. The existing conservation test (llvm_deep_retain_test.go)
// counts allocations against releases across a whole function. Totals are the
// wrong granularity for a leak that lives on one edge: the `[head, ...tail]`
// guard leak had one allocation and one release — perfectly balanced — while the
// guard-false edge carried the box past its only release. Nothing caught it. Not
// the counts, not the behavioral tests (the program returned the right answer),
// and not AddressSanitizer, which on macOS reports use-after-free and double-free
// but not leaks. It took reading the control-flow graph by hand.
//
// This walks the CFG instead: from each `lyra_rc_alloc`, follow the box forward
// and report if a `ret` is reachable with the box neither released nor escaped.
//
// The bar is **no false positives**, because a noisy assertion gets deleted. So
// every uncertainty resolves toward silence: any use the analysis doesn't fully
// understand marks the value *escaped* and drops it from consideration. That
// admits false negatives by design — this is a net for one specific, repeatedly
// costly shape (a path that skips a release), not a verifier.

// escapes/releases are the two ways a tracked allocation legitimately leaves the
// analysis. A release is an event on a CFG edge; an escape is a property of the
// whole allocation (once it can outlive the function, no path proves anything).
type conservationLeak struct {
	fn    string // function holding the allocation
	block string // block containing the `lyra_rc_alloc` call
	exit  string // block whose `ret` is reachable with the box outstanding
}

func (l conservationLeak) String() string {
	return fmt.Sprintf("%s: allocation in %%%s reaches the `ret` in %%%s unreleased", l.fn, l.block, l.exit)
}

// findConservationLeaks reports allocations that can reach a `ret` without being
// released, for every function in the module.
func findConservationLeaks(m *ir.Module) []conservationLeak {
	var out []conservationLeak
	for _, fn := range m.Funcs {
		out = append(out, leaksInFunc(fn)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func leaksInFunc(fn *ir.Func) []conservationLeak {
	var out []conservationLeak
	for _, block := range fn.Blocks {
		for _, inst := range block.Insts {
			call, ok := inst.(*ir.InstCall)
			if !ok || calleeName(call) != "lyra_rc_alloc" {
				continue
			}
			t := newBoxTracker(fn, call)
			if t.escaped {
				continue // the box can outlive this function; no path claim to make
			}
			if exit, leaked := t.reachesRetUnreleased(block); leaked {
				out = append(out, conservationLeak{
					fn:    fn.GlobalIdent.Name(),
					block: block.LocalIdent.Name(),
					exit:  exit,
				})
			}
		}
	}
	return out
}

// boxTracker follows one allocation's box through a function: which values alias
// it, which instructions release it, and whether it escapes.
type boxTracker struct {
	fn      *ir.Func
	alloc   *ir.InstCall
	alias   map[value.Value]bool // values that refer to this box
	slots   map[value.Value]bool // local allocas the box has been stored into
	release map[*ir.Block]bool   // blocks containing a release of this box
	escaped bool
}

func newBoxTracker(fn *ir.Func, alloc *ir.InstCall) *boxTracker {
	t := &boxTracker{
		fn:      fn,
		alloc:   alloc,
		alias:   map[value.Value]bool{alloc: true},
		slots:   map[value.Value]bool{},
		release: map[*ir.Block]bool{},
	}
	// The alias set grows as derived values are discovered (a bitcast of a load of
	// a slot the box was stored into, …), and a later instruction can feed an
	// earlier one across a back edge, so iterate to a fixed point.
	for changed := true; changed; {
		changed = t.scan()
	}
	return t
}

// scan makes one pass over the function, growing the alias/slot/release sets.
// Returns whether anything changed.
func (t *boxTracker) scan() bool {
	changed := false
	note := func(v value.Value) {
		if v != nil && !t.alias[v] {
			t.alias[v] = true
			changed = true
		}
	}
	for _, block := range t.fn.Blocks {
		for _, inst := range block.Insts {
			switch in := inst.(type) {
			case *ir.InstCall:
				t.scanCall(block, in, note)

			case *ir.InstStore:
				if !t.alias[in.Src] {
					continue
				}
				// Storing the box into a *local slot* keeps it in the function; loads
				// of that slot are the same box. Storing it anywhere else (into a
				// field, through a computed pointer) puts it somewhere this analysis
				// does not model — treat as escaped.
				if _, ok := in.Dst.(*ir.InstAlloca); ok {
					if !t.slots[in.Dst] {
						t.slots[in.Dst] = true
						changed = true
					}
					continue
				}
				t.escaped = true

			case *ir.InstLoad:
				if t.slots[in.Src] {
					note(in)
				}

			// Value-producing instructions that carry the box along. A bitcast or GEP
			// is the same box at a different type/offset (the runtime moves between
			// box and payload by ±8); a phi/select merges it; insert/extractvalue
			// carries it through an aggregate (a string's fat pointer is built this
			// way).
			case *ir.InstBitCast:
				if t.alias[in.From] {
					note(in)
				}
			case *ir.InstGetElementPtr:
				if t.alias[in.Src] {
					note(in)
				}
			case *ir.InstPtrToInt:
				if t.alias[in.From] {
					note(in)
				}
			case *ir.InstIntToPtr:
				if t.alias[in.From] {
					note(in)
				}
			case *ir.InstPhi:
				for _, inc := range in.Incs {
					if t.alias[inc.X] {
						note(in)
						break
					}
				}
			case *ir.InstSelect:
				if t.alias[in.ValueTrue] || t.alias[in.ValueFalse] {
					note(in)
				}
			case *ir.InstInsertValue:
				if t.alias[in.Elem] || t.alias[in.X] {
					note(in)
				}
			case *ir.InstExtractValue:
				if t.alias[in.X] {
					note(in)
				}
			}
		}
		// Returning the box (or an aggregate carrying it) hands it to the caller.
		if ret, ok := block.Term.(*ir.TermRet); ok && ret.X != nil && t.alias[ret.X] {
			t.escaped = true
		}
	}
	return changed
}

// scanCall classifies a call that mentions the box: the runtime's own release
// forms are the release event, retain is neutral, and anything else may take
// ownership, so it escapes.
func (t *boxTracker) scanCall(block *ir.Block, call *ir.InstCall, note func(value.Value)) {
	mentions := false
	for _, arg := range call.Args {
		if t.alias[arg] {
			mentions = true
			break
		}
	}
	if !mentions {
		return
	}
	switch calleeName(call) {
	case "lyra_rc_release", "lyra_rc_drop_reuse", "free":
		// drop_reuse consumes the reference exactly like a release: it either hands
		// back the box for reuse or frees it.
		t.release[block] = true
	case "lyra_rc_retain":
		// Neutral: adds a reference, doesn't discharge one. The extra reference is a
		// balance question (what the counting test covers), not a path question.
	case "memcpy", "memcmp", "write", "snprintf", "lyra_utf8_count":
		// Read/write the *bytes*; they neither store the pointer nor free it.
	default:
		// Any other callee may take ownership — including the per-type drop glue,
		// which frees what it is handed.
		t.escaped = true
	}
	note(call)
}

// reachesRetUnreleased walks forward from the allocation's block looking for a
// `ret` reachable without passing through a release.
//
// The allocation's own block counts as releasing only if the release follows the
// alloc within it, so that case is handled by the caller passing the alloc block
// and checking instruction order here. `unreachable` terminators (the language's
// traps) are not exits: the process aborts, so an outstanding box is moot.
func (t *boxTracker) reachesRetUnreleased(from *ir.Block) (string, bool) {
	if t.releasedAfterAlloc(from) {
		return "", false
	}
	seen := map[*ir.Block]bool{from: true}
	queue := []*ir.Block{from}
	for len(queue) > 0 {
		b := queue[0]
		queue = queue[1:]
		if ret, ok := b.Term.(*ir.TermRet); ok {
			_ = ret
			return b.LocalIdent.Name(), true
		}
		for _, succ := range successors(b) {
			if seen[succ] || t.release[succ] {
				continue // a released successor discharges the box on that edge
			}
			seen[succ] = true
			queue = append(queue, succ)
		}
	}
	return "", false
}

// releasedAfterAlloc reports whether the allocation's own block releases the box
// at some point after allocating it.
func (t *boxTracker) releasedAfterAlloc(block *ir.Block) bool {
	if !t.release[block] {
		return false
	}
	seenAlloc := false
	for _, inst := range block.Insts {
		if inst == t.alloc {
			seenAlloc = true
			continue
		}
		call, ok := inst.(*ir.InstCall)
		if !ok || !seenAlloc {
			continue
		}
		switch calleeName(call) {
		case "lyra_rc_release", "lyra_rc_drop_reuse", "free":
			for _, arg := range call.Args {
				if t.alias[arg] {
					return true
				}
			}
		}
	}
	return false
}

func successors(b *ir.Block) []*ir.Block {
	// Terminator targets are typed value.Value in llir; they are always *ir.Block
	// in a well-formed module.
	asBlock := func(v value.Value) *ir.Block {
		blk, _ := v.(*ir.Block)
		return blk
	}
	var out []*ir.Block
	switch term := b.Term.(type) {
	case *ir.TermBr:
		out = []*ir.Block{asBlock(term.Target)}
	case *ir.TermCondBr:
		out = []*ir.Block{asBlock(term.TargetTrue), asBlock(term.TargetFalse)}
	case *ir.TermSwitch:
		out = []*ir.Block{asBlock(term.TargetDefault)}
		for _, c := range term.Cases {
			out = append(out, asBlock(c.Target))
		}
	default:
		return nil // ret, unreachable
	}
	return slices.DeleteFunc(out, func(b *ir.Block) bool { return b == nil })
}

func calleeName(call *ir.InstCall) string {
	if fn, ok := call.Callee.(*ir.Func); ok {
		return fn.GlobalIdent.Name()
	}
	return ""
}
