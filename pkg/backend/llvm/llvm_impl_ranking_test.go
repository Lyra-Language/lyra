package llvm

import "testing"

// **A more specific impl wins, and the body it runs is the specific one** — which a type
// check alone would not show. `impl Show for Box<i64>` beside `impl Show<t> for Box<t>` is
// not an ambiguity: `Box<t>` matches `Box<i64>` by binding `t`, `Box<i64>` matches `Box<t>`
// not at all, so the concrete target subsumes the general one (09/23).
//
// Ranking runs in resolveTraitMethodNamed, so `.method()`, operator dispatch and
// `Trait::method` paths all reach it — specificity is a property of the impls rather than
// of how they were reached.
func TestExec_MoreSpecificImplWins(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The specific body runs, not merely "a body type-checks".
			"the concrete target is taken",
			`struct Box<t> { value: t }
trait Rank { rank: (Self) -> i64 }
impl Rank<t> for Box<t> { rank = (self) => 1 }
impl Rank for Box<i64> { rank = (self) => 9 }
let main = () -> u8 => u8(Box { value: 5 }.rank())`,
			9,
		},
		{
			// …and the general one still runs where nothing is more specific, which is
			// what makes this a ranking rather than the concrete impl shadowing it.
			"the general target still applies elsewhere",
			`struct Box<t> { value: t }
trait Rank { rank: (Self) -> i64 }
impl Rank<t> for Box<t> { rank = (self) => 1 }
impl Rank for Box<i64> { rank = (self) => 9 }
let main = () -> u8 => u8(Box { value: "s" }.rank())`,
			1,
		},
		{
			// Both in one program, so the choice is made twice from the same pair.
			"both impls reachable from one program",
			`struct Box<t> { value: t }
trait Rank { rank: (Self) -> i64 }
impl Rank<t> for Box<t> { rank = (self) => 1 }
impl Rank for Box<i64> { rank = (self) => 9 }
let main = () -> u8 => u8(Box { value: 5 }.rank() + Box { value: true }.rank())`,
			10,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}
