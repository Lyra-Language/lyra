package llvm

import (
	"strings"
	"testing"
)

// `match` on a data value lowers to a tag switch: store the scrutinee, load its
// tag, switch to a block per arm, and (for a data pattern) reinterpret the
// payload blob as that variant's payload struct and bind the fields. The arms
// feed a merge phi, so a match is a value. This is what finally makes a
// constructed data value observable end to end — construct, match, extract a
// field, return it — so these are all buildAndRun.
func TestExec_DataMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Enum: the tag alone selects the arm; arm literals take the return width.
		{
			"enum",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 1,
			   Green => 2,
			   Blue => 3,
			 }
			 let main = () -> u8 => f(Green)`,
			2,
		},
		// Single payload, bare binding `Some x`: the payload field is extracted
		// and returned.
		{
			"single payload binding",
			`data Maybe = None | Some(u8)
			 let f = (m: Maybe) -> u8 => match m {
			   None => 0,
			   Some x => x,
			 }
			 let main = () -> u8 => f(Some(42))`,
			42,
		},
		// Multi-field payload `Rect(w, h)`: both fields bind and feed a computation.
		{
			"multi-field payload",
			`data Shape = Circle(u8) | Rect(u8, u8)
			 let f = (s: Shape) -> u8 => match s {
			   Circle(r) => r,
			   Rect(w, h) => w + h,
			 }
			 let main = () -> u8 => f(Rect(20, 22))`,
			42,
		},
		// Wildcard catch-all as the switch default.
		{
			"wildcard catch-all",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 7,
			   _ => 0,
			 }
			 let main = () -> u8 => f(Blue)`,
			0,
		},
		// Identifier catch-all binds the whole scrutinee (unused here) and is the
		// default arm.
		{
			"identifier catch-all",
			`data Color = Red | Green | Blue
			 let f = (c: Color) -> u8 => match c {
			   Red => 1,
			   other => 5,
			 }
			 let main = () -> u8 => f(Blue)`,
			5,
		},
		// Construction and match composed inline: Some(9) is built and immediately
		// matched, x binds to 9.
		{
			"construct then match inline",
			`data Maybe = None | Some(u8)
			 let main = () -> u8 => match Some(9) {
			   None => 0,
			   Some x => x + 1,
			 }`,
			10,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_DataMatchIR pins the match shape: a switch on the loaded tag, and an
// extractvalue reading a bound payload field.
func TestEmit_DataMatchIR(t *testing.T) {
	got, err := emitSource(t, `data Maybe = None | Some(u8)
	 let f = (m: Maybe) -> u8 => match m {
	   None => 0,
	   Some x => x,
	 }
	 let main = () -> u8 => f(Some(7))`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"switch",      // the tag dispatch
		"extractvalue", // reading the bound payload field
	} {
		if !strings.Contains(got, want) {
			t.Errorf("match IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_MatchGuard_Deferred: an arm guard (`if x > 0`) isn't lowered yet, so
// it errors loudly rather than silently dropping the condition.
func TestEmit_MatchGuard_Deferred(t *testing.T) {
	src := `data Maybe = None | Some(u8)
	 let f = (m: Maybe) -> u8 => match m {
	   Some x if x > 0 => x,
	   _ => 0,
	 }
	 let main = () -> u8 => f(Some(5))`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: match guards are not implemented yet")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Errorf("expected a guard error, got: %v", err)
	}
}

// TestEmit_MatchLiteralPayload_Deferred: a value-testing payload sub-pattern (a
// literal `Some(0)`) in a `data` match arm isn't implemented — the tag switch
// selects the Some variant but can't also test the payload and fall through to
// another Some arm, which would need a per-variant test ladder. (Destructuring
// payloads — `W((a, b))`, `Boxed({ x, y })` — and a data pattern nested in an
// aggregate — `(c, Some(x))` — now lower.)
func TestEmit_MatchLiteralPayload_Deferred(t *testing.T) {
	src := `data Maybe = None | Some(u8)
	 let f = (m: Maybe) -> u8 => match m {
	   Some(0) => 1,
	   Some(x) => x,
	   None => 9,
	 }
	 let main = () -> u8 => 0`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: a value-testing data payload sub-pattern is not implemented yet")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected a not-implemented error, got: %v", err)
	}
}

// `match` on a bool or integer scrutinee lowers to an if-else ladder of
// comparisons feeding a merge phi. These run the compiled program so a wrong
// predicate or arm selection shows up as the wrong exit code.
func TestExec_ScalarMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Integer literal arms: the matching literal's arm is taken.
		{
			"int literal arm",
			`let f = (n: u8) -> u8 => match n {
			   0 => 10,
			   1 => 20,
			   _ => 30,
			 }
			 let main = () -> u8 => f(1)`,
			20,
		},
		// The wildcard is taken when no literal matches.
		{
			"int wildcard fallthrough",
			`let f = (n: u8) -> u8 => match n {
			   0 => 10,
			   1 => 20,
			   _ => 30,
			 }
			 let main = () -> u8 => f(9)`,
			30,
		},
		// Bool scrutinee: true/false arms are exhaustive (no wildcard needed).
		{
			"bool",
			`let f = (b: bool) -> u8 => match b {
			   true => 1,
			   false => 2,
			 }
			 let main = () -> u8 => f(false)`,
			2,
		},
		// An identifier catch-all binds the scrutinee value and is usable in the arm.
		{
			"identifier catch-all binds the value",
			`let f = (n: u8) -> u8 => match n {
			   0 => 100,
			   other => other + 1,
			 }
			 let main = () -> u8 => f(41)`,
			42,
		},
		// Range pattern (half-open) — the value falls inside the range.
		{
			"range hit",
			`let f = (n: u8) -> u8 => match n {
			   0..<10 => 1,
			   _ => 2,
			 }
			 let main = () -> u8 => f(5)`,
			1,
		},
		// Range pattern — the value is outside, so the wildcard is taken.
		{
			"range miss",
			`let f = (n: u8) -> u8 => match n {
			   0..<10 => 1,
			   _ => 2,
			 }
			 let main = () -> u8 => f(20)`,
			2,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_ScalarMatchIR pins the ladder shape: an icmp test per literal arm and
// a cond-br to the arm body or the next test (no `switch` — the ladder is used
// uniformly so a range arm fits).
func TestEmit_ScalarMatchIR(t *testing.T) {
	got, err := emitSource(t, `let f = (n: u8) -> u8 => match n {
	   0 => 10,
	   1 => 20,
	   _ => 30,
	 }
	 let main = () -> u8 => f(1)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"icmp eq", "br i1"} {
		if !strings.Contains(got, want) {
			t.Errorf("scalar-match IR missing %q:\n%s", want, got)
		}
	}
}

// `match` on a struct value lowers to a ladder (a struct has one shape, so no
// tag/switch): a struct pattern `{ x, y }` matches unconditionally and binds its
// fields (extractvalue), while a literal field sub-pattern (`{ x: 0, y }`) makes
// the arm conditional on that field. These run the compiled program.
func TestExec_StructMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Bind every field (shorthand) and use both in the body.
		{
			"bind all fields",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   { x, y } => x + y,
			 }
			 let main = () -> u8 => f(Pt { x: 20, y: 22 })`,
			42,
		},
		// Bind only a subset of the fields.
		{
			"partial fields",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   { x } => x,
			 }
			 let main = () -> u8 => f(Pt { x: 7, y: 9 })`,
			7,
		},
		// A literal field sub-pattern makes the arm conditional; here the field
		// matches, so the arm is taken and the other field binds.
		{
			"literal field test taken",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   { x: 0, y } => y,
			   _ => 99,
			 }
			 let main = () -> u8 => f(Pt { x: 0, y: 5 })`,
			5,
		},
		// The literal field doesn't match, so the wildcard arm is taken.
		{
			"literal field test skipped",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   { x: 0, y } => y,
			   _ => 99,
			 }
			 let main = () -> u8 => f(Pt { x: 3, y: 5 })`,
			99,
		},
		// `{ x: a, y: b }` renames the fields to a/b in the arm body.
		{
			"rename bindings",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   { x: a, y: b } => a + b,
			 }
			 let main = () -> u8 => f(Pt { x: 1, y: 2 })`,
			3,
		},
		// The type-named form `Pt { x, y }` (symmetric with construction) lowers
		// identically — the collector reclassifies it to a named StructPattern.
		{
			"type-named struct pattern",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let f = (p: Pt) -> u8 => match p {
			   Pt { x, y } => x + y,
			 }
			 let main = () -> u8 => f(Pt { x: 20, y: 22 })`,
			42,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_StructMatchIR pins the shape: fields read via extractvalue (no tag
// switch, since a struct has a single shape).
func TestEmit_StructMatchIR(t *testing.T) {
	got, err := emitSource(t, `struct Pt {
	   x: u8,
	   y: u8,
	 }
	 let f = (p: Pt) -> u8 => match p {
	   { x, y } => x + y,
	 }
	 let main = () -> u8 => f(Pt { x: 1, y: 2 })`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "extractvalue") {
		t.Errorf("struct-match IR should read fields via extractvalue:\n%s", got)
	}
}

// `match` on a tuple value lowers to the same aggregate ladder as a struct, but
// positional: a tuple pattern `(a, b)` binds elements by index (extractvalue),
// and a literal element (`(0, b)`) makes the arm conditional on that position.
func TestExec_TupleMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Bind both elements positionally and combine them.
		{
			"bind positional",
			`let main = () -> u8 => {
			   let t = (20, 22)
			   match t {
			     (a, b) => u8(a + b),
			     _ => 0,
			   }
			 }`,
			42,
		},
		// A literal element makes the arm conditional; here it matches.
		{
			"literal element taken",
			`let main = () -> u8 => {
			   let t = (0, 5)
			   match t {
			     (0, b) => u8(b),
			     _ => 99,
			   }
			 }`,
			5,
		},
		// The literal element doesn't match, so the wildcard arm is taken.
		{
			"literal element skipped",
			`let main = () -> u8 => {
			   let t = (3, 5)
			   match t {
			     (0, b) => u8(b),
			     _ => 99,
			   }
			 }`,
			99,
		},
		// A wildcard element binds nothing; only the first element is used.
		{
			"wildcard element",
			`let main = () -> u8 => {
			   let t = (7, 9)
			   match t {
			     (a, _) => u8(a),
			   }
			 }`,
			7,
		},
		// A named tuple scrutinee matched positionally.
		{
			"named tuple",
			`tuple Point(u8, u8)
			 let f = (p: Point) -> u8 => match p {
			   (a, b) => a + b,
			 }
			 let main = () -> u8 => f(Point(20, 22))`,
			42,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// Nested sub-patterns inside a struct/tuple scrutinee recurse via extractvalue
// (no tag/branch needed on a single-shape aggregate): a struct/tuple sub-pattern
// binds and tests its own elements, to arbitrary depth.
func TestExec_NestedAggregatePatterns(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// A tuple nested in a tuple: ((a, b), c) binds all three.
		{
			"tuple in tuple",
			`let main = () -> u8 => {
			   let t = ((1, 2), 3)
			   match t {
			     ((a, b), c) => u8(a + b + c),
			     _ => 0,
			   }
			 }`,
			6,
		},
		// A struct field that is itself a struct, destructured two levels deep.
		{
			"struct in struct",
			`struct Inner {
			   v: u8,
			 }
			 struct Outer {
			   inner: Inner,
			 }
			 let f = (o: Outer) -> u8 => match o {
			   { inner: { v } } => v,
			   _ => 0,
			 }
			 let main = () -> u8 => f(Outer { inner: Inner { v: 7 } })`,
			7,
		},
		// A struct nested inside a tuple element, using the type-named form.
		{
			"named struct in tuple",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 let main = () -> u8 => {
			   let t = (Pt { x: 20, y: 22 }, 0)
			   match t {
			     (Pt { x, y }, _) => u8(x + y),
			     _ => 0,
			   }
			 }`,
			42,
		},
		// A literal deep inside the nesting makes the whole arm conditional.
		{
			"nested literal test",
			`let main = () -> u8 => {
			   let t = ((0, 5), 9)
			   match t {
			     ((0, b), c) => u8(b + c),
			     _ => 99,
			   }
			 }`,
			14,
		},
		{
			"nested literal skipped",
			`let main = () -> u8 => {
			   let t = ((3, 5), 9)
			   match t {
			     ((0, b), c) => u8(b + c),
			     _ => 99,
			   }
			 }`,
			99,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// Nested `data` patterns: destructuring inside a data payload (the tag switch
// selects the variant, then the payload is bound recursively), and a data pattern
// nested inside an aggregate (the data value's tag becomes a comparison in the
// aggregate ladder, then its payload is bound on the taken path).
func TestExec_NestedDataPatterns(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// A data payload that is a tuple, destructured (`Wrapped((a, b))`). i64
		// payload avoids the separate nested-tuple-literal construction-width gap.
		{
			"data payload tuple",
			`data Wrap = Wrapped((i64, i64))
			 let f = (w: Wrap) -> u8 => match w {
			   Wrapped((a, b)) => u8(a + b),
			 }
			 let main = () -> u8 => f(Wrapped((20, 22)))`,
			42,
		},
		// A data payload that is a struct, destructured (`Boxed({ x, y })`).
		{
			"data payload struct",
			`struct Pt {
			   x: u8,
			   y: u8,
			 }
			 data Box = Boxed(Pt)
			 let f = (b: Box) -> u8 => match b {
			   Boxed({ x, y }) => x + y,
			 }
			 let main = () -> u8 => f(Boxed(Pt { x: 20, y: 22 }))`,
			42,
		},
		// A data value nested in a tuple: the arm's tag decides which is taken.
		{
			"data in tuple (Some arm)",
			`data Maybe = None | Some(u8)
			 let main = () -> u8 => {
			   let t = (7, Some(5))
			   match t {
			     (c, Some(x)) => u8(x),
			     (c, None) => u8(c),
			   }
			 }`,
			5,
		},
		{
			"data in tuple (None arm)",
			`data Maybe = None | Some(u8)
			 let main = () -> u8 => {
			   let t = (7, None)
			   match t {
			     (c, Some(x)) => u8(x),
			     (c, None) => u8(c),
			   }
			 }`,
			7,
		},
		// A data value in a struct field, with its payload bound.
		{
			"data in struct",
			`data Maybe = None | Some(u8)
			 struct Box {
			   m: Maybe,
			 }
			 let f = (b: Box) -> u8 => match b {
			   { m: Some(x) } => x,
			   _ => 0,
			 }
			 let main = () -> u8 => f(Box { m: Some(9) })`,
			9,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// A tuple literal whose *first* element is a bare name — an identifier (`(a, b)`)
// or a nullary constructor (`(None, 7)`) — used to fail to parse: the leading
// name was precedence-committed to a lambda parameter / data pattern with no GLR
// fallback to the tuple reading (grammar fix in tree-sitter-lyra). These construct
// such a tuple and match it, exercising the parse end to end.
func TestExec_LeadingNameTupleMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Leading bare identifier: `(a, b)`.
		{
			"leading identifier",
			`let main = () -> u8 => {
			   let a = 20
			   let b = 22
			   let t = (a, b)
			   match t {
			     (x, y) => u8(x + y),
			   }
			 }`,
			42,
		},
		// Leading nullary constructor: `(None, 7)` — the None arm is taken.
		{
			"leading nullary constructor",
			`data Maybe = None | Some(u8)
			 let main = () -> u8 => {
			   let t = (None, 7)
			   match t {
			     (Some(x), c) => u8(x),
			     (None, c) => u8(c),
			   }
			 }`,
			7,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}
