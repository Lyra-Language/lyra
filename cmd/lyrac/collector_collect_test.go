package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The bootstrap's third slice: `examples/collector` reads a `.lyra` file and prints its
// AST, and the **Go collector's own golden files are the assertion**. No hand-built trees
// any more — source in, AST out, compared byte for byte against what an independent
// implementation produced for the same source.
//
// Slices 1 and 2 are why a failure here means this walk: the accessors were checked against
// the grammar, and the printer was checked against these same goldens from trees built by
// hand. Debugging a walk and a format at once is what that ordering was for.
//
// **The subset is these cases, and the golden is the shared anchor.** The sources are
// copied from `pkg/analyzer/collector/tests`, which is duplication with a safety net: if
// one of those tests changes its source, its golden changes with it and the case here
// fails rather than quietly testing something else.
//
// 13 of the 238 goldens whose sources can be recovered match today. The number is not
// asserted — it would be a test about a count rather than about behaviour — but it is the
// honest measure of where the subset stands, and the slice grows by making more of them
// match.
func TestCollector_CollectsFromSourceIntoTheGoldens(t *testing.T) {
	bin := buildCollector(t)
	root := repoRoot(t)

	for _, c := range []struct{ golden, source string }{
		// A declaration whose value is a lambda with nothing in it: the lambda's fields
		// are all zero, so the printer drops the whole `Value:` line (slice 2's rule),
		// and the comment inside the block must not become a statement — a comment is a
		// named node, the compiler's hazard 16.
		{"basic_function_declaration_without_params", "\n\tlet foo = () => {\n\t\t// do stuff\n\t}"},
		{"basic_function_declaration_with_visibility", "\n\tpub let foo = () => {\n\t\t// do stuff\n\t}"},
		// The four bases, each read from the *child's kind* rather than re-derived from
		// the text, and `1_000_000` proving the underscore is ignored rather than parsed.
		{"simple_integer_literal_expr", "let one_million = 1_000_000"},
		{"hexadecimal_integer_literal_expr", "let fourty_two = 0x2A"},
		{"binary_integer_literal_expr", "let fourty_two = 0b101010"},
		{"octal_integer_literal_expr", "let fourty_two = 0o52"},
		// `'é'` is a literal rune, not an escape, whatever its golden is called — two
		// bytes in the source and one code point in the AST. It is the end-to-end check
		// that slice 1's byte-indexed `text` is right: a rune-indexed read would take the
		// wrong bytes and produce a different number here.
		{"simple_char_literal_expr", "let c = 'a'"},
		{"char_literal_expr_simple_escape", `let newline = '\n'`},
		{"char_literal_expr_unicode_escape", "let e_accent = 'é'"},
		// An empty string is an empty composite, so it too prints no `Value:` line.
		{"empty_string_literal_expr", `let blank = ""`},
		{"simple_string_literal_expr", `let greeting = "Hello, World!"`},
		{"string_literal_expr_with_simple_escapes", `let line = "Hello\n\tWorld\\!"`},
		{"string_literal_expr_with_hash_escape", `let s = "Phone \#: not an interp"`},
		// Slice 4: a lambda's real shape. Parameters with primitive annotations, a
		// declared return, an arithmetic operator and the identifiers around it — and
		// `ExpressionStmt`, for a lambda standing alone as a statement.
		{"lambda_expression_with_no_return_type", "\n\tlet double = (x: i64) => x * 2"},
		{"lambda_expression", "\n\t(a: i64, b: i64) -> i64 => a + b"},
		{"rune_type_annotation", "let f = (c: rune) -> rune => c"},
	} {
		t.Run(c.golden, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "in.lyra")
			if err := os.WriteFile(source, []byte(c.source), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(bin, source).Output()
			if err != nil {
				t.Fatalf("the collector exited non-zero on %q: %v", c.source, err)
			}
			want, err := os.ReadFile(filepath.Join(root, "pkg", "analyzer", "collector",
				"tests", "testdata", c.golden+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("collecting %q differs from %s.golden\n got:\n%s\nwant:\n%s",
					c.source, c.golden, got, want)
			}
		})
	}
}

// A construct outside the subset is **skipped, not guessed at**, so the output is short
// rather than wrong. That is what keeps a growing subset honest: the next slice fails by
// printing too little, which a diff shows plainly, rather than by inventing a node that
// happens to print.
func TestCollector_SkipsWhatItDoesNotYetCollect(t *testing.T) {
	bin := buildCollector(t)
	source := filepath.Join(t.TempDir(), "in.lyra")
	// A struct declaration and a trait are not in the subset; the two `let`s are.
	program := "struct Point { x: i64 }\nlet a = 1\ntrait Show2 { show2: (Self) -> string }\nlet b = 2\n"
	if err := os.WriteFile(source, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(bin, source).Output()
	if err != nil {
		t.Fatalf("the collector exited non-zero: %v", err)
	}
	want := "Program(2 statements) {\n\tStatements: {\n\t\tVarDeclStmt(a) {\n\t\t\tBindingKind: let\n" +
		"\t\t\tName: a\n\t\t\tValue: {\n\t\t\t\tIntegerLiteralExpr {\n\t\t\t\t\tBase: 10\n" +
		"\t\t\t\t\tValue: 1\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t\tVarDeclStmt(b) {\n\t\t\tBindingKind: let\n" +
		"\t\t\tName: b\n\t\t\tValue: {\n\t\t\t\tIntegerLiteralExpr {\n\t\t\t\t\tBase: 10\n" +
		"\t\t\t\t\tValue: 2\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n"
	if string(got) != want {
		t.Errorf("the two collectable statements should be all of it\n got:\n%s\nwant:\n%s", got, want)
	}
}

// **A comment at the top level is a named child of the root**, beside the declarations,
// so a walk over named children meets it and must not count it as a statement. That is the
// compiler's own hazard 16, checked here rather than assumed: the statement count in the
// header line is what would give it away, and it is the first number a reader trusts.
func TestCollector_ATopLevelCommentIsNotAStatement(t *testing.T) {
	bin := buildCollector(t)
	source := filepath.Join(t.TempDir(), "in.lyra")
	if err := os.WriteFile(source,
		[]byte("// a leading comment\nlet a = 1\n// a trailing one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(bin, source).Output()
	if err != nil {
		t.Fatalf("the collector exited non-zero: %v", err)
	}
	want := "Program(1 statements) {\n\tStatements: {\n\t\tVarDeclStmt(a) {\n\t\t\tBindingKind: let\n" +
		"\t\t\tName: a\n\t\t\tValue: {\n\t\t\t\tIntegerLiteralExpr {\n\t\t\t\t\tBase: 10\n" +
		"\t\t\t\t\tValue: 1\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n"
	if string(got) != want {
		t.Errorf("a comment became a statement\n got:\n%s\nwant:\n%s", got, want)
	}
}

// buildCollector compiles examples/collector, skipping when the tree-sitter runtime or the
// grammar is absent — the same three conditions the formatter's build skips on.
func buildCollector(t *testing.T) string {
	t.Helper()
	root := treeSitterEnv(t)
	return buildLinkedToTreeSitter(t, filepath.Join(root, "examples", "collector", "collector.lyra"))
}
