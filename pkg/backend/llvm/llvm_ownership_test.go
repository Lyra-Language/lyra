package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
}

// TestExec_Ownership runs each ownership program and checks its result. A wrong
// answer means a managed value was freed while still in use (or never built);
// the program crashing means a double free.
func TestExec_Ownership(t *testing.T) {
	for _, c := range ownershipCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_OwnershipASan compiles each case with AddressSanitizer and checks it
// still exits with the right code — a double free or use-after-free from a
// mis-placed release aborts under ASan instead. (macOS ASan doesn't run the leak
// sanitizer, so this proves memory *safety*, not the absence of the known,
// deliberate leaks — aggregates, break paths.) Skips if the toolchain can't build
// an ASan binary.
func TestExec_OwnershipASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range ownershipCases {
		if got := buildAndRunASan(t, clang, c.src); got != c.want {
			t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_OwnershipIR pins the retain/release balance for a few canonical
// shapes, proving strings are actually freed (a leak would show a missing
// release) and copies retained.
func TestEmit_OwnershipIR(t *testing.T) {
	count := func(src, needle string) int {
		got, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return strings.Count(got, needle)
	}

	// A single owned binding, compared then dropped: one alloc, one release, no
	// retain (no copy). If the release were missing this string would leak.
	single := `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   if a == "xy" { 1 } else { 0 }
	 }`
	if n := count(single, "call void @lyra_rc_release"); n != 1 {
		t.Errorf("single binding: want 1 release, got %d", n)
	}
	if n := count(single, "call void @lyra_rc_retain"); n != 0 {
		t.Errorf("single binding: want 0 retains, got %d", n)
	}

	// A copy retains once, and both bindings are released (two releases).
	copy := `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   let b: string = a
	   if b == "xy" { 1 } else { 0 }
	 }`
	if n := count(copy, "call void @lyra_rc_retain"); n != 1 {
		t.Errorf("copy: want 1 retain, got %d", n)
	}
	if n := count(copy, "call void @lyra_rc_release"); n != 2 {
		t.Errorf("copy: want 2 releases, got %d", n)
	}
}

// buildAndRunASan emits IR for src, compiles it with -fsanitize=address, runs the
// binary, and returns its exit code. detect_leaks is off (macOS has no LSan, and
// deliberate leaks remain), so this checks only memory-safety violations.
func buildAndRunASan(t *testing.T, clang, src string) int {
	t.Helper()
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	dir := t.TempDir()
	llPath := filepath.Join(dir, "prog.ll")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(llPath, []byte(ir), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(clang, "-fsanitize=address", llPath, "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("clang -fsanitize=address rejected the IR: %v\n%s", err, out)
	}
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("running the asan binary failed: %v", err)
	}
	return 0
}

// asanAvailable reports whether the toolchain can build and run an ASan binary
// (some CI images lack the runtime).
func asanAvailable(t *testing.T, clang string) bool {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.c")
	bin := filepath.Join(dir, "probe")
	if err := os.WriteFile(src, []byte("int main(void){return 0;}"), 0o644); err != nil {
		return false
	}
	if err := exec.Command(clang, "-fsanitize=address", src, "-o", bin).Run(); err != nil {
		return false
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	return cmd.Run() == nil
}
