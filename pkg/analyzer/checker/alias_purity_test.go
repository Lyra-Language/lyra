package checker_test

import "testing"

// A variable set to an array shares its buffer, so a write through a local alias of a
// parameter's array grows the caller's (10/19). The purity walk asked only whether the body
// declared the written root, so `var all = xs; all.push(99)` was a local write: W018
// advised `pure` and a `pure` bound accepted it. Sulafat's frame layout grew its sessions'
// blocks a frame at a time this way.

const aliasPrelude = `
struct Bag { items: []i64 }
struct Cursor { data: []u8, at: i64 }
let step = (c: mut Cursor) -> i64 => { c.at += 1; c.at }
let grow = (c: mut Cursor) -> void => { c.data.push(0) }
`

// Every shape that reaches the caller's buffer through a local is refused: a push and an
// element write, a field holding an array, a row of a parameter's array of arrays, an alias
// of an alias, a callee writing into the buffer, and a captured array.
func TestPurity_AWriteThroughAnAliasOfAParameter_Error(t *testing.T) {
	assertPurityCount(t, checkPurity(t, aliasPrelude+`
let shared_list: []i64 = [1]
let push = pure (xs: []i64) -> i64 => {
    var all = xs
    all.push(99)
    all[0] = 7
    all.len()
}
let field = pure (b: Bag) -> i64 => {
    var items = b.items
    items.push(5)
    items.len()
}
let rows = pure (grid: [][]i64) -> i64 => {
    for row in grid {
        var r = row
        r.push(1)
    }
    grid.len()
}
let chain = pure (xs: []i64) -> i64 => {
    var a = xs
    var b = a
    b.push(3)
    b.len()
}
let callee = pure (data: []u8) -> i64 => {
    var c = Cursor { data: data, at: 0 }
    grow(c)
    c.at
}
let captured = pure () -> i64 => {
    var all = shared_list
    all.push(9)
    all.len()
}`), 7)
}

// What stays the function's own is still pure: a copy, a scalar read out, a struct's inline
// field written directly or by a callee that writes only it, an `own` parameter's buffer,
// and a local array of its own.
func TestPurity_AWriteToWhatTheFunctionOwns_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, aliasPrelude+`
let copy = pure (xs: []i64) -> i64 => {
    var c = [...xs]
    c.push(1)
    var n = xs.len()
    n += 1
    c.len() + n
}
let inline_field = pure (data: []u8) -> i64 => {
    var c = Cursor { data: data, at: 0 }
    step(c)
    c.at += 1
    c.at
}
let owned = pure (xs: own []i64) -> i64 => {
    var all = xs
    all.push(1)
    all.len()
}
let fresh = pure () -> i64 => {
    var a: []i64 = [1, 2]
    var b = a
    b.push(3)
    b.len()
}`), 0)
}

// The advice follows: a function writing through an alias is not a W018 candidate.
func TestMissingPureBound_AWriteThroughAnAlias(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
let grows = (xs: []i64) -> i64 => {
    var all = xs
    all.push(99)
    all.len()
}
let copies = (xs: []i64) -> i64 => {
    var all = [...xs]
    all.push(99)
    all.len()
}
`), "copies")
}

// Only a literal's parts whose own type shares a buffer can alias, and an array literal is a
// new buffer: Sheliak's `var kept: []string = [path]` and a struct literal of scalars and
// fresh arrays (`script_bus`) were refused by the first cut. An element of an array literal
// still reaches its source when a write goes into it.
func TestPurity_ALiteralSharesOnlyWhatItHolds(t *testing.T) {
	assertPurityCount(t, checkPurity(t, `
struct Bus { log: []string, level: u8 }
let recent = pure (path: string, others: []string) -> []string => {
    var kept: []string = [path]
    for o in others { kept.push(o) }
    kept
}
let bus = pure (level: u8) -> Bus => {
    var b = Bus { log: [], level: level }
    b.log.push("start")
    b
}
let outer = pure (xs: []i64) -> i64 => {
    var rows = [xs]
    rows.push([1])
    rows.len()
}`), 0)
	assertPurityCount(t, checkPurity(t, `
let into_element = pure (xs: []i64) -> i64 => {
    var rows = [xs]
    rows[0].push(1)
    rows.len()
}`), 1)
}

// A write into a plain parameter's buffer lands on whatever the caller passes, as a `mut`
// parameter's does, so it is charged at the call: free on the caller's own fresh value (a
// clone, a temporary), passed on through the caller's own parameter (Vega's `refused_at`
// runs `applied` on a clone, and is pure; `applied` itself is not).
func TestPurity_AWriteIntoAPlainParameterIsChargedByItsArgument(t *testing.T) {
	src := `
let grow = (xs: []i64) -> i64 => {
    var all = xs
    all.push(1)
    all.len()
}
let on_a_clone = pure (xs: []i64) -> i64 => grow([...xs])
let on_a_temporary = pure () -> i64 => grow([1, 2])
let on_a_local = pure () -> i64 => {
    var mine: []i64 = [1]
    grow(mine)
}
`
	assertPurityCount(t, checkPurity(t, src), 0)
	assertPurityMessages(t, checkPurity(t, src+`
let passes_its_own = pure (xs: []i64) -> i64 => grow(xs)
`), `passes parameter "xs" to "grow", which writes into its buffer`)
}
