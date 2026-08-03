package typechecker_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// UFCS — `x.f(y)` resolving to a free function `f(x, y)` that opted in by naming its
// first parameter `self`.
//
// The ladder these pin is field → trait method → UFCS → builtin. Precedence is asserted
// through *return types* rather than through which call succeeds, since both candidates
// would otherwise accept the same call and the test would pass either way.

func TestUFCS_ResolvesFreeFunction(t *testing.T) {
	res := parseCollectAndCheck(t, `
let scaled = pure (self: i64, by: i64) -> i64 => self * by
let x = 21
let y: i64 = x.scaled(2)`, false)
	assertNoErrors(t, res)
}

// The receiver participates in solving the callee's type variables, exactly as it would
// as a written first argument — this is what the desugar buys.
func TestUFCS_GenericReceiver(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some t

let unwrap<t> = pure (self: Maybe<t>, fallback: t) -> t => match self {
  Some v => v,
  None => fallback,
}

let m: Maybe<i64> = Some 7
let v: i64 = m.unwrap(0)`, false)
	assertNoErrors(t, res)
}

// A struct field holding a function beats a same-named UFCS candidate. The two return
// different types, so the arm that wins is the one the annotation rejects.
func TestUFCS_StructFieldWins(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Holder { run: (i64) -> i64 }

let run = pure (self: Holder, n: i64) -> string => "ufcs"

let h = Holder { run: (n: i64) -> i64 => n }
let s: string = h.run(5)`, false)
	if len(res.errors) == 0 {
		t.Fatal("the field should have won, making the string annotation an error")
	}
	if !strings.Contains(res.errors[0].Message, "string") {
		t.Errorf("expected a string/i64 mismatch from the field's signature, got %q", res.errors[0].Message)
	}
}

// A real trait impl beats a UFCS candidate, by the same reading.
func TestUFCS_TraitMethodWins(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Counter { n: i64 }

trait Peek { peek: (Self) -> i64 }

impl Peek for Counter { peek = (self) => self.n }

let peek = pure (self: Counter) -> string => "ufcs"

let c = Counter { n: 1 }
let s: string = c.peek()`, false)
	if len(res.errors) == 0 {
		t.Fatal("the trait impl should have won, making the string annotation an error")
	}
}

// Opting in is the whole rule: a free function that never named a parameter `self` is
// call-only, and the message says so rather than leaving the reader hunting for a method.
func TestUFCS_RequiresSelfParameter(t *testing.T) {
	res := parseCollectAndCheck(t, `
let scaled = pure (n: i64, by: i64) -> i64 => n * by
let x = 21
let y = x.scaled(2)`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error: scaled did not opt in")
	}
	msg := res.errors[0].Message
	if !strings.Contains(msg, `has no method "scaled"`) || !strings.Contains(msg, "`self`") {
		t.Errorf("expected the no-method error to hint at the `self` opt-in, got %q", msg)
	}
}

// An `own` receiver is refused rather than quietly moved: the receiver syntax would hide
// the transfer, and the error names the call form that does not.
func TestUFCS_OwnReceiverRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Buf { n: i64 }

let consume = (self: own Buf) -> i64 => self.n

let b = Buf { n: 3 }
let v = b.consume()`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error for the `own` receiver")
	}
	msg := res.errors[0].Message
	if !strings.Contains(msg, "`own` receiver") || !strings.Contains(msg, "consume(b)") {
		t.Errorf("expected the refusal to name the call form, got %q", msg)
	}
	// Exactly one diagnostic: a refusal that also fell through to "has no method" would
	// leave the reader with two answers, one of them wrong.
	if len(res.errors) != 1 {
		t.Errorf("expected exactly one error, got %d: %v", len(res.errors), res.errors)
	}
}

// The receiver has to fit the `self` parameter. A mismatch is not a UFCS call at all, so
// it falls through to the ordinary "no such member" error.
func TestUFCS_ReceiverTypeMustMatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Buf { n: i64 }

let widen = pure (self: Buf, by: i64) -> i64 => self.n + by

let s = "not a Buf"
let v = s.widen(1)`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error: a string is not a Buf")
	}
	if !strings.Contains(res.errors[0].Message, `has no method "widen"`) {
		t.Errorf("expected the no-method error, got %q", res.errors[0].Message)
	}
}

// A multi-clause function is a candidate like any other. Worth pinning: the obvious
// exclusion (`len(LambdaClauses) > 0`) would have made membership depend on whether the
// declaration had been checked yet, since checking consumes the clauses.
func TestUFCS_MultiClauseCandidate(t *testing.T) {
	res := parseCollectAndCheck(t, `
let describe = pure (self: i64) -> string {
  (0) => "zero",
  (_) => "other"
}

let n = 3
let s: string = n.describe()`, false)
	assertNoErrors(t, res)
}

// --- reach across modules ---------------------------------------------------
//
// These need a real import graph, so they go through modules.Resolve + AnalyzeUnits the
// way lyrac does, rather than the single-source helper above.

func analyzeTree(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root}, modules.Options{})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

const shapesModule = `module shapes

pub let doubled = pure (self: i64) -> i64 => self * 2
`

// With the import, the call resolves.
func TestUFCS_CrossModuleWithImport(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"shapes.lyra": shapesModule,
		"app.lyra": `import shapes

let n = 21
let m: i64 = n.doubled()`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("expected the imported module's function to be callable, got %v", errs)
	}
}

// Without it, it does not — what a file may call is decided by its own import list, so an
// unrelated file's import cannot change what this one means.
func TestUFCS_CrossModuleWithoutImport(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"shapes.lyra": shapesModule,
		"other.lyra": `module other
import shapes
pub let unused = pure (n: i64) -> i64 => n`,
		"app.lyra": `import other

let n = 21
let m = n.doubled()`,
	})
	errs := res.Errors()
	if len(errs) == 0 {
		t.Fatal("expected an error: app.lyra never imported shapes")
	}
	if !strings.Contains(errs[0].Message, `has no method "doubled"`) ||
		!strings.Contains(errs[0].Message, "shapes") {
		t.Errorf("expected the error to name the module worth importing, got %q", errs[0].Message)
	}
}

// The import that permitted a UFCS call is *used*, though its name appears nowhere in the
// file. The syntactic unused-import check cannot see such a use on its own, and its advice
// — delete the import — would break the program.
func TestUFCS_ImportIsNotReportedUnused(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"shapes.lyra": shapesModule,
		"app.lyra": `import shapes

let n = 21
let m: i64 = n.doubled()`,
	})
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "never used") {
			t.Errorf("the import is what permitted the call: %q", d.Message)
		}
	}
}
