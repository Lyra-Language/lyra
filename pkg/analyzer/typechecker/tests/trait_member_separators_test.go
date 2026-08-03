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
