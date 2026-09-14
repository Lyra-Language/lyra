package typechecker_test

import "testing"

// `Self` inside a composite in a trait method's signature — `[]Self`, `Maybe<Self>`,
// `(Self, Self)` — means the implementing type, exactly as a bare `Self` does. It used to
// be replaced only at the top level of each parameter and the return, so every impl of
// such a method was refused (`expected DynamicArray<Self>, got StaticArray<i64, 2>`) and a
// call answered a type mentioning `Self`.

func TestTraitNestedSelf_ImplsAccepted(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"array": `
trait Dup { dup: (Self) -> []Self }
impl Dup for i64 { dup = (self) => [self, self] }`,
		"maybe": `
data Maybe<t> = None | Some(t)
trait Dec { dec: (Self) -> Maybe<Self> }
impl Dec for i64 { dec = (self) => Some(self) }`,
		"tuple": `
trait Two { two: (Self) -> (Self, Self) }
impl Two for i64 { two = (self) => (self, self) }`,
		"result": `
data Result<t, e> = Ok(t) | Err(e)
trait Checked { checked: (Self) -> Result<Self, string> }
impl Checked for i64 { checked = (self) => Ok(self) }`,
		"parameter": `
trait Sum { sum: (Self, []Self) -> Self }
impl Sum for i64 { sum = (self, rest) => { var n = self; for r in rest { n = n + r }; n } }`,
		"callback": `
trait Apply { apply: (Self, (Self) -> Self) -> Self }
impl Apply for i64 { apply = (self, f) => f(self) }`,
		"generic impl": `
struct Box<t> { value: t }
trait Dup { dup: (Self) -> []Self }
impl Dup for Box<t> { dup = (self) => [self, self] }`,
		"default method": `
data Maybe<t> = None | Some(t)
trait Dec {
  one: (Self) -> Self
  dec: (Self) -> Maybe<Self> = (self) => Some(self.one())
}
impl Dec for i64 { one = (self) => self }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// The nested `Self` is the implementing type, not "anything": a body of another type is
// still refused.
func TestTraitNestedSelf_WrongElementRefused(t *testing.T) {
	t.Parallel()
	res := parseCollectAndCheck(t, `
trait Dup { dup: (Self) -> []Self }
impl Dup for i64 { dup = (self) => ["a", "b"] }`, false)
	assertHasErrorContaining(t, res, "dup: return type mismatch")
}

// A call answers the concrete type, so the result is usable as one.
func TestTraitNestedSelf_CallAnswersConcreteType(t *testing.T) {
	t.Parallel()
	res := parseCollectAndCheck(t, `
trait Two { two: (Self) -> (Self, Self) }
impl Two for i64 { two = (self) => (self, self) }
let f = (n: i64) -> i64 => {
  let p = n.two()
  p.0 + p.1
}
let g = (n: i64) -> string => {
  let s: string = n.two().0
  "x"
}`, false)
	assertErrorsAre(t, res, "s: cannot assign i64 to string")
}
