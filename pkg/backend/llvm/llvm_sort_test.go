package llvm

import (
	"strings"
	"testing"
)

// The prelude's sorts, checked against each other and against a hand-made order on the
// inputs that break a naive quicksort: sorted, reversed, all equal, and many duplicates.
// Everything goes through `compare`, so a derived `Ord` on a struct is the last case.
func TestExec_Sort(t *testing.T) {
	t.Parallel()
	src := `
@derive(Ord)
struct Pt { x: i64, y: i64 }

let is_sorted = pure (xs: []i64) -> bool => {
  var ok = true
  for i in 1..<xs.len() { if xs[i - 1] > xs[i] { ok = false } }
  ok
}

let check = (name: string, xs: []i64) -> void => {
  var a = xs
  var b = xs
  a.quick_sort()
  b.heap_sort()
  var same = a.len() == b.len()
  for i in 0..<a.len() { if a[i] != b[i] { same = false } }
  println("${name}: ${is_sorted(a)} ${same}")
}

let main = () -> void => {
  var rng = rng_seeded(11)
  var random: []i64 = []
  for _ in 0..<5000 { random.push(rng.between(-1000, 1000)) }
  check("random", random)

  var sorted: []i64 = []
  for i in 0..<3000 { sorted.push(i) }
  check("sorted", sorted)

  var reversed: []i64 = []
  for i in 3000..>0 { reversed.push(i) }
  check("reversed", reversed)

  check("all equal", [7; 2000])

  var dups: []i64 = []
  for i in 0..<4000 { dups.push(i %% 3) }
  check("duplicates", dups)

  var organ: []i64 = []
  for i in 0..<1000 { organ.push(i) }
  for i in 1000..>0 { organ.push(i) }
  check("organ pipe", organ)

  check("small", [3, 1, 2])
  check("one", [42])
  check("empty", [])

  var ws: []string = ["pear", "apple", "fig", "banana", "apple"]
  ws.sort()
  println(ws.join(","))

  var ps: []Pt = [Pt { x: 2, y: 1 }, Pt { x: 1, y: 9 }, Pt { x: 1, y: 2 }]
  ps.sort()
  for p in ps { print("${p.x},${p.y} ") }
  println("")
}`
	got := buildAndRunWithPrelude(t, src, "")
	want := strings.Join([]string{
		"random: true true", "sorted: true true", "reversed: true true",
		"all equal: true true", "duplicates: true true", "organ pipe: true true",
		"small: true true", "one: true true", "empty: true true",
		"apple,apple,banana,fig,pear", "1,2 1,9 2,1 ", "",
	}, "\n")
	if got != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got, want)
	}
}

// The heapsort fallback is private and median-of-three defeats the classic adversaries
// (sorted, reversed, organ pipe), so it is reached through `sort` with Musser's
// median-of-three killer: a permutation that makes every partition on the first, middle
// and last element split off two elements, so the partition depth passes 2·log2(n) and
// the budget runs out. Verified against the prelude's exact pivot rule when written
// (09/07) by a temporary print in the fallback branch: it fired once at every size
// here. **Tied to that pivot rule** — a different pivot choice keeps this test green
// without reaching the fallback, so re-verify the same way if `partition` changes.
func TestExec_Sort_HeapsortFallback(t *testing.T) {
	t.Parallel()
	src := `
let median_of_three_killer = pure (n: i64) -> []i64 => {
  let k = n / 2
  var xs: []i64 = [0; n]
  for i in 1..<=k {
    if i %% 2 == 1 {
      xs[i - 1] = i
      if i < k { xs[i] = k + i }
    }
    xs[k + i - 1] = 2 * i
  }
  xs
}

let main = () -> void => {
  for n in [64, 1024, 4096] {
    var xs = median_of_three_killer(n)
    xs.sort()
    var ok = xs.len() == n
    for i, x in xs { if x != i + 1 { ok = false } }
    println("${n} ${ok}")
  }
}`
	got := buildAndRunWithPrelude(t, src, "")
	if want := "64 true\n1024 true\n4096 true\n"; got != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", got, want)
	}
}
