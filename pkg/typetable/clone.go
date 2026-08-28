package typetable

import (
	"fmt"
	"sort"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// The tables a typechecking run accumulates, copied so a run can start from a previous one's
// results instead of re-deriving them.
//
// **Why this is worth having.** An editor re-typechecks the whole merged program on every
// keystroke, and `Check`'s per-statement loop is ~1.0 ms of a 2.7 ms analysis — of which
// ~99% is the standard prelude, which cannot have changed. The four setup passes before that
// loop (impl collection, coherence, newtype cycles, trait defaults) are together under 1% and
// simply re-run, which is what keeps the snapshot to output tables rather than to the
// checker's whole state.
//
// Every map here is keyed by an **AST pointer**, so a clone shares the AST with the original
// and only the maps are copied. That is the same arrangement the collector's snapshot relies
// on, and safe for the same reason: re-analysis over one collected AST is idempotent.

// Clone returns a copy of the table. The AST keys and the type values are shared.
func (t *TypeTable) Clone() *TypeTable {
	if t == nil {
		return nil
	}
	return &TypeTable{
		entries:            cloneMap(t.entries),
		callees:            cloneMap(t.callees),
		variadicPromotions: cloneMap(t.variadicPromotions),
		baseReadouts:       cloneMap(t.baseReadouts),
		unresolvedCallees:  cloneMap(t.unresolvedCallees),
	}
}

// Clone returns a copy of the method table.
//
// `boundCandidates` and `operatorCandidates` hold a map per call, so those inner maps are
// copied too — sharing one would let a later run's candidates appear in an earlier run's
// results, which is a wrong dispatch rather than a stale count.
func (t *MethodTable) Clone() *MethodTable {
	if t == nil {
		return nil
	}
	return &MethodTable{
		entries:             cloneMap(t.entries),
		resolutions:         cloneMap(t.resolutions),
		boundCalls:          cloneMap(t.boundCalls),
		builtins:            cloneMap(t.builtins),
		builtinAllocs:       cloneMap(t.builtinAllocs),
		boundCandidates:     cloneNestedMap(t.boundCandidates),
		operatorResolutions: cloneMap(t.operatorResolutions),
		operatorCandidates:  cloneNestedMap(t.operatorCandidates),
		operatorBounds:      cloneMap(t.operatorBounds),
	}
}

// Clone returns a copy of the instantiation table.
func (t *InstantiationTable) Clone() *InstantiationTable {
	if t == nil {
		return nil
	}
	return &InstantiationTable{
		byCall:     cloneMap(t.byCall),
		discovered: cloneMap(t.discovered),
	}
}

// Clone returns a copy of the constraint table.
func (t *ConstraintTable) Clone() *ConstraintTable {
	if t == nil {
		return nil
	}
	return &ConstraintTable{checks: cloneMap(t.checks)}
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	if in == nil {
		return nil
	}
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneNestedMap[K comparable, IK comparable, V any](in map[K]map[IK]V) map[K]map[IK]V {
	if in == nil {
		return nil
	}
	out := make(map[K]map[IK]V, len(in))
	for k, inner := range in {
		out[k] = cloneMap(inner)
	}
	return out
}

// Absorb copies another table's entries into this one, leaving existing entries alone.
//
// It exists because the *caller* owns the TypeTable — it hands the same one to every pass
// after typechecking — so a resumed run cannot simply swap in the snapshot's copy. Filling
// the caller's table is what lets the snapshot's recorded types be visible to ownership,
// purity and the backend under the identity they already hold.
func (t *TypeTable) Absorb(other *TypeTable) {
	if t == nil || other == nil {
		return
	}
	for k, v := range other.entries {
		if _, ok := t.entries[k]; !ok {
			t.entries[k] = v
		}
	}
	if other.callees != nil {
		if t.callees == nil {
			t.callees = make(map[*ast.FunctionCallExpr]*ast.LambdaExpr, len(other.callees))
		}
		for k, v := range other.callees {
			if _, ok := t.callees[k]; !ok {
				t.callees[k] = v
			}
		}
	}
	for k, v := range other.variadicPromotions {
		if _, ok := t.variadicPromotions[k]; !ok {
			t.SetVariadicPromotion(k, v)
		}
	}
	for call := range other.baseReadouts {
		t.SetBaseReadout(call)
	}
	for call := range other.unresolvedCallees {
		t.SetUnresolvedCallee(call)
	}
}

// Fingerprint renders the table's contents in a form comparable **across runs**.
//
// The maps are keyed by AST pointer, and two analyses of one program collect two sets of AST
// nodes, so the keys cannot be compared directly. What can be compared is the multiset of
// what was recorded — every type, sorted — which is what a snapshot must reproduce exactly.
// Without this a differential test over cached-versus-uncached analysis is blind to the very
// tables the snapshot carries.
func (t *TypeTable) Fingerprint() []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.entries))
	for _, v := range t.entries {
		if v == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, v.String())
	}
	sort.Strings(out)
	return out
}

// Fingerprint is MethodTable's cross-run comparable rendering: how many of each kind of
// resolution were recorded, plus the sorted names of what they resolved to.
func (t *MethodTable) Fingerprint() []string {
	if t == nil {
		return nil
	}
	out := []string{
		fmt.Sprintf("entries=%d", len(t.entries)),
		fmt.Sprintf("resolutions=%d", len(t.resolutions)),
		fmt.Sprintf("boundCalls=%d", len(t.boundCalls)),
		fmt.Sprintf("builtins=%d", len(t.builtins)),
		fmt.Sprintf("builtinAllocs=%d", len(t.builtinAllocs)),
		fmt.Sprintf("boundCandidates=%d", len(t.boundCandidates)),
		fmt.Sprintf("operatorResolutions=%d", len(t.operatorResolutions)),
		fmt.Sprintf("operatorCandidates=%d", len(t.operatorCandidates)),
		fmt.Sprintf("operatorBounds=%d", len(t.operatorBounds)),
	}
	for _, m := range t.entries {
		if m != nil {
			out = append(out, "impl:"+m.Name.Key())
		}
	}
	for _, r := range t.boundCalls {
		out = append(out, "bound:"+r.Trait+"::"+r.Method)
	}
	sort.Strings(out)
	return out
}

// Fingerprint is the instantiation set by key — the specializations the backend will emit.
func (t *InstantiationTable) Fingerprint() []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.discovered)+len(t.byCall))
	for k := range t.discovered {
		out = append(out, "discovered:"+k)
	}
	for _, inst := range t.byCall {
		out = append(out, "call:"+inst.Key())
	}
	sort.Strings(out)
	return out
}
