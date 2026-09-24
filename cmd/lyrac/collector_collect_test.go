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
// 163 of the 238 goldens whose sources can be recovered match today. The number is not
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
		// **A second harvest pass (09/24)**, run because the first one's lesson is that a
		// finished node still hides fields: after the literal family, two goldens used
		// only kinds already built and still differed. A `const` is the same declaration
		// under another node kind — the Go collector routes `const_declaration` into the
		// same collector and the `keyword` field carries the word — and a call can carry
		// type arguments at its callee, the field `TupleLiteral` already had.
		{"variable_declaration_with_const", "const PI: f64 = 3.14159"},
		{"function_that_accepts_lambda_as_parameter",
			"\n\tlet nums = [1, 2, 3]\n\tlet doubled = map::<i64, i64>(nums, (x) => 2 * x)"},
		// **Struct instances (09/24)**, the cheapest node left by the measurement and 101
		// to 111 — every stored golden that builds a struct. Four shapes behind one node:
		// named fields, the positional shorthand, a record update, and the anonymous
		// literal, which is a *second node kind* over the same fields.
		{"struct_instance", "let point = Point { x: 1, y: 2 }"},
		{"struct_instance_shorthand", "let point = Point { 1, 2 }"},
		{"struct_instance_with_generic_arguments", "let point = Point::<i32> { x: 1, y: 2 }"},
		{"anonymous_struct_instance", "let point = { x: 1, y: 2 }"},
		{"record_update_single_field", "Player { existingPlayer | health: newHealth }"},
		{"record_update_multiple_fields",
			"Player { existingPlayer | health: newHealth, stamina: 100 }"},
		{"record_update_with_expression_fields",
			"let p = { player | health: player.health - 10, x: player.x + dx }"},
		// `...xs` is its own node, met first as a field's value. One spread alone is a
		// parse error (`Point { ...base }`), so the shorthand case carries two.
		{"struct_with_generics_and_spread_field_values", "let p = Point::<i32> { x: ...xs }"},
		{"struct_shorthand_with_multiple_spread_values", "let merged = Point { ...base, ...extra }"},
		{"array_repeat_initialization_with_structs", "let arr = [Vec3 { x: 0, y: 0, z: 0 }; 100]"},
		// **Control flow (09/24)**, the structural slice: `if`, the two loops, `match`, and
		// the pattern kinds `match` travels with. 111 to 163, the largest single move, and
		// most of it is patterns — a destructuring `let` and a match arm take the same
		// pattern node, so collecting one collected the other.
		//
		// `else if` is an `else` holding another `IfExpr`, not a flattened ladder, and the
		// fields print alphabetically — `Condition`, `Else`, `Then` — so reading a golden
		// top to bottom is not reading the program.
		{"simple_if_block_expr", "\n\tif x == 3 {\n\t\tprintln(\"x is 3\")\n\t}"},
		{"if_block_expr_with_else", "\n\tif x == 3 {\n\t\tprintln(\"x is 3\")\n\t} else {\n\t\tprintln(\"x is not 3\")\n\t}"},
		{"if_block_expr_with_else_if", "\n\tif x == 3 {\n\t\tprintln(\"x is 3\")\n\t} else if x == 4 {\n\t\tprintln(\"x is 4\")\n\t} else {\n\t\tprintln(\"x is not 3 or 4\")\n\t}"},
		{"nested_if_block_expr", "\n\tif x == 0 {\n\t\tif y == 0 {\n\t\t\tprintln(\"At Origin\")\n\t\t} else {\n\t\t\tprintln(\"On Vertical Axis\")\n\t\t}\n\t} else {\n\t\tif y == 0 {\n\t\t\tprintln(\"On Horizontal Axis\")\n\t\t} else {\n\t\t\tprintln(\"At ${x},${y}\")\n\t\t}\n\t}"},
		{"interpolated_string_expr_nested", "let greeting = \"Hello, ${if is_male { \"Mr. ${last_name}\" } else { \"Mrs. ${last_name}\" }}!\""},
		// A `for/in` loop's parts are children rather than fields, and which binding is
		// which comes from the child's kind: `Key` is the first name and `Value` the
		// second. `break item` records `item` as a **label**, not as a value — the grammar
		// reads a bare name that way, and only `break outer i + j` has both.
		{"for_in_loop_with_identifier", "\n\tfor item in my_collection {\n\t\tprintln(item)\n\t}"},
		{"for_in_loop_with_key_and_value", "\n\tfor item, idx in [1, 2, 3] {\n\t\tprintln(item)\n\t}"},
		{"for_in_loop_with_range", "\n\tfor i in 1..<=3 {\n\t\tprintln(\"i == ${i}\")\n\t}"},
		{"for_in_loop_with_tuple", "\n\tfor i in (1, \"2\", 3) {\n\t\tprintln(\"i == ${i}\")\n\t}"},
		{"for_in_loop_with_postfix_expr", "\n\tfor item in get_array() {\n\t\tprintln(item)\n\t}"},
		{"for_in_loop_as_expression", "\n\tlet found = for item in items {\n\t\tif item > 10 {\n\t\t\tbreak item\n\t\t}\n\t}"},
		{"labeled_for_in_loop", "\n\touter: for item in [1, 2, 3] {\n\t\tbreak outer\n\t}"},
		{"labeled_for_loop_break_with_value", "\n\tlet result = outer: for i in 0..<=10 {\n\t\tfor j in 0..<=10 {\n\t\t\tbreak outer i + j\n\t\t}\n\t}"},
		// The C-style loop, in all five combinations its three optional header parts
		// make. `Init` is a declaration held by a field, the one place in this AST a
		// statement is not in a list — and it is the same `declaration` node under another
		// kind, so it reuses the declaration collector.
		{"infinite_for_loop", "\n\tfor {\n\t\tprintln(\"All work and no play makes Homer something something...\")\n\t}"},
		{"for_loop_with_condition", "\n\tvar i = 0\n\tfor i < 10 {\n\t\tprintln(\"i == ${i}\")\n\t\ti += 1\n\t}"},
		{"for_loop_with_condition_and_post", "\n\tvar i = 0\n\tfor i < 10; i += 1 {\n\t\tprintln(\"i == ${i}\")\n\t}"},
		{"for_loop_with_init_and_condition", "\n\tfor var i = 0; i < 10 {\n\t\tprintln(\"i == ${i}\")\n\t\ti += 1\n\t}"},
		{"for_loop_with_init_and_condition_and_post", "\n\tfor var i = 0; i < 10; i += 1 {\n\t\tprintln(\"i == ${i}\")\n\t}"},
		{"for_loop_as_expression", "\n\tlet result = for var i = 0; i < 100; i += 1 {\n\t\tif i * i > 50 {\n\t\t\tbreak i * i\n\t\t}\n\t}"},
		{"for_loop_with_break", "\n\tfor var i = 0; i < 10; i += 1 {\n\t\tbreak\n\t}"},
		{"for_loop_with_continue", "\n\tfor var i = 0; i < 10; i += 1 {\n\t\tif i % 2 == 0 {\n\t\t\tcontinue\n\t\t}\n\t\tprintln(\"i == ${i}\")\n\t}"},
		{"labeled_for_loop_with_break", "\n\touter: for var i = 0; i < 10; i += 1 {\n\t\tfor var y = 0; y < 10; y += 1 {\n\t\t\tif y == 5 {\n\t\t\t\tbreak outer\n\t\t\t}\n\t\t\tprintln(\"y == ${y}\")\n\t\t}\n\t}"},
		{"labeled_for_loop_with_continue", "\n\touter: for var i = 0; i < 10; i += 1 {\n\t\tfor var y = 0; y < 10; y += 1 {\n\t\t\tif y % 2 == 0 {\n\t\t\t\tcontinue outer\n\t\t\t}\n\t\t\tprintln(\"y == ${y}\")\n\t\t}\n\t}"},
		// A compound assignment is an expression with its own node kind, so `+=` is never
		// reached by the arithmetic operator table. A reassignment's name has **no field**
		// — it is the first named child, which is how the Go collector reads it, and
		// asking for `name` made the statement disappear.
		{"math_assign_op_expr", "\n\tvar x = 5\n\tx += 3\n\tx -= 1\n\tx *= 3\n\tx /= 3\n\tx %= 3\n\tx %%= 3\n\t"},
		{"stmt_var_reassignment", "\n\tvar x = 1\n\tx = 2"},
		{"variable_declaration_without_value", "\n\tvar the_answer\n\tthe_answer = 42"},
		// `match`: arms are children, the scrutinee is a field, and a guard is a node of
		// its own (`GuardExpr`) rather than a bare condition. A character pattern keeps
		// its **decoded code point** where every other literal pattern keeps raw source
		// text — `"foo"` prints with its quotes.
		{"match_expression", "\n\tlet bar = match foo {\n\t\tSome 42 => \"The Answer!\",\n\t\tSome _ => \"Just some number\",\n\t\tNone => \"huh?\",\n\t}"},
		{"match_expression_with_blocks", "\n\tmatch foo {\n\t\t[a] => {\n\t\t\tprintln(\"An array with one element\")\n\t\t},\n\t\t[a, b] => {\n\t\t\tprintln(\"An array with two elements\")\n\t\t},\n\t\t_ => {\n\t\t\tprintln(\"A wildcard match\")\n\t\t},\n\t}"},
		{"match_expression_with_guards", "\n\tmatch foo {\n\t\tSome x if x > 0 && x < 10 => print(\"1-9\"),\n\t\tSome x if x >= 10 && x < 100 => print(\"10-99\"),\n\t\tNone => print(\"No number!\"),\n\t}"},
		{"match_expression_with_range_patterns", "\n\tmatch foo {\n\t\t0..<=9 => print(\"one digit\"),\n\t\t42 => print(\"The Answer!\"),\n\t\t10..<=99 => print(\"two digits\"),\n\t\t_ => print(\"lots of digits!\"),\n\t}"},
		{"match_expression_with_structs_returned", "\n\tlet foo = match bar {\n\t\t\"foo\" => { a: \"b\" },\n\t\t\"bar\" => { b: \"a\" },\n\t\t_ => { c: \"d\" },\n\t}"},
		// The pattern kinds, met through a destructuring `let` rather than a match: the
		// two positions take the same node, which is why this slice scored twice. A rest
		// element is legal at either end of a tuple pattern and in its middle.
		{"destructuring_array", `let [a, b, c] = some_array`},
		{"destructuring_array_with_var", `var [a, b, c] = some_array`},
		{"destructuring_array_with_rest_at_end", `let [a, b, c, ...tail] = some_array`},
		{"destructuring_tuple", `let (x, y, z) = ThreeInts(1, 2, 3)`},
		{"destructuring_tuple_from_identifier", `let (x, y, z) = some_tuple`},
		{"destructuring_tuple_with_rest_at_beginning", `let (...rest, x, y, z) = some_tuple`},
		{"destructuring_tuple_with_rest_in_middle", `let (x, y, ...rest, z) = some_tuple`},
		{"destructuring_tuple_with_rest_at_end", `let (x, y, z, ...rest) = some_tuple`},
		{"destructuring_struct", `let {a, b, c} = some_struct`},
		{"destructuring_struct_with_rename", `let {a: foo, b: bar, c: baz} = some_struct`},
		{"destructuring_struct_with_rest", `let {a, b, ...rest} = some_struct`},
		{"destructuring_struct_with_sub_patterns", `let {a: [x, y, z], b: (a, b), c: {d, e, f}} = some_struct`},
		{"destructuring_data_with_array_pattern", `let Some [x, y, z] = some_data`},
		{"destructuring_data_with_tuple_pattern", `let Some (x, y) = some_data`},
		{"destructuring_data_with_struct_pattern", `let Some {x, y, z} = some_data`},
		{"destructuring_data_with_data_pattern", `let Some AnotherData(x, y) = some_data`},
		// A struct pattern has **four shapes of field**: a bare name with no pattern at
		// all, `y: 0` with one, `a: foo` which renames (a `new_name`, not a pattern), and
		// `...rest`, which becomes a field literally named `...`.
		{"binding_pattern_in_destructuring", `let all @ [a, b, ...rest] = some_array`},
		{"binding_pattern_in_match", "\n\tmatch xs {\n\t\tall @ [head, ...tail] => print(all),\n\t\t_ => print(\"no match\"),\n\t}"},
		{"binding_pattern_nested", "\n\tmatch point {\n\t\t{ x: px @ 0..<=100, y } => print(px),\n\t\t_ => print(\"out of range\"),\n\t}"},
		{"complex_patterns_destructuring", "\n\tlet {\n\t\tfoo: [a, b, ...rest],\n\t\tbar: (c, Some d),\n\t\tbaz: {e, f, ...rest}\n\t} = some_complex_struct"},
		{"complex_patterns_pattern_matching", "\n\tmatch some_complex_struct {\n\t\t{ foo: [a, b, ...rest] } => print(\"An array with three elements\"),\n\t\t{ bar: (c, Some d) } => print(\"A tuple with two elements\"),\n\t\t{ baz: {e, f, ...rest} } => print(\"A struct with three elements\"),\n\t\t_ => print(\"No match\"),\n\t}"},
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
