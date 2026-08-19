package modules_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// These check what a directory module *means* once analyzed, which the resolver tests
// (package `modules`) deliberately do not: they stop at which units came back.
//
// The whole reason multi-file modules exist rather than "split it into more modules" is
// that a module is the unit several rules are keyed on. Each test below is one of those
// rules, asserted across a file boundary.

// analyzeTree resolves and analyzes a fixture tree entered at app.lyra.
func analyzeTree(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		write(t, filepath.Join(dir, filepath.FromSlash(name)), body)
	}
	units, diags := modules.Resolve(filepath.Join(dir, "app.lyra"), []string{dir}, modules.Options{})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

// **Receiver-keyed overloading spans a module's files.** This is the property that makes
// the split safe: `unwrap_or` for `Maybe` and for `Result` may coexist only because they
// are in one module, so had the prelude been split into `std.maybe` and `std.result`
// instead, they would have become a cross-module duplicate and one of them would have had
// to be renamed. Here the two declarations are in different *files* of one module, and
// both resolve.
func TestMultiFile_ReceiverOverloadsSpanFiles(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.shape.{ Square, Circle }
let main = () -> u8 => {
  let s = Square { side: 3 }
  let c = Circle { r: 2 }
  u8(s.area() + c.area())
}`,
		"util/shape/square.lyra": `module util.shape
pub struct Square { side: i64 }
pub let area = pure (self: Square) -> i64 => self.side * self.side`,
		"util/shape/circle.lyra": `module util.shape
pub struct Circle { r: i64 }
pub let area = pure (self: Circle) -> i64 => 3 * self.r * self.r`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("two `area`s in one module's two files should both resolve on their own receiver; got %v", errs)
	}
}

// The other side of it: overlapping receiver heads are still refused, and being in
// different files does not launder the clash. Two `area`s for `Square` would need a
// specificity ordering the language does not have.
func TestMultiFile_OverlappingReceiverHeadsAreStillRefused(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.shape
let main = () -> u8 => u8(Square { side: 3 }.area())`,
		"util/shape/a.lyra": `module util.shape
pub struct Square { side: i64 }
pub let area = pure (self: Square) -> i64 => self.side * self.side`,
		"util/shape/b.lyra": `module util.shape
pub let area = pure (self: Square) -> i64 => 0`,
	})
	if len(res.Errors()) == 0 {
		t.Error("two declarations of `area` for the same receiver head must be refused, in one file or two")
	}
}

// A module's files share one scope, so a **private** declaration in one is visible to
// another. `pub` is exactly the module boundary — enforcing it between a module's own
// files would make a helper unusable by the module that wrote it.
func TestMultiFile_PrivateDeclarationIsVisibleWithinTheModule(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.math.{ util_double }
let main = () -> u8 => u8(util_double(4))`,
		"util/math/helper.lyra": `module util.math
let twice = pure (n: i64) -> i64 => n * 2`,
		"util/math/api.lyra": `module util.math
pub let util_double = pure (n: i64) -> i64 => twice(n)`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a module's files share a scope, so a private helper should be reachable; got %v", errs)
	}
}

// And the boundary itself is unmoved: that private helper is still private *outside* the
// module. A directory module must not become a way to leak a name.
func TestMultiFile_PrivateDeclarationStaysPrivateOutside(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.math
let main = () -> u8 => u8(twice(4))`,
		"util/math/helper.lyra": `module util.math
let twice = pure (n: i64) -> i64 => n * 2`,
		"util/math/api.lyra": `module util.math
pub let util_double = pure (n: i64) -> i64 => twice(n)`,
	})
	if len(res.Errors()) == 0 {
		t.Error("a private declaration must stay private outside its module, however many files the module has")
	}
}

// A type declared in one file is the same type in another — one module, one identity, so
// a value built in one file's function is accepted by another's.
func TestMultiFile_TypeIdentityIsSharedAcrossFiles(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.shape.{ make, width }
let main = () -> u8 => u8(width(make(5)))`,
		"util/shape/decl.lyra": `module util.shape
pub struct Box { w: i64 }
pub let make = pure (w: i64) -> Box => Box { w: w }`,
		"util/shape/use.lyra": `module util.shape
pub let width = pure (b: Box) -> i64 => b.w`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a type declared in one file of a module is the same type in another; got %v", errs)
	}
}

// A duplicate *without* receivers is the error it always was, across files as within one.
// The relaxation is receiver-keyed overloading and nothing else.
func TestMultiFile_PlainDuplicateAcrossFilesIsStillAnError(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"app.lyra": `import util.math
let main = () -> u8 => u8(one())`,
		"util/math/a.lyra": `module util.math
pub let one = pure () -> i64 => 1`,
		"util/math/b.lyra": `module util.math
pub let one = pure () -> i64 => 2`,
	})
	if len(res.Errors()) == 0 {
		t.Error("two plain declarations of one name in a module must clash, in one file or two")
	}
}

// A file of a multi-file module analyzed **on its own** still sees its siblings, which is
// the analysis-level statement of what entering at one file resolves to. This is the
// property that lets the shipped prelude be checked standalone now that it is a
// directory.
func TestMultiFile_EntryAtOneFileSeesItsSiblings(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "util", "math", "api.lyra"), `module util.math
pub let util_double = pure (n: i64) -> i64 => twice(n)`)
	write(t, filepath.Join(dir, "util", "math", "helper.lyra"), `module util.math
let twice = pure (n: i64) -> i64 => n * 2`)

	entry := filepath.Join(dir, "util", "math", "api.lyra")
	units, diags := modules.Resolve(entry, []string{dir}, modules.Options{})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	errs := driver.AnalyzeUnits(units).Errors()
	if len(errs) != 0 {
		t.Fatalf("checking one file of a module must bring the rest of it; got %v", errs)
	}
	// Guard the guard: `twice` must genuinely be the sibling's, so a future change that
	// stops gathering siblings fails here rather than passing on an empty program.
	var names []string
	for _, u := range units {
		names = append(names, filepath.Base(u.File))
	}
	if got := strings.Join(names, ","); got != "api.lyra,helper.lyra" {
		t.Errorf("got units %q; want both files of util.math", got)
	}
}
