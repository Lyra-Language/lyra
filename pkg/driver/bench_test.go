package driver_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// The analysis pipeline is re-run over the whole program *plus the prelude* on every
// LSP didChange, so what these measure is per-keystroke cost, not per-build cost.
// They resolve against the real std/ rather than a stub prelude, since the prelude is
// most of what a small program's analysis actually walks.

// benchSource builds a program that leans on the constructs the hot paths key off:
// data constructors (Some/None/Ok/Err), UFCS method calls, matches over generic data
// types, and trait dispatch. `scale` repeats the block so the cost is measurable.
func benchSource(scale int) string {
	var b strings.Builder
	b.WriteString(`struct Pt { x: i64, y: i64 }
trait Area { area: (Self) -> i64 }
impl Area for Pt { area = (self) => self.x * self.y }
`)
	for i := 0; i < scale; i++ {
		fmt.Fprintf(&b, `
let pick%[1]d = (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }
let use%[1]d = (n: i64) -> i64 => match pick%[1]d(n) {
  Some(v) => v + Pt { x: v, y: 2 }.area(),
  None => 0,
}
let chain%[1]d = (n: i64) -> i64 => pick%[1]d(n).unwrap_or(7) + use%[1]d(n)
`, i)
	}
	b.WriteString("\nlet main = () -> u8 => u8(chain0(1))\n")
	return b.String()
}

// benchUnits writes the program beside the repo's real std/ and resolves it, so the
// units include std.prelude exactly as lyrac and lyra-lsp see it.
func benchUnits(b *testing.B, scale int) []modules.Unit {
	b.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "std", "prelude")); err != nil {
		b.Skipf("no std/prelude/ beside the repo root: %v", err)
	}
	dir := b.TempDir()
	app := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(app, []byte(benchSource(scale)), 0o644); err != nil {
		b.Fatal(err)
	}
	units, diags := modules.Resolve(app, []string{repoRoot, dir}, modules.Options{Prelude: modules.PreludeModule})
	if len(units) == 0 {
		b.Fatalf("resolve produced no units: %v", diags)
	}
	return units
}

func benchmarkAnalyze(b *testing.B, scale int) {
	units := benchUnits(b, scale)
	// Fail loudly on a program that does not analyze cleanly: a benchmark over an
	// erroring program measures the error paths, not the ones that matter.
	if res := driver.AnalyzeUnits(units); res.HasErrors() {
		b.Fatalf("benchmark program has errors: %v", res.Errors())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		driver.AnalyzeUnits(units)
	}
}

func BenchmarkAnalyze_Small(b *testing.B)  { benchmarkAnalyze(b, 1) }
func BenchmarkAnalyze_Medium(b *testing.B) { benchmarkAnalyze(b, 10) }
func BenchmarkAnalyze_Large(b *testing.B)  { benchmarkAnalyze(b, 40) }

// benchWideSource stresses what the *scans* in the typechecker key off, which the
// programs above do not: many data types with many constructors
// (findDataTypeByConstructor loops every type × every constructor per constructor
// expression) and many trait impls (resolveTraitMethod loops every impl per method
// call). If those linear scans matter anywhere, it is here.
func benchWideSource(types, uses int) string {
	var b strings.Builder
	for i := 0; i < types; i++ {
		fmt.Fprintf(&b, "data D%[1]d = A%[1]d | B%[1]d(i64) | C%[1]d(i64)\n", i)
		fmt.Fprintf(&b, "struct Rec%[1]d {\n  v: i64,\n}\n", i)
		fmt.Fprintf(&b, "trait T%[1]d { get%[1]d: (Self) -> i64 }\n", i)
		fmt.Fprintf(&b, "impl T%[1]d for Rec%[1]d { get%[1]d = (self) => self.v }\n", i)
	}
	for i := 0; i < uses; i++ {
		n := i % types
		fmt.Fprintf(&b, `
let use%[1]d = (k: i64) -> i64 => {
  let s = Rec%[2]d { v: k }
  match B%[2]d(k) {
    A%[2]d => 0,
    B%[2]d(v) => v + s.get%[2]d(),
    C%[2]d(v) => v,
  }
}
`, i, n)
	}
	b.WriteString("\nlet main = () -> u8 => u8(use0(1))\n")
	return b.String()
}

func benchmarkWide(b *testing.B, nTypes, uses int) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	app := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(app, []byte(benchWideSource(nTypes, uses)), 0o644); err != nil {
		b.Fatal(err)
	}
	units, diags := modules.Resolve(app, []string{repoRoot, dir}, modules.Options{Prelude: modules.PreludeModule})
	if len(units) == 0 {
		b.Fatalf("resolve produced no units: %v", diags)
	}
	if res := driver.AnalyzeUnits(units); res.HasErrors() {
		b.Fatalf("benchmark program has errors: %v", res.Errors()[:min(3, len(res.Errors()))])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		driver.AnalyzeUnits(units)
	}
}

func BenchmarkAnalyze_WideTypes(b *testing.B) { benchmarkWide(b, 60, 120) }
