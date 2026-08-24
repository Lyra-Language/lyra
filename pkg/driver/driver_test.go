package driver

import (
	"strings"
	"testing"
	"time"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// TestAnalyze_CleanProgram: a well-typed program yields no errors and every
// table a later stage (codegen) needs is populated.
func TestAnalyze_CleanProgram(t *testing.T) {
	res := Analyze([]byte("let x: i64 = 42\nlet y = x + 1\n"))
	if res.HasErrors() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if res.Program == nil || res.SymbolTable == nil || res.ScopeTable == nil ||
		res.TypeTable == nil || res.MethodTable == nil {
		t.Fatalf("expected all tables populated, got program=%v sym=%v scope=%v type=%v method=%v",
			res.Program != nil, res.SymbolTable != nil, res.ScopeTable != nil,
			res.TypeTable != nil, res.MethodTable != nil)
	}
}

// TestAnalyze_TypeError surfaces a typechecker diagnostic through the unified
// diagnostic list.
func TestAnalyze_TypeError(t *testing.T) {
	res := Analyze([]byte(`let x: i64 = "hi"` + "\n"))
	if !res.HasErrors() {
		t.Fatal("expected a type error")
	}
	if !hasCode(res, diag.CodeTypeError) {
		t.Fatalf("expected %s, got: %v", diag.CodeTypeError, res.Diagnostics)
	}
	// The program is still returned (partial), so later passes/tooling can run.
	if res.Program == nil {
		t.Fatal("expected a (partial) program even with type errors")
	}
}

// TestAnalyze_SyntaxError surfaces a CST-level parse error as an error diagnostic.
func TestAnalyze_SyntaxError(t *testing.T) {
	res := Analyze([]byte("let x: i64 =\n"))
	if !res.HasErrors() {
		t.Fatalf("expected a syntax error, got: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "syntax error", "missing") {
		t.Fatalf("expected a parse diagnostic, got: %v", res.Diagnostics)
	}
}

func hasCode(res *Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func hasMessageContaining(res *Result, substrs ...string) bool {
	for _, d := range res.Diagnostics {
		for _, s := range substrs {
			if strings.Contains(d.Message, s) {
				return true
			}
		}
	}
	return false
}

// TestAnalyze_LargeU64Literal_TypeChecks: a literal that overflows int64 but
// fits u64 (18446744073709551615) is a valid u64 value — it type-checks in a
// u64 context. This is also the original crash reproducer (a nil literal node
// once panicked propagateLiteralType); it must analyze without panic or error.
func TestAnalyze_LargeU64Literal_TypeChecks(t *testing.T) {
	src := "let big = (n: u64) -> u64 => n\n" +
		"let main = () -> u8 => {\n  let x = big(18446744073709551615)\n  0\n}\n"
	res := Analyze([]byte(src)) // must not panic
	if res.HasErrors() {
		t.Fatalf("expected a large u64 literal to type-check, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_LargeU64Literal_RejectsSignedTarget: the same literal is NOT
// silently accepted as a signed value — assigning it to i64 is a clean error,
// never a wrong (negative) value.
func TestAnalyze_LargeU64Literal_RejectsSignedTarget(t *testing.T) {
	res := Analyze([]byte("let main = () -> u8 => {\n  let x: i64 = 18446744073709551615\n  0\n}\n"))
	if !res.HasErrors() {
		t.Fatal("expected a type error assigning a large u64 literal to i64")
	}
	if !hasMessageContaining(res, "cannot assign u64 to i64") {
		t.Fatalf("expected a u64-to-i64 assignability error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_HugeIntLiteral_NoPanic: a literal beyond even **128-bit** range is a
// clean error, not a crash.
//
// The boundary moved on 08/08, when literals grew past 64 bits: the value this test
// used to carry (10^26) is now an ordinary `i128` literal, so the case has to be
// re-picked to still be out of range. The property under test is unchanged — an
// unrepresentable literal must produce a diagnostic and a placeholder node rather than a
// typed nil that crashes a later pass.
func TestAnalyze_HugeIntLiteral_NoPanic(t *testing.T) {
	huge := strings.Repeat("9", 40) // ~10^40, past u128's 3.4e38
	res := Analyze([]byte("let x = " + huge + "\nlet main = () -> u8 => 0\n"))
	if !res.HasErrors() {
		t.Fatal("expected an out-of-range diagnostic")
	}
	if !hasMessageContaining(res, "too large to represent") {
		t.Fatalf("expected a too-large message, got: %v", res.Diagnostics)
	}
}

// The other side of that boundary: a literal that fits 128 bits is accepted, which is
// what the change was for.
func TestAnalyze_WideIntLiteralIsAccepted(t *testing.T) {
	res := Analyze([]byte("let x: i128 = 170141183460469231731687303715884105727\nlet main = () -> u8 => 0\n"))
	for _, d := range res.Errors() {
		t.Errorf("expected no error for an i128-max literal, got: %v", d)
	}
}

// TestAnalyze_OutOfRangeFloatLiteral_NoPanic: the float parse-failure path is
// the same typed-nil hazard as the integer one.
func TestAnalyze_OutOfRangeFloatLiteral_NoPanic(t *testing.T) {
	res := Analyze([]byte("let x = 1.0e999999\nlet main = () -> u8 => 0\n"))
	if !res.HasErrors() {
		t.Fatal("expected an out-of-range diagnostic")
	}
	if !hasMessageContaining(res, "out of range for f64") {
		t.Fatalf("expected a float-range message, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteOverflow: the value-range pass is wired into
// the pipeline, so a provable overflow (via a branch refinement — beyond what the
// literal range check catches) surfaces as an error through Analyze.
func TestAnalyze_RangeAnalysis_DefiniteOverflow(t *testing.T) {
	src := "let f = (x: i8) -> i8 => if x > 100 { x + 100 } else { 0 }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !res.HasErrors() {
		t.Fatal("expected a definite-overflow error")
	}
	if !hasMessageContaining(res, "always overflows i8") {
		t.Fatalf("expected an overflow message, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteDivideByZero: the range pass surfaces a
// definite divide-by-zero (lyra-E021) on a *variable* divisor proven zero by flow
// — beyond the typechecker's literal/folded-constant check.
func TestAnalyze_RangeAnalysis_DefiniteDivideByZero(t *testing.T) {
	src := "let f = (a: u8, b: u8) -> u8 => if b == 0 { a / b } else { a }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeDivideByZero) {
		t.Fatalf("expected a definite divide-by-zero (%s), got: %v", diag.CodeDivideByZero, res.Diagnostics)
	}
}

// A literal-zero divisor stays the typechecker's constant-fold check; the range
// pass deliberately does not also emit E021, so the bug is reported exactly once.
func TestAnalyze_LiteralDivideByZero_NoDuplicate(t *testing.T) {
	res := Analyze([]byte("let main = () -> u8 => 5 / 0\n"))
	if hasCode(res, diag.CodeDivideByZero) {
		t.Fatalf("a literal 5/0 should be the typechecker's error only, not a duplicate E021: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "division by zero") {
		t.Fatalf("expected the typechecker's division-by-zero error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteOutOfBounds: the range pass surfaces a
// definite out-of-bounds (lyra-E022) on an index proven past the end by flow —
// beyond the typechecker's constant-index range check.
func TestAnalyze_RangeAnalysis_DefiniteOutOfBounds(t *testing.T) {
	src := "let at = (xs: [3]u8, i: u8) -> u8 => if i >= 3 { xs[i] } else { 0 }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeIndexOutOfBounds) {
		t.Fatalf("expected a definite out-of-bounds (%s), got: %v", diag.CodeIndexOutOfBounds, res.Diagnostics)
	}
}

// A constant index stays the typechecker's own range check; the range pass (whose
// OOB diagnostic requires a non-singleton range) does not also emit E022, so the
// bug is reported exactly once.
func TestAnalyze_ConstantOutOfBounds_NoDuplicate(t *testing.T) {
	src := "let main = () -> u8 => {\n  let xs: [3]u8 = [1, 2, 3]\n  xs[5]\n}\n"
	res := Analyze([]byte(src))
	if hasCode(res, diag.CodeIndexOutOfBounds) {
		t.Fatalf("a constant xs[5] should be the typechecker's error only, not a duplicate E022: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "out of range for array") {
		t.Fatalf("expected the typechecker's out-of-range error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeConstraint_Violation: a constant outside a range-constrained
// newtype's declared range surfaces as lyra-E023 through the full pipeline.
func TestAnalyze_RangeConstraint_Violation(t *testing.T) {
	src := "newtype Percent = u8 where range(0..<=100)\n" +
		"let p: Percent = 150\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeRangeConstraintViolation) {
		t.Fatalf("expected a range-constraint violation (%s), got: %v", diag.CodeRangeConstraintViolation, res.Diagnostics)
	}
}

// TestAnalyze_RangeConstraint_FlowSensitive: a *non-constant* value proven (by the
// value-range analysis) entirely outside a range-constrained newtype's range is
// E023 — beyond the typechecker's constant-only check.
func TestAnalyze_RangeConstraint_FlowSensitive(t *testing.T) {
	src := "newtype Percent = u8 where range(0..<=100)\n" +
		"let f = (x: u8) -> u8 => if x > 100 { let p: Percent = x\n0 } else { 0 }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeRangeConstraintViolation) {
		t.Fatalf("expected a flow-sensitive range violation (%s), got: %v", diag.CodeRangeConstraintViolation, res.Diagnostics)
	}
}

// A literal violation is reported exactly once — the typechecker's constant check
// owns it, and the flow-sensitive pass (scoped to identifier values) does not also
// fire, so there's no duplicate.
func TestAnalyze_RangeConstraint_LiteralSingleReport(t *testing.T) {
	src := "newtype Percent = u8 where range(0..<=100)\n" +
		"let p: Percent = 150\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	n := 0
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeRangeConstraintViolation {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one E023 for a literal violation, got %d: %v", n, res.Diagnostics)
	}
}

// TestAnalyze_ForInRange_OutOfBounds: a for-in over a provably-non-empty numeric
// range bounds the loop counter, so an index past the end is a definite E022.
func TestAnalyze_ForInRange_OutOfBounds(t *testing.T) {
	src := "let f = (xs: [3]u8) -> u8 => {\n" +
		"  var s: u8 = 0\n" +
		"  for i in 5..<10 { s += xs[i] }\n" +
		"  s\n" +
		"}\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeIndexOutOfBounds) {
		t.Fatalf("expected a for-in out-of-bounds (%s), got: %v", diag.CodeIndexOutOfBounds, res.Diagnostics)
	}
}

// TestAnalyze_ForInLoopVariableResolves: the for-in loop variable now resolves in
// the body (it used to be an "undefined identifier" — no non-empty body was tested).
func TestAnalyze_ForInLoopVariableResolves(t *testing.T) {
	src := "let f = () -> u8 => {\n" +
		"  var t: u8 = 0\n" +
		"  for i in 0..<3 { t = i }\n" +
		"  t\n" +
		"}\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("the for-in loop variable should resolve in the body, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_WeakType_TypeChecks: `weak T` used to hard-error in the collector
// ("unknown type node kind: weak_type"), making it unusable. A recursive type
// whose back-edge is a `weak` field now analyzes cleanly through the whole
// pipeline — weak breaks the size cycle like `shared` (lyra-E014).
func TestAnalyze_WeakType_TypeChecks(t *testing.T) {
	src := "struct Node {\n  value: i64,\n  parent: weak Node,\n}\n" +
		"data List = Nil | Cons(i64, weak List)\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("expected a weak-broken recursive type to analyze cleanly, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_MalformedMemberCall_NoPanic guards a front-end crash: a call whose
// callee is a member/tuple-index expression with a missing property (`f.()`, a
// natural mid-edit state, and the shapes it error-recovers to) collected to a nil
// expression node, and inferFunctionCallExpr then dereferenced it (`.GetName()` on
// the nil callee) — panicking `lyrac check` instead of reporting a diagnostic. The
// collectors now return an inert placeholder and the typechecker guards a nil
// callee. Each input must Analyze to a normal (errored) result, never panic.
func TestAnalyze_MalformedMemberCall_NoPanic(t *testing.T) {
	for _, src := range []string{
		"let x = f.()\n",
		"let x = a.b.()\n",
		"let x = a.0.()\n",
		"let x = f[].()\n",
	} {
		res := Analyze([]byte(src)) // a panic here fails the test
		if res == nil || res.Program == nil {
			t.Errorf("%q: expected a (partial) program, got nil", src)
		}
		if !res.HasErrors() {
			t.Errorf("%q: expected an error diagnostic for the malformed call", src)
		}
	}
}

// TestAnalyze_ScreamingCaseConstructors: an all-caps / single-capital data
// constructor or named-tuple name lexes as a const_identifier, so its use in
// expression position collected to an IdentifierExpr / FunctionCallExpr and
// failed with a misleading "undefined identifier". The collector now reclassifies
// those into the DataConstructorExpr / named TupleLiteralExpr a PascalCase
// constructor produces, so the natural enum compiles; a same-named const shadows.
func TestAnalyze_ScreamingCaseConstructors(t *testing.T) {
	clean := func(name, src string) {
		if res := Analyze([]byte(src)); res.HasErrors() {
			t.Errorf("%s: expected no errors, got: %v", name, res.Diagnostics)
		}
	}
	// Nullary enum in expression + pattern position.
	clean("nullary enum", `data Dir = N | S | E | W
	 let toNum = (d: Dir) -> u8 => match d { N => 0, S => 1, E => 2, W => 3 }
	 let d: Dir = N`)
	// Applied constructor and named tuple.
	clean("applied + named tuple", `data Wrap = FOO(i64)
	 tuple POINT(i32, i32)
	 let w: Wrap = FOO(7)
	 let p: POINT = POINT(3, 4)`)
	// A const of the same name shadows the constructor (resolves to the i64 const).
	clean("const shadows constructor", `const RED = 5
	 data Color = RED | GREEN
	 let x: i64 = RED`)
	// With no shadowing const, the same name is the constructor.
	clean("constructor when unshadowed", `data Color = RED | GREEN
	 let c: Color = RED`)
	// **The node kinds the hand-copied rewriter had fallen behind on.** Reclassification
	// used to walk the AST through a copy of ast.walkExprChildren; the copy had no case
	// for TupleIndexExpr, BitwiseNotExpr or a deref assignment's *target*, so a
	// constructor anywhere beneath one of them kept its const-identifier spelling and
	// surfaced as `undefined identifier "N"` — a diagnostic naming a constructor the
	// program plainly declares. The pass now goes through ast.RewriteStmt, which pkg/ast's
	// exhaustiveness test holds to the canonical walker.
	clean("constructor under a bitwise not", `data Dir = N | S
	 let to_num = pure (d: Dir) -> i64 => match d { N => 0, S => 1 }
	 let a: i64 = ~to_num(N)`)
	clean("constructor under a tuple index", `data Dir = N | S
	 let to_num = pure (d: Dir) -> i64 => match d { N => 0, S => 1 }
	 let pair_of = pure (d: Dir) -> (i64, i64) => (to_num(d), 9)
	 let b: i64 = pair_of(S).0`)
	clean("constructor under a deref assignment target", `data Dir = N | S
	 let to_num = pure (d: Dir) -> i64 => match d { N => 0, S => 1 }
	 let pick = unsafe pure (d: Dir, a: ^mut i64, b: ^mut i64) -> ^mut i64 =>
	   if to_num(d) == 0 { a } else { b }
	 let main = () -> void => {
	   var x = 1
	   var y = 2
	   unsafe { pick(N, &mut x, &mut y)^ = 7 }
	 }`)
}

// TestAnalyze_DeepExpressionIsLinear is a coarse guard against the quadratic
// propagateLiteralType behavior fixed in typechecker.go: it re-descended a
// left-nested arithmetic chain at every level, so a chain of n operators cost
// O(n²). A 50k-operator chain (a stand-in for long flat `a + b + … + z`
// expressions or generated code) took >25s before the fix and ~0.3s after. The
// bound is deliberately generous — this asserts "not quadratic", not a precise
// time — so it stays robust on a loaded machine while still catching a
// reintroduced O(n²).
func TestAnalyze_DeepExpressionIsLinear(t *testing.T) {
	const n = 50000
	src := "let main = () -> i64 => 1" + strings.Repeat("+1", n)
	start := time.Now()
	res := Analyze([]byte(src))
	elapsed := time.Since(start)
	if res.HasErrors() {
		t.Fatalf("deep chain should type-check cleanly, got: %v", res.Diagnostics)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("analyzing a %d-operator chain took %v (>5s) — propagateLiteralType may be quadratic again", n, elapsed)
	}
}

// TestAnalyze_ForwardReferencedConst_IsTyped: a function body may reference a
// top-level `const` declared *later* (consts are order-independent, checked before
// bodies). The use must carry a recorded type — a plain "no errors" held even
// before the fix, but the use was left untyped, so a backend op needing its type
// (a conversion) errored "type not found". This asserts the type is recorded.
func TestAnalyze_ForwardReferencedConst_IsTyped(t *testing.T) {
	src := "let f = () -> u8 => u8(LIMIT)\nconst LIMIT = 77\nlet main = () -> u8 => f()\n"
	res := Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("a forward-referenced const should type-check, got: %v", res.Diagnostics)
	}
	var use ast.Expression
	for _, s := range res.Program.Statements {
		if st, ok := s.(ast.Statement); ok {
			ast.WalkStmt(st, nil, func(e ast.Expression) bool {
				if id, ok := e.(*ast.IdentifierExpr); ok && id.Name == "LIMIT" && use == nil {
					use = e
				}
				return true
			})
		}
	}
	if use == nil {
		t.Fatal("did not find the LIMIT identifier use")
	}
	if ty, ok := res.TypeTable.Get(use); !ok || ty == nil {
		t.Fatal("the forward-referenced const use has no recorded type (the pre-fix bug)")
	}
}

// TestAnalyze_DefinitionCycleDoesNotCrash: the whole pipeline survives a binding whose
// type depends on itself, and reports it.
//
// This is the shape that mattered, and why the guard is in the typechecker rather than in
// `lyrac`: `Analyze` is what **`lyra-lsp` runs on every keystroke**, so the stack overflow
// did not merely fail a build — it killed the language server, in the middle of typing,
// which is the only time a half-written cycle exists. A `lyrac` user at least saw a
// traceback; an editor user saw completions and diagnostics stop, with no indication why.
//
// Asserted through `Analyze` rather than the typechecker alone because that is the
// entry point with the property worth protecting: it must always return, for any input.
func TestAnalyze_DefinitionCycleDoesNotCrash(t *testing.T) {
	for _, src := range []string{
		"let f = f(1)\n",
		"let a = b(1)\nlet b = a(1)\n",
		"let a = b(1)\nlet b = c(1)\nlet c = a(1)\n",
	} {
		res := Analyze([]byte(src))
		if !res.HasErrors() {
			t.Errorf("%q: expected a diagnostic for the cycle, got none", src)
			continue
		}
		found := false
		for _, d := range res.Diagnostics {
			if strings.Contains(d.Message, "depends on itself") {
				found = true
			}
			if strings.Contains(d.Message, "%!") {
				t.Errorf("%q: a format verb leaked into a diagnostic: %s", src, d.Message)
			}
		}
		if !found {
			t.Errorf("%q: no diagnostic named the cycle; got %v", src, res.Diagnostics)
		}
	}
}
