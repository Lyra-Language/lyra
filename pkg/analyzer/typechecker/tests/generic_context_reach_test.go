package typechecker_test

import "testing"

// Positions where a type the program had already stated did not reach a value that needed
// it, found by `ui` (10/27, and the enclosing function's own variable 11/18). Each was a
// turbofish or a binding written only to carry the type in.
func TestGenericContext_ReachesTheValue(t *testing.T) {
	for name, src := range map[string]string{
		// The place's type is the value's context while it is inferred.
		"a field assignment": `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Host { cache: Store<string, i64> }
let f = (h: Host) -> void => {
    var g = h
    g.cache = store_new()
}`,
		// A settled field goes first, in its own context; the rest wait for it.
		"a generic struct literal's field": `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Host<e> { cache: Store<string, e>, tag: e }
let f = () -> i64 => {
    let h = Host { cache: store_new(), tag: 1 }
    h.tag
}`,
		// The annotation settles what no field does.
		"a generic struct literal's annotation": `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<e> { cache: Store<string, e> }
let f = () -> void => {
    let a: Only<i64> = Only { cache: store_new() }
}`,
		// An empty array solves nothing, so the return type can complete the nullary `L`.
		"a nullary constructor beside an empty array": `
data K<e> = L | Ev(e)
struct Nd<e> { k: K<e>, xs: []e }
let mk<e> = () -> Nd<e> => Nd { k: L, xs: [] }`,
		// Each literal gets its own copy of a leaf default.
		"a default at two instantiations": `
data Opt<e> = Has(e) | Nope
struct Laid<e> { v: e }
struct Host<e> { shown: Opt<Laid<e>> = Nope, n: i64 }
let f = () -> i64 => {
    let a: Host<i64> = Host { n: 1 }
    let b: Host<string> = Host { n: 2 }
    a.n + b.n
}`,
		// A lambda payload is elaborated from its field, generic or not.
		"an untyped lambda payload": `
struct Theme { dark: bool }
struct Sheet { n: i64 }
data Rule = Scope((Theme) -> Sheet) | Plain(Sheet)
data Handler<e> = OnKey((i64) -> e) | Idle
let f = (sheet: Sheet) -> Rule => Scope((_) => sheet)
let g = () -> Handler<i64> => OnKey((k) => k * 2)`,
		// A typed sibling is the context for an element that needs one.
		"a generic call beside a typed sibling": `
data Opt<e> = Has(e) | Nope
struct Node<e> { ev: Opt<e> }
data Ev = Click
let boxed = () -> Node<Ev> => Node { ev: Has(Click) }
let sized<e> = () -> Node<e> => Node { ev: Nope }
let count<e> = (xs: []Node<e>) -> i64 => xs.len()
let f = () -> i64 => count([boxed(), sized()])`,
		// A method whose return settles its receiver takes the context to it.
		"a method's receiver": `
data Opt<e> = Has(e) | Nope
struct Node<e> { ev: Opt<e> }
data Ev = Click
let text<e> = (s: string) -> Node<e> => Node { ev: Nope }
let joined<t> = (self: [][]t) -> []t => {
    var out: []t = []
    for xs in self { for x in xs { out.push(x) } }
    out
}
let f = () -> []Node<Ev> => [[text("a")], [text("b")]].joined()`,
		// The function's own variable is a type in its body, so its return type is a
		// context naming it (11/18).
		"a struct literal under the function's own variable": `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<e> { cache: Store<string, e> }
let mk<e> = () -> Only<e> => Only { cache: store_new() }`,
		"a constructor's payload under the function's own variable": `
data Opt<e> = Has(e) | Nope
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<e> { cache: Store<string, e> }
let mk<e> = () -> Opt<Only<e>> => Has(Only { cache: store_new() })`,
		// A lambda takes the function's own variable from the type it is written against:
		// a binding's annotation, a return, a field — and a call's return context (11/20).
		"a lambda in a binding naming the function's variable": `
data Opt<e> = Has(e) | Nope
let make<t> = (x: t) -> Opt<t> => {
    let h: (i64) -> Opt<t> = (_) => Nope
    h(1)
}`,
		"a lambda returned under the function's variable": `
data Opt<e> = Has(e) | Nope
let make<t> = (x: t) -> (i64) -> Opt<t> => (_: i64) => Nope`,
		"a lambda in a field naming the function's variable": `
data Opt<e> = Has(e) | Nope
struct Holder<t> { f: (i64) -> Opt<t> }
let make<t> = (x: t) -> Holder<t> => Holder { f: (_: i64) => Nope }`,
		"a call whose return is the function's variable": `
data Opt<e> = Has(e) | Nope
let apply<u> = (f: (i64) -> Opt<u>) -> Opt<u> => f(1)
let settled<w> = (x: w) -> Opt<w> => apply((_: i64) => Nope)`,
		"a local generic's own variable": `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<e> { cache: Store<string, e> }
let f = () -> i64 => {
    let mk<e> = () -> Only<e> => Only { cache: store_new() }
    let o: Only<i64> = mk()
    o.cache.n
}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// What the context must not do: overrule a value. A field that disagrees with the
// annotation is still named precisely (the seed stands back), a chain whose result does
// not settle its receiver keeps the barrier, and a generic call nothing settles is still
// refused — once.
func TestGenericContext_DoesNotOverruleOrInvent(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<e> { cache: Store<string, e> }
let f = () -> void => {
    let b = Only { cache: store_new() }
}`, false)
	assertErrorsAre(t, res,
		"store_new: cannot infer type variables k, v from these arguments; name them explicitly with ::<k, v>")
}

// A lambda passed where nothing settles the callee's variable takes no type for it, though
// the enclosing function has a variable of that name: `apply`'s `u` is open, so `Nope` is
// still unsolved (11/20).
func TestGenericContext_ALambdaTakesNoOpenVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<e> = Has(e) | Nope
let apply<u> = (f: (i64) -> Opt<u>) -> Opt<u> => f(1)
let unsettled<u> = (x: u) -> i64 => {
    let r = apply((_: i64) => Nope)
    0
}`, false)
	if len(res.errors) == 0 {
		t.Fatal("apply's open u was taken for the enclosing function's")
	}
}

// A call argument's context names the callee's open variables, and one sharing its name with
// the enclosing function's is not that variable: `take<t>`'s `t` is solved by nothing here, so
// the literal stays unsolved rather than taking `mk<t>`'s (11/18).
func TestGenericContext_ACalleesOpenVariableIsNotTheEnclosingOne(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Store<k, v> { n: i64 }
let store_new<k, v> = () -> Store<k, v> => Store { n: 0 }
struct Only<t> { cache: Store<string, t> }
let take<t> = (o: Only<t>) -> i64 => o.cache.n
let mk<t> = (x: t) -> i64 => take(Only { cache: store_new() })`, false)
	assertErrorsAre(t, res,
		"store_new: cannot infer type variables k, v from these arguments; name them explicitly with ::<k, v>",
		"take: cannot infer type variable t from these arguments")
}
