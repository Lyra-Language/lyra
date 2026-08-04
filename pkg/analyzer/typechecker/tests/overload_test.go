package typechecker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// Receiver-keyed overloading — two functions of one name in one module, told apart by
// the type of their `self` parameter.
//
// The point of the feature is that a module can give two types the same vocabulary:
// `Maybe` and `Result` both want `unwrap_or`, and before this the second one had to be
// spelled differently or live in another module. Resolution is asserted through *return
// types* wherever both candidates would otherwise accept the call, so a test cannot pass
// by picking the wrong member.

const twoUnwraps = `
data Maybe<t> = None | Some t
data Result<t, e> = Ok(t) | Err(e)

let unwrap_or<t> = pure (self: Maybe<t>, fallback: t) -> t => match self {
  Some v => v,
  None => fallback,
}

let unwrap_or<t, e> = pure (self: Result<t, e>, fallback: t) -> t => match self {
  Ok v => v,
  Err _ => fallback,
}
`

func TestOverload_MethodFormPicksByReceiver(t *testing.T) {
	res := parseCollectAndCheck(t, twoUnwraps+`
let m: Maybe<i64> = Some 7
let r: Result<i64, string> = Ok 9
let a: i64 = m.unwrap_or(0)
let b: i64 = r.unwrap_or(0)`, false)
	assertNoErrors(t, res)
}

// The same two calls written as plain calls. The receiver is argument 0 either way, which
// is what lets one resolution rule serve both spellings.
func TestOverload_CallFormPicksByFirstArgument(t *testing.T) {
	res := parseCollectAndCheck(t, twoUnwraps+`
let m: Maybe<i64> = Some 7
let r: Result<i64, string> = Ok 9
let a: i64 = unwrap_or(m, 0)
let b: i64 = unwrap_or(r, 0)`, false)
	assertNoErrors(t, res)
}

// Which member was chosen, pinned by a difference the annotation can see: the `Maybe`
// overload returns a string here, so a call on a Maybe that type-checks as i64 would mean
// the Result member had been picked.
func TestOverload_ChoosesTheRightMemberNotTheFirst(t *testing.T) {
	src := `
data Maybe<t> = None | Some t
data Result<t, e> = Ok(t) | Err(e)

let describe<t> = pure (self: Maybe<t>) -> string => "maybe"
let describe<t, e> = pure (self: Result<t, e>) -> i64 => 1
`
	res := parseCollectAndCheck(t, src+`
let m: Maybe<i64> = Some 7
let r: Result<i64, string> = Ok 1
let s: string = m.describe()
let n: i64 = r.describe()`, false)
	assertNoErrors(t, res)

	// And the mirror: asking for the other member's return type must fail.
	bad := parseCollectAndCheck(t, src+`
let m: Maybe<i64> = Some 7
let n: i64 = m.describe()`, false)
	if len(bad.errors) == 0 {
		t.Fatal("expected the Maybe overload's string return to reject an i64 annotation")
	}
}

// A receiver no member accepts names the receiver types that do exist — the reader's next
// question is "then what does it take", so the message answers it.
func TestOverload_NoMemberForReceiver(t *testing.T) {
	res := parseCollectAndCheck(t, twoUnwraps+`
let n = 5
let x = n.unwrap_or(0)`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error: no overload takes an i64 receiver")
	}
	joined := allMessages(res)
	if !strings.Contains(joined, "Maybe") || !strings.Contains(joined, "Result") {
		t.Errorf("expected the candidates to be named, got %q", joined)
	}
}

// Two members with the same receiver head are refused at the declaration rather than at
// each call, since ranking them would need a specificity ordering the language lacks.
func TestOverload_SameReceiverHeadRefused(t *testing.T) {
	errs := collectOnly(t, `
data Maybe<t> = None | Some t

let unwrap_or<t> = pure (self: Maybe<t>, fallback: t) -> t => fallback
let unwrap_or<t> = pure (self: Maybe<t>, fallback: t, extra: t) -> t => fallback
`)
	if errs == "" {
		t.Fatal("expected two Maybe receivers to be refused")
	}
	if !strings.Contains(errs, "Maybe` receiver") {
		t.Errorf("expected the message to name the clashing receiver, got %q", errs)
	}
}

// A type variable receiver accepts everything, so it cannot be one candidate among
// several — there would be no receiver that picks between it and any other member.
func TestOverload_TypeVariableReceiverRefused(t *testing.T) {
	errs := collectOnly(t, `
data Maybe<t> = None | Some t

let show<t> = pure (self: Maybe<t>) -> string => "maybe"
let show<t> = pure (self: t) -> string => "anything"
`)
	if errs == "" {
		t.Fatal("expected a bare type-variable receiver to be refused as an overload")
	}
}

// Overloading is opt-in through `self`, exactly as UFCS is: two same-named functions
// without receivers stay the redeclaration they always were.
func TestOverload_NonReceiverFunctionsStillClash(t *testing.T) {
	errs := collectOnly(t, `
let helper = pure (n: i64) -> i64 => n
let helper = pure (s: string) -> string => s
`)
	if errs == "" {
		t.Fatal("expected two non-receiver functions of one name to remain an error")
	}
	// No overload clause: neither takes a receiver, so the author was not overloading
	// and a sentence about the rule would be noise.
	if strings.Contains(errs, "receiver") {
		t.Errorf("a plain redeclaration should not be explained in overload terms, got %q", errs)
	}
}

// An overloaded name has no single type, so it cannot be used as a value.
func TestOverload_BareReferenceIsAnError(t *testing.T) {
	res := parseCollectAndCheck(t, twoUnwraps+`
let f = unwrap_or`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected a bare reference to an overloaded name to be an error")
	}
	if joined := allMessages(res); !strings.Contains(joined, "overloaded") {
		t.Errorf("expected the message to say the name is overloaded, got %q", joined)
	}
}

func allMessages(res checkResult) string {
	var b strings.Builder
	for _, e := range res.errors {
		b.WriteString(e.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// collectOnly runs the collector and hands back its errors, which is where a *declaration
// time* refusal is reported — parseCollectAndCheck treats those as fatal, since for every
// other test they mean the fixture is broken rather than that the fixture is the point.
func collectOnly(t *testing.T, source string) string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	_, _, _, errs := c.Collect(tree.RootNode())
	var b strings.Builder
	for _, e := range errs {
		b.WriteString(e.Error())
		b.WriteString("\n")
	}
	return b.String()
}
