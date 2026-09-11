package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// The declarations every case below is written against: a resource type, the function
// that releases it, one that merely borrows it, and one that adopts it.
const mustReleasePrelude = `
data Maybe<t> = None | Some(t)
@must_release(unload_sound) struct Sound { id: i64 }
let try_load = (n: i64) -> Maybe<Sound> => Some(Sound { id: n })
let load_sound = (n: i64) -> Sound => Sound { id: n }
let unload_sound = (s: Sound) -> void => {}
let play_sound = (s: Sound) -> void => {}
let adopt = (s: own Sound) -> void => {}
`

// mustReleaseWarnings runs the pipeline lyra-W022 needs — it reads the TypeTable and the
// typechecker's callee resolution — and returns one message per warning.
func mustReleaseWarnings(t *testing.T, body string) []string {
	t.Helper()
	source := mustReleasePrelude + body
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	tc.Check(program)
	var msgs []string
	for _, d := range checker.CheckMustRelease(program, symTable, tt, tc.MethodTable()) {
		if d.Code != diag.CodeUnreleasedResource {
			t.Fatalf("unexpected code %q", d.Code)
		}
		if d.Severity != diag.SeverityWarning {
			t.Fatalf("lyra-W022 must be a warning, got severity %v", d.Severity)
		}
		msgs = append(msgs, d.Message)
	}
	return msgs
}

func assertLeaks(t *testing.T, body, wantBinding string) {
	t.Helper()
	msgs := mustReleaseWarnings(t, body)
	if len(msgs) != 1 {
		t.Fatalf("want exactly one warning naming %q, got %d: %v", wantBinding, len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], `"`+wantBinding+`"`) {
		t.Errorf("warning should name %q; got: %s", wantBinding, msgs[0])
	}
}

func assertClean(t *testing.T, body string) {
	t.Helper()
	if msgs := mustReleaseWarnings(t, body); len(msgs) != 0 {
		t.Errorf("expected no warning, got: %v", msgs)
	}
}

// The bug the pass exists for.
func TestMustRelease_UnreleasedLocalIsReported(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = load_sound(1)
  play_sound(s)
}
`, "s")
}

func TestMustRelease_ReleasedLocalIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = load_sound(1)
  play_sound(s)
  unload_sound(s)
}
`)
}

// **The distinction the whole design rests on.** If any call counted as a discharge,
// this case — acquire, use, forget — would be the one shape that never reported, and it
// is the commonest form of the bug.
func TestMustRelease_APlainCallIsABorrowNotARelease(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = load_sound(1)
  play_sound(s)
  play_sound(s)
}
`, "s")
}

// `own` is how a program says the obligation went with the value, so passing to one is
// an escape rather than a leak.
func TestMustRelease_OwnParameterTakesTheObligation(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = load_sound(1)
  adopt(s)
}
`)
}

func TestMustRelease_ReturningItHandsTheObligationToTheCaller(t *testing.T) {
	assertClean(t, `
let make = () -> Sound => {
  let s = load_sound(1)
  s
}
let main = () -> void => {
  let s = make()
  unload_sound(s)
}
`)
}

func TestMustRelease_ExplicitReturnAlsoEscapes(t *testing.T) {
	assertClean(t, `
let make = () -> Sound => {
  let s = load_sound(1)
  return s
}
`)
}

// The join is the intersection: released down any one path counts. This deliberately
// under-reports, and the pass header says why — the alternative fires on correct
// early-return code, and a false positive on a warning nobody can suppress is worse
// than a miss.
func TestMustRelease_ReleasedInOneBranchIsAccepted(t *testing.T) {
	assertClean(t, `
let main = (flag: bool) -> void => {
  let s = load_sound(1)
  if flag { unload_sound(s) } else { }
}
`)
}

func TestMustRelease_ReleasedInEveryMatchArmIsClean(t *testing.T) {
	assertClean(t, `
let main = (n: i64) -> void => {
  let s = load_sound(1)
  match n {
    0 => unload_sound(s),
    _ => unload_sound(s),
  }
}
`)
}

// A resource acquired inside a block dies at that block's brace, so the report has to
// happen per block. Without it the branch join would drop the binding and the leak would
// vanish silently — the intersection is exactly what suppresses a report.
func TestMustRelease_LeakInsideABranchIsStillReported(t *testing.T) {
	assertLeaks(t, `
let main = (flag: bool) -> void => {
  if flag {
    let s = load_sound(1)
    play_sound(s)
  } else { }
}
`, "s")
}

func TestMustRelease_AcquiredAndReleasedInALoopIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  for i in 0..<3 {
    let s = load_sound(i)
    play_sound(s)
    unload_sound(s)
  }
}
`)
}

// A loop body may not run, so a release inside one cannot discharge something acquired
// outside it.
func TestMustRelease_ReleaseInsideALoopDoesNotDischargeAnOuterResource(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = load_sound(1)
  for i in 0..<3 {
    unload_sound(s)
  }
}
`, "s")
}

// A closure may run later, elsewhere, or never, so a captured resource has a fate this
// pass cannot see.
func TestMustRelease_CaptureByAClosureEscapes(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = load_sound(1)
  let f = () -> void => unload_sound(s)
  f()
}
`)
}

// Every binding is its own obligation.
func TestMustRelease_EachUnreleasedBindingIsReportedOnce(t *testing.T) {
	msgs := mustReleaseWarnings(t, `
let main = () -> void => {
  let a = load_sound(1)
  let b = load_sound(2)
  unload_sound(b)
  let c = load_sound(3)
}
`)
	if len(msgs) != 2 {
		t.Fatalf("want two warnings (a and c), got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], `"a"`) || !strings.Contains(msgs[1], `"c"`) {
		t.Errorf("want warnings for a then c, got: %v", msgs)
	}
}

// A type with no attribute carries no obligation, so the pass is invisible to every
// program that does not use the feature.
func TestMustRelease_AnUnmarkedTypeIsNeverReported(t *testing.T) {
	assertClean(t, `
struct Plain { id: i64 }
let make_plain = () -> Plain => Plain { id: 1 }
let main = () -> void => {
  let p = make_plain()
}
`)
}

// The message has to name the call that fixes it — a diagnostic saying only that
// something is wrong sends the reader to find the release function themselves.
func TestMustRelease_MessageNamesTheReleasingCall(t *testing.T) {
	msgs := mustReleaseWarnings(t, `
let main = () -> void => {
  let s = load_sound(1)
}
`)
	if len(msgs) != 1 {
		t.Fatalf("want one warning, got: %v", msgs)
	}
	if !strings.Contains(msgs[0], "unload_sound(s)") {
		t.Errorf("message should name the call to write; got: %s", msgs[0])
	}
}

// A method-style call reaches the same declaration, so it discharges too. UFCS desugars
// the receiver into the argument list before this pass runs, which is what makes the two
// spellings one call here (hazard 10's advice, taken).
func TestMustRelease_MethodStyleReleaseDischarges(t *testing.T) {
	assertClean(t, `
@must_release(close) struct Handle { fd: i64 }
let open_handle = () -> Handle => Handle { fd: 3 }
let close = (self: Handle) -> void => {}
let main = () -> void => {
  let h = open_handle()
  h.close()
}
`)
}

// **The shape the pass exists for in real code.** Acquiring a foreign resource can
// fail, so every acquisition in a binding module answers `Maybe<T>` — raylib's
// `load_sound` and `wave_from_memory` both do. Tracking only a bare `Sound` left the
// check silent on the one shape that matters, which is how this was found: deleting an
// `unload_sound` from `examples/raylib/breakout.lyra` produced no warning at all.
func TestMustRelease_AResourceInsideAMaybeIsTracked(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = try_load(1)
}
`, "s")
}

func TestMustRelease_UnwrappingAndReleasingIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = try_load(1)
  match s {
    Some(v) => unload_sound(v),
    None => {},
  }
}
`)
}

// Unwrapping is not releasing — but **the report lands on the binding, not on the arm's
// payload**, because that is the thing that still holds the resource after the match ends.
//
// The payload is a *view*: releasing through it discharges the binding, and merely using
// it does not. Getting that backwards produced a false positive on the shape every program
// with a long-lived optional resource writes —
// `match held { Some(t) => draw(t), None => {} }` once a frame — which is how it was
// found, writing `examples/raylib/textures.lyra`.
func TestMustRelease_UnwrappingWithoutReleasingIsReported(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = try_load(1)
  match s {
    Some(v) => play_sound(v),
    None => {},
  }
}
`, "s")
}

// The shape the view rule exists for: a binding unwrapped to be *used*, over and over,
// and released once somewhere else.
func TestMustRelease_UnwrappingToUseIsABorrow(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = try_load(1)
  match s {
    Some(v) => play_sound(v),
    None => {},
  }
  match s {
    Some(v) => unload_sound(v),
    None => {},
  }
}
`)
}

// A diagnostic naming a call the reader cannot write is worse than one naming none:
// `unload_sound(s)` does not compile when `s` is a `Maybe<Sound>`. The test takes the
// advice literally, which is the only way to know a suggested fix is real rather than
// plausible.
func TestMustRelease_WrappedMessageSuggestsAFixThatCompiles(t *testing.T) {
	msgs := mustReleaseWarnings(t, `
let main = () -> void => {
  let s = try_load(1)
}
`)
	if len(msgs) != 1 {
		t.Fatalf("want one warning, got: %v", msgs)
	}
	if strings.Contains(msgs[0], "call `unload_sound(s)`") {
		t.Errorf("must not suggest calling the release function on a Maybe; got: %s", msgs[0])
	}
	if !strings.Contains(msgs[0], "match s { Some(v) => unload_sound(v), None => {} }") {
		t.Errorf("should suggest the unwrap; got: %s", msgs[0])
	}
	// Now write exactly what it advised, and require that it checks clean.
	assertClean(t, `
let main = () -> void => {
  let s = try_load(1)
  match s { Some(v) => unload_sound(v), None => {} }
}
`)
}

// --- `if let`, the other way the language unwraps a Maybe -------------------------

func TestMustRelease_IfLetUnwrapAndReleaseIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = try_load(1)
  if let Some(v) = s { unload_sound(v) }
}
`)
}

// The mirror of the `match` case, and the same rule: the binding is what is reported,
// since it is what still holds the resource once the branch is over.
func TestMustRelease_IfLetUnwrapWithoutReleasingIsReported(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = try_load(1)
  if let Some(v) = s { play_sound(v) }
}
`, "s")
}

// Where the scrutinee is a **temporary**, the payload really is the only handle and the
// arm really is the last chance — so there the report lands on the payload. That is the
// distinction the view rule draws, and both halves of it are pinned.
func TestMustRelease_ATemporaryScrutineeReportsThePayload(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  if let Some(v) = try_load(1) { play_sound(v) }
}
`, "v")
}

func TestMustRelease_IfLetWithAnElseIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let s = try_load(1)
  if let Some(v) = s { unload_sound(v) } else { }
}
`)
}

// --- `let … else`, which is not a branch --------------------------------------------
//
// It is Rust's `let … else`: the payload binds in the **enclosing** scope and the else
// block is the diverging path that never sees it. The obvious reading is the opposite
// one, and `CheckUseAfterMove` takes it — so these pin the real semantics, which were
// checked against the compiler rather than assumed.

func TestMustRelease_LetElseMovesTheObligationToThePayload(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let Some(v) = try_load(1) else { return }
  play_sound(v)
}
`, "v")
}

func TestMustRelease_LetElseReleasedAfterwardsIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  let Some(v) = try_load(1) else { return }
  play_sound(v)
  unload_sound(v)
}
`)
}

// The else block runs on a path where the payload was never bound, so nothing it does
// can discharge what the main path is holding.
func TestMustRelease_LetElseDivergingBranchDischargesNothing(t *testing.T) {
	assertLeaks(t, `
let other = () -> Sound => load_sound(9)
let main = () -> void => {
  let Some(v) = try_load(1) else { return }
  play_sound(v)
}
`, "v")
}

// --- what the one-name rule declines to guess ----------------------------------------

// A pattern binding is an `IdentifierPattern` and not an expression, so there is no
// per-name type to consult; matching a pattern's shape against the scrutinee's type
// positionally would be a new structural walk with a case missing for every pattern kind
// added later (rule 8's family). So a multi-binding pattern falls through to the escape
// default and stays **silent** — under-reporting, the direction this pass errs in
// everywhere. This pins that as a known limit rather than leaving it to be rediscovered.
func TestMustRelease_AMultiBindingPatternIsNotTracked(t *testing.T) {
	assertClean(t, `
let pair = (n: i64) -> (Sound, i64) => (load_sound(n), n)
let main = () -> void => {
  let (snd, n) = pair(1)
  play_sound(snd)
}
`)
}

// --- acquiring and unwrapping in one statement ----------------------------------------
//
// This is what the idiom actually looks like — the resource never sits in a binding of
// its own — so it is the shape that matters most, not a corner.

func TestMustRelease_IfLetOnADirectAcquisitionIsTracked(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  if let Some(v) = try_load(1) { play_sound(v) }
}
`, "v")
}

func TestMustRelease_IfLetOnADirectAcquisitionReleasedIsClean(t *testing.T) {
	assertClean(t, `
let main = () -> void => {
  if let Some(v) = try_load(1) { unload_sound(v) }
}
`)
}

func TestMustRelease_MatchOnADirectAcquisitionIsTracked(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  match try_load(1) {
    Some(v) => play_sound(v),
    None => {},
  }
}
`, "v")
}

// Reading a field is a **borrow**, not a handover: the value itself is going nowhere, so
// the obligation stays. Without this the feature is gutted for any resource with a
// readable field — `println("${w.frame_count}")` would silence the check on a `Wave` —
// and that is not hypothetical: it is how the miss was found, on a program written in
// the if-let style against the real raylib bindings.
func TestMustRelease_ReadingAFieldDoesNotEscape(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  let s = load_sound(1)
  println("${s.id}")
}
`, "s")
}

func TestMustRelease_ReadingAFieldInAnIfLetDoesNotEscape(t *testing.T) {
	assertLeaks(t, `
let main = () -> void => {
  if let Some(v) = try_load(1) { println("${v.id}") }
}
`, "v")
}

// ---------------------------------------------------------------------------------------
// A value read from a stored place is a view, not an acquisition
// ---------------------------------------------------------------------------------------
//
// **Both of these advised a double free** until 09/11. A function that only *looks* at a
// resource held in a struct it was handed — a model viewer reading its model's animation
// clips — was told to release it, and following that advice frees a resource its owner is
// still using. The rule the pass already applied to a field read ("reading a field or an
// element is a borrow") had not reached a `match` scrutinee or a `let` initializer.
// Found by examples/raylib/gltf_viewer.lyra.

// Matching on a field of a borrowed parameter unwraps a view of storage the caller owns.
func TestMustRelease_MatchingOnAFieldOfAParameterIsAView(t *testing.T) {
	assertClean(t, `
struct Holder { res: Maybe<Sound> }
let peek = (h: Holder) -> void =>
  match h.res {
    Some(s) => play_sound(s),
    None => { },
  }
let main = () -> void => { }
`)
}

// Copying a resource out of a field makes a second handle to the same resource, not a new
// one — the model viewer copies its model to give the copy a different transform.
func TestMustRelease_CopyingAFieldOfAParameterIsAView(t *testing.T) {
	assertClean(t, `
struct Holder { s: Sound }
let peek = (h: Holder) -> void => {
  let copy = h.s
  play_sound(copy)
}
let main = () -> void => { }
`)
}

// The line these two must not cross: a *call* that answers a resource is still an
// acquisition wherever its result is bound, and still leaks when it is not released.
func TestMustRelease_ACallIsStillAnAcquisitionBesideAView(t *testing.T) {
	assertLeaks(t, `
struct Holder { s: Sound }
let peek = (h: Holder) -> void => {
  let copy = h.s
  play_sound(copy)
  let fresh = load_sound(2)
  play_sound(fresh)
}
let main = () -> void => { }
`, "fresh")
}
