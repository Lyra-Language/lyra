package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildAndRunCapture compiles src, runs the binary, and returns its stdout plus
// exit code. Unlike buildAndRun (exit code only), print's observable behavior is
// what it writes to stdout, so these tests capture it.
func buildAndRunCapture(t *testing.T, src string) (string, int) {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping behavioral test")
	}
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
	if out, err := exec.Command(clang, llPath, "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("clang rejected the IR: %v\n%s\n--- IR ---\n%s", err, out, ir)
	}
	out, err := exec.Command(binPath).Output()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running the binary failed: %v", err)
		}
	}
	return string(out), code
}

// print/println write a string to stdout via libc write(1, data, len). println
// adds a trailing newline. Both work in main (void or exit-code) and in a
// user-defined void function, and preserve program order across calls (each is
// its own write syscall).
func TestExec_Print(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantOut string
		wantErr int
	}{
		{
			"println hello world",
			`let main = () -> void => println("Hello, world!")`,
			"Hello, world!\n",
			0,
		},
		{
			"print has no newline",
			`let main = () -> void => print("no newline")`,
			"no newline",
			0,
		},
		{
			"ordering preserved across calls",
			`let main = () -> u8 => {
			   print("a")
			   print("b")
			   println("c")
			   0
			 }`,
			"abc\n",
			0,
		},
		{
			"empty string is a valid no-op write",
			`let main = () -> void => {
			   print("")
			   println("x")
			 }`,
			"x\n",
			0,
		},
		{
			"concatenated argument",
			`let main = () -> void => println("Hello, " ++ "Lyra")`,
			"Hello, Lyra\n",
			0,
		},
		{
			"through a void helper function",
			`let greet = (name: string) -> void => println("Hi, " ++ name)
			 let main = () -> void => {
			   greet("a")
			   greet("b")
			 }`,
			"Hi, a\nHi, b\n",
			0,
		},
		{
			"print then exit code",
			`let main = () -> u8 => {
			   println("side effect")
			   7
			 }`,
			"side effect\n",
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := buildAndRunCapture(t, c.src)
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != c.wantErr {
				t.Errorf("exit = %d; want %d", code, c.wantErr)
			}
		})
	}
}

// Non-string print formatting: integers (snprintf %lld/%llu by signedness),
// floats (%g), bool ("true"/"false"), and runes (UTF-8 encoded). Numeric
// formatting goes through snprintf-into-memory + write, so it stays in program
// order with the raw writes that string/bool/rune print use.
func TestExec_PrintFormatting(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{"int", `let main = () -> void => println(42)`, "42\n"},
		{"negative int", `let f = (n: i32) -> i32 => n
			 let main = () -> void => println(f(0 - 5))`, "-5\n"},
		{"unsigned wraps high", `let f = (n: u8) -> u8 => n
			 let main = () -> void => println(f(200))`, "200\n"},
		{"bool true", `let main = () -> void => println(true)`, "true\n"},
		{"bool false", `let main = () -> void => println(false)`, "false\n"},
		{"rune ascii", `let main = () -> void => println('A')`, "A\n"},
		{"rune 3-byte utf8", `let main = () -> void => println('世')`, "世\n"},
		{"rune 4-byte utf8", `let main = () -> void => println('😀')`, "😀\n"},
		{"float", `let main = () -> void => println(3.14)`, "3.14\n"},
		{"mixed string and int order", `let main = () -> void => {
			   print("count: ")
			   println(7)
			 }`, "count: 7\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := buildAndRunCapture(t, c.src)
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != 0 {
				t.Errorf("exit = %d; want 0", code)
			}
		})
	}
}

// An integer print goes through libc snprintf into a stack buffer (not stdio),
// then write — so ordering with raw string writes is preserved.
func TestEmit_PrintIntUsesSnprintf(t *testing.T) {
	got, err := emitSource(t, `let main = () -> void => println(42)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@snprintf", "@write"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
}

// print/println lower to a libc write(1, ...); a println emits two writes (the
// string then the newline byte). The IR pins that shape.
func TestEmit_PrintIR(t *testing.T) {
	got, err := emitSource(t, `let main = () -> void => println("hi")`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"declare i64 @write(i32", // the libc shim (llir prints param names)
		"call i64 @write(i32 1",  // fd 1 (stdout)
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "call i64 @write"); n != 2 {
		t.Errorf("println should emit 2 writes (string + newline); got %d", n)
	}
}

// A heap temporary passed to a borrowing print (`println("a" ++ b)`) is released
// after the call, not leaked — the ownership pass treats print/println as
// borrowing their argument. Verified as alloc/release balance in the IR.
func TestEmit_PrintReleasesConcatTemp(t *testing.T) {
	for _, src := range []string{
		`let main = () -> void => println("Hi, " ++ "x")`,
		`let main = () -> u8 => { println("Hi, " ++ "x")
		   0 }`,
	} {
		got, err := emitSource(t, src)
		if err != nil {
			t.Fatal(err)
		}
		allocs := strings.Count(got, "call i8* @lyra_rc_alloc")
		releases := strings.Count(got, "call void @lyra_rc_release")
		if allocs != releases {
			t.Errorf("alloc/release imbalance (leak): %d alloc, %d release\nsrc: %s", allocs, releases, src)
		}
	}
}
