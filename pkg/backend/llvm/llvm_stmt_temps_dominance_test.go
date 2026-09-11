package llvm

import (
	"os/exec"
	"testing"
)

// A statement's temporaries are released at its end only where their production
// block dominates it, and that dominance used to be computed from the function's
// entry — mid-lowering, while an enclosing `match` had not yet sealed its dispatch.
// From the entry the statement's blocks were unreachable, `dominates` answered false,
// and false there means "release where it was produced": the left operand of `++`
// was freed at the float guard of `x.floor()` in the right one, then memcpy'd from.
// Three things are needed to reach it — a statement inside a match arm, a temporary
// produced after a block split (the unprovable `clip + 1` check), and a later split
// in the same statement (the float guard). Found by the glTF viewer, whose clip line
// came out with its first 23 bytes zeroed. Verified under AddressSanitizer.
func TestExec_StatementTempsOutliveALaterGuardInAMatchArmASan(t *testing.T) {
	t.Parallel()
	src := `struct Held { name: string, n: i64 }
let label = pure (h: Held, i: i64) -> string => "${h.name}#${i}"
let main = () -> u8 => {
  var viewing: Maybe<Held> = Some(Held { name: "fox", n: 3 })
  var clip = program_args().len()
  var frame: f32 = 7.5
  var line = "none"
  match viewing {
    Some(o) => {
      line = "clip ${clip + 1}: ${label(o, clip)}   frame " ++ "${f64(frame).floor()}/9"
    },
    None => { },
  }
  if line == "clip 2: fox#1   frame 7/9" { 0 } else { 1 }
}
`
	clang := lookClang(t)
	if got := exitCodeOf(t, exec.Command(preludeBinary(t, src)).Run()); got != 0 {
		t.Errorf("exited %d; want 0 (the line intact)", got)
	}
	if got := exitCodeOf(t, exec.Command(compileCached(t, clang, instrumentForASan(emitWithPrelude(t, src)), "-fsanitize=address")).Run()); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}

// The same premature release in value position: the arm's tail *is* the match's
// value, so its temporaries are held for the enclosing `let` and flushed there — where
// the arm's production block does not dominate the merge, since `None` reaches it too.
func TestExec_ValueArmTempsOutliveALaterGuardASan(t *testing.T) {
	t.Parallel()
	src := `let label = pure (i: i64) -> string => "fox#${i}"
let main = () -> u8 => {
  let viewing: Maybe<i64> = if program_args().len() > 0 { Some(3) } else { None }
  var clip = program_args().len()
  var frame: f32 = 7.5
  let line = match viewing {
    Some(o) => "clip ${clip + 1}: ${label(o)}   frame " ++ "${f64(frame).floor()}/9",
    None => "none",
  }
  if line == "clip 2: fox#3   frame 7/9" { 0 } else { 1 }
}
`
	clang := lookClang(t)
	if got := exitCodeOf(t, exec.Command(preludeBinary(t, src)).Run()); got != 0 {
		t.Errorf("exited %d; want 0 (the line intact)", got)
	}
	if got := exitCodeOf(t, exec.Command(compileCached(t, clang, instrumentForASan(emitWithPrelude(t, src)), "-fsanitize=address")).Run()); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}
