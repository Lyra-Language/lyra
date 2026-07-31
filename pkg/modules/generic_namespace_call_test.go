package modules_test

import (
	"sort"
	"testing"
)

// A generic function called through a module namespace (`opt.wrap(7)`) is instantiated
// from the call's arguments, exactly as the same function called by name is.
//
// It used to be checked against the *declared* signature instead, whose type variables are
// still free — `moduleMemberType` handed back a `*types.LambdaType` and the member-call
// path had nothing to solve with. So every generic namespace call failed with "cannot
// assign Opt<i64> to Opt<t>", and importing the same function unqualified
// (`import util.opt.{ wrap }`) was the only way to call it.
func TestNamespaceCall_GenericIsInstantiated(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.opt
let main = () -> u8 => u8(opt.unwrap(opt.wrap(7), 0))`,
		"util/opt.lyra": `module util.opt
pub data Opt<t> = Nil | One(t)
pub let wrap<t> = (v: t) -> Opt<t> => One(v)
pub let unwrap<t> = (o: Opt<t>, fallback: t) -> t => match o {
  One(v) => v,
  Nil => fallback,
}`,
	})
	res := analyze(t, root)
	if res.HasErrors() {
		t.Fatalf("a generic namespace call should check: %v", res.Errors())
	}

	// Solving is only half of it: the specialization has to be *recorded* per call site,
	// since that table is the only thing the backend emits a generic function from. A
	// call that type-checks but records nothing fails later, in codegen, as
	// `unsupported method call`.
	var names []string
	for _, inst := range res.Instantiations.All() {
		names = append(names, inst.Name)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "unwrap" || names[1] != "wrap" {
		t.Errorf("want specializations for wrap and unwrap, got %v", names)
	}
}

// The fix must not have loosened checking into "anything goes through a namespace".
// A generic namespace call with an argument that cannot be unified is still rejected,
// and the diagnostic names the function.
func TestNamespaceCall_GenericStillTypeChecked(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.opt
let main = () -> u8 => u8(opt.unwrap(opt.wrap(7), "no"))`,
		"util/opt.lyra": `module util.opt
pub data Opt<t> = Nil | One(t)
pub let wrap<t> = (v: t) -> Opt<t> => One(v)
pub let unwrap<t> = (o: Opt<t>, fallback: t) -> t => match o {
  One(v) => v,
  Nil => fallback,
}`,
	})
	res := analyze(t, root)
	if !res.HasErrors() {
		t.Fatal("unwrap(Opt<i64>, string) should not check")
	}
	if !errorsContaining(res, "unwrap") {
		t.Errorf("the diagnostic should name the callee; got %v", res.Errors())
	}
}

// Arity is checked against the declaration too — the generic path reports it before
// trying to solve, since a missing argument is a variable with nothing to bind it.
func TestNamespaceCall_GenericArity(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.opt
let main = () -> u8 => u8(opt.unwrap(opt.wrap(7)))`,
		"util/opt.lyra": `module util.opt
pub data Opt<t> = Nil | One(t)
pub let wrap<t> = (v: t) -> Opt<t> => One(v)
pub let unwrap<t> = (o: Opt<t>, fallback: t) -> t => match o {
  One(v) => v,
  Nil => fallback,
}`,
	})
	res := analyze(t, root)
	if !errorsContaining(res, "expected 2 argument(s), got 1") {
		t.Errorf("want an arity diagnostic, got %v", res.Errors())
	}
}
