package llvm

import "testing"

// A trait imported under an alias is the trait, wherever the alias is written: an impl's
// head here and in another module, a bound on a function and an impl, a supertrait,
// and a `Trait::method` path. The impl registered under the alias satisfied no bound naming
// the trait (lyra-E036), and from another module the backend found no impl to call (11/13).
func TestTraitAlias_IsTheTraitEverywhere(t *testing.T) {
	got := runModules(t, map[string]string{
		"shapes.lyra": `module shapes
pub trait Shape {
  area: (Self) -> i64
}
pub let total<s> where s: Shape = (x: s) -> i64 => x.area()
`,
		"squares.lyra": `module squares
import shapes.{ Shape as Figure }
pub struct Square { side: i64 }
impl Figure for Square {
  area = (self) => self.side * self.side
}
`,
		"main.lyra": `module main
import shapes
import shapes.{ Shape as Figure }
import squares.{ Square }
struct Rect { w: i64, h: i64 }
impl Figure for Rect {
  area = (self) => self.w * self.h
}
trait Solid: Figure {
  depth: (Self) -> i64
}
impl Solid for Rect {
  depth = (self) => 2
}
struct Pair<t> { a: t, b: t }
let both<t> where t: Figure = (p: Pair<t>) -> i64 => p.a.area() + p.b.area()
let volume<t> where t: Solid = (x: t) -> i64 => x.area() * x.depth()
struct Twice<t> { x: t }
impl Figure<t> for Twice<t> where t: Figure {
  area = (self) => self.x.area() * 2
}
let main = () -> void => {
  let r = Rect { w: 2, h: 3 }
  println("${shapes.total(Square { side: 4 })} ${shapes.total(r)} ${Figure::area(r)}")
  println("${both(Pair { a: r, b: r })} ${volume(r)} ${shapes.total(Twice { x: Square { side: 1 } })}")
}
`,
	})
	if want := "16 6 6\n12 12 2\n"; got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}
