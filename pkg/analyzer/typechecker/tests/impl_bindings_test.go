package typechecker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Dispatch records the bindings a generic impl unified with, and they travel on the
// resolution. They were computed all along — `where` bounds are checked against them —
// but stopped at the typechecker, which is why the backend emitted one function for every
// instantiation of a generic impl.
//
// These go through driver.Analyze rather than the collector helper because the MethodTable
// is what carries the resolutions, and the driver is where a caller gets one.

func specKeys(res *driver.Result) []string {
	var out []string
	for _, r := range res.MethodTable.Specializations() {
		out = append(out, r.SpecKey())
	}
	return out
}

func TestImplBindings_GenericImplRecordsItsSubstitution(t *testing.T) {
	res := driver.Analyze([]byte(`
data Opt<t> = Nil | Just t
trait Unwrap<e> { unwrap: (Self, e) -> e }
impl Unwrap<t> for Opt<t> {
  unwrap = (self, fallback) => match self {
    Just v => v,
    Nil => fallback,
  }
}
let f = (o: Opt<i64>) -> i64 => o.unwrap(0)`))
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	specs := res.MethodTable.Specializations()
	if len(specs) != 1 {
		t.Fatalf("expected one specialization, got %v", specKeys(res))
	}
	bound, ok := specs[0].Bindings["t"]
	if !ok {
		t.Fatalf("the impl's `t` should be bound; got %v", specs[0].Bindings)
	}
	if p, isPrim := bound.(types.PrimitiveType); !isPrim || p.Name != types.Int64 {
		t.Errorf("t bound to %v; want i64", bound)
	}
}

// Two receivers, two specializations — the property the emitted symbol depends on.
func TestImplBindings_DistinctReceiversAreDistinctSpecializations(t *testing.T) {
	res := driver.Analyze([]byte(`
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> { get = (self) => self.value }
let a = (b: Box<i64>) -> i64 => b.get()
let c = (b: Box<bool>) -> bool => b.get()`))
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := specKeys(res); len(got) != 2 {
		t.Errorf("expected two specializations, got %v", got)
	}
}

// …and the same receiver twice is one specialization, so ten calls do not emit ten
// functions.
func TestImplBindings_SameReceiverIsOneSpecialization(t *testing.T) {
	res := driver.Analyze([]byte(`
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> { get = (self) => self.value }
let a = (b: Box<i64>) -> i64 => b.get()
let c = (b: Box<i64>) -> i64 => b.get() + b.get()`))
	if got := specKeys(res); len(got) != 1 {
		t.Errorf("expected one specialization, got %v", got)
	}
}

// A non-generic impl binds nothing, and its key is the bare method — so its emitted symbol
// is unchanged by any of this.
func TestImplBindings_NonGenericImplHasNoBindings(t *testing.T) {
	res := driver.Analyze([]byte(`
struct Counter { n: i64 }
trait Peek { peek: (Self) -> i64 }
impl Peek for Counter { peek = (self) => self.n }
let f = (c: Counter) -> i64 => c.peek()`))
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	specs := res.MethodTable.Specializations()
	if len(specs) != 1 {
		t.Fatalf("expected one specialization, got %v", specKeys(res))
	}
	if len(specs[0].Bindings) != 0 {
		t.Errorf("a non-generic impl binds nothing; got %v", specs[0].Bindings)
	}
	if want := "Counter$Peek$peek"; specs[0].SpecKey() != want {
		t.Errorf("key %q; want %q", specs[0].SpecKey(), want)
	}
}

// Every specialization the program reaches gets an ownership table, which is what the
// backend consults while lowering that body. Before 08/03 there was none for any method
// body, generic or not — the pass only ever walked top-level declarations.
func TestImplBindings_EveryMethodSpecializationHasAnOwnershipTable(t *testing.T) {
	res := driver.Analyze([]byte(`
data Opt<t> = Nil | Just t
trait Unwrap<e> { unwrap: (Self, e) -> e }
impl Unwrap<t> for Opt<t> {
  unwrap = (self, fallback) => match self {
    Just v => v,
    Nil => fallback,
  }
}
let f = (o: Opt<string>) -> string => o.unwrap("x")
let g = (o: Opt<i64>) -> i64 => o.unwrap(0)`))
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	for _, r := range res.MethodTable.Specializations() {
		if res.OwnershipByMethod[r.SpecKey()] == nil {
			t.Errorf("no ownership table for %s", r.SpecKey())
		}
	}
	// The managed instantiation records retains; the i64 one does not. Same body, same
	// nodes, different answers — which is why the tables cannot be merged.
	var managed, plain int
	for key, tbl := range res.OwnershipByMethod {
		switch {
		case strings.Contains(key, "string"):
			managed = len(tbl.Retain)
		case strings.Contains(key, "i64"):
			plain = len(tbl.Retain)
		}
	}
	if managed == 0 {
		t.Error("the string specialization should record at least one retain")
	}
	if plain != 0 {
		t.Errorf("an i64 payload is not reference-counted; got %d retain(s)", plain)
	}
}
