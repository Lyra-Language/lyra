package llvm

import (
	"strings"
	"testing"
)

// A top-level `let`/`var` holding **data** (08/14).
//
// It type-checked clean and died in the backend as `llvm: unbound identifier` — hazard 5
// inverted. Nothing collected it: `const` was recorded for inlining, functions went through
// forEachUserFunction, and a top-level binding that was neither fell through both, so
// `const` was the only way to give a module a string. Escape-sequence tables are the
// natural thing to write that way, which is how it surfaced.
//
// They get a module-level slot, zero-initialized, filled at the top of `main` in
// declaration order — `main` being the one place guaranteed to run before anything reads
// one, which avoids llvm.global_ctors and the cross-unit ordering it would bring.
func TestExec_TopLevelValueBindings(t *testing.T) {
	t.Parallel()
	src := `
let greeting = "hello"
let count = 42
var tally = 0
let doubled = count * 2
let read_it = () -> string => greeting
let main = () -> void => {
  println(read_it());
  println("${greeting} ${count} ${doubled}");
  tally = tally + 7;
  tally += 3;
  println("${tally}");
}
`
	want := "hello\nhello 42 84\n10"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **A global's initializer may read an earlier global**, which is why declaration order is
// the initialization order — the same order the use-before-declaration checker already
// enforces on the way in, so this is not a second policy.
func TestExec_TopLevelBindingInitializedFromAnother(t *testing.T) {
	t.Parallel()
	src := `
let shades = " .:-=+*#%@"
let width = shades.len()
let doubled = width * 2
let main = () -> void => { println("${width} ${doubled}"); }
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "10 20" {
		t.Errorf("got %q; want \"10 20\"", got)
	}
}

// A managed global — a string, an array — is a ref-counted box the global owns for the
// life of the program. Mutating one from a *function* is the case that makes it module
// state rather than a constant, and it has to persist across calls.
func TestExec_TopLevelManagedBindingIsModuleState(t *testing.T) {
	t.Parallel()
	src := `
var hits: []i64 = []
let table: []string = ["red", "green", "blue"]
let bump = (n: i64) -> i64 => { hits.push(n); hits.len() }
let main = () -> void => {
  println("${bump(1)} ${bump(2)} ${bump(3)}");
  println(table.join(", "));
}
`
	want := "1 2 3\nred, green, blue"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **A local shadows a global**, which is the ordering every path shares — reading,
// assigning, reassigning and compound-assigning all go through one lookup (slotFor) for
// exactly this reason: a site that disagreed would produce a wrong *value*, not an error.
func TestExec_LocalShadowsAGlobal(t *testing.T) {
	t.Parallel()
	src := `
let name = "global"
let inner = () -> string => { let name = "local"; name }
let main = () -> void => { println("${inner()} then ${name}"); }
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "local then global" {
		t.Errorf("got %q; want \"local then global\"", got)
	}
}

// The case that surfaced it: ANSI escapes as module-level data, which is what a TUI's
// colour table is. `\e` and `\x1b` both reach stdout as byte 27, so the display half of a
// terminal UI needs nothing from the compiler — only somewhere to put the strings.
func TestExec_TopLevelEscapeSequenceTable(t *testing.T) {
	t.Parallel()
	src := `
let reset = "\e[0m"
let bold = "\x1b[1m"
let main = () -> void => { print(bold ++ "hi" ++ reset ++ "\n"); }
`
	if got := buildAndRunWithPrelude(t, src, ""); got != "\x1b[1mhi\x1b[0m\n" {
		t.Errorf("got %q; want the bolded string with both escapes", got)
	}
}

// **A global declared below the function reading it is retained when stored** (10/19).
// The house style puts helpers and their tables below their users, and an unannotated
// global's read there had no recorded type — the typechecker reached the declaration
// after the function — so ownership, which asks the recorded type whether a value is
// managed, stored `sheet` into each construction without a retain. Two constructions,
// two releases of one array: a use-after-free in the drop glue, found by
// `bindings.imgui.ui`'s components, whose style sheets are globals written below them.
// Plain and generic, since the generic specialization is where it first showed.
func TestExec_GlobalBelowItsReaderIsRetainedWhenStored(t *testing.T) {
	t.Parallel()
	src := `
struct Sheet { rules: []string = [] }
data Node<e> = Comp(Sheet, []Node<e>) | Leaf(e)
data Plain = PlainComp(Sheet) | PlainLeaf

let main = () -> u8 => {
  let nodes: []Node<i64> = [wrap(1), wrap(2)]
  let plain: []Plain = [wrap_plain(), wrap_plain()]
  var n = 0
  for node in nodes {
    match node {
      Comp(s, children) => { n += s.rules.len() + children.len() },
      Leaf(_) => {},
    }
  }
  for p in plain {
    match p {
      PlainComp(s) => { n += s.rules.len() },
      PlainLeaf => {},
    }
  }
  u8(n)
}

let wrap = (v: e) -> Node<e> => Comp(sheet, [Leaf(v)])

let wrap_plain = () -> Plain => PlainComp(sheet)

let sheet = Sheet { rules: ["a", "b"] }
`
	// (2 rules + 1 child) twice, then 2 rules twice.
	if got := buildAndRunASanWithPrelude(t, src); got != 10 {
		t.Errorf("under ASan: exited %d; want 10", got)
	}
}
