package driver

import (
	"strings"
	"testing"
	"time"
)

// Closing the instantiation set: a generic body calling another generic at a
// variable-dependent instantiation needs a specialization no call site names.

// The composed specialization exists, keyed at the caller's concrete type. Before this,
// `expect<t=t>` was the only entry and the backend rejected it as unlowerable.
func TestCloseInstantiations_ComposesThroughAGenericCaller(t *testing.T) {
	res := Analyze([]byte(`
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let unwrap<t> = (self: Maybe<t>) -> t => expect(self, "unwrap on a None")

let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  u8(m.unwrap())
}`))
	if res.HasErrors() {
		t.Fatalf("expected no errors, got %v", res.Errors())
	}
	want := map[string]bool{"unwrap<t=i64>": false, "expect<t=i64>": false}
	for _, inst := range res.Instantiations.Concrete() {
		if _, expected := want[inst.Key()]; expected {
			want[inst.Key()] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("expected a concrete specialization %s; got %v", key, concreteKeys(res))
		}
	}
}

// Every concrete specialization gets its own ownership table. This is why the closure runs
// before the ownership pass rather than in the backend: a specialization discovered later
// would fall back to the program-wide table, which is analyzed generically — where a type
// variable is not reference-counted — so a managed payload would be neither retained nor
// released.
func TestCloseInstantiations_DiscoveredSpecializationsGetOwnershipTables(t *testing.T) {
	res := Analyze([]byte(`
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let unwrap<t> = (self: Maybe<t>) -> t => expect(self, "unwrap on a None")

let main = () -> u8 => {
  let s: Maybe<string> = Some "x"
  let v = s.unwrap()
  0
}`))
	if res.HasErrors() {
		t.Fatalf("expected no errors, got %v", res.Errors())
	}
	for _, inst := range res.Instantiations.Concrete() {
		if res.OwnershipBySpec[inst.Key()] == nil {
			t.Errorf("specialization %s has no ownership table of its own", inst.Key())
		}
	}
}

// A template — a call inside a generic body, whose bindings are the enclosing body's own
// type variables — is never emitted and never analyzed. It has to stay in the table (it is
// what composition reads), but it is not a specialization.
func TestCloseInstantiations_TemplatesAreNotConcrete(t *testing.T) {
	res := Analyze([]byte(`
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let unwrap<t> = (self: Maybe<t>) -> t => expect(self, "unwrap on a None")

let main = () -> u8 => {
  let m: Maybe<i64> = Some 1
  u8(m.unwrap())
}`))
	for _, inst := range res.Instantiations.Concrete() {
		if !inst.IsConcrete() {
			t.Errorf("Concrete() returned a template: %s", inst.Key())
		}
	}
	if res.OwnershipBySpec["expect<t=t>"] != nil {
		t.Error("a template should not be analyzed for ownership")
	}
}

// **Polymorphic recursion is refused, and refused quickly.** `deep<t>` calling
// `deep<Box<t>>` has infinitely many specializations. The bound that matters is on type
// *depth*: a count alone terminates only after the set is both enormous and individually
// huge, which measured at over a minute and a gigabyte — indistinguishable from the hang
// it is meant to prevent. The deadline here is what pins that, not just the diagnostic.
func TestCloseInstantiations_PolymorphicRecursionIsRefused(t *testing.T) {
	done := make(chan struct{})
	var messages string
	go func() {
		res := Analyze([]byte(`
data Box<t> = Wrap(t)
let deep<t> = (b: Box<t>) -> i64 => deep(Wrap(b))
let main = () -> u8 => u8(deep(Wrap(1)))`))
		var b strings.Builder
		for _, e := range res.Errors() {
			b.WriteString(e.Message)
		}
		messages = b.String()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("polymorphic recursion did not terminate within 30s — the depth bound is not firing")
	}
	if !strings.Contains(messages, "unbounded number of specializations") {
		t.Errorf("expected a divergence diagnostic, got %q", messages)
	}
	// The message must stay readable: the type at the point of detection is two dozen
	// constructors deep, and printing it in full buries the explanation.
	if strings.Count(messages, "Box<") > 5 {
		t.Errorf("the divergence message should abbreviate the type, got %q", messages)
	}
}

// Ordinary recursion at the *same* type is not polymorphic recursion and must still
// compile — the set closes after one specialization because the composed key repeats.
func TestCloseInstantiations_SameTypeRecursionIsFine(t *testing.T) {
	res := Analyze([]byte(`
data List<t> = Nil | Cons(t, shared List<t>)

let length<t> = (self: shared List<t>) -> i64 => match self {
  Nil => 0,
  Cons(_, rest) => 1 + length(rest),
}

let main = () -> u8 => {
  let l: shared List<i64> = Cons(1, Cons(2, Nil))
  u8(length(l))
}`))
	if res.HasErrors() {
		t.Fatalf("recursion at one type should compile; got %v", res.Errors())
	}
}

func concreteKeys(res *Result) []string {
	var out []string
	for _, inst := range res.Instantiations.Concrete() {
		out = append(out, inst.Key())
	}
	return out
}
