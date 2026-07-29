package llvm

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// Deep-retain-on-copy: copying a plain **stack** aggregate (struct / tuple / [N]T)
// retains every managed value it transitively owns, and the copy's death releases
// them again. Before this, a stack aggregate was not an owning slot at all — a copy
// duplicated its fields' fat pointers with no retain — so whoever *did* own them (a
// box's drop glue, an interior assignment) freed them out from under every copy.
//
// Each program below makes a copy, lets one side die or reads both, and checks the
// string is still intact — so it fails on a plain run (wrong exit code) as well as
// aborting under ASan on a use-after-free or double free.
func TestExec_DeepRetainOnCopy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			// The original reproducer: a struct read by value out of a []T box, which
			// then dies and runs its per-element drop glue.
			"aggregate read out of a dying box",
			`struct Person { name: string }
let first = () -> Person => {
  let ps: []Person = [Person { name: "a" ++ "b" }]
  ps[0]
}
let main = () -> u8 => {
  let q = first()
  if q.name == "ab" { 0 } else { 1 }
}`,
		},
		{
			"binding copy, both sides live",
			`struct Person { name: string }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let q = p
  if p.name == "ab" { if q.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			"nested struct copy",
			`struct Person { name: string }
struct Team { lead: Person }
let main = () -> u8 => {
  let t: Team = Team { lead: Person { name: "a" ++ "b" } }
  let u = t
  if u.lead.name == "ab" { 0 } else { 1 }
}`,
		},
		{
			"tuple copy",
			`let main = () -> u8 => {
  let t: (string, i64) = ("a" ++ "b", 7)
  let u = t
  if u.0 == "ab" { 0 } else { 1 }
}`,
		},
		{
			"fixed-size array of aggregates",
			`struct Person { name: string }
let main = () -> u8 => {
  let xs: [2]Person = [Person { name: "a" ++ "b" }, Person { name: "c" ++ "d" }]
  let ys = xs
  if ys[0].name == "ab" { 0 } else { 1 }
}`,
		},
		{
			// The copy flows into another aggregate's field — an owning position, so the
			// nested value must carry its own +1 while the original stays live.
			"aggregate-field initialization from a live copy",
			`struct Person { name: string }
struct Team { lead: Person }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let t: Team = Team { lead: p }
  if p.name == "ab" { if t.lead.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			// The same value copied into two array elements: two retains, and the box's
			// drop glue releases both.
			"array-literal elements from a live copy",
			`struct Person { name: string }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let xs: []Person = [p, p]
  if xs[1].name == "ab" { if p.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			// A bare parameter is a *borrow*: the caller keeps ownership and the callee
			// must not release its by-value copy.
			"borrowed parameter",
			`struct Person { name: string }
let peek = (p: Person) -> u8 => { if p.name == "ab" { 0 } else { 1 } }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let r = peek(p)
  if p.name == "ab" { r } else { 9 }
}`,
		},
		{
			// An `own` parameter is consumed: the caller transfers and the callee
			// releases what the aggregate owns at its exit.
			"owned parameter consumed by the callee",
			`struct Person { name: string }
let consume = (p: own Person) -> u8 => { if p.name == "ab" { 0 } else { 1 } }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  consume(p)
}`,
		},
		{
			// Exercises emitRetainData's tag switch: only the live variant's fields are
			// retained, mirroring emitDropData.
			"data payload containing an aggregate",
			`struct Person { name: string }
data Box = Wrap(Person) | Empty
let main = () -> u8 => {
  let b: Box = Wrap(Person { name: "a" ++ "b" })
  let c = b
  match c { Wrap(p) => if p.name == "ab" { 0 } else { 1 }, Empty => 2 }
}`,
		},
		{
			// A match arm binding is a borrow (the scrutinee still owns it), but binding
			// it onward into a `let` is an owning copy that must retain.
			"match arm binding copied into a binding",
			`struct Person { name: string }
data Box = Wrap(Person) | Empty
let main = () -> u8 => {
  let b: Box = Wrap(Person { name: "a" ++ "b" })
  match b { Wrap(p) => { let q = p
    if q.name == "ab" { 0 } else { 1 } }, Empty => 2 }
}`,
		},
		{
			// The loop variable borrows each element; the array still owns it.
			"for-in over an array of aggregates",
			`struct Person { name: string }
let main = () -> u8 => {
  let xs: []Person = [Person { name: "a" ++ "b" }]
  var n: u8 = 9
  for x in xs { if x.name == "ab" { n = 0 } else { n = 1 } }
  n
}`,
		},
		{
			// Reassigning an owning binding releases what the old value owned; the
			// original binding it was copied from must survive.
			"reassignment releases the old aggregate",
			`struct Person { name: string }
let main = () -> u8 => {
  let a: Person = Person { name: "a" ++ "b" }
  let b: Person = Person { name: "c" ++ "d" }
  var q: Person = a
  q = b
  if q.name == "cd" { if a.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			// Both branches are coerced to +1 so the merged value can be released once.
			"if-merge of two aggregates",
			`struct Person { name: string }
let main = () -> u8 => {
  let a: Person = Person { name: "a" ++ "b" }
  let b: Person = Person { name: "c" ++ "d" }
  let q = if 1 > 0 { a } else { b }
  if q.name == "ab" { if a.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			// A stack aggregate read by value out of a `shared` box — the same hazard as
			// the []T case, since the box's drop glue owns the inlined struct's fields.
			"aggregate read out of a shared box",
			`struct Person { name: string }
struct Wrapper { p: Person }
let main = () -> u8 => {
  let w: shared Wrapper = Wrapper { p: Person { name: "a" ++ "b" } }
  let q = w.p
  if q.name == "ab" { 0 } else { 1 }
}`,
		},
		{
			// An owned aggregate *temporary* in a borrowing position must be deep-released
			// after the statement — the field read borrows out of a value nothing binds.
			"owned aggregate temporary read and discarded",
			`struct Person { name: string }
let mk = () -> Person => { Person { name: "a" ++ "b" } }
let main = () -> u8 => { if mk().name == "ab" { 0 } else { 1 } }`,
		},
		{
			// The same, as a borrowed argument: the callee will not free it, so the
			// caller must.
			"freshly constructed aggregate passed to a borrow",
			`struct Person { name: string }
let peek = (p: Person) -> u8 => { if p.name == "ab" { 0 } else { 1 } }
let main = () -> u8 => { peek(Person { name: "a" ++ "b" }) }`,
		},
		{
			// `own` consumes, and the binding is read *after* the call — so the argument
			// must dup rather than move, or the later read is a use-after-free.
			"owned argument with the binding still live afterwards",
			`struct Person { name: string }
let consume = (p: own Person) -> u8 => { if p.name == "ab" { 0 } else { 1 } }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let r = consume(p)
  if p.name == "ab" { r } else { 9 }
}`,
		},
		{
			// A stack aggregate inside a `shared data` payload: the box's drop glue frees
			// the struct's field, and the arm binding borrows it.
			"aggregate inside a shared data payload",
			`struct Person { name: string }
data List = Nil | Cons(Person, shared List)
let main = () -> u8 => {
  let xs: shared List = Cons(Person { name: "a" ++ "b" }, Nil)
  match xs { Cons(h, t) => if h.name == "ab" { 0 } else { 1 }, Nil => 2 }
}`,
		},
		{
			// Interior assignment followed by a copy: the copy must retain the *new*
			// field value, which the assignment installed with a fresh +1.
			"interior assignment then copy",
			`struct Person { name: string }
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  p.name = "x" ++ "y"
  let q = p
  if q.name == "xy" { if p.name == "xy" { 0 } else { 1 } } else { 2 }
}`,
		},
		{
			// Repeated copy into the same slot: each iteration dups the source and
			// releases what the slot held, so the count must come back to where it
			// started rather than drifting up (leak) or down (use-after-free).
			"repeated copy-and-reassign in a loop",
			`struct Person { name: string }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  var q: Person = p
  for var i: u8 = 0; i < 3; i += 1 { q = p }
  if q.name == "ab" { if p.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		},
	}

	clang, clangErr := exec.LookPath("clang")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != 0 {
				t.Errorf("expected exit 0 (managed field intact), got %d", got)
			}
			if clangErr != nil || !asanAvailable(t, clang) {
				t.Skip("ASan runtime not available; ran without it")
			}
			if got := buildAndRunASan(t, clang, c.src); got != 0 {
				t.Errorf("under ASan: expected exit 0, got %d", got)
			}
		})
	}
}

// TestEmit_DeepRetainConservation is the leak half of the story. ASan on macOS
// cannot detect leaks, and macOS `leaks` only reports *unreachable* memory — a
// never-freed box still referenced by a live stack frame goes unreported — so
// neither tool can tell "freed exactly once" from "never freed". This counts the
// ref-counting traffic the program actually performs instead: for a straight-line
// program, allocations + retains must equal releases, or a box's count never reaches
// zero (a leak) or drops below it (the use-after-free ASan would catch).
//
// The expected numbers below are hand-derived per program, so the test pins the
// intended ownership decisions and not merely their sum — a compensating pair of
// errors would still show up as the wrong retain or release count.
func TestEmit_DeepRetainConservation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                      string
		src                       string
		allocs, retains, releases int
		why                       string
	}{
		{
			name: "copy with both sides live",
			src: `struct Person { name: string }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let q = p
  if p.name == "ab" { if q.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
			allocs: 1, retains: 1, releases: 2,
			why: "`p` is read after the copy, so the copy dups; both bindings release at scope exit",
		},
		{
			name: "copy at the last use transfers",
			src: `struct Person { name: string }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let q = p
  if q.name == "ab" { 0 } else { 1 }
}`,
			allocs: 1, retains: 0, releases: 1,
			why: "the copy is `p`'s last use, so the reference moves (Perceus transfer) and `p`'s slot is retired",
		},
		{
			name: "nested struct copy reaches the inner string",
			src: `struct Person { name: string }
struct Team { lead: Person }
let main = () -> u8 => {
  let t: Team = Team { lead: Person { name: "a" ++ "b" } }
  let u = t
  if t.lead.name == "ab" { if u.lead.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
			allocs: 1, retains: 1, releases: 2,
			why: "the retain glue must walk Team -> Person -> string, not stop at the outer struct",
		},
		{
			name: "field initialization from a live copy",
			src: `struct Person { name: string }
struct Team { lead: Person }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let t: Team = Team { lead: p }
  if p.name == "ab" { if t.lead.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
			allocs: 1, retains: 1, releases: 2,
			why: "an aggregate field is an owning position, so the nested copy dups while `p` stays live",
		},
		{
			name: "borrowed parameter costs no refcount traffic",
			src: `struct Person { name: string }
let peek = (p: Person) -> u8 => { if p.name == "ab" { 0 } else { 1 } }
let main = () -> u8 => {
  let p: Person = Person { name: "a" ++ "b" }
  let r = peek(p)
  if p.name == "ab" { r } else { 9 }
}`,
			allocs: 1, retains: 0, releases: 1,
			why: "a bare parameter borrows: the caller neither dups nor the callee releases",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ir, err := emitSource(t, c.src)
			if err != nil {
				t.Fatalf("emit: %v", err)
			}
			allocs, retains, releases := rcTraffic(t, ir)
			if allocs != c.allocs || retains != c.retains || releases != c.releases {
				t.Errorf("rc traffic = %d allocs, %d retains, %d releases; want %d/%d/%d\n(%s)",
					allocs, retains, releases, c.allocs, c.retains, c.releases, c.why)
			}
			if allocs+retains != releases {
				t.Errorf("unbalanced: %d allocs + %d retains != %d releases — a box's count never reaches zero (leak) or goes negative (use-after-free)",
					allocs, retains, releases)
			}
		})
	}
}

var (
	llFuncHeader = regexp.MustCompile(`(?m)^define [^@]*@([A-Za-z0-9_.]+)\(`)
	llGlueCall   = regexp.MustCompile(`call void @(lyra_(?:retain|drop)_[A-Za-z0-9_]+)\(`)
)

// rcTraffic returns how many times a **straight-line** program calls each
// ref-counting primitive at run time.
//
// A plain text count over the module would be wrong in both directions: the glue
// functions (retain.go / drop.go) hold the retains and releases for an aggregate, so
// their bodies are counted once no matter how many times they are called, while a
// glue called from several sites executes several times. So this walks @main's call
// sites and multiplies each glue call by that glue's own primitive count.
//
// Valid only when every call site executes exactly once: no loops, and no `data`
// glue (whose tag switch runs a single arm, which a static count would over-attribute)
// or dynamic-array element glue (whose drop loops over a runtime length). The
// programs above are all straight-line struct/tuple code for that reason. Glue
// functions never call other glue — emitDropValue/emitRetainValue recurse *inline*
// through stack aggregates and stop at managed fields — so one level of walking is
// exact.
func rcTraffic(t *testing.T, ir string) (allocs, retains, releases int) {
	t.Helper()
	bodies := llFuncBodies(ir)
	main, ok := bodies["main"]
	if !ok {
		t.Fatalf("no @main in emitted IR:\n%s", ir)
	}
	count := func(body string) (a, rt, rl int) {
		return strings.Count(body, "call i8* @lyra_rc_alloc"),
			strings.Count(body, "call void @lyra_rc_retain"),
			strings.Count(body, "call void @lyra_rc_release")
	}
	allocs, retains, releases = count(main)
	for _, m := range llGlueCall.FindAllStringSubmatch(main, -1) {
		body, ok := bodies[m[1]]
		if !ok {
			t.Fatalf("@main calls %s but it is not defined in the module", m[1])
		}
		a, rt, rl := count(body)
		allocs, retains, releases = allocs+a, retains+rt, releases+rl
	}
	return allocs, retains, releases
}

// llFuncBodies splits a module's IR text into function name -> body. Bodies run from
// the `define` header to the closing brace at column 0.
func llFuncBodies(ir string) map[string]string {
	out := map[string]string{}
	locs := llFuncHeader.FindAllStringSubmatchIndex(ir, -1)
	for _, loc := range locs {
		name := ir[loc[2]:loc[3]]
		body := ir[loc[1]:]
		if end := strings.Index(body, "\n}"); end >= 0 {
			body = body[:end]
		}
		out[name] = body
	}
	return out
}

// The retain glue must mirror the drop glue exactly: whatever a copy retains, the
// copy's death has to release, or the two drift into a leak (retain without drop) or
// a use-after-free (drop without retain). Rather than trust that by inspection, this
// checks the generated pair for the same program covers the same number of managed
// fields.
func TestEmit_RetainGlueMirrorsDropGlue(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`struct Person { name: string }
struct Team { lead: Person, tag: string }
let main = () -> u8 => {
  let t: Team = Team { lead: Person { name: "a" ++ "b" }, tag: "c" ++ "d" }
  let u = t
  if t.tag == "cd" { if u.lead.name == "ab" { 0 } else { 1 } } else { 2 }
}`,
		`struct Person { name: string }
let main = () -> u8 => {
  let xs: [2]Person = [Person { name: "a" ++ "b" }, Person { name: "c" ++ "d" }]
  let ys = xs
  if xs[1].name == "cd" { if ys[0].name == "ab" { 0 } else { 1 } } else { 2 }
}`,
	} {
		ir, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		bodies := llFuncBodies(ir)
		var retainSites, dropSites int
		var found bool
		for name, body := range bodies {
			switch {
			case strings.HasPrefix(name, "lyra_retain_"):
				retainSites += strings.Count(body, "call void @lyra_rc_retain")
				found = true
			case strings.HasPrefix(name, "lyra_drop_"):
				dropSites += strings.Count(body, "call void @lyra_rc_release")
			}
		}
		if !found {
			t.Errorf("no retain glue generated for a copied aggregate that owns a string:\n%s", src)
			continue
		}
		if retainSites != dropSites {
			t.Errorf("retain glue covers %d managed fields but drop glue covers %d — they must mirror each other:\n%s",
				retainSites, dropSites, src)
		}
	}
}

// A type owning nothing managed must generate no glue and pay no refcount traffic —
// deep ownership stays pay-for-what-you-use, exactly like the drop side.
func TestEmit_NoGlueForUnmanagedAggregate(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `struct Point { x: i64, y: i64 }
let main = () -> u8 => {
  let p: Point = Point { x: 1, y: 2 }
  let q = p
  if q.x == 1 { 0 } else { 1 }
}`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, sym := range []string{"@lyra_retain_", "@lyra_drop_", "@lyra_rc_retain", "@lyra_rc_release"} {
		if strings.Contains(ir, sym) {
			t.Errorf("a scalar-only struct copy should emit no %s", strings.TrimPrefix(sym, "@"))
		}
	}
}

// Guards the invariant the whole design rests on: the ownership pass and the backend
// must agree on which values are owning. The pass mints a +1 where OwnsManaged says
// so, and the backend frames and releases where needsDrop says so — if those two ever
// diverge the result is a leak or a double free, so needsDrop delegates to
// OwnsManaged rather than reimplementing it. This pins the delegation with a shape
// that only the deep answer gets right.
func TestOwnsManaged_MatchesNeedsDrop(t *testing.T) {
	t.Parallel()
	src := `struct Person { name: string }
struct Team { lead: Person }
struct Point { x: u8 }
let main = () -> u8 => {
  let t: Team = Team { lead: Person { name: "a" ++ "b" } }
  let p: Point = Point { x: 0 }
  if t.lead.name == "ab" { p.x } else { 1 }
}`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// Team transitively owns a string, so it gets glue; Point owns nothing, so it
	// must not. Both facts come from the same predicate.
	if !strings.Contains(ir, "Team") {
		t.Fatalf("expected the Team type in the emitted IR:\n%s", ir)
	}
	if n := strings.Count(ir, "define void @lyra_drop"); n != 1 {
		t.Errorf("want exactly 1 drop glue (Team's, reaching the nested string), got %d", n)
	}
	if strings.Contains(ir, fmt.Sprintf("%s_Point", "lyra_drop")) {
		t.Error("a scalar-only struct must not get drop glue")
	}
}
