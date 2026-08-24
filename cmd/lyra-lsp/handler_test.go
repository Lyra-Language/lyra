package main

import (
	"errors"
	"testing"
)

// **A panicking handler must answer empty, not take the server down.** If one request
// kills the process the editor loses every other feature with it, and the user sees a
// language server that "stopped working" with nothing to report.
//
// Thirteen handlers each carried their own copy of the deferred recover; they defer
// recoverHandler now, and this is the check that the one copy still does what thirteen did.
// The mechanism is easy to break by accident: recover() reports a panic only when called by
// the deferred function itself, so wrapping recoverHandler in a closure — the obvious
// tidy-up — would silently stop it working, and every test here would still pass except
// this one.
func TestRecoverHandler_ReturnsZeroValueAndNoError(t *testing.T) {
	boom := func() (result []int, retErr error) {
		defer recoverHandler("test", &result, &retErr)
		result = []int{1, 2, 3} // a handler part-way through its answer
		panic("kaboom")
	}
	got, err := boom()
	if err != nil {
		t.Errorf("expected a nil error so the client reads an empty answer, got %v", err)
	}
	if got != nil {
		t.Errorf("expected the zero value, got %v — a half-built answer must not escape", got)
	}
}

// The guard must be inert when nothing panics: a handler's real result and its real error
// both have to survive it.
func TestRecoverHandler_PassesThroughWhenNoPanic(t *testing.T) {
	sentinel := errors.New("real error")
	fine := func() (result []int, retErr error) {
		defer recoverHandler("test", &result, &retErr)
		return []int{7}, sentinel
	}
	got, err := fine()
	if !errors.Is(err, sentinel) {
		t.Errorf("a real error must survive the guard, got %v", err)
	}
	if len(got) != 1 || got[0] != 7 {
		t.Errorf("a real result must survive the guard, got %v", got)
	}
}
