package llvm

import (
	"strings"
	"testing"
)

// The three destructuring *statements* lower. All type-checked before but none
// compiled ("block statement lowering not implemented for *ast.…"), the same
// front-end-enforces-what-the-backend-can't-build gap `newtype` had.
//
// They share one mechanism, differing only in what happens when the pattern does
// not match: an irrefutable `let (a, b) = v` has no failure path, an `if let`
// branches, and a `let … else` requires the else to diverge. All three drive the
// pattern machinery `match` is built on, so a pattern means the same thing in a
// match arm and in an `if let`.
func TestExec_DestructuringStatements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Irrefutable: a tuple pattern always matches its type.
			"plain tuple destructuring",
			`let main = () -> u8 => {
			   let pair = (3, 4)
			   let (a, b) = pair
			   u8(a + b)
			 }`,
			7,
		},
		{
			"plain struct destructuring",
			`struct Pt { x: i64, y: i64 }
			 let main = () -> u8 => {
			   let p = Pt { x: 3, y: 4 }
			   let { x, y } = p
			   u8(x + y)
			 }`,
			7,
		},
		{
			// if-let on a matching data value: the then-branch runs with the payload
			// bound.
			"if let, pattern matches",
			`data Maybe = Some(i64) | None
			 let main = () -> u8 => {
			   let m = Some(5)
			   var out = 0
			   if let Some(v) = m { out = v } else { out = 99 }
			   u8(out)
			 }`,
			5,
		},
		{
			// The same statement on a non-matching value takes the else branch.
			"if let, pattern fails",
			`data Maybe = Some(i64) | None
			 let main = () -> u8 => {
			   let m = None
			   var out = 0
			   if let Some(v) = m { out = v } else { out = 42 }
			   u8(out)
			 }`,
			42,
		},
		{
			// No else branch: control just joins.
			"if let with no else",
			`struct Pt { x: i64, y: i64 }
			 let main = () -> u8 => {
			   let p = Pt { x: 3, y: 4 }
			   var out = 0
			   if let Pt { x, y } = p { out = x + y }
			   u8(out)
			 }`,
			7,
		},
		{
			// A nested pattern inside the payload.
			"if let with a nested tuple payload",
			`data Wrap = Wrapped((i64, i64)) | Empty
			 let main = () -> u8 => {
			   let w = Wrapped((3, 4))
			   var out = 0
			   if let Wrapped((a, b)) = w { out = a + b }
			   u8(out)
			 }`,
			7,
		},
		{
			// A `shared` scrutinee is a box pointer, so the match unboxes it — the
			// same path a `match` on a shared value takes.
			"if let on a shared data value",
			`data Shape = Circle(i64) | Square(i64)
			 let main = () -> u8 => {
			   let s: shared Shape = Circle(7)
			   var out = 0
			   if let Circle(r) = s { out = r }
			   u8(out)
			 }`,
			7,
		},
		{
			// let-else: the names persist after the statement (unlike if-let), which
			// is sound because the else branch cannot fall through.
			"let else, pattern matches",
			`data Maybe = Some(i64) | None
			 let first = (m: Maybe) -> i64 => {
			   let Some(v) = m else { return -1 }
			   v * 2
			 }
			 let main = () -> u8 => u8(first(Some(5)) + 1)`,
			11,
		},
		{
			// The diverging branch: a non-match returns early.
			"let else, pattern fails",
			`data Maybe = Some(i64) | None
			 let first = (m: Maybe) -> i64 => {
			   let Some(v) = m else { return 7 }
			   v * 2
			 }
			 let main = () -> u8 => u8(first(None))`,
			7,
		},
		{
			// An if-let inside a loop body, over a value built per iteration.
			"if let inside a loop body",
			`data Maybe = Some(i64) | None
			 let main = () -> u8 => {
			   var total = 0
			   for x in [1, 2, 3] {
			     let m = if x > 1 { Some(x) } else { None }
			     if let Some(v) = m { total = total + v }
			   }
			   u8(total)
			 }`,
			5, // 2 + 3
		},
		{
			// A one-armed `if` as the last statement of an if-let branch. An if-let is
			// a statement, so neither branch is in value position — this was rejected
			// with "`if` used as a value must have an `else` branch" until both
			// branches started being checked for effect.
			"a one-armed if ending an if-let branch",
			`data Maybe = Some(i64) | None
			 let main = () -> u8 => {
			   let m = Some(5)
			   var out = 0
			   if let Some(v) = m {
			     if v > 1 { out = v }
			   }
			   u8(out)
			 }`,
			5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// An if-let's bindings are scoped to its then-branch, and a managed payload bound
// there is a borrow out of the scrutinee — the scrutinee still owns it, so the
// branch must not release it.
func TestExec_DestructuringManagedPayload(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `data Maybe = Some(string) | None
	 let main = () -> u8 => {
	   let m = Some("a" ++ "b")
	   var out = 0
	   if let Some(s) = m {
	     if s == "ab" { out = 7 }
	   }
	   u8(out)
	 }`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d; want 7", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 7 {
		t.Errorf("ASan run exited %d; want 7", got)
	}
	// One allocation and one release: the branch borrows the payload out of the
	// scrutinee, so the only reference is the one the scrutinee binding holds and
	// releases at scope exit. A retain here would mean the branch took a reference
	// it never gives back; a second release would free the scrutinee's.
	//
	// Counted rather than checked path-sensitively: the box is stored into a `data`
	// value, which the conservation analysis treats as an escape and stops
	// reasoning about (correctly — it cannot see who owns it after that).
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	retains := strings.Count(ir, "call void @lyra_rc_retain")
	releases := strings.Count(ir, "call void @lyra_rc_release")
	if allocs != 1 || retains != 0 || releases != 1 {
		t.Errorf("expected 1 alloc / 0 retains / 1 release; got %d/%d/%d", allocs, retains, releases)
	}
}

// A refutable pattern in an irrefutable position is refused rather than bound on a
// path where the match may not hold. (The typechecker does not reject it today, so
// this is the backend's own "never emit wrong code" guard.)
func TestEmit_RefutablePlainDestructuring_Error(t *testing.T) {
	t.Parallel()
	_, err := emitSource(t, `data Maybe = Some(i64) | None
	 let main = () -> u8 => {
	   let m = Some(5)
	   let Some(v) = m
	   u8(v)
	 }`)
	if err == nil {
		t.Fatal("expected an error for a refutable pattern in a plain `let` destructuring")
	}
	if !strings.Contains(err.Error(), "needs an `else` branch") {
		t.Errorf("expected the error to point at `let … else`; got: %v", err)
	}
}
