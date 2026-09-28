package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// A call through a `where` bound is charged the join of its trait method's impls, and
// "its trait" is a declaration, not a name. Until 09/28 the purity pass grouped impls by
// the trait's *name*: two modules' same-named traits pooled their impls (a false lyra-E007
// on a pure generic), and an impl written through an alias (`impl S for Loud` with
// `Speak as S`) sat under the alias, invisible to the bound — so its effect was missed.

const speakLib = `module a
pub trait Speak { say: (Self) -> i64 }
pub struct Quiet { n: i64 }
impl Speak for Quiet { say = pure (self) => self.n }
pub let loudness<t> where t: Speak = pure (x: t) -> i64 => x.say()
`

func purityErrors(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeFiles(t, files).Diagnostics {
		if d.Code == diag.CodePurityViolation {
			out = append(out, d.Message)
		}
	}
	return out
}

func TestBoundTraitIdentity_SameNamedTraitsDoNotPool(t *testing.T) {
	got := purityErrors(t, map[string]string{
		"a.lyra": speakLib,
		"b.lyra": `module b
pub trait Speak { say: (Self) -> i64 }
pub struct Loud { n: i64 }
impl Speak for Loud { say = (self) => { println("LOUD"); self.n } }
pub let shout = (l: Loud) -> i64 => l.say()
`,
		"main.lyra": `module main
import a.{ Quiet, loudness }
import b.{ Loud, shout }
let main = () -> u8 => u8(loudness(Quiet { n: 3 }) + shout(Loud { n: 4 }))
`,
	})
	if len(got) != 0 {
		t.Errorf("b's Speak is not a's: want no purity error; got %v", got)
	}
}

func TestBoundTraitIdentity_TheSameTraitStillJoins(t *testing.T) {
	got := purityErrors(t, map[string]string{
		"a.lyra": speakLib,
		"main.lyra": `module main
import a.{ Speak, Quiet, loudness }
struct Loud { n: i64 }
impl Speak for Loud { say = (self) => { println("LOUD"); self.n } }
let main = () -> u8 => u8(loudness(Loud { n: 4 }))
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], `non-pure trait method "say" via a bound`) {
		t.Errorf("an impure impl of a's Speak must reach loudness; got %v", got)
	}
}

func TestBoundTraitIdentity_AnImplThroughAnAliasJoins(t *testing.T) {
	got := purityErrors(t, map[string]string{
		"a.lyra": speakLib,
		"main.lyra": `module main
import a.{ Speak as S, Quiet, loudness }
struct Loud { n: i64 }
impl S for Loud { say = (self) => { println("LOUD"); self.n } }
let main = () -> u8 => u8(loudness(Loud { n: 4 }))
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], `non-pure trait method "say" via a bound`) {
		t.Errorf("`impl S` is an impl of a's Speak and must reach loudness; got %v", got)
	}
}
