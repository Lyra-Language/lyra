package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// String interpolation (`"… ${expr} …"`) lowers to a heap string: each segment
// is formatted to bytes (the same formatForPrint machinery print uses) and the
// pieces are concatenated into one fresh ref-counted box. These tests print the
// interpolated string and check the exact stdout — the observable result.

func TestExec_StringInterpolation(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{
			"all scalar kinds",
			`let main = () -> void => {
			   let name: string = "world"
			   let n: i32 = 42
			   let pi: f64 = 3.14
			   let flag: bool = true
			   let ch: rune = 'x'
			   println("Hello, ${name}! n=${n} pi=${pi} flag=${flag} ch=${ch}")
			 }`,
			"Hello, world! n=42 pi=3.14 flag=true ch=x\n",
		},
		{
			// The whitespace-preservation regression: text right after a `}` (and a
			// plain leading space) must survive — the collector reconstructs literal
			// chunks from raw source, since tree-sitter strips a content chunk's
			// leading whitespace.
			"whitespace around interpolations",
			`let main = () -> void => {
			   let n: i32 = 1
			   println("a ${n} b ${n} c")
			 }`,
			"a 1 b 1 c\n",
		},
		{
			"adjacent interpolations, no literals between",
			`let main = () -> void => {
			   let a: string = "X"
			   let b: string = "Y"
			   println("${a}${b}")
			 }`,
			"XY\n",
		},
		{
			"leading and trailing interpolation",
			`let main = () -> void => {
			   let a: i32 = 7
			   println("${a}!")
			 }`,
			"7!\n",
		},
		{
			"unsigned formats unsigned (no sign)",
			`let main = () -> void => {
			   let a: u8 = 200
			   println("v=${a}")
			 }`,
			"v=200\n",
		},
		{
			"interpolating a computed expression",
			`let main = () -> void => {
			   let a: i32 = 20
			   let b: i32 = 22
			   println("sum=${a + b}")
			 }`,
			"sum=42\n",
		},
		{
			// A concatenation of the result proves the interpolated value is a real
			// string usable everywhere a string is.
			"interpolated string composes with ++",
			`let main = () -> void => {
			   let n: i32 = 3
			   println("[" ++ "n=${n}" ++ "]")
			 }`,
			"[n=3]\n",
		},
	}
	for _, c := range cases {
		out, code := buildAndRunCapture(t, c.src)
		if code != 0 {
			t.Errorf("%s: exited %d, want 0", c.name, code)
		}
		if out != c.wantOut {
			t.Errorf("%s: stdout = %q, want %q", c.name, out, c.wantOut)
		}
	}
}

// TestEmit_InterpolationIR pins that interpolation heap-allocates its result
// (one ref-counted box) and that the ownership pass frees it — a leak would show
// a missing release, a double free a second one.
func TestEmit_InterpolationIR(t *testing.T) {
	// The interpolated string is built (one alloc), printed (borrowed), then dead:
	// the ownership pass releases it once.
	src := `let main = () -> void => {
	   let n: i32 = 1
	   println("n=${n}")
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if n := strings.Count(ir, "call i8* @lyra_rc_alloc"); n != 1 {
		t.Errorf("want exactly 1 heap allocation for the interpolated string, got %d", n)
	}
	if n := strings.Count(ir, "call void @lyra_rc_release"); n != 1 {
		t.Errorf("want exactly 1 release (the interpolated temp is freed), got %d", n)
	}
}

// TestExec_InterpolationASan compiles interpolation programs with AddressSanitizer
// to prove the heap result is memory-safe (a double free / use-after-free aborts).
func TestExec_InterpolationASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	cases := []string{
		`let main = () -> u8 => {
		   let n: i32 = 1
		   println("n=${n}")
		   0
		 }`,
		// A heap-string segment plus binding the result and using it twice.
		`let main = () -> u8 => {
		   let who: string = "a" ++ "b"
		   let s: string = "hi ${who} bye"
		   println(s)
		   if s == "hi ab bye" { 0 } else { 1 }
		 }`,
	}
	for i, src := range cases {
		if got := buildAndRunASan(t, clang, src); got != 0 {
			t.Errorf("case %d (asan): exited %d, want 0", i, got)
		}
	}
}
