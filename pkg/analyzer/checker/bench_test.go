package checker_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// CheckPurity in isolation, over the **real prelude** plus a program that leans on the
// shapes its fixpoint costs the most on: many top-level functions, call chains between
// them, trait methods, and callbacks (which is what makes a body's effect depend on
// another's and so forces the fixpoint to iterate rather than settle in one round).
//
// It is measured on its own because the whole-pipeline benchmarks in pkg/driver cannot see
// it: parsing and collection dominate those so completely that CheckPurity does not appear
// in a CPU profile of BenchmarkAnalyze_Large at all. That is worth knowing in both
// directions — it means a regression here is invisible end-to-end, and it means an
// optimization here should not be sold as a pipeline win.

func purityBenchSource(scale int) string {
	var b strings.Builder
	b.WriteString(`trait Runner { run: (Self, () -> i64) -> i64 }
struct Box { n: i64 }
impl Runner for Box { run = (self, f) => f() + self.n }
`)
	for i := 0; i < scale; i++ {
		fmt.Fprintf(&b, `
let leaf%[1]d = pure (n: i64) -> i64 => n * 2
let apply%[1]d = (b: Box, f: () -> i64) -> i64 => b.run(f)
let mid%[1]d = (b: Box, n: i64) -> i64 => apply%[1]d(b, () => leaf%[1]d(n))
let top%[1]d = (b: Box, n: i64) -> i64 => mid%[1]d(b, n) + leaf%[1]d(n)
`, i)
	}
	b.WriteString("\nlet main = () -> u8 => u8(top0(Box { n: 1 }, 2))\n")
	return b.String()
}

func purityBenchResult(b *testing.B, scale int) *driver.Result {
	b.Helper()
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "std", "prelude")); err != nil {
		b.Skipf("no std/prelude/ beside the repo root: %v", err)
	}
	dir := b.TempDir()
	app := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(app, []byte(purityBenchSource(scale)), 0o644); err != nil {
		b.Fatal(err)
	}
	units, diags := modules.Resolve(app, []string{repoRoot, dir}, modules.Options{Prelude: modules.PreludeModule})
	if len(units) == 0 {
		b.Fatalf("resolve produced no units: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	// A benchmark over an erroring program measures the error paths, not the ones that
	// matter.
	if res.HasErrors() {
		b.Fatalf("benchmark program has errors: %v", res.Errors())
	}
	return res
}

func benchmarkCheckPurity(b *testing.B, scale int) {
	res := purityBenchResult(b, scale)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckPurity(res.Program, res.ScopeTable, res.TypeTable, res.MethodTable, res.Captures)
	}
}

func BenchmarkCheckPurity_Small(b *testing.B)  { benchmarkCheckPurity(b, 1) }
func BenchmarkCheckPurity_Medium(b *testing.B) { benchmarkCheckPurity(b, 20) }
func BenchmarkCheckPurity_Large(b *testing.B)  { benchmarkCheckPurity(b, 80) }
