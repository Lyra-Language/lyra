package llvm

import (
	"strings"
	"testing"
)

// A user function's emitted symbol is module-qualified under a `lyra.` prefix, which
// keeps it out of the way of two other namespaces sharing the module: libc, and Lyra's
// own emitted runtime.
//
// This is not hypothetical tidiness. Emitted functions used to take their source name
// verbatim, so a program with a function called `malloc`, `write` or `lyra_rc_alloc`
// produced a module clang rejected outright — "invalid redefinition" — and those are
// names a program has every right to use. Each case below failed to compile before.
func TestExec_UserSymbolsDoNotCollideWithTheRuntime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Reaches libc malloc because the string concatenation allocates.
			"a function named malloc",
			`let malloc = (n: i64) -> i64 => 5
			 let main = () -> u8 => {
			   let s = "x" ++ "y"
			   if s == "xy" { u8(malloc(1)) } else { 0 }
			 }`,
			5,
		},
		{
			// print lowers to a libc write.
			"a function named write",
			`let write = (n: i64) -> i64 => 3
			 let main = () -> u8 => u8(write(1))`,
			3,
		},
		{
			// The ref-counted runtime is emitted into the module itself, so its own
			// symbols are in scope to clash with.
			"a function named lyra_rc_alloc",
			`let lyra_rc_alloc = (n: i64) -> i64 => 4
			 let main = () -> u8 => {
			   let s = "x" ++ "y"
			   if s == "xy" { u8(lyra_rc_alloc(1)) } else { 0 }
			 }`,
			4,
		},
		{
			"a function named memcmp",
			`let memcmp = (n: i64) -> i64 => 6
			 let main = () -> u8 => {
			   let s = "a" ++ "b"
			   if s == "ab" { u8(memcmp(1)) } else { 0 }
			 }`,
			6,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// The prefix carries the module, which is what makes a symbol unique across modules —
// the property separate compilation and per-module private names will both rest on.
func TestEmit_UserSymbolIsModuleQualified(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `let helper = () -> i64 => 1
	 let main = () -> u8 => u8(helper())`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "@lyra.helper(") {
		t.Errorf("expected the user function to be emitted as @lyra.helper:\n%s", ir)
	}
	// main keeps its own name: it is the C entry point the platform links against.
	if !strings.Contains(ir, "define i32 @main(") {
		t.Errorf("main must keep the C entry-point symbol:\n%s", ir)
	}
	// The prefix always has a dot after `lyra`, so it can never spell one of the
	// runtime's own `lyra_`-prefixed symbols.
	if strings.Contains(ir, "@lyra_helper") {
		t.Errorf("the prefix must not collide with the runtime's lyra_ namespace:\n%s", ir)
	}
}
