package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// A trait name is *written* in four positions — an impl head, a `where` bound, a
// supertrait list and a `Trait::method` path — and each must reach the trait through an
// import, as a written type name must. Until 09/28 all four resolved through the
// program-wide rung unimported (Sheliak's `impl Bus for TestBus` compiled with `Bus` never
// imported), and an import that did list the trait drew lyra-W004 for the impl head and
// the bound, which nothing counted as a use.

const writtenTraitLib = `module lib
pub trait Tag { pure raw: (Self) -> string }
pub trait Zero { pure zero: () -> Self }
pub struct Box { v: i64 }
impl Tag for Box { raw = pure (self) => "box" }
impl Zero for Box { zero = pure () => Box { v: 0 } }
trait Hidden { pure secret: (Self) -> i64 }
`

// Each position, as the body of main.lyra after its import line.
var writtenTraitPositions = map[string]string{
	"impl head": `struct Mine { v: i64 }
impl Tag for Mine { raw = pure (self) => "mine" }
let main = () -> void => println(Mine { v: 1 }.v)
`,
	"where bound": `let count<t> where t: Tag = pure (xs: []t) -> i64 => xs.len()
let main = () -> void => println(count([Box { v: 1 }]))
`,
	"supertrait": `trait Loud: Tag { pure shout: (Self) -> string }
let main = () -> void => println(1)
`,
	"Trait::method path": `let main = () -> void => {
  let b: Box = Zero::zero()
  println(b.v)
}
`,
}

// traitFor is the trait each position names.
func traitFor(position string) string {
	if position == "Trait::method path" {
		return "Zero"
	}
	return "Tag"
}

func TestWrittenTraitName_RefusedUnimported(t *testing.T) {
	for position, body := range writtenTraitPositions {
		t.Run(position, func(t *testing.T) {
			trait := traitFor(position)
			res := analyzeFiles(t, map[string]string{
				"lib.lyra":  writtenTraitLib,
				"main.lyra": "module main\nimport lib.{ Box }\n" + body,
			})
			want := `unknown trait "` + trait + `"`
			hint := "import lib.{ " + trait + " }"
			for _, d := range res.Diagnostics {
				if d.Severity == diag.SeverityError && strings.Contains(d.Message, want) &&
					strings.Contains(d.Message, hint) {
					return
				}
			}
			t.Errorf("no %s error naming %q; got %v", want, hint, res.Diagnostics)
		})
	}
}

func TestWrittenTraitName_ImportedIsAcceptedAndUsed(t *testing.T) {
	for position, body := range writtenTraitPositions {
		t.Run(position, func(t *testing.T) {
			trait := traitFor(position)
			res := analyzeFiles(t, map[string]string{
				"lib.lyra":  writtenTraitLib,
				"main.lyra": "module main\nimport lib.{ Box, " + trait + " }\n" + body,
			})
			for _, d := range res.Diagnostics {
				if d.Severity == diag.SeverityError {
					t.Errorf("error with the trait imported: %s", d.Message)
				}
				if d.Code == diag.CodeUnusedImport && strings.Contains(d.Message, `"`+trait+`"`) {
					t.Errorf("the import the %s needs is reported unused: %s", position, d.Message)
				}
			}
		})
	}
}

// A private trait is "not yours", not unknown: the author can see it in the other file.
// (The import of `Box` is what loads `lib` at all.)
func TestWrittenTraitName_PrivateTraitSaysSo(t *testing.T) {
	res := analyzeFiles(t, map[string]string{
		"lib.lyra": writtenTraitLib,
		"main.lyra": `module main
import lib.{ Box }
struct Mine { v: i64 }
impl Hidden for Mine { secret = pure (self) => 7 }
let main = () -> void => println(Mine { v: 1 }.v)
`,
	})
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodePrivateAccess && strings.Contains(d.Message, `private to module "lib"`) {
			return
		}
	}
	t.Errorf("no lyra-E028 for implementing lib's private Hidden; got %v", res.Diagnostics)
}
