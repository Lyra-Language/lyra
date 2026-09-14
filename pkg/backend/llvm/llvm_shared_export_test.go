package llvm

import "testing"

// **Two modules exporting one name build to two of everything** (09/13). The name was a
// program-wide claim until then, so the backend never met two exported functions or two
// exported types of one name in one program; both must lower to distinct symbols and
// layouts. A generic instantiated at the second type is not covered here — see todo.md's
// entry on a type argument from another module, which predates shared exports.
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
