package llvm

import "testing"

// **Two modules exporting one name build to two of everything** (09/13). The name was a
// program-wide claim until then, so the backend never met two exported functions or two
// exported types of one name in one program; both must lower to distinct symbols and
// layouts.
func TestExec_TwoModulesExportingOneName(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"app.lyra": `import one.{ helper, Point }
import two
let main = () -> u8 => {
  let a = Point { x: 3 }
  let b = two.make()
  u8(helper() * 100 + two.helper() * 10 + a.x + b.y + b.z)
}
`,
		"one.lyra": `module one
pub let helper = () -> i64 => 1
pub struct Point { x: i64 }
`,
		"two.lyra": `module two
pub let helper = () -> i64 => 2
pub struct Point { y: i64, z: i64 }
pub let make = () -> Point => Point { y: 1, z: 2 }
`,
	})
	if got != 126 {
		t.Errorf("exited %d; want 126 (100 + 20 + 3 + 3)", got)
	}
}

// **Another module's type as a generic argument** (09/13). A resolved named type carried no
// declaration, so `Has(two.make())` in a module that also has its own `Point` stored two's
// value into this module's layout — and, with no `Point` here at all, failed as an unknown
// type. A type whose name is not program-wide now carries its declaration key: the backend
// lowers it by that key, and an instantiation over it is named apart (`Opt$two_Point`). Both
// modules' `Point`s go through one generic data type and one generic function here, and one
// holds a string, so drop glue must tell them apart too.
func TestExec_AnotherModulesTypeAsAGenericArgument(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"app.lyra": `import two
struct Point { x: i64 }
data Opt<t> = Has(t) | Gone
let idf<t> = (v: t) -> t => v
let main = () -> u8 => {
  let mine = Has(Point { x: 3 })
  let theirs = Has(two.make())
  let a = match mine { Has(p) => idf(p).x, Gone => 0 }
  let b = match theirs { Has(p) => idf(p).y + idf(p).label.len(), Gone => 0 }
  u8(a * 10 + b)
}
`,
		"two.lyra": `module two
struct Point { y: i64, label: string }
pub let make = () -> Point => Point { y: 4, label: "ab" ++ "c" }
`,
	})
	if got != 37 {
		t.Errorf("exited %d; want 37 (3 * 10 + 4 + 3)", got)
	}
}
