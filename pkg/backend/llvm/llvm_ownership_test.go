package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ownershipCases are string programs that exercise the ownership model: copies
// (retain), returns of a local (retain-on-escape) and of a temporary (transfer),
// concatenation chains, reassignment, `own` parameters, and conditionals. Each
// must compute the right result — a premature free would corrupt a later
// comparison, a double free would crash. They are shared by the plain behavioral
// test and the AddressSanitizer test.
var ownershipCases = []struct {
	name string
	src  string
	want int
}{
	{
		"copy retains",
		`let main = () -> u8 => {
		   let a: string = "foo" ++ "bar"
		   let b: string = a
		   if a == "foobar" && b == "foobar" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		"return a local string",
		`let make = () -> string => {
		   let s: string = "ab" ++ "cd"
		   s
		 }
		 let main = () -> u8 => if make() == "abcd" { 1 } else { 0 }`,
		1,
	},
	{
		"return a temporary string",
		`let make = () -> string => "ab" ++ "cd"
		 let main = () -> u8 => if make() == "abcd" { 1 } else { 0 }`,
		1,
	},
	{
		"concat chain",
		`let main = () -> u8 => {
		   let s: string = "a" ++ "b" ++ "c" ++ "d"
		   if s == "abcd" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		"reassign a var string releases the old value",
		`let main = () -> u8 => {
		   var s: string = "a" ++ "b"
		   s = s ++ "c"
		   if s == "abc" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		"own parameter consumes the argument",
		`let consume = (s: own string) -> u8 => if s == "hi" { 1 } else { 0 }
		 let main = () -> u8 => consume("h" ++ "i")`,
		1,
	},
	{
		"borrow parameter, caller keeps ownership",
		`let len4 = (s: string) -> u8 => if s == "abcd" { 1 } else { 0 }
		 let main = () -> u8 => {
		   let s: string = "ab" ++ "cd"
		   let x: u8 = len4(s)
		   if x == 1 && s == "abcd" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		"conditional producing a heap string",
		`let pick = (c: bool) -> string => if c { "a" ++ "b" } else { "c" ++ "d" }
		 let main = () -> u8 => if pick(true) == "ab" && pick(false) == "cd" { 1 } else { 0 }`,
		1,
	},
	{
		"identity function threads ownership through",
		`let id = (s: string) -> string => s
		 let main = () -> u8 => {
		   let r: string = id("x" ++ "y")
		   if r == "xy" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		"heap string reused across many comparisons",
		`let main = () -> u8 => {
		   let s: string = "abc" ++ "def"
		   if s == "abcdef" && s != "abc" && s == "abcdef" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		// A binding used only as borrows is dropped at its final borrow (last-use
		// precision), then referenced no more — must still compute correctly.
		"last-use drop after final borrow",
		`let eq = (a: string, b: string) -> u8 => if a == b { 1 } else { 0 }
		 let main = () -> u8 => {
		   let s: string = "ab" ++ "cd"
		   let hit: u8 = eq(s, "abcd")
		   if hit == 1 && s == "abcd" { 9 } else { 0 }
		 }`,
		9,
	},
	{
		// A binding whose *last* use is inside a branch: drop fusion frees it after
		// the enclosing statement (post-dominating the branch), on every path.
		"conditional last use",
		`let use = (s: string) -> u8 => if s == "hit" { 1 } else { 0 }
		 let main = () -> u8 => {
		   let s: string = "hi" ++ "t"
		   let r: u8 = if s == "hit" { use(s) } else { 9 }
		   r
		 }`,
		1,
	},
	{
		// The last use sits in a statement that also early-returns on another path
		// (a nested seal): the return path frees via the frame, the fall-through at
		// last use — the drop must not be "stolen" from the fall-through.
		"nested return in the last-use statement",
		`let pick = (s: string, c: bool) -> u8 => if c { 3 } else { if s == "yo" { 7 } else { 0 } }
		 let main = () -> u8 => {
		   let s: string = "y" ++ "o"
		   pick(s, false)
		 }`,
		7,
	},
	{
		// The last use is inside a nested block, but the binding is declared in the
		// outer block — it's dropped by the outer block after that statement.
		"last use in a nested block",
		`let main = () -> u8 => {
		   let s: string = "ab" ++ "cd"
		   let r: u8 = { if s == "abcd" { 4 } else { 0 } }
		   r
		 }`,
		4,
	},
	{
		// A conditional early return before a binding's last use: the return path
		// frees via the frame, the fall-through frees at last use — exactly once each.
		"early return before last use",
		`let main = () -> u8 => {
		   let s: string = "x" ++ "y"
		   let flag: string = "go" ++ "!"
		   if flag == "stop" { return 3 }
		   if s == "xy" { 7 } else { 0 }
		 }`,
		7,
	},
	{
		// A chain of copies: each `let` is the previous binding's last use, so each
		// transfers (no dups); only the final binding is live at the comparison.
		"chain of transferring copies",
		`let main = () -> u8 => {
		   let a: string = "hi" ++ "!"
		   let b: string = a
		   let c: string = b
		   if c == "hi!" { 5 } else { 0 }
		 }`,
		5,
	},
	{
		// A fresh concat temporary each call (built, borrowed by ==, released) — the
		// loop must not accumulate or double-free across iterations.
		"concat temporary called in a loop",
		`let hit = () -> i64 => if ("x" ++ "y") == "xy" { 1 } else { 0 }
		 let main = () -> u8 => {
		   var hits = 0
		   for var i = 0; i < 5; i += 1 {
		     hits += hit()
		   }
		   u8(hits)
		 }`,
		5,
	},
	{
		// Regression: a borrowed managed value bound into an owning `let` *inside a
		// loop body* needs a retain each iteration to balance the per-iteration frame
		// release. The ownership pass previously never walked loop bodies, so the
		// retain was missing and `s` was released with no dup → heap-use-after-free
		// when read after the loop (ASan-confirmed pre-fix).
		"own string bound inside a loop body",
		`let f = (s: own string) -> u8 => {
		   for var i = 0; i < 2; i += 1 {
		     let y: string = s
		   }
		   if s == "ab" { 3 } else { 4 }
		 }
		 let main = () -> u8 => f("a" ++ "b")`,
		3,
	},
	{
		// Regression: a managed element read out of a container into an owning binding
		// (`let y = xs[0]`, xs a `[]string`) must be duplicated — the container still
		// owns and drops the element, so a bare bind double-frees it. IndexExpr used to
		// hit `default` and record no retain.
		"managed element read into an owning binding",
		`let main = () -> u8 => {
		   let xs: []string = ["a" ++ "b"]
		   let y: string = xs[0]
		   if y == "ab" { 1 } else { 0 }
		 }`,
		1,
	},
	{
		// Regression: a heap temp used as an *earlier* call argument must survive
		// until the call, even when a *later* argument contains a branch (or block)
		// that moves control into a different block before the call happens. The temp
		// was released in its production block — before the call — a use-after-free.
		// Fixed by releasing a start-block temp at the statement-end block instead.
		"temp arg outlives a branch in a later arg (block body)",
		`let f = (a: string, b: string) -> u8 => if a == "xy" { 3 } else { 4 }
		 let g = () -> u8 => 0
		 let main = () -> u8 => {
		   let r = f("x" ++ "y", if 1 < 2 { let z = g()  "p" } else { "q" })
		   r
		 }`,
		3,
	},
	{
		// The same hazard when the function body is a bare expression (flushed at the
		// tail return, not a block boundary) and the later argument is a `match`.
		"temp arg outlives a match in a later arg (expression body)",
		`let f = (a: string, b: string) -> u8 => if a == "xy" { 3 } else { 4 }
		 let main = () -> u8 => f("x" ++ "y", match 1 { 1 => "p", _ => "q" })`,
		3,
	},
	{
		// Two heap temps straddling a branch-valued middle argument: both must outlive
		// the call. A `&&` in the callee also exercises a conditional temp on the
		// borrowing side (which must still release in its own block).
		"two temp args straddle a branch middle arg",
		`let f = (a: string, b: string, c: string) -> u8 => if a == "xy" && c == "mn" { 7 } else { 4 }
		 let main = () -> u8 => f("x" ++ "y", if 1 < 2 { "p" } else { "q" }, "m" ++ "n")`,
		7,
	},
	{
		// A heap temp built inside a *void* branch of a statement-position `if` must be
		// freed on the branch's own (conditional) path — not leaked, not double-freed
		// on the path the branch didn't take. Exercises the void-branch `if` lowering
		// with the conditional-temp flush.
		"heap temp inside a void if-branch",
		`let main = () -> u8 => {
		   var i: u8 = 5
		   if i > 3 { println("a" ++ "b") } else { println("c" ++ "d") }
		   i
		 }`,
		5,
	},
}

// TestExec_Ownership runs each ownership program and checks its result. A wrong
// answer means a managed value was freed while still in use (or never built);
// the program crashing means a double free.
func TestExec_Ownership(t *testing.T) {
	t.Parallel()
	for _, c := range ownershipCases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestExec_OwnershipASan compiles each case with AddressSanitizer and checks it
// still exits with the right code — a double free or use-after-free from a
// mis-placed release aborts under ASan instead. (macOS ASan doesn't run the leak
// sanitizer, so this proves memory *safety*, not the absence of the known,
// deliberate leaks — aggregates, break paths.) Skips if the toolchain can't build
// an ASan binary.
func TestExec_OwnershipASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range ownershipCases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_OwnershipIR pins the retain/release balance for a few canonical
// shapes, proving strings are actually freed (a leak would show a missing
// release) and copies retained.
func TestEmit_OwnershipIR(t *testing.T) {
	t.Parallel()
	count := func(src, needle string) int {
		got, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return strings.Count(got, needle)
	}

	// A single owned binding, used then dead: no retain (nothing is copied), and
	// drop fusion frees it at its last use with exactly one release (no scope-exit
	// no-op backstop).
	single := `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   if a == "xy" { 1 } else { 0 }
	 }`
	if n := count(single, "call void @lyra_rc_retain"); n != 0 {
		t.Errorf("single binding: want 0 retains, got %d", n)
	}
	if n := count(single, "call void @lyra_rc_release"); n != 1 {
		t.Errorf("single binding: want exactly 1 release (drop fusion), got %d", n)
	}

	// A copy is a *transfer* under Perceus last-use — no dup. Before last-use this
	// emitted one retain; now it emits none (the reference moves into b).
	cp := `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   let b: string = a
	   if b == "xy" { 1 } else { 0 }
	 }`
	if n := count(cp, "call void @lyra_rc_retain"); n != 0 {
		t.Errorf("copy (transfer): want 0 retains, got %d", n)
	}
	if n := count(cp, "call void @lyra_rc_release"); n < 1 {
		t.Errorf("copy (transfer): want ≥1 release, got %d", n)
	}

	// Stage-2 fusion end to end: a chain a→b→c transfers a and b at the moves
	// (retired from the frame, no release) and drop-fuses c at its last use — so the
	// whole chain, which allocates one box, emits exactly one release and no retains.
	chain := `let main = () -> u8 => {
	   let a: string = "hi" ++ "!"
	   let b: string = a
	   let c: string = b
	   if c == "hi!" { 1 } else { 0 }
	 }`
	if n := count(chain, "call void @lyra_rc_release"); n != 1 {
		t.Errorf("transfer chain: want exactly 1 release (a,b fused-transferred, c drop-fused), got %d", n)
	}
	if n := count(chain, "call void @lyra_rc_retain"); n != 0 {
		t.Errorf("transfer chain: want 0 retains, got %d", n)
	}

	// Conservation for straight-line code: every heap allocation is released
	// exactly once — releases == allocs. Fewer would leak, more would double-free
	// (and macOS ASan can't see leaks, so this pins it statically). Two heap
	// strings, each dropped at its last use.
	src := `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   let b: string = "p" ++ "q"
	   if a == "xy" && b == "pq" { 1 } else { 0 }
	 }`
	allocs := count(src, "call i8* @lyra_rc_alloc")
	rels := count(src, "call void @lyra_rc_release")
	if allocs != 2 {
		t.Errorf("conservation: expected 2 allocations, got %d", allocs)
	}
	if rels != allocs {
		t.Errorf("conservation: %d releases != %d allocations (leak or double free)", rels, allocs)
	}
}

// asanRunSlots caps how many ASan binaries execute at once. Each ASan process
// reserves a huge shadow-memory address mapping at startup, and launching many
// simultaneously (parallel subtests, warm binary cache) can make that mapping
// sporadically fail on macOS — a transient exit that reads as a test failure.
// The binaries themselves run in milliseconds, so a small cap costs nothing.
var asanRunSlots = make(chan struct{}, 4)

// buildAndRunASan emits IR for src, compiles it with -fsanitize=address, runs the
// binary, and returns its exit code. detect_leaks is off (macOS has no LSan, and
// deliberate leaks remain), so this checks only memory-safety violations.
func buildAndRunASan(t *testing.T, clang, src string) int {
	t.Helper()
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	cmd := exec.Command(compileCached(t, clang, ir, "-fsanitize=address"))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("running the asan binary failed: %v", err)
	}
	return 0
}

// asanAvailable reports whether the toolchain can build and run an ASan binary
// (some CI images lack the runtime). The probe compiles a C program, so the
// result is computed once per test process and shared — the answer can't
// differ between tests, and re-probing cost a clang invocation per caller.
var asanProbe struct {
	once sync.Once
	ok   bool
}

func asanAvailable(t *testing.T, clang string) bool {
	t.Helper()
	asanProbe.once.Do(func() {
		dir, err := os.MkdirTemp("", "lyra-asan-probe")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "probe.c")
		bin := filepath.Join(dir, "probe")
		if err := os.WriteFile(src, []byte("int main(void){return 0;}"), 0o644); err != nil {
			return
		}
		if err := exec.Command(clang, "-fsanitize=address", src, "-o", bin).Run(); err != nil {
			return
		}
		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
		asanProbe.ok = cmd.Run() == nil
	})
	return asanProbe.ok
}
