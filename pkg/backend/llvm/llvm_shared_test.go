package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A `shared` value is heap-allocated in a ref-counted box and freed by the
// ownership model like a string. These cases construct shared structs (and a
// recursive `data`), read fields through the box, and — via buildAndRun/ASan —
// confirm the value is freed exactly once.
var sharedCases = []struct {
	name string
	src  string
	want int
}{
	{
		"construct and read a field",
		`struct Box { v: u8, }
		 let main = () -> u8 => {
		   let b: shared Box = Box { v: 42 }
		   b.v
		 }`,
		42,
	},
	{
		// A shared-to-shared copy is the binding's last use → transfer (no dup); the
		// box moves to the new binding, which frees it at its last use.
		"transfer a shared value, then read",
		`struct Box { v: u8, }
		 let main = () -> u8 => {
		   let a: shared Box = Box { v: 9 }
		   let b: shared Box = a
		   b.v
		 }`,
		9,
	},
	{
		// A wider payload read back and narrowed through the box.
		"i64 payload read through the box",
		`struct Box { v: i64, }
		 let main = () -> u8 => {
		   let b: shared Box = Box { v: 200 }
		   u8(b.v)
		 }`,
		200,
	},
	{
		// A recursive `data` whose recursive field is `shared` (a pointer): the
		// nested constructor is heap-boxed and stored into the field.
		"recursive shared data construction",
		`data List = Nil | Cons(i64, shared List)
		 let main = () -> u8 => {
		   let c: shared List = Cons(1, Nil)
		   0
		 }`,
		0,
	},
}

// TestExec_Shared runs each shared program and checks its result — a wrong answer
// means the box was mis-read or freed while live.
func TestExec_Shared(t *testing.T) {
	for _, c := range sharedCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_SharedASan runs each shared program under AddressSanitizer — a
// double-free or use-after-free from a mis-placed release aborts.
func TestExec_SharedASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range sharedCases {
		if got := buildAndRunASan(t, clang, c.src); got != c.want {
			t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_SharedIR pins the representation and conservation: a `shared` value is
// a pointer to a `{ i64, payload }` box (lyra_rc_alloc), and the constructed value
// is released exactly once (allocations == releases — no leak, no double free).
func TestEmit_SharedIR(t *testing.T) {
	got, err := emitSource(t, `struct Box { v: u8, }
	 let main = () -> u8 => {
	   let b: shared Box = Box { v: 42 }
	   b.v
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"call i8* @lyra_rc_alloc", "{ i64, %Box }"} {
		if !strings.Contains(got, want) {
			t.Errorf("shared IR missing %q:\n%s", want, got)
		}
	}
	allocs := strings.Count(got, "call i8* @lyra_rc_alloc")
	rels := strings.Count(got, "call void @lyra_rc_release")
	if allocs != 1 {
		t.Errorf("want 1 box allocation, got %d", allocs)
	}
	if rels != allocs {
		t.Errorf("conservation: %d releases != %d allocations (leak or double free)", rels, allocs)
	}
}
