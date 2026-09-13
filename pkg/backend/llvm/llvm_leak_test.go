package llvm

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// buildAndRunLSanWithPrelude is buildAndRunASanWithPrelude with **leak detection on**, for
// the tests that exist to say a shape does not leak. Every other ASan helper turns it off,
// because the ownership model still leaks in known places and a suite failing on those
// would bury the faults ASan is there for — so without this nothing checks a leak fix stays
// fixed. Linux only: LeakSanitizer is not available with Apple's clang, and CI runs Linux.
//
// A leak exits 1, so a program under test answers something else.
func buildAndRunLSanWithPrelude(t *testing.T, src string) int {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("LeakSanitizer runs on Linux only; ./asan.sh runs this test there")
	}
	clang := lookClang(t)
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	cmd := exec.Command(compileCached(t, clang, instrumentForASan(emitWithPrelude(t, src)), "-fsanitize=address"))
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=1")
	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	return exitCode(t, cmd.Run())
}

// Temporaries that were never released, found by LeakSanitizer once CI's Linux runner ran a
// test without the helper that turns it off (09/13). Three families, each with the program
// that found it:
//
//   - **a builtin's owned result read as borrowed** — `program_arg(i)` copies argv[i] into a
//     fresh string, and only `read_line` was listed as owning, so every `program_args()`
//     leaked one string per argument;
//   - **a temporary receiver of a `.`-call** — the ownership pass walked a receiver only when
//     the method consumed it, so `mk().len()` and `xs.join(",").len()` leaked the value the
//     method was called on;
//   - **a literal aggregate in a borrowing position** — an array, repeat, comprehension or
//     struct literal passed to a borrowed parameter owned its box and everything moved into
//     it, and only the tuple literal was marked for release.
func TestExec_TemporariesDoNotLeak(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"program_args bound", `let main = () -> u8 => { let a = program_args(); u8(a.len()) + 2 }`, 3},
		{"program_args as a receiver", `let main = () -> u8 => u8(program_args().len()) + 2`, 3},
		{"a call's array as a receiver", `let mk = () -> []i64 => [1, 2, 3]
let main = () -> u8 => u8(mk().len())`, 3},
		{"a call's string as a receiver", `let s = (n: i64) -> string => "n=${n}"
let main = () -> u8 => u8(s(5).len() + s(5).byte_len())`, 6},
		{"a chain of temporaries", `let mk = () -> []string => ["a", "b"]
let main = () -> u8 => u8(mk().join(",").len() + mk().slice(0, 1).len())`, 4},
		{"a field of a temporary as a receiver", `struct H { xs: []i64 }
let mk = () -> H => H { xs: [1, 2, 3] }
let main = () -> u8 => u8(mk().xs.len())`, 3},
		{"a comprehension as a receiver", `let main = () -> u8 => u8([i in 0..<3 | "${i}"].len())`, 3},
		{"literals passed to a borrow", `struct W { xs: []i64 }
let n = (xs: []i64) -> i64 => xs.len()
let w = (v: W) -> i64 => v.xs.len()
let f = (xs: [2]string) -> i64 => xs[0].len()
let main = () -> u8 => u8(n([9, 9]) + w(W { xs: [9] }) + n([i in 0..<3 | i]) + f(["${12}", "b"]))`, 8},
		{"a std.json read chain", `module main
import std.json.{ parse_json, JsonNull, field, as_text }
let main = () -> u8 => match parse_json("{\"a\": \"xyz\"}") {
  Ok(d) => u8(d.field("a").unwrap_or(JsonNull).as_text().unwrap_or("").len()),
  Err(_) => 2,
}`, 3},
		{"a literal default inside a callee", `let total = (m: Maybe<[]i64>) -> i64 => m.unwrap_or([]).len()
let main = () -> u8 => u8(total(Some([9, 9]))) + 1`, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunLSanWithPrelude(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d (1 is LeakSanitizer reporting a leak)", got, c.want)
			}
		})
	}
}
