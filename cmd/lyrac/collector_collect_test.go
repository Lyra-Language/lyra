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
// 99 of the 238 goldens whose sources can be recovered match today. The number is not
// asserted — it would be a test about a count rather than about behaviour — but it is the
// honest measure of where the subset stands, and the slice grows by making more of them
// match.
func TestCollector_CollectsFromSourceIntoTheGoldens(t *testing.T) {
	bin := buildCollector(t)
	root := repoRoot(t)

	// A Lyra raw string is backtick-delimited, and a backtick cannot appear inside a Go
	// raw string — so the cases exercising them are concatenated in.
	const bt = "\x60"

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
		// A `newtype`. Its `where` constraints are not collected yet, so the goldens that
		// carry them still differ — these two are the forms without.
		{"basic_constrained_type_without_constraints", "newtype Angle = f64"},
		{"parameterized_constrained_type", "newtype Point<t> = Tuple"},
		// A declaration whose left side matches rather than names. The same grammar node
		// as a `let`, carrying a `pattern` field instead of a `name`.
		{"destructuring_simple_data", "let Some x = some_data"},
		// Interpolation, where a text run is itself a StringLiteralExpr segment — so the
		// segment list is homogeneous. The escaped-hash case is the one that checks a
		// text run's escapes are decoded like any other string's.
		{"interpolated_string_expr_only", `let v = "${name}"`},
		{"interpolated_string_expr_simple", `let greeting = "Hello, ${name}!"`},
		{"interpolated_string_expr_multiple_segments", `let point = "At (${x}, ${y})"`},
		{"interpolated_string_expr_with_arithmetic", `let msg = "Total: ${price * qty}"`},
		{"interpolated_string_expr_complex_expression", `let msg = "Hello, ${employee.get_name()}!"`},
		{"interpolated_string_expr_escaped_hash", `let s = "Phone \#: ${phone_num}"`},
		// **The harvest (09/24).** None of these was blocked by a missing *node kind* —
		// every one uses nodes slices 2 to 8 already built. What stopped them was a field
		// on a node already collected, which is the class of gap a curated golden set
		// hides: the subset looks complete node by node while a flag on one of them is
		// never read. Measuring which goldens use only supported kinds, and then running
		// them, is what turned 50 into 75.
		//
		// The first three needed no code at all — they were failing against goldens that
		// had been hand-edited into shapes the Go printer does not produce, which the
		// whitespace-insensitive comparison in `pkg/analyzer/collector/tests` had been
		// absorbing. See `cmpOutput`.
		{"expr_boolean_binary_and", `let x = a && b`},
		{"expr_boolean_binary_or", `let x = a || b`},
		{"interpolated_string_expr_leading", `let greeting = "${name} is here"`},
		{"basic_function_declaration_with_params", "\n\tlet add(a: Int, b: String) => a + b"},
		// The numeric escapes, in a char and in a string. `\0` is the one that found the
		// printer's emptiness rule: `pkg/printer` never treats an integer field as zero,
		// so `Value: 0` prints and `CharacterLiteralExpr` is never an empty composite —
		// where this printer had been dropping it. No other golden can tell the two rules
		// apart.
		{"char_literal_expr_hex_escape", `let a = '\x41'`},
		{"char_literal_expr_large_unicode_escape", `let emoji = '\U0001F600'`},
		{"char_literal_expr_nul_escape", `let nul = '\0'`},
		{"char_literal_expr_unicode_directly", "let e_accent = 'é'"},
		{"string_literal_expr_with_numeric_escapes", `let s = "A=\x41 e=é A'=\o101"`},
		// Raw strings, where the point is that nothing inside is an escape: the first
		// carries a `\n` that stays two characters and a `${…}` that stays text, and the
		// second is delimited by `##` so the content may hold a backtick.
		{"empty_raw_string_literal_expr", "let s = " + bt + bt},
		{"raw_string_literal_expr", "let s = " + bt + `C:\new ${name}` + bt},
		{"raw_string_literal_expr_with_hashes",
			"let s = ##" + bt + "a " + bt + " and " + bt + "# inside" + bt + "##"},
		// Optional chaining is a **node kind of its own** (`optional_member_expr`), not a
		// flag on `.`, and a const name likewise (`const_identifier`) — so neither is read
		// off the text. The chains are the ones that compose them with calls and `?`.
		{"postfix_optional_property_access", "let name = person?.name"},
		{"postfix_optional_array_indexing", "let first = array?[0]"},
		{"postfix_chained_optional_index_access", "let value = matrix?[0]?[1]"},
		{"postfix_chained_optional_member_calls", "let data = open_file(path)?.read()?.parse()?"},
		{"postfix_complex_chain", `let foo = struct.function("arg")?[0].property`},
		{"postfix_member_const_property_access", "let max = limits.MAX"},
		// The declaration's own fields: modifiers, generic parameters and a written type
		// annotation. **Both spellings put `pure` in the same place in the AST** — written
		// before the name it is a field of the declaration, written after the `=` it is a
		// field of the lambda — so these two are the juxtaposed form deliberately.
		{"pure_function_declaration", "\n\tlet pure add(a: i64, b: i64) -> i64 => a + b"},
		{"pure_async_function_declaration", "\n\tlet pure async compute(n: i64) -> i64 => n * 2"},
		{"function_with_generic_params", "\n\tlet sum<n>(a: n, b: n) -> n => a + b"},
		{"function_declaration_with_default_parameter_values",
			"\n\tlet greet = (name: string, prefix: string = \"Hello\") -> string => \"${prefix} ${name}\""},
		{"variable_declaration_with_var", "var the_answer: i64 = 42"},
		{"expr_negation_narrow_min", "let x: i8 = -128"},
		// A tuple type written inside another one, which is the same anonymous tuple a
		// packed data payload carries and prints under the same `?` name.
		{"tuple_type_declaration_with_nested_tuple", "tuple RGBA((i64, i64, i64), f64)"},
		// **The literal family (09/24).** Five small nodes on the tree slices 4 to 6
		// built, and the best-scoring slice so far: 75 to 99. The measurement predicted
		// it — these were the sole blocker of more goldens than anything else, exactly as
		// the postfix family was in slice 5.
		//
		// A float travels as a **number**, not as its digits: `0.03141592e2`,
		// `314.1592e-2` and `3.141592` are one value and collect identically. That works
		// because Lyra's float formatting is Go's `%v` — both render the shortest decimal
		// that reads back as the same double — and because the parse is exact in this
		// range (`float_value`).
		{"simple_float_literal_expr", "let pi = 3.14159"},
		{"float_literal_expr_with_exponent", "let pi = 0.03141592e2"},
		{"float_literal_expr_with_positive_exponent", "let pi = 0.03141592e+2"},
		{"float_literal_expr_with_negative_exponent", "let pi = 314.1592e-2"},
		{"float_literal_expr_with_underscores", "let pi = 3.141_59"},
		{"variable_declaration_with_let", "let pi: f64 = 3.14159"},
		{"variable_declaration_without_type_annotation", "let pi = 3.14159"},
		// `++` is a node kind of its own, so nothing here classifies an operator — the
		// third node sharing `BinaryOp`'s shape, after the arithmetic and boolean ones.
		{"expr_string_concat_literals", `"hello" ++ " world"`},
		{"expr_string_concat_variables", "greeting ++ name"},
		{"expr_string_concat", `let greeting = "Hello, " ++ name`},
		{"expr_string_concat_in_let_declaration", `let s = "Hello, " ++ name`},
		{"expr_string_concat_chained", `let s = "a" ++ "b" ++ "c"`},
		{"expr_string_concat_function_call_operands", "foo() ++ bar()"},
		// A tuple literal is one node named or not: `(1, 2)` takes `?`, the same
		// placeholder an unnamed tuple *type* carries, and `Point(1, 2)` is not a call.
		{"simple_anonymous_tuple_literal", "let point: (i64, i64) = (1, 2)"},
		{"simple_anonymous_tuple_literal_assigned_to_named_tuple", "let point: Point = (1, 2)"},
		{"simple_named_tuple_literal", "let point = Point(1, 2)"},
		{"simple_named_tuple_literal_with_generic_parameters", "let point = Point::<i32>(1, 2)"},
		{"simple_named_tuple_literal_assigned_to_named_tuple",
			"\ntuple Point(i32, i32)\nlet point: Point = Point(1, 2)\n\t"},
		{"variable_declaration_with_tuple_type", "let the_answer: (i64, i64) = (42, 13)"},
		// **The opener is the flavor**: `#[` is a fixed `[N]T` and `[` a dynamic `[]T`,
		// exposed as a `fixed` field so nothing infers it from the text. Both spellings of
		// both nodes are here for that reason.
		{"simple_static_array_literal", "let arr = #[1, 2, 3]"},
		{"array_literal_with_expression", "let arr = [1, 2 * PI, 3]"},
		{"array_repeat_initialization", "let arr = #[0; 8]"},
		{"array_repeat_initialization_with_compile_time_constant_count", "let arr = #[0; SIZE]"},
		{"dynamic_array_repeat_initialization", "let arr = [0; n]"},
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
