package llvm

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// **`@noinline` reaches the IR on every way a function is emitted**: a plain one, each
// specialization of a generic one — and on nothing else. A specialization is declared on a
// path of its own (monomorphize.go), which the first version missed.
func TestEmit_NoInlineMarksTheFunction(t *testing.T) {
	t.Parallel()
	const src = `module main
@noinline
let once = pure (n: i64) -> i64 => n * 3
@noinline
let first<t> = pure (xs: []t) -> t => xs[0]
let inlined = pure (n: i64) -> i64 => n + 1
let main = () -> void => {
  println("${once(2)} ${first([1, 2])} ${first(["a"])} ${inlined(1)}")
}
`
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	out, err := New().Emit(res, ep)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]bool{}
	define := regexp.MustCompile(`^define [^@]*@("[^"]+"|[^(]+)\(`)
	for _, line := range strings.Split(string(out), "\n") {
		if m := define.FindStringSubmatch(line); m != nil {
			marked[strings.Trim(m[1], `"`)] = strings.Contains(line[strings.LastIndex(line, ")"):], "noinline")
		}
	}
	want := func(part string, noinline bool, least int) {
		t.Helper()
		n := 0
		for name, has := range marked {
			if strings.Contains(name, part) {
				n++
				if has != noinline {
					t.Errorf("%s: noinline %v, want %v", name, has, noinline)
				}
			}
		}
		if n < least {
			t.Errorf("found %d functions named like %q, want at least %d (defined: %v)", n, part, least, marked)
		}
	}
	want(".once", true, 1)
	want(".first", true, 2)
	want(".inlined", false, 1)
}
