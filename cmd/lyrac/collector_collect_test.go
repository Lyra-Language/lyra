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
// 41 of the 238 goldens whose sources can be recovered match today. The number is not
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
		// Slice 5: the postfix family. `pair.0.1` is the one that pins the recursion —
		// a tuple index whose object is itself one.
		{"postfix_property_access", "let name = person.name"},
		{"postfix_array_indexing", "let first = array[0]"},
		{"postfix_tuple_index", "let x = pair.0"},
		{"postfix_nested_tuple_index", "let x = pair.0.1"},
		// Slice 6. The comparison operators are one node kind with the symbol recorded,
		// so all six are one code path checked six ways; `a < b && c > d` is the nesting.
		{"expr_boolean_binary_eq", "let x = a == b"},
		{"expr_boolean_binary_neq", "let x = a != b"},
		{"expr_boolean_binary_lt", "let x = a < b"},
		{"expr_boolean_binary_lte", "let x = a <= b"},
		{"expr_boolean_binary_gt", "let x = a > b"},
		{"expr_boolean_binary_gte", "let x = a >= b"},
		{"expr_boolean_binary_chained", "let x = a < b && c > d"},
		{"expr_negation", "let x = -42"},
		{"expr_negation_of_expr", "let x = -foo()"},
		{"postfix_try_expression", `let file = open_file("foo.txt")?`},
		// A range's bounds are each wrapped by the grammar, and its parts are each
		// optional: no step here, a step there, and expressions rather than literals in
		// the third — where taking the wrapper instead of its child would print nothing.
		{"simple_range_expression", "0..<10"},
		{"range_expression_with_step", "0..<=10:2"},
		{"range_expression_with_start_expression", "start*2..<=end-1"},
		// Slice 7: type declarations. The three forms, each with and without the parts
		// that are optional — visibility, generic parameters, a field default.
		{"tuple_type_declaration", "tuple Point2D(f64, f64)"},
		{"tuple_type_declaration_with_visibility", "pub tuple Point2D(f64, f64)"},
		{"tuple_type_declaration_with_generic_parameters", "tuple Point2D<t>(t, t)"},
		{"struct_no_derive", "\n\t\tstruct Point {\n\t\t\tx: f32,\n\t\t\ty: f32,\n\t\t}\n\t"},
		{"basic_struct_type_declaration", "\n\t\tpub struct Point {\n\t\t\tx: i64,\n\t\t\ty: i64 = 0,\n\t\t}\n\t"},
		{"basic_data_type", "pub data ColorName = Red | Green | Blue"},
		{"struct_type_declaration_with_generic_parameters", "\n\t\tpub struct Point<t> {\n\t\t\tx: t,\n\t\t\ty: t,\n\t\t}\n\t"},
		// `Some t` is the *juxtaposed* payload — one type directly. Its parenthesised
		// sibling `Some(t)` is a different tree, not a different spelling, which no
		// stored golden covers and the differential test does.
		{"data_type_with_generic_parameter", "pub data Maybe<t> = Nil | Some t"},
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
	// A trait and an impl are not in the subset; the two `let`s are. The struct that used
	// to stand here was collected by slice 7, which is the right way for this test to
	// fail — it names the boundary, so it moves when the boundary does.
	program := "trait Show2 { show2: (Self) -> string }\nlet a = 1\n" +
		"impl Show2 for i64 { show2 = (self) => \"n\" }\nlet b = 2\n"
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
