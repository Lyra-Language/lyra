package llvm

import (
	"strings"
	"testing"
)

// The `?` (try) postfix operator — error propagation (try.go).
//
// `x?` unwraps a Result/Maybe on success and returns the failure variant from the
// enclosing function otherwise, rebuilt at *that* function's return type. These are
// behavioral tests: they run the compiled program and observe which branch it took
// and with what payload, which is the only way to tell a correct rebuild from one
// that forwarded the wrong union or the wrong tag.

// A Result whose Ok and Err payloads are both i64 — nothing managed, so this is
// purely about control flow and the tag/payload encoding.
const tryResultPrelude = `data Result<t, e> = Ok(t) | Err(e)

let half = (n: i64) -> Result<i64, i64> =>
  if n % 2 == 0 { Ok(n / 2) } else { Err(n) }

let quarter = (n: i64) -> Result<i64, i64> => {
  let h = half(n)?
  half(h)
}
`

func TestExec_TryResultPropagation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		main string
		want int
	}{
		// Both `?`s succeed: 20 → 10 → 5, and the Ok payload reaches the caller.
		{"both succeed", `match quarter(20) { Ok(v) => u8(v), Err(_) => 99 }`, 5},
		// The *first* `?` fails: half(7) is Err(7), so quarter returns Err(7) without
		// ever running the second half — the payload is the original 7, proving the
		// error was carried across rather than recomputed.
		{"first fails", `match quarter(7) { Ok(_) => 99, Err(e) => u8(e) }`, 7},
		// The first `?` succeeds and the *second* call fails: 6 → 3, then half(3) is
		// Err(3). The failure here is the tail call's own value, not a `?`.
		{"second fails", `match quarter(6) { Ok(_) => 99, Err(e) => u8(e) }`, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := tryResultPrelude + "let main = () -> u8 => " + c.main + "\n"
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("exited %d; want %d\n%s", got, c.want, src)
			}
		})
	}
}

// A Maybe: the failure variant is nullary, so propagation builds `None` with no
// payload to carry — the path where buildDataValue gets an empty field list.
func TestExec_TryMaybePropagation(t *testing.T) {
	t.Parallel()
	const prelude = `data Maybe<t> = None | Some(t)

let positive = (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }

let doubled = (n: i64) -> Maybe<i64> => {
  let v = positive(n)?
  Some(v * 2)
}
`
	cases := []struct {
		name string
		main string
		want int
	}{
		{"some", `match doubled(21) { Some(v) => u8(v), None => 99 }`, 42},
		{"none", `match doubled(0) { Some(_) => 1, None => 7 }`, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := prelude + "let main = () -> u8 => " + c.main + "\n"
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("exited %d; want %d\n%s", got, c.want, src)
			}
		})
	}
}

// The operand's Result and the enclosing function's Result are *different*
// instantiations (`Result<i64, i64>` propagated out of a `-> Result<bool, i64>`
// function). They are distinct LLVM types, so the error cannot be forwarded as-is:
// the payload is extracted and a fresh Err is built at the enclosing type. This is
// the case that would silently emit a type-confused union if the rebuild were
// skipped, so it is worth its own test.
func TestExec_TryRebuildsErrorAtEnclosingType(t *testing.T) {
	t.Parallel()
	const src = `data Result<t, e> = Ok(t) | Err(e)

let half = (n: i64) -> Result<i64, i64> =>
  if n % 2 == 0 { Ok(n / 2) } else { Err(n) }

let isSmall = (n: i64) -> Result<bool, i64> => {
  let h = half(n)?
  Ok(h < 10)
}

let main = () -> u8 => match isSmall(9) {
  Ok(true) => 1,
  Ok(false) => 2,
  Err(e) => u8(e),
}
`
	if got := buildAndRun(t, src); got != 9 {
		t.Errorf("exited %d; want 9 (Err(9) rebuilt at Result<bool, i64>)\n%s", got, src)
	}
}

// A `?` in the tail position of a function, rather than bound by a `let`: the
// success block is where the body's value comes from, so the implicit tail return
// has to be emitted into that block and not the one the operand was lowered in.
func TestExec_TryInTailPosition(t *testing.T) {
	t.Parallel()
	const src = `data Result<t, e> = Ok(t) | Err(e)

let half = (n: i64) -> Result<i64, i64> =>
  if n % 2 == 0 { Ok(n / 2) } else { Err(n) }

let unwrapped = (n: i64) -> Result<i64, i64> => Ok(half(n)? + 1)

let main = () -> u8 => match unwrapped(8) { Ok(v) => u8(v), Err(_) => 99 }
`
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("exited %d; want 5\n%s", got, src)
	}
}

// `?` inside a *generic* function. The enclosing return type is stored unlowered and
// unsubstituted (`retLyra`), so here it is still `Maybe<t>` when the specialization is
// lowered; rebuilding `None` at the right instantiation depends on running it back
// through applyTypeSubst. Two instantiations, so a substitution that leaked across them
// would show up as one of the two returning the wrong shape.
func TestExec_TryInGenericFunction(t *testing.T) {
	t.Parallel()
	const src = `data Maybe<t> = None | Some(t)

let twice<t> = (m: Maybe<t>, f: (t) -> t) -> Maybe<t> => {
  let v = m?
  Some(f(f(v)))
}

let main = () -> u8 => {
  let a: u8 = match twice(Some(3), (n: i64) -> i64 => n * 2) { Some(v) => u8(v), None => 0 }
  let b: u8 = match twice(Some(true), (x: bool) -> bool => x) { Some(_) => 1, None => 0 }
  let c: u8 = match twice(None, (n: i64) -> i64 => n + 1) { Some(_) => 9, None => 40 }
  a + b + c
}
`
	// 12 (3 doubled twice) + 1 (the bool instantiation) + 40 (None propagated) == 53
	if got := buildAndRun(t, src); got != 53 {
		t.Errorf("exited %d; want 53\n%s", got, src)
	}
}

// A managed (string) error payload. The propagated string must survive the rebuild
// with its bytes intact — the interesting half is refcounting: the operand is an
// owned temporary whose reference transfers into the rebuilt Err, so no dup is
// minted (try.go's `transferring`). Printing the payload proves it is neither freed
// early nor garbage.
const tryManagedSrc = `data Result<t, e> = Ok(t) | Err(e)

let parse = (s: string) -> Result<i64, string> =>
  if s == "42" { Ok(42) } else { Err("bad input: " ++ s) }

let doubled = (s: string) -> Result<i64, string> => {
  let n = parse(s)?
  Ok(n * 2)
}
`

func TestExec_TryManagedErrorPayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		main string
		want string
		code int
	}{
		{
			"propagates the string",
			`match doubled("zz") { Ok(_) => 0, Err(e) => { print(e); 7 } }`,
			"bad input: zz",
			7,
		},
		{
			"success path ignores it",
			`match doubled("42") { Ok(v) => { print("ok"); u8(v / 42) }, Err(_) => 99 }`,
			"ok",
			2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := tryManagedSrc + "let main = () -> u8 => " + c.main + "\n"
			out, code := buildAndRunCapture(t, src)
			if strings.TrimSpace(out) != c.want {
				t.Errorf("printed %q; want %q\n%s", out, c.want, src)
			}
			if code != c.code {
				t.Errorf("exited %d; want %d\n%s", code, c.code, src)
			}
		})
	}
}

// A *borrowed* operand rather than an owned temporary: the Result is held by a
// binding that still owns it, so the propagated error needs a reference of its own
// (try.go retains when `transferring` is false). Getting this backwards hands the
// caller a string the binding's scope-exit release then frees — a use-after-free
// rather than a wrong value, which is why the ASan run below covers it too.
const tryBorrowedSrc = `data Result<t, e> = Ok(t) | Err(e)

let describe = (n: i64) -> Result<i64, string> =>
  if n > 0 { Ok(n) } else { Err("not positive: " ++ "n") }

let checked = (n: i64) -> Result<i64, string> => {
  let r = describe(n)
  let v = r?
  Ok(v + 1)
}
`

func TestExec_TryBorrowedOperand(t *testing.T) {
	t.Parallel()
	src := tryBorrowedSrc + `let main = () -> u8 => match checked(0) {
  Ok(_) => 0,
  Err(e) => { print(e); 7 }
}
`
	out, code := buildAndRunCapture(t, src)
	if strings.TrimSpace(out) != "not positive: n" {
		t.Errorf("printed %q; want %q", out, "not positive: n")
	}
	if code != 7 {
		t.Errorf("exited %d; want 7", code)
	}
}

// A `?` leaves every enclosing scope at once, exactly as `return` does, so the
// managed bindings those scopes hold have to be released on the propagating path
// too. Here `note` is allocated *before* the `?` and is dead on both paths: the
// success path releases it at scope exit, and the propagating path must release it
// at the early return (emitReturn's releaseAllManagedFrames).
//
// It is checked with the path-sensitive analysis rather than a runtime assertion
// because a leak on one edge of a branch is invisible to a count of allocations
// against releases — the two balance while a single path carries the box past its
// only release. `note` is read locally and never handed onward, which is what keeps
// it inside the analysis (a value passed to a call is conservatively an escape).
const tryScopeReleaseSrc = `data Result<t, e> = Ok(t) | Err(e)

let parse = (s: string) -> Result<i64, string> =>
  if s == "42" { Ok(42) } else { Err("bad") }

let use = (s: string) -> Result<i64, string> => {
  let note = "n=" ++ "x"
  let n = parse(s)?
  if note == "n=x" { Ok(n + 3) } else { Ok(n) }
}
`

func TestConservation_TryReleasesEnclosingScope(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{`"42"`, `"zz"`} { // the success and propagating paths
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			assertNoConservationLeak(t, tryScopeReleaseSrc+
				"let main = () -> u8 => match use("+arg+") { Ok(_) => 0, Err(_) => 1 }\n")
		})
	}
}

func TestExec_TryReleasesEnclosingScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		arg  string
		want int
	}{
		{`"42"`, 45}, // 42 + 3, the local string having survived to its comparison
		{`"zz"`, 1},  // propagates, so `note` is released by the early return
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			t.Parallel()
			src := tryScopeReleaseSrc +
				"let main = () -> u8 => match use(" + c.arg + ") { Ok(v) => u8(v), Err(_) => 1 }\n"
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("exited %d; want %d\n%s", got, c.want, src)
			}
		})
	}
}

func TestASan_TryReleasesEnclosingScope(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	for _, arg := range []string{`"42"`, `"zz"`} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			src := tryScopeReleaseSrc +
				"let main = () -> u8 => match use(" + arg + ") { Ok(_) => 0, Err(_) => 0 }\n"
			if got := buildAndRunASan(t, clang, src); got != 0 {
				t.Errorf("asan run exited %d\n%s", got, src)
			}
		})
	}
}

// ASan over the managed cases: a `?` that propagates a heap-allocated string is
// exactly where a mis-modeled retain shows up as a use-after-free or double free
// rather than a wrong answer.
func TestASan_TryManagedPayload(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
	}{
		{"owned temporary, error path", tryManagedSrc + `let main = () -> u8 => match doubled("zz") { Ok(_) => 0, Err(e) => { print(e); 0 } }` + "\n"},
		{"owned temporary, success path", tryManagedSrc + `let main = () -> u8 => match doubled("42") { Ok(v) => u8(v / 42), Err(_) => 9 }` + "\n"},
		{"borrowed operand, error path", tryBorrowedSrc + `let main = () -> u8 => match checked(0) { Ok(_) => 0, Err(e) => { print(e); 0 } }` + "\n"},
		{"borrowed operand, success path", tryBorrowedSrc + `let main = () -> u8 => match checked(1) { Ok(v) => u8(v), Err(_) => 9 }` + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != 0 && got != 2 {
				t.Errorf("asan run exited %d (a sanitizer report exits non-zero)\n%s", got, c.src)
			}
		})
	}
}
