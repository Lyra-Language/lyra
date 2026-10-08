package llvm

import (
	"strings"
	"testing"
)

// `impl Person { let say_hi = (self) => … }` is sugar for top-level
// `let say_hi = (self: Person) => …` (10/07), so each thing a hand-written method does has
// to survive the block: both call spellings, a `mut` receiver writing through, a generic
// target whose variables a member's own list is extended with, and one name in two blocks
// overloading by receiver.
func TestExec_InherentImplMembersAreMethods(t *testing.T) {
	t.Parallel()
	const src = `
module main

struct Person { name: string, age: u8 }

impl Person {
  let say_hi = (self) => println("${self.name} is ${self.age}")
  let birthday = (self: mut, age: u8) => { self.age = age }
  let older = pure (self: ref, other: Person) -> bool => self.age > other.age
}

struct Robot { serial: i64 }

impl Robot {
  let say_hi = (self) => println("beep ${self.serial}")
}

struct Box<t> { value: t }

impl Box<t> {
  let get = pure (self) -> t => self.value
  let map<u> = pure (self, f: (t) -> u) -> Box<u> => Box { value: f(self.value) }
}

let main = () -> void => {
  var p = Person { name: "Ada", age: 36 }
  p.say_hi()
  p.birthday(37)
  say_hi(p)
  println("${p.older(Person { name: "Bo", age: 3 })}")
  Robot { serial: 7 }.say_hi()
  let b = Box { value: 20 }
  println("${b.map((x) => "n=${x * 2 + 2}").get()}")
}
`
	want := "Ada is 36\nAda is 37\ntrue\nbeep 7\nn=42"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("inherent methods gave:\n%s\nwant:\n%s", got, want)
	}
}

// A member is exported by `pub` and reached method-style from another module, as a
// top-level `pub let` with a `self` receiver is.
func TestExec_InherentImplMemberAcrossModules(t *testing.T) {
	t.Parallel()
	out := runModules(t, map[string]string{
		"shapes.lyra": `module shapes
pub struct Square { side: i64 }
pub let square = pure (side: i64) -> Square => Square { side: side }
impl Square {
  /// The square's area.
  pub let area = pure (self) -> i64 => self.side * self.side
}
`,
		"main.lyra": `module main
import shapes
let main = () -> void => {
  let s = shapes.square(3)
  println("${s.area()} ${shapes.area(s)}")
}
`,
	})
	if got := strings.TrimSpace(out); got != "9 9" {
		t.Errorf("output = %q; want \"9 9\"", got)
	}
}

// `impl t where t: Ord` is the prelude's `min`/`max` (10/07): the block's bound reaches a
// member with no list of its own, and a block over a bare variable is the generic fallback
// every receiver reaches.
func TestExec_InherentImplWhereBoundsItsMembers(t *testing.T) {
	t.Parallel()
	const src = `
module main

impl t where t: Ord {
  let least = pure noalloc (self, other: t) -> t =>
    match self.compare(other) { Greater => other, _ => self }
}

struct Pair<k, v> { key: k, value: v }

impl Pair<k, v> where k: Show, v: Show {
  let describe = pure (self) -> string => "${self.key}=${self.value}"
}

let main = () -> void => {
  println("${3.least(5)} ${least("b", "a")}")
  println(Pair { key: "x", value: 1 }.describe())
}
`
	want := "3 a\nx=1"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("bounded inherent methods gave:\n%s\nwant:\n%s", got, want)
	}
}
