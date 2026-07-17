package ownership_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// analyze runs the front-end and returns the ownership table, failing on any
// analysis error so a test only ever sees a well-typed program.
func analyze(t *testing.T, src string) (retains, temps int) {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	if res.Ownership == nil {
		t.Fatal("no ownership table")
	}
	return len(res.Ownership.Retain), len(res.Ownership.ReleaseTemp)
}

// A copy of a managed binding retains once (the copy mints its own +1); nothing
// is a temporary to release (both are named bindings, dropped at scope exit).
func TestOwnership_CopyRetains(t *testing.T) {
	retains, temps := analyze(t, `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   let b: string = a
	   if b == "xy" { 1 } else { 0 }
	 }`)
	if retains != 1 {
		t.Errorf("copy: want 1 retain, got %d", retains)
	}
	if temps != 0 {
		t.Errorf("copy: want 0 temp-releases, got %d", temps)
	}
}

// A concat bound to a `let` transfers into the binding — no retain, no temp
// release (the binding owns it and drops it at scope exit).
func TestOwnership_ConcatBindingTransfers(t *testing.T) {
	retains, temps := analyze(t, `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   if a == "xy" { 1 } else { 0 }
	 }`)
	if retains != 0 || temps != 0 {
		t.Errorf("concat binding: want 0/0 retain/temp, got %d/%d", retains, temps)
	}
}

// A concat consumed directly by a comparison (never bound) is an owned temporary
// released after the statement.
func TestOwnership_ConcatTemporaryReleased(t *testing.T) {
	retains, temps := analyze(t, `let main = () -> u8 => if ("x" ++ "y") == "xy" { 1 } else { 0 }`)
	if retains != 0 {
		t.Errorf("temp: want 0 retains, got %d", retains)
	}
	if temps != 1 {
		t.Errorf("temp: want 1 temp-release, got %d", temps)
	}
}

// Returning a borrowed local (an identifier) from an owned-return function
// retains it for the caller.
func TestOwnership_ReturnLocalRetains(t *testing.T) {
	retains, _ := analyze(t, `let make = () -> string => {
	   let s: string = "a" ++ "b"
	   s
	 }
	 let main = () -> u8 => if make() == "ab" { 1 } else { 0 }`)
	// One retain: `s` in the tail (owned-return) position. (make() in main is an
	// owned result consumed by ==, a temp release, not a retain.)
	if retains != 1 {
		t.Errorf("return local: want 1 retain, got %d", retains)
	}
}

// A borrow (`ref`/bare) parameter isn't owned by the callee; passing an owned
// argument to it leaves the temporary to the caller to release. An `own`
// parameter takes it (transfer, no caller release).
func TestOwnership_OwnParamTransfers(t *testing.T) {
	// bare param: the argument temporary is released by the caller.
	_, tempsBare := analyze(t, `let f = (s: string) -> u8 => if s == "hi" { 1 } else { 0 }
	 let main = () -> u8 => f("h" ++ "i")`)
	if tempsBare != 1 {
		t.Errorf("bare param: want 1 temp-release (caller frees), got %d", tempsBare)
	}
	// own param: ownership transfers to the callee, so the caller does not release.
	_, tempsOwn := analyze(t, `let f = (s: own string) -> u8 => if s == "hi" { 1 } else { 0 }
	 let main = () -> u8 => f("h" ++ "i")`)
	if tempsOwn != 0 {
		t.Errorf("own param: want 0 temp-releases (callee frees), got %d", tempsOwn)
	}
}
