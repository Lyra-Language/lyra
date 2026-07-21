package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// Aggregate-field drop: a managed value stored *inside* a `shared` box — a string
// field, a nested `shared` value, the tail of a recursive list — is released when
// the box that owns it dies, via the per-type drop glue (drop.go) passed as
// lyra_rc_release's drop_fn.
//
// Each case builds such a structure, reads through it (so a premature free
// corrupts the answer), and lets it die. A wrong exit code means something was
// freed too early; a crash under ASan means it was freed twice or used after free.
// Leak-freedom is checked separately in TestEmit_AggregateDropIR, since macOS ASan
// doesn't run the leak sanitizer.
var aggregateDropCases = []struct {
	name string
	src  string
	want int
}{
	{
		// The base case: a boxed struct owning a heap string. The box's release runs
		// @lyra_drop_Person, which releases the string's own box.
		"string field in a shared struct",
		`struct Person { name: string, }
		 let main = () -> u8 => {
		   let p: shared Person = Person { name: "a" ++ "b" }
		   if p.name == "ab" { 42 } else { 0 }
		 }`,
		42,
	},
	{
		// Two managed fields in one payload — the glue must drop both, not just the
		// first.
		"two string fields in a shared struct",
		`struct Pair { a: string, b: string, }
		 let main = () -> u8 => {
		   let p: shared Pair = Pair { a: "x" ++ "y", b: "z" ++ "w" }
		   if p.a == "xy" && p.b == "zw" { 42 } else { 0 }
		 }`,
		42,
	},
	{
		// A managed field sitting next to unmanaged ones: the glue must index the
		// right fields (it skips the scalars entirely).
		"mixed managed and scalar fields",
		`struct Rec { n: i64, name: string, flag: bool, }
		 let main = () -> u8 => {
		   let r: shared Rec = Rec { n: 40, name: "o" ++ "k", flag: true }
		   if r.name == "ok" { u8(r.n + 2) } else { 0 }
		 }`,
		42,
	},
	{
		// A `data` payload: the glue switches on the tag, so only the live variant's
		// fields are dropped. The `Empty` arm's blob is undefined — reading it would
		// be a wild free.
		"string payload in a shared data value",
		`data Msg = Empty | Text(string)
		 let main = () -> u8 => {
		   let m: shared Msg = Text("h" ++ "i")
		   match m { Text(s) => if s == "hi" { 42 } else { 0 }, Empty => 0 }
		 }`,
		42,
	},
	{
		// The nullary variant of a type that *has* a managed variant: the tag switch
		// must fall through to the exit without touching the undefined payload blob.
		"nullary variant of a droppable data type",
		`data Msg = Empty | Text(string)
		 let main = () -> u8 => {
		   let m: shared Msg = Empty
		   match m { Text(s) => 0, Empty => 42 }
		 }`,
		42,
	},
	{
		// Nesting: the outer box's glue releases the inner box, whose *own* glue
		// releases the string. Freeing is a chain of drop_fn calls, one box at a time.
		"shared value nested in a shared value",
		`struct Inner { name: string, }
		 struct Outer { inner: shared Inner, }
		 let main = () -> u8 => {
		   let o: shared Outer = Outer { inner: Inner { name: "d" ++ "eep" } }
		   if o.inner.name == "deep" { 42 } else { 0 }
		 }`,
		42,
	},
	{
		// A string reached through a by-value nested struct: sizing the box payload
		// needs the UnresolvedType field resolved (resolveForLayout), and the glue then
		// has to descend *into* the inline struct to find the string — one box, but two
		// levels of aggregate between it and the managed value.
		"string inside a by-value nested struct",
		`struct Inner { name: string, }
		 struct Outer { inner: Inner, }
		 let main = () -> u8 => {
		   let o: shared Outer = Outer { inner: Inner { name: "d" ++ "eep" } }
		   if o.inner.name == "deep" { 42 } else { 0 }
		 }`,
		42,
	},
	{
		// The headline case: a recursive list of strings. One self-referential glue
		// function frees the whole spine — each cell's release drops its string and
		// its tail, and the tail's release re-enters the same glue.
		"recursive list of strings is freed end to end",
		`data List = Nil | Cons(string, shared List)
		 let main = () -> u8 => {
		   let xs: shared List = Cons("a" ++ "b", Cons("c" ++ "d", Nil))
		   match xs { Cons(h, t) => if h == "ab" { 42 } else { 0 }, Nil => 0 }
		 }`,
		42,
	},
	{
		// A borrowed (`ref`) parameter: the callee must not drop the caller's fields.
		// If it did, the caller's later read would be a use-after-free.
		"borrowed shared value keeps its fields",
		`struct Person { name: string, }
		 let peek = (p: ref shared Person) -> u8 => if p.name == "ab" { 1 } else { 0 }
		 let main = () -> u8 => {
		   let p: shared Person = Person { name: "a" ++ "b" }
		   let first = peek(p)
		   if first == 1 && p.name == "ab" { 42 } else { 0 }
		 }`,
		42,
	},
	{
		// An `own` parameter consumes the argument, so the *callee* drops the box and
		// its fields. The caller transferred at the last use and must not drop again.
		"own shared parameter drops the fields once",
		`struct Person { name: string, }
		 let consume = (p: own shared Person) -> u8 => if p.name == "ab" { 42 } else { 0 }
		 let main = () -> u8 => {
		   let p: shared Person = Person { name: "a" ++ "b" }
		   consume(p)
		 }`,
		42,
	},
	{
		// Drop-reuse interaction: the reclaimed shell's *old* payload is dropped at
		// the merge, after the arms have duplicated what they bound. Dropping it at
		// reclaim time instead would free the string this arm is about to compare.
		"drop-reuse frees the old payload, not the arm's binding",
		`data Msg = Empty | Text(string)
		 let relabel = (m: own shared Msg) -> shared Msg => match m {
		   Text(s) => Text("n" ++ "ew"),
		   Empty => Empty
		 }
		 let main = () -> u8 => {
		   let a: shared Msg = Text("o" ++ "ld")
		   let b: shared Msg = relabel(a)
		   match b { Text(s) => if s == "new" { 42 } else { 0 }, Empty => 0 }
		 }`,
		42,
	},
	{
		// Repeated construction and destruction in a loop: a leak or a double free
		// compounds per iteration rather than staying invisible.
		"boxed string built and dropped in a loop",
		`struct Person { name: string, }
		 let once = () -> i64 => {
		   let p: shared Person = Person { name: "a" ++ "b" }
		   if p.name == "ab" { 1 } else { 0 }
		 }
		 let main = () -> u8 => {
		   var hits = 0
		   for var i = 0; i < 6; i += 1 {
		     hits += once()
		   }
		   u8(hits * 7)
		 }`,
		42,
	},
}

// TestExec_AggregateDrop runs each case and checks its result. A field freed while
// still reachable corrupts the comparison; a double free crashes.
func TestExec_AggregateDrop(t *testing.T) {
	for _, c := range aggregateDropCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_AggregateDropASan is the memory-safety check: dropping an aggregate's
// fields is where a double free or use-after-free would appear (the field is
// reachable from both the dying box and whatever bound it), and ASan aborts on
// either. Skips if the toolchain can't build an ASan binary.
func TestExec_AggregateDropASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range aggregateDropCases {
		if got := buildAndRunASan(t, clang, c.src); got != c.want {
			t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_AggregateDropIR pins the glue's shape: that it is generated and wired
// into the release, that it is *not* generated for a payload owning nothing, and —
// for a straight-line program — that allocations and releases still balance. macOS
// ASan can't see leaks, so this static conservation check is what proves the field
// is actually freed rather than merely not double-freed.
func TestEmit_AggregateDropIR(t *testing.T) {
	emit := func(src string) string {
		got, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return got
	}

	// A boxed struct with a string field: one glue function, passed as the box's
	// drop_fn. Two allocations (the concat box, the Person box) and two releases —
	// the binding's, and the string's from inside the glue.
	withString := emit(`struct Person { name: string, }
	 let main = () -> u8 => {
	   let p: shared Person = Person { name: "a" ++ "b" }
	   if p.name == "ab" { 1 } else { 0 }
	 }`)
	if n := strings.Count(withString, "define void @lyra_drop"); n != 1 {
		t.Errorf("string field: want exactly 1 drop glue function, got %d", n)
	}
	if !strings.Contains(withString, "bitcast (void (i8*)* @lyra_drop") {
		t.Error("string field: the glue is never passed as a drop_fn")
	}
	if allocs, releases := strings.Count(withString, "call i8* @lyra_rc_alloc"),
		strings.Count(withString, "call void @lyra_rc_release"); allocs != releases {
		t.Errorf("string field: %d allocations vs %d releases; they must balance", allocs, releases)
	}

	// The same shape with a scalar field owns nothing, so no glue is generated and
	// the box's release keeps a null drop_fn — the glue is pay-for-what-you-use.
	scalarOnly := emit(`struct Box { v: u8, }
	 let main = () -> u8 => {
	   let b: shared Box = Box { v: 42 }
	   b.v
	 }`)
	if strings.Contains(scalarOnly, "define void @lyra_drop") {
		t.Error("scalar-only payload: no drop glue should be generated")
	}

	// A recursive type gets exactly *one* glue function that calls itself through
	// the tail's release — generation must not unroll (or diverge) on the cycle.
	recursive := emit(`data List = Nil | Cons(string, shared List)
	 let main = () -> u8 => {
	   let xs: shared List = Cons("a" ++ "b", Nil)
	   match xs { Cons(h, t) => 1, Nil => 0 }
	 }`)
	if n := strings.Count(recursive, "define void @lyra_drop"); n != 1 {
		t.Fatalf("recursive list: want exactly 1 (self-referential) glue function, got %d", n)
	}
	glue := recursive[strings.Index(recursive, "define void @lyra_drop"):]
	glue = glue[:strings.Index(glue, "\n}\n")]
	if !strings.Contains(glue, "@lyra_drop") || strings.Count(glue, "@lyra_rc_release") != 2 {
		t.Errorf("recursive list: glue should release both the string and the tail (self-referentially):\n%s", glue)
	}
}
