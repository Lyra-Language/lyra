package driver_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// largeParams are the parameters lyra-W031 names, sorted, as `name:bytes`.
func largeParams(t *testing.T, files map[string]string) []string {
	t.Helper()
	res := analyzeModules(t, files)
	for _, d := range res.Errors() {
		t.Errorf("unexpected error: %s", d.Message)
	}
	quoted := regexp.MustCompile(`^parameter "([^"]+)" is a (\d+)-byte`)
	var got []string
	for _, d := range res.Diagnostics {
		if d.Code != diag.CodeLargeByValueParam {
			continue
		}
		m := quoted.FindStringSubmatch(d.Message)
		if m == nil {
			t.Fatalf("unexpected message: %s", d.Message)
		}
		got = append(got, m[1]+":"+m[2])
	}
	sort.Strings(got)
	return got
}

// A large aggregate taken by a bare parameter is copied at every call, and said so at the
// declaration (lyra-W031): a struct, a fixed array, a `data` type, a newtype over one, a
// generic struct instantiated large, an `impl` member's `self` — each sized as the backend lays it out, a struct's
// fields from its own declaration, so lib's `Cells` is lib's and not this module's.
func TestLargeByValueParams_AreReportedAtTheDeclaration(t *testing.T) {
	got := largeParams(t, map[string]string{
		"lib.lyra": `module lib
struct Cells { c: [40]i64 }
pub struct Bus { cells: Cells, n: i64 }
`,
		"main.lyra": `module main
import lib.{ Bus }
struct Cells { c: i64 }
struct Pair<t> { a: t, b: t }
data Frame = Full([64]i64) | Empty
type Screen = [64]i64
pub let bus = pure (b: Bus) -> i64 => b.n
pub let ram = pure (r: [64]i64) -> i64 => r[0]
pub let frame = pure (f: Frame) -> i64 => match f { Full(px) => px[0], Empty => 0 }
pub let screen = pure (s: Screen) -> i64 => 0
pub let pair = pure (p: Pair<[32]i64>) -> i64 => p.a[0]
pub let small = pure (c: Cells) -> i64 => c.c
struct Grid { c: [64]i64 }
impl Grid {
  pub let first = pure (self) -> i64 => self.c[0]
}
`,
	})
	want := []string{"b:328", "f:520", "p:512", "r:512", "s:512", "self:512"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Nothing is said where the parameter is not a copy, where `ref` would not compile — a
// function used as a value (lyra-E082), directly or through a namespace, and a nested
// lambda — or where a borrow could outlive the call, a generator's.
func TestLargeByValueParams_StaySilentWhereRefIsNoAnswer(t *testing.T) {
	got := largeParams(t, map[string]string{
		"lib.lyra": `module lib
pub struct Big { c: [64]i64 }
pub let through = pure (b: Big) -> i64 => b.c[0]
`,
		"main.lyra": `module main
import lib
import lib.{ Big }
struct Node { c: [64]i64 }
pub let borrowed = pure (b: ref Big) -> i64 => b.c[0]
pub let owned = pure (b: own Big) -> i64 => b.c[0]
pub let generic<t> = pure (x: t) -> t => x
pub let boxed = pure (n: shared Node) -> i64 => n.c[0]
pub let listed = pure (xs: []Big) -> i64 => xs.len()
pub let valued = pure (b: Big) -> i64 => b.c[0]
pub let apply = (f: (Big) -> i64, b: ref Big) -> i64 => f(b)
pub let uses = (b: ref Big) -> i64 => apply(valued, b) + apply(lib.through, b)
pub let outer = pure (b: ref Big) -> i64 => {
  let inner = (c: Big) -> i64 => c.c[1]
  inner(b)
}
pub let counted = gen (b: Big) -> Seq<i64> => { yield b.c[0] }
`,
	})
	if len(got) != 0 {
		t.Errorf("expected no lyra-W031, got %v", got)
	}
}

// The fix is spelled as it is written: `name: ref T`, and for `self` the `impl` block's
// `self: ref` beside the top level's `self: ref T`.
func TestLargeByValueParams_SayHowToWriteTheFix(t *testing.T) {
	res := analyzeModules(t, map[string]string{"main.lyra": `module main
struct Grid { c: [64]i64 }
impl Grid {
  pub let first = pure (self) -> i64 => self.c[0]
}
pub let corner = pure (g: Grid) -> i64 => g.c[63]
`})
	var messages []string
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeLargeByValueParam {
			messages = append(messages, d.Message)
		}
	}
	sort.Strings(messages)
	if len(messages) != 2 ||
		!strings.Contains(messages[0], "`g: ref Grid`") ||
		!strings.Contains(messages[1], "`self: ref` (`self: ref Grid` outside an `impl` block)") {
		t.Errorf("unexpected messages: %q", messages)
	}
}
