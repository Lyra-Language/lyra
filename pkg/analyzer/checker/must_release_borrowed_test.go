package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// **A `@borrowed` function's result carries no obligation.** raylib's `default_font()`
// answers raylib's own static `Font`, and binding one drew lyra-W022 advising
// `unload_font` — a call raylib documents as wrong on that font (09/13).
func TestMustRelease_BorrowedResultIsNotHeld(t *testing.T) {
	const borrowed = `
@borrowed
let shared_sound = () -> Sound => Sound { id: 0 }
@borrowed
let maybe_shared = () -> Maybe<Sound> => None
`
	assertClean(t, borrowed+`
let main = () -> void => {
  let s = shared_sound()
  play_sound(s)
  let u = unsafe { shared_sound() }
  play_sound(u)
  if let Some(m) = maybe_shared() { play_sound(m) }
  match maybe_shared() { Some(v) => play_sound(v), None => {} }
}
`)
	// A loader beside it is still tracked, so the attribute exempts the call and not the type.
	assertLeaks(t, borrowed+`
let main = () -> void => {
  let s = shared_sound()
  play_sound(s)
  let mine = load_sound(1)
  play_sound(mine)
}
`, "mine")
}

// `@borrowed` on a function whose result is not a resource says nothing, and is refused
// rather than left to read as though it did.
func TestMustRelease_BorrowedOnANonResourceIsRefused(t *testing.T) {
	source := mustReleasePrelude + `
@borrowed
let answer = () -> i64 => 42
`
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
		msgs = append(msgs, d.Message)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "i64 is not a `@must_release` type") {
		t.Errorf("want the misplaced-@borrowed error, got %v", msgs)
	}
}

// mustReleaseDiagnostics runs the pass over the shared declarations and returns every
// diagnostic as "code: message".
func mustReleaseDiagnostics(t *testing.T, body string) []string {
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
	var out []string
	for _, d := range checker.CheckMustRelease(program, symTable, tt, tc.MethodTable()) {
		out = append(out, d.Code+": "+d.Message)
	}
	return out
}

// **Releasing a borrowed resource is lyra-W025**: it frees what the lender still uses —
// `unload_font(default_font())`, the call raylib documents as wrong. Each way the value can
// reach the release is caught: as the argument itself, through a binding, through an unwrap,
// and out of an `unsafe` block.
func TestMustRelease_ReleasingABorrowedResource(t *testing.T) {
	const borrowed = `
@borrowed
let shared_sound = () -> Sound => Sound { id: 0 }
@borrowed
let maybe_shared = () -> Maybe<Sound> => None
`
	for _, c := range []struct{ name, body, want string }{
		{"as the argument", `let main = () -> void => { unload_sound(shared_sound()) }`,
			"`unload_sound` releases the Sound `shared_sound()` answers; `shared_sound` is marked `@borrowed`"},
		{"through a binding", `let main = () -> void => {
  let s = shared_sound()
  play_sound(s)
  unload_sound(s)
}`, "`unload_sound` releases \"s\", which holds the Sound `shared_sound()` answered;"},
		{"through an unwrap", `let main = () -> void => {
  if let Some(m) = maybe_shared() { unload_sound(m) }
}`, "releases \"m\""},
		{"out of an unsafe block", `let main = () -> void => {
  let u = unsafe { shared_sound() }
  unload_sound(u)
}`, "releases \"u\""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := mustReleaseDiagnostics(t, borrowed+c.body)
			if len(got) != 1 || !strings.HasPrefix(got[0], "lyra-W025: ") || !strings.Contains(got[0], c.want) {
				t.Errorf("want one lyra-W025 containing %q, got %v", c.want, got)
			}
		})
	}
	// An owned resource released is still simply released.
	if got := mustReleaseDiagnostics(t, borrowed+`let main = () -> void => { unload_sound(load_sound(1)) }`); len(got) != 0 {
		t.Errorf("releasing an owned resource should say nothing, got %v", got)
	}
}
