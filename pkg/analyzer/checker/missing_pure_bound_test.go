package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// missingPureBounds runs the same pipeline checkPurity does and returns the
// warning half — the names lyra-W018 reports, in source order.
func missingPureBounds(t *testing.T, source string) []string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	tc.Check(program)
	caps := captures.Analyze(program, symTable, tt)
	_, warnings := checker.CheckPurity(program, scopeTable, tt, tc.MethodTable(), caps)
	var names []string
	for _, w := range warnings {
		if w.Code != diag.CodeMissingPureBound {
			t.Fatalf("unexpected warning code %q from CheckPurity", w.Code)
		}
		if w.Severity != diag.SeverityWarning {
			t.Fatalf("lyra-W018 must be a warning, got severity %v", w.Severity)
		}
		// The reported name is the first quoted run in the message.
		parts := strings.SplitN(w.Message, `"`, 3)
		if len(parts) < 3 {
			t.Fatalf("message does not name a callable: %q", w.Message)
		}
		names = append(names, parts[1])
	}
	return names
}

func assertWarned(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("lyra-W018: want %v, got %v", want, got)
	}
}

// The base case: an effect-free top-level function with no bound is reported.
func TestMissingPureBound_EffectFreeFunction(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let double = (n: i64) -> i64 => n * 2
`), "double")
}

// Already annotated, so there is nothing to say.
func TestMissingPureBound_AlreadyPure_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let double = pure (n: i64) -> i64 => n * 2
`))
}

// A function with an observable effect is not a candidate — that is the whole
// premise. `println` is EffectOutput, which `pure` forbids.
func TestMissingPureBound_ImpureFunction_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let announce = (n: i64) -> void => println(n)
`))
}

// Transitive: a function whose only callee is impure inherits the effect and so
// is not a candidate, while its effect-free sibling still is.
func TestMissingPureBound_TransitiveImpurity(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let announce = (n: i64) -> void => println(n)
let shout = (n: i64) -> void => announce(n)
let quiet = (n: i64) -> i64 => n + 1
`), "quiet")
}

// `main` is called by nothing, so there is no caller for a later effect's blame
// to land on — the one thing the bound would buy. Never reported.
func TestMissingPureBound_MainExempt(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let main = () -> i64 => 1 + 1
`))
}

// Only *declarations* are reported. The fixpoint covers every lambda in the
// program, and an inline closure argument is an expression, not an interface.
func TestMissingPureBound_InlineClosure_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let run = (xs: []i64) -> void => println(xs.map((x) => x * 2).len())
`))
}

// A nested named helper is left out too: its only callers are in the body around
// it, where the reader is already looking.
func TestMissingPureBound_NestedDeclaration_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let outer = (n: i64) -> i64 => {
    let inner = (m: i64) -> i64 => m * 2
    inner(n) + println_free(n)
}
let println_free = (n: i64) -> i64 => n
`), "outer", "println_free")
}

// A trait-impl method is reported, at the impl.
func TestMissingPureBound_ImplMethod(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
struct Counter { n: i64 }
trait Bump { bump: (Self) -> i64 }
impl Bump for Counter { bump = (self) => self.n + 1 }
`), "Bump::bump")
}

// A method the *trait* declares `pure` is already bound by contract — every impl
// must satisfy it — so telling the impl to write it down would be advice to
// restate what the compiler already enforces.
func TestMissingPureBound_TraitDeclaredPure_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
struct Counter { n: i64 }
trait Bump { pure bump: (Self) -> i64 }
impl Bump for Counter { bump = (self) => self.n + 1 }
`))
}

// An impl method with an effect is not a candidate, same as a free function.
func TestMissingPureBound_ImpureImplMethod_Silent(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
struct Counter { n: i64 }
trait Yell { yell: (Self) -> i64 }
impl Yell for Counter { yell = (self) => { println("hi"); self.n } }
`))
}

// `pure` permits allocation, so an allocating-but-effect-free function is still a
// candidate — and the message says so, because a reader told their allocating
// function is effect-free will otherwise suspect the compiler of missing it.
func TestMissingPureBound_Allocating_MentionsAlloc(t *testing.T) {
	src := `
let pair = (a: i64, b: i64) -> []i64 => [a, b]
`
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(src))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	tc.Check(program)
	caps := captures.Analyze(program, symTable, tt)
	_, warnings := checker.CheckPurity(program, scopeTable, tt, tc.MethodTable(), caps)
	if len(warnings) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].Message, "`noalloc` concern") {
		t.Fatalf("allocating candidate should say `pure` permits it: %q", warnings[0].Message)
	}
}

// Only `pure` is reported. A print wrapper is `det`-legal (EffectOutput is
// determinism-safe) and non-allocating, and neither fact is warned about — the
// measured cost of doing so is a warning on every escape-sequence helper in a
// program. See CodeMissingPureBound.
func TestMissingPureBound_DetAndNoAllocNotReported(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let cursor_hide = () -> void => print("\e[?25l")
`))
}
