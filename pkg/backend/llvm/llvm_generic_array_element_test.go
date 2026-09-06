package llvm

import (
	"strings"
	"testing"
)

// A generic type as an **array element** — `[]Maybe<i64>`, `[]Slot<k, v>`.
//
// These typechecked and then died in the backend with `unknown named type "Maybe"`, which
// is the same failure `propagateInstantiation` was written to fix in return and annotated-
// `let` position. The cause is the one that comment describes: a construction that solves
// only some of its parameters stays the bare declaration on purpose — `None` fixes nothing
// and `Ok(v)` fixes `t` but not `e` — so the instantiation has to come from the context.
// Every site that pushes a context into a construction pairs `propagateExpectedType` with
// `propagateInstantiation`; the two array arms pushed only the width, so the element kept
// a layout-less `Maybe` and the backend had nothing to lower.
//
// Element position is where it bites hardest: every open-addressing table is a
// `[]Maybe<Entry>` or a `[]Slot<k, v>`, so a hash map could not be written at all.
func TestExec_GenericTypeAsArrayElement(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			// A literal whose elements are a *mix* of solved and unsolved
			// constructions: `Some(1)` fixes `t` by itself, `None` cannot.
			name: "dynamic array of Maybe from an element list",
			src: `
  let xs: []Maybe<i64> = [Some(1), None]
  match xs[0] { Some(v) => println(v), None => println("-") }
  match xs[1] { Some(v) => println(v), None => println("-") }`,
			want: "1\n-",
		},
		{
			// The repeat form, which is how a table is actually sized. Its single
			// value solves nothing at all, so it depends entirely on the context.
			name: "repeat-initialized table of None",
			src: `
  var xs: []Maybe<i64> = [None; 4]
  xs[2] = Some(9)
  match xs[2] { Some(v) => println(v), None => println("-") }
  match xs[0] { Some(v) => println(v), None => println("-") }`,
			want: "9\n-",
		},
		{
			// Fixed arrays take the same path and had the same failure.
			name: "fixed array of Maybe",
			src: `
  let xs: [2]Maybe<i64> = [Some(3), None]
  match xs[0] { Some(v) => println(v), None => println("-") }`,
			want: "3",
		},
		{
			// `Result` is the two-parameter case: neither `Ok` nor `Err` solves both,
			// so *every* element needs the context rather than just the nullary one.
			// It failed with a different message ("type variable \"t\" has no concrete
			// type here"), which is the same cause reached one step further along.
			name: "array of Result",
			src: `
  let xs: []Result<i64, string> = [Ok(5), Err("e")]
  match xs[0] { Ok(v) => println(v), Err(e) => println(e) }
  match xs[1] { Ok(v) => println(v), Err(e) => println(e) }`,
			want: "5\ne",
		},
		{
			// Not a `Maybe` special case: a user's own generic data type failed
			// identically, which is what shows this is about instantiation and not
			// about the prelude.
			name: "a user-declared generic data type",
			src: `
  var xs: []Slot<string, i64> = [Empty; 4]
  xs[0] = Full("a", 7)
  match xs[0] { Full(k, v) => println("${k}=${v}"), Empty => println("-") }`,
			want: "a=7",
		},
		{
			// The repeat form *does* reach a narrower width, because its single value
			// solves nothing and so takes the context wholesale.
			//
			// The element-list form does not yet: `[]Maybe<u8> = [Some(200), None]` is
			// still refused with "cannot assign StaticArray<Maybe, 2> to
			// DynamicArray<Maybe<u8>>". That is a separate gap one step earlier — the
			// literal's *own* inferred type is settled from its elements before the
			// annotation narrows them, so a solved element (`Some(200)`, i64 by
			// default) and an unsolved one (`None`) join to a bare `Maybe` that the
			// annotation is then compared against. Left untested rather than pinned as
			// correct: it is a bug, not a rule.
			name: "a narrower payload width, via repeat",
			src: `
  var xs: []Maybe<u8> = [None; 2]
  xs[0] = Some(200)
  match xs[0] { Some(v) => println(v), None => println("-") }`,
			want: "200",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "data Slot<k, v> = Empty | Full(k, v)\nlet main = () -> void => {" + c.src + "\n}\n"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// The shape the fix was for: a generic open-addressing hash map with linear probing,
// whose table is a `[]Maybe<Entry<k, v>>` and whose key type is bounded by a trait the
// program declares.
//
// Note there is no equality bound. `==` is structural by default and works on a bare type
// variable — `Eq` *overrides* it rather than enabling it (ordering.lyra) — so `where k:
// Hash` alone is enough, and `where k: Eq` would in fact be unsatisfiable, the prelude
// shipping no `Eq` impls.
func TestExec_GenericHashMapOverAnArrayOfMaybe(t *testing.T) {
	t.Parallel()
	src := `
pub trait Hash { pure hash: (Self) -> u64 }
impl Hash for string {
  hash = pure (self) => {
    var h: u64 = 14695981039346656037
    for b in self.encode_utf8() { h = (h ~ u64(b)).wrapping_mul(1099511628211) }
    h
  }
}
struct Entry<k, v> { key: k, value: v }
struct HashMap<k, v> { slots: []Maybe<Entry<k, v>>, count: i64 }

let insert<k,v> where k: Hash = (self: mut HashMap<k,v>, key: k, value: v) -> void => {
  var i = i64(key.hash() % u64(self.slots.len()))
  for {
    match self.slots[i] {
      Some(e) => {
        if e.key == key { self.slots[i] = Some(Entry { key: key, value: value }); return }
      },
      None => {
        self.slots[i] = Some(Entry { key: key, value: value })
        self.count = self.count + 1
        return
      },
    }
    i = (i + 1) % self.slots.len()
  }
}

let get<k,v> where k: Hash = (self: HashMap<k,v>, key: k) -> Maybe<v> => {
  var i = i64(key.hash() % u64(self.slots.len()))
  for _ in 0..<self.slots.len() {
    match self.slots[i] {
      Some(e) => { if e.key == key { return Some(e.value) } },
      None => { return None },
    }
    i = (i + 1) % self.slots.len()
  }
  None
}

let main = () -> void => {
  var m: HashMap<string, i64> = HashMap { slots: [None; 16], count: 0 }
  m.insert("the", 3)
  m.insert("fox", 1)
  m.insert("the", 4)          // overwrites, does not add
  match m.get("the") { Some(v) => println("the=${v}"), None => println("missing") }
  match m.get("fox") { Some(v) => println("fox=${v}"), None => println("missing") }
  match m.get("cat") { Some(v) => println("cat=${v}"), None => println("cat absent") }
  println("count=${m.count}")
}
`
	want := "the=4\nfox=1\ncat absent\ncount=2"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
