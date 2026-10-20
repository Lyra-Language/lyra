package llvm

import (
	"strings"
	"testing"
)

// `std.collections`'s `Set<t>` operations that build or compare whole sets. A set has no
// order, so each result is printed through `show`, which sorts its members first; the
// tests check answers, never the table's layout.

// setPreamble declares `show` and `of`, the two helpers every test below uses: a set's
// members sorted and joined with commas, and a set built from an array.
const setPreamble = `module main
import std.collections.{ Set, set_new }
let show = (s: Set<i64>) -> string => s.members().sorted().map((x) => "${x}").join(",")
let of = (xs: []i64) -> Set<i64> => {
  var s = set_new::<i64>()
  for x in xs { let _ = s.insert(x) }
  s
}
`

// Union, intersection and both differences of two overlapping sets, and of a set with an
// empty one. The operands are printed again at the end: each operation builds a new set
// and must leave both of its inputs as they were.
func TestExec_SetCombinations(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, setPreamble+`let main = () -> void => {
  let a = of([1, 2, 3, 4])
  let b = of([3, 4, 5])
  let none = of([])
  println("union ${show(a.union(b))} ${show(b.union(a))} ${show(a.union(none))}")
  println("intersection ${show(a.intersection(b))} ${show(b.intersection(a))} [${show(a.intersection(none))}]")
  println("difference ${show(a.difference(b))} ${show(b.difference(a))} ${show(a.difference(none))} [${show(none.difference(a))}]")
  println("symmetric ${show(a.symmetric_difference(b))} ${show(b.symmetric_difference(a))} [${show(a.symmetric_difference(a))}]")
  println("unchanged ${show(a)} ${show(b)} [${show(none)}]")
}
`, "")
	want := strings.Join([]string{
		"union 1,2,3,4,5 1,2,3,4,5 1,2,3,4",
		"intersection 3,4 3,4 []",
		"difference 1,2 5 1,2,3,4 []",
		"symmetric 1,2,5 1,2,5 []",
		"unchanged 1,2,3,4 3,4,5 []",
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("set combinations:\n%s\nwant:\n%s", got, want)
	}
}

// Subset, proper subset and disjointness at their edges: the empty set, a set against
// itself, an equal set built separately, a larger set, and two sets of one size that
// differ. Each line is one relation asked of the same pairs, in the order the comment on
// it gives.
func TestExec_SetRelations(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, setPreamble+`let main = () -> void => {
  let a = of([1, 2, 3])
  let same = of([3, 2, 1])
  let part = of([1, 2])
  let other = of([1, 2, 4])
  let apart = of([7, 8])
  let none = of([])
  // none⊆a, a⊆a, a⊆same, part⊆a, a⊆part, other⊆a, none⊆none
  println("subset ${none.is_subset_of(a)} ${a.is_subset_of(a)} ${a.is_subset_of(same)} ${part.is_subset_of(a)} ${a.is_subset_of(part)} ${other.is_subset_of(a)} ${none.is_subset_of(none)}")
  // the same pairs, proper
  println("proper ${none.is_proper_subset_of(a)} ${a.is_proper_subset_of(a)} ${a.is_proper_subset_of(same)} ${part.is_proper_subset_of(a)} ${a.is_proper_subset_of(part)} ${other.is_proper_subset_of(a)} ${none.is_proper_subset_of(none)}")
  // a/apart both ways, a/part, a/other, a/none, none/none
  println("disjoint ${a.disjoint(apart)} ${apart.disjoint(a)} ${a.disjoint(part)} ${a.disjoint(other)} ${a.disjoint(none)} ${none.disjoint(none)}")
}
`, "")
	want := strings.Join([]string{
		"subset true true true true false false true",
		"proper true false false true false false false",
		"disjoint true true false false true true",
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("set relations:\n%s\nwant:\n%s", got, want)
	}
}

// The identities that tie the operations together, over sets large enough to grow the
// table underneath: a set is the disjoint union of its difference and its intersection
// with another; the symmetric difference is the union less the intersection; and each
// result is a subset of the union. Multiples of 2 and of 3 below 300 overlap at the
// multiples of 6, so every count is known: 150, 100, 50 shared, 200 in the union.
func TestExec_SetIdentitiesOverAGrownTable(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, setPreamble+`let main = () -> void => {
  var evens = set_new::<i64>()
  var threes = set_new::<i64>()
  for i in 0..<300 {
    if i %% 2 == 0 { let _ = evens.insert(i) }
    if i %% 3 == 0 { let _ = threes.insert(i) }
  }
  let both = evens.union(threes)
  let shared = evens.intersection(threes)
  let only = evens.difference(threes)
  let either = evens.symmetric_difference(threes)
  let parts_rebuild = only.union(shared).len() == evens.len() && only.disjoint(shared)
  let sym_is_union_less_shared = show(either) == show(both.difference(shared))
  let all_inside = shared.is_subset_of(both) && only.is_subset_of(both) && either.is_proper_subset_of(both)
  println("${evens.len()} ${threes.len()} ${shared.len()} ${both.len()} ${only.len()} ${either.len()}")
  println("${parts_rebuild} ${sym_is_union_less_shared} ${all_inside}")
}
`, "")
	want := "150 100 50 200 100 150\ntrue true true"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("set identities:\n%s\nwant:\n%s", got, want)
	}
}

// String members under ASan: every result set holds strings shared with its operands, so a
// missing retain when an item is copied into a new set is a use-after-free once the
// operands are released at the end of each round.
func TestExec_SetCombinationsOfStringsASan(t *testing.T) {
	t.Parallel()
	src := `module main
import std.collections.{ Set, set_new }
let of = (prefix: string, from: i64, to: i64) -> Set<string> => {
  var s = set_new::<string>()
  for i in from..<to { let _ = s.insert(prefix ++ "${i}") }
  s
}
let main = () -> u8 => {
  var total = 0
  for round in 0..<10 {
    let a = of("k", 0, 40)
    let b = of("k", 20, 60)
    let u = a.union(b)
    let n = a.intersection(b)
    let d = a.difference(b)
    let x = a.symmetric_difference(b)
    if u.contains("k59") && n.contains("k20") && !d.contains("k20") && x.contains("k0") && a.disjoint(of("z", 0, 5)) {
      total += u.len() + n.len() + d.len() + x.len()
    }
  }
  if total == 10 * (60 + 20 + 20 + 40) { 3 } else { 1 }
}`
	if got := exitCode(t, command(preludeBinary(t, src)).Run()); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
