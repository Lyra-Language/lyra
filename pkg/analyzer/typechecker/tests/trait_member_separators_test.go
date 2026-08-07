package typechecker_test

import "testing"

// A trait's and an impl's methods may be separated by newlines as well as commas
// (tree-sitter-lyra 01c7d7c). Statements gained a terminator on 07/31 and these lists did
// not, so the newline form failed with "missing }" pointed at the end of the *first*
// signature — several lines above the actual problem.
//
// These live on the Go side because the grammar's corpus pins the tree while this pins what
// the collector and typechecker make of it: two methods, dispatchable, whichever separator
// was written.

func TestTraitSeparators_NewlineSeparated(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { n: i64 }
trait Ops {
  a: (Self) -> i64
  b: (Self) -> i64
}
impl Ops for Node {
  a = (self) => self.n
  b = (self) => self.n * 2
}
let f = (x: Node) -> i64 => x.a() + x.b()`, false)
	assertNoErrors(t, res)
}

// Commas still work, and mixing the two is fine — a list is not required to pick one.
func TestTraitSeparators_CommasAndMixed(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Node { n: i64 }
trait Ops { a: (Self) -> i64, b: (Self) -> i64 }
impl Ops for Node { a = (self) => 1, b = (self) => 2 }
let f = (x: Node) -> i64 => x.a() + x.b()`, false))

	assertNoErrors(t, parseCollectAndCheck(t, `
struct Node { n: i64 }
trait Ops {
  a: (Self) -> i64, b: (Self) -> i64
  c: (Self) -> i64
}
impl Ops for Node {
  a = (self) => 1, b = (self) => 2
  c = (self) => 3
}
let f = (x: Node) -> i64 => x.a() + x.b() + x.c()`, false))
}

// A signature wrapped across lines is not two members. The scanner only offers a terminator
// where the grammar accepts one, so a newline inside an unfinished parameter list never
// reaches it — the property the whole separator design rests on.
func TestTraitSeparators_WrappedSignatureIsOneMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { n: i64 }
trait Ops {
  a: (Self,
      i64) -> i64
  b: (Self) -> i64
}
impl Ops for Node {
  a = (self, k) => self.n + k
  b = (self) => 1
}
let f = (x: Node) -> i64 => x.a(1) + x.b()`, false)
	assertNoErrors(t, res)
}

// Effect bounds and default bodies sit inside a member, so neither is mistaken for the end
// of one.
func TestTraitSeparators_ModifiersAndDefaults(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { n: i64 }
trait Ops {
  pure a: (Self) -> i64 = (self) => 1
  pure b: (Self) -> i64
}
impl Ops for Node {
  b = pure (self) => 2
}
let f = (x: Node) -> i64 => x.b()`, false)
	assertNoErrors(t, res)
}

// --- struct declarations ------------------------------------------------------
//
// The same separator, for the same reason: a struct declaration's fields were the last
// users of the comma-only list shape, giving the same "missing }" aimed at the first field.

func TestStructSeparators_FieldsOnSeparateLines(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node {
  n: i64
  tag: string
}
let f = (x: Node) -> i64 => x.n`, false)
	assertNoErrors(t, res)
}

// Commas, mixed separators, defaults, `readonly` and an attribute list all still work —
// the field is a member, and only how members are *separated* changed.
func TestStructSeparators_FieldModifiersAndDefaults(t *testing.T) {
	res := parseCollectAndCheck(t, `
@packed
struct Node {
  readonly n: i64 = 3, tag: string
  flag: bool = false
}
let f = (x: Node) -> i64 => x.n`, false)
	assertNoErrors(t, res)
}

// An anonymous struct *type* shares the rule, so it wraps across lines too.
func TestStructSeparators_AnonymousStructType(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (p: { x: i64
  y: i64 }) -> i64 => p.x + p.y`, false)
	assertNoErrors(t, res)
}

// A struct *literal* is deliberately unchanged: its field list sits inside the
// literal-vs-block ambiguity, so it is a separate question rather than the same one-word
// change. Pinned so the distinction is a decision on the record rather than an oversight.
// A struct *literal*'s fields still require commas, unlike a struct declaration's. This
// is one of the few tests whose source is *meant* not to parse, so it opts out of the
// does-it-parse guard explicitly rather than being exempted by accident.
func TestStructSeparators_LiteralStillRequiresCommas(t *testing.T) {
	res := parseCollectAndCheckAllowingSyntaxErrors(t, `
struct Node { n: i64, tag: string }
let f = () -> i64 => {
  let x = Node {
    n: 7
    tag: "hi"
  }
  x.n
}`, false)
	if len(res.errors) == 0 {
		t.Error("a struct literal's fields still need commas; if that changed on purpose, this test should change with it")
	}
}
