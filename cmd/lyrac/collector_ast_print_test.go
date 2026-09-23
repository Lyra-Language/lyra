package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The bootstrap's second slice: the AST in Lyra, and a printer matching `pkg/printer`.
//
// **The point of matching it is the oracle.** `pkg/printer` dumps the Go AST by reflection
// and `pkg/analyzer/collector/tests/testdata` holds 245 golden files of those dumps. A
// Lyra printer producing the same bytes turns every one of them into a differential test
// against an independent implementation — the trick `std.temporal` used against Go's
// `time` and Python's `zoneinfo`. It is also why the Lyra AST mirrors the Go type and
// field names rather than choosing its own: an AST of its own design would leave the
// bootstrap checked only against itself.
//
// There is no collector yet (slice 3), so each case hand-builds the tree the Go collector
// would produce for that source and asserts the printed text **is the golden file, byte
// for byte** — the same file the Go collector is held to. What that pins is the format,
// which is the part slice 3 would otherwise have to debug at the same time as the walk.
func TestCollector_LyraPrinterMatchesTheGoldens(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)

	for _, c := range []struct {
		golden string
		// source is the Lyra the Go collector was given, for the reader; tree is that
		// same program's AST, hand-built.
		source string
		tree   string
	}{
		{
			// The empty-composite rule, and the case that makes it worth a test: every
			// field of the lambda is zero, so the *field holding it* disappears. A printer
			// that emitted `Value: { LambdaExpr { } }` would look right and match nothing.
			golden: "basic_function_declaration_without_params",
			source: "let foo = () => { }",
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: false, name: "foo",
    value: Some(LambdaExpr(Lambda { parameters: [] })),
  })] }`,
		},
		{
			// `IsPublic: true` sorts between BindingKind and Name — alphabetical order of
			// the *Go* field names, not declaration order.
			golden: "basic_function_declaration_with_visibility",
			source: "pub let foo = () => { }",
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: true, name: "foo",
    value: Some(LambdaExpr(Lambda { parameters: [] })),
  })] }`,
		},
		{
			// The same rule reached the other way: a string literal whose text is empty is
			// itself an empty composite, so `let blank = ""` prints no Value either.
			golden: "empty_string_literal_expr",
			source: `let blank = ""`,
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: false, name: "blank",
    value: Some(StringLiteralExpr(StringLiteral { value: "" })),
  })] }`,
		},
		{
			golden: "char_literal_expr_simple_escape",
			source: `let newline = '\n'`,
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: false, name: "newline",
    value: Some(CharacterLiteralExpr(CharacterLiteral { value: 10 })),
  })] }`,
		},
		{
			// Base is what the source spelled, not what the value is: 0x2A and 0b101010
			// are the same 42 under different bases, and only the base tells them apart.
			golden: "hexadecimal_integer_literal_expr",
			source: "let fourty_two = 0x2A",
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: false, name: "fourty_two",
    value: Some(IntegerLiteralExpr(IntegerLiteral { base: 16, value: 42 })),
  })] }`,
		},
		{
			golden: "binary_integer_literal_expr",
			source: "let fourty_two = 0b101010",
			tree: `Program { statements: [VarDeclStmt(VarDecl {
    binding_kind: "let", is_public: false, name: "fourty_two",
    value: Some(IntegerLiteralExpr(IntegerLiteral { base: 2, value: 42 })),
  })] }`,
		},
	} {
		t.Run(c.golden, func(t *testing.T) {
			probe := filepath.Join(t.TempDir(), "probe.lyra")
			program := `import examples.collector.ast.{
  Program, Stmt, VarDeclStmt, VarDecl, Expr, IntegerLiteralExpr, CharacterLiteralExpr,
  StringLiteralExpr, LambdaExpr, IntegerLiteral, CharacterLiteral, StringLiteral, Lambda,
}
import examples.collector.print.{ print_ast }

// ` + c.source + `
let main = () -> u8 => {
  print(print_ast(` + c.tree + `))
  0
}
`
			if err := os.WriteFile(probe, []byte(program), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(t.TempDir(), "probe")
			if _, stderr, code := captureRun(t, "build", "-o", bin, probe); code != 0 {
				t.Fatalf("building the probe exited %d\nstderr: %s", code, stderr)
			}
			got, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("running the probe: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(root, "pkg", "analyzer", "collector",
				"tests", "testdata", c.golden+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("the Lyra printer and %s.golden differ\n got:\n%s\nwant:\n%s",
					c.golden, got, want)
			}
		})
	}
}
