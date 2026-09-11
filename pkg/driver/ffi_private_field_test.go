package driver_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// **A struct's C layout does not depend on whether its fields' types are exported.**
//
// `hasCLayout` resolved every field's type with an empty location — no module context — so
// a field whose type was *private* to the declaring module resolved to nothing, and a
// struct containing one was refused as having "no C spelling" (lyra-E063). `bindings/raylib`
// hit it with `Model`, which holds a private `ModelSkeleton`: twelve externs taking a
// `Model` failed until the helper struct was made `pub`, which has nothing to do with
// layout. A struct's fields are now resolved from the struct's own declaration.
//
// It needs a named module to show up: the entry module's names resolve without context, so
// a single-file program never had the problem.
func TestFFI_APrivateNestedStructHasACLayout(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"main.lyra": `module main
import lib.{ use_outer }
let main = () -> void => use_outer()
`,
		"lib.lyra": `module lib
struct Inner { a: i32, b: i32 }
pub struct Outer { inner: Inner, c: f32 }
extern takes_outer: (o: Outer) -> void
pub let use_outer = () -> void => { }
`,
	})
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeNotFFISafe {
			t.Errorf("a struct with a private nested struct was refused: %s", d.Message)
		}
	}
}
