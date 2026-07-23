package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// checkMoves runs the front-end up to (and including) typechecking — the pass
// needs the TypeTable to know which values are managed — and returns the
// use-after-move diagnostics.
func checkMoves(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, collectErrs := c.Collect(tree.RootNode())
	if len(collectErrs) > 0 {
		t.Fatalf("collector errors: %v", collectErrs)
	}
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	return checker.CheckUseAfterMove(program, symTable, tt)
}

func assertMoveErrors(t *testing.T, source string, want int) []diag.Diagnostic {
	t.Helper()
	got := checkMoves(t, source)
	if len(got) != want {
		t.Fatalf("expected %d use-after-move error(s), got %d: %v", want, len(got), got)
	}
	for _, d := range got {
		if d.Code != diag.CodeUseAfterMove {
			t.Errorf("wrong code: got %q, want %q", d.Code, diag.CodeUseAfterMove)
		}
		if d.Severity != diag.SeverityError {
			t.Errorf("use-after-move should be an error, got severity %v", d.Severity)
		}
	}
	return got
}

// A preamble giving the tests a managed (`shared`) type plus an owning and a
// borrowing consumer of it.
const movePreamble = `struct Person { name: string, }
let consume = (p: own shared Person) -> u8 => 1
let peek = (p: ref shared Person) -> u8 => 2
`

// --- the base case ---

// Reading a binding after it was moved into an `own` parameter is the error.
func TestMove_UseAfterOwnArgument(t *testing.T) {
	got := assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let a = consume(p)
  let b = consume(p)
  a
}`, 1)
	if !strings.Contains(got[0].Message, `"p" is used after it was moved`) {
		t.Errorf("message should name the binding: %q", got[0].Message)
	}
}

// Moving without reading again is the whole point of `own` — never an error.
func TestMove_MoveWithoutLaterUse_Ok(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  consume(p)
}`, 0)
}

// A borrow leaves the caller owning the value, so any number of reads are fine.
func TestMove_BorrowIsNotAMove(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let a = peek(p)
  let b = peek(p)
  a
}`, 0)
}

// A field read after the move is still a read of the moved binding.
func TestMove_FieldReadAfterMove(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let a = consume(p)
  if p.name == "a" { 1 } else { 0 }
}`, 1)
}

// Two moves of the same binding in one call: the second argument reads it after
// the first argument consumed it.
func TestMove_SameBindingTwiceInOneCall(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let both = (a: own shared Person, b: own shared Person) -> u8 => 1
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  both(p, p)
}`, 1)
}

// --- what is NOT a move ---

// An `own` scalar is copied, not consumed — the original stays valid.
func TestMove_OwnScalarIsCopied_Ok(t *testing.T) {
	assertMoveErrors(t, `
let takeNum = (n: own i64) -> i64 => n
let main = () -> u8 => {
  let x: i64 = 5
  let a = takeNum(x)
  u8(x)
}`, 0)
}

// An `own` *stack* aggregate is likewise copied by value.
func TestMove_OwnStackStructIsCopied_Ok(t *testing.T) {
	assertMoveErrors(t, `struct Box { v: i64, }
let takeBox = (b: own Box) -> i64 => b.v
let main = () -> u8 => {
  let b: Box = Box { v: 5 }
  let a = takeBox(b)
  u8(b.v)
}`, 0)
}

// Strings are managed too, so `own string` really does consume.
func TestMove_OwnStringIsAMove(t *testing.T) {
	assertMoveErrors(t, `
let eat = (s: own string) -> u8 => 1
let main = () -> u8 => {
  let s: string = "a" ++ "b"
  let a = eat(s)
  if s == "ab" { 1 } else { 0 }
}`, 1)
}

// --- flow sensitivity ---

// Alternatives, not sequence: a move in one arm doesn't affect a read in another.
func TestMove_MoveInOneBranchReadInOther_Ok(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  if p.name == "a" { consume(p) } else { peek(p) }
}`, 0)
}

// Conservative at the join: moved in *either* branch means moved afterwards.
func TestMove_ConditionalMoveThenUse(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let a = if p.name == "a" { consume(p) } else { 0 }
  let b = peek(p)
  a
}`, 1)
}

// Same join rule across match arms.
func TestMove_MoveInMatchArmThenUse(t *testing.T) {
	assertMoveErrors(t, `data Tag = Red | Blue
struct Person { name: string, }
let consume = (p: own shared Person) -> u8 => 1
let peek = (p: ref shared Person) -> u8 => 2
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let t: Tag = Red
  let a = match t { Red => consume(p), Blue => 0 }
  peek(p)
}`, 1)
}

// A move inside a loop body is a use-after-move on the next iteration, even
// though the read precedes the move textually.
func TestMove_MoveInsideLoop(t *testing.T) {
	got := assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  for var i = 0; i < 3; i += 1 {
    let a = peek(p)
    let b = consume(p)
  }
  0
}`, 1)
	if !strings.Contains(got[0].Message, "later iteration") {
		t.Errorf("loop-carried move should say so: %q", got[0].Message)
	}
}

// A value created *inside* the loop is a fresh binding each iteration, so
// consuming it there is fine — the declaration clears the seeded move.
func TestMove_ValueBuiltAndMovedInsideLoop_Ok(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let main = () -> u8 => {
  for var i = 0; i < 3; i += 1 {
    let p: shared Person = Person { name: "a" }
    let a = consume(p)
  }
  0
}`, 0)
}

// Reassigning a `var` gives the name a new value, clearing the move.
func TestMove_ReassignmentClearsTheMove_Ok(t *testing.T) {
	assertMoveErrors(t, `
let eat = (s: own string) -> u8 => 1
let main = () -> u8 => {
  var s: string = "a" ++ "b"
  let a = eat(s)
  s = "c" ++ "d"
  if s == "cd" { 1 } else { 0 }
}`, 0)
}

// A later `let` of the same name is a fresh binding, not the moved one.
func TestMove_RebindClearsTheMove_Ok(t *testing.T) {
	assertMoveErrors(t, `
let eat = (s: own string) -> u8 => 1
let main = () -> u8 => {
  let s: string = "a" ++ "b"
  let a = eat(s)
  let s: string = "c" ++ "d"
  if s == "cd" { 1 } else { 0 }
}`, 0)
}

// --- conservatism: an unrecognized call shape must never produce a false positive ---

// An unresolvable callee (here a method call) records no move at all.
func TestMove_UnresolvableCalleeNeverFlags_Ok(t *testing.T) {
	assertMoveErrors(t, `
let main = () -> u8 => {
  let s: string = "a" ++ "b"
  let n = s.length()
  if s == "ab" { 1 } else { 0 }
}`, 0)
}

// A move in a *different* function doesn't leak into this one.
func TestMove_PerFunctionState_Ok(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let first = (p: own shared Person) -> u8 => consume(p)
let main = () -> u8 => {
  let p: shared Person = Person { name: "a" }
  let a = peek(p)
  peek(p)
}`, 0)
}

// An `own` parameter is itself a binding that can be moved onward and then
// wrongly re-read.
func TestMove_OwnParameterMovedOnward(t *testing.T) {
	assertMoveErrors(t, movePreamble+`
let relay = (p: own shared Person) -> u8 => {
  let a = consume(p)
  peek(p)
}`, 1)
}

// --- trait-impl methods ---
// A trait-impl method body IS analyzed: a move followed by a read inside one
// method is flagged.
func TestMove_InsideTraitMethod(t *testing.T) {
	assertMoveErrors(t, `struct Person { name: string, }
let consume = (p: own shared Person) -> u8 => 1
let peek = (p: ref shared Person) -> u8 => 2
trait Doer { go: (self) -> u8 }
impl Doer for Person {
  go = (self) -> u8 => {
    let p: shared Person = Person { name: "a" }
    let x = consume(p)
    peek(p)
  }
}
let main = () -> u8 => 0`, 1)
}

// ...but each method is a separate function: a move in one must not reach the next.
func TestMove_TraitMethodsAreSeparate(t *testing.T) {
	assertMoveErrors(t, `struct Person { name: string, }
let consume = (p: own shared Person) -> u8 => 1
let peek = (p: ref shared Person) -> u8 => 2
trait Doer { first: (self) -> u8, second: (self) -> u8 }
impl Doer for Person {
  first = (self) -> u8 => {
    let p: shared Person = Person { name: "a" }
    consume(p)
  }
  second = (self) -> u8 => {
    let p: shared Person = Person { name: "b" }
    peek(p)
  }
}
let main = () -> u8 => 0`, 0)
}
