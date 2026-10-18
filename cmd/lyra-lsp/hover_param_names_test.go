package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// TestHover_FunctionShowsParameterNames pins a function's hover naming its parameters —
// `(w: i64, h: i64) -> i64`, not `(i64, i64) -> i64`, which says what a function takes
// but not which argument is which. Every spelling of a reference to it answers the same:
// the declaration's name, a call, a method-style call, a generic function (whose use
// shows the type it was declared with, `t` and all) and an `extern`, whose names are part
// of its type.
func TestHover_FunctionShowsParameterNames(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let area = (w: i64, h: i64) -> i64 => w * h
let twice = (self: i64) -> i64 => self * 2
let first<t> = (xs: []t, fallback: t) -> t => fallback
let a = area(2, 3)
let b = 4.twice()
let c = first([1, 2], 0)
unsafe extern clamp: (value: i64, low: i64, high: i64) -> i64`
	openAndWait(t, h, src)
	for _, tc := range []struct {
		what      string
		line, col int
		want      string
	}{
		{"the declaration's name", 1, 5, "area: (w: i64, h: i64) -> i64"},
		{"a call", 4, 9, "area: (w: i64, h: i64) -> i64"},
		{"a method-style call", 5, 11, "twice: (self: i64) -> i64"},
		{"a generic call", 6, 9, "first: (xs: []t, fallback: t) -> t"},
		{"an extern's name", 7, 15, "clamp: (value: i64, low: i64, high: i64) -> i64"},
	} {
		hover, err := h.Hover(testURI, tc.line, tc.col)
		if err != nil {
			t.Fatalf("%s: Hover: %v", tc.what, err)
		}
		if hover == nil {
			t.Errorf("%s: no hover", tc.what)
			continue
		}
		if !strings.Contains(hover.Contents.Value, tc.want) {
			t.Errorf("%s: hover = %q; want it to contain %q", tc.what, hover.Contents.Value, tc.want)
		}
	}
}

// TestHover_FunctionValueWithoutADeclarationShowsTypesOnly pins the fallback: a parameter
// of function type has no declaration to take names from, so it shows the type as before.
func TestHover_FunctionValueWithoutADeclarationShowsTypesOnly(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)`
	openAndWait(t, h, src)
	hover, err := h.Hover(testURI, 1, 48) // `f` in `f(x)`
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("no hover")
	}
	if !strings.Contains(hover.Contents.Value, "f: (i64) -> i64") {
		t.Errorf("hover = %q; want `f: (i64) -> i64`", hover.Contents.Value)
	}
}

// TestHover_OverloadedFunctionShowsTheChosenDeclarationsNames pins a receiver-overloaded
// name taking its parameter names from the declaration the typechecker chose for the call
// (`TypeTable.Callee`), not from whichever one the scope holds under the name.
func TestHover_OverloadedFunctionShowsTheChosenDeclarationsNames(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Meters { n: i64 }
struct Feet { n: i64 }
let scale = pure (self: Meters, by: i64) -> i64 => self.n * by
let scale = pure (self: Feet, factor: i64) -> i64 => self.n * factor
let m = Meters { n: 1 }
let f = Feet { n: 1 }
let a = m.scale(2)
let b = f.scale(3)`
	openAndWait(t, h, src)
	if diags := h.Diagnostics(testURI); len(diags) > 0 {
		t.Fatalf("the program should compile clean; got %d diagnostics, first: %s",
			len(diags), diags[0].Message)
	}
	for _, tc := range []struct {
		line, col int
		want      string
	}{
		{7, 11, "(self: Meters, by: i64) -> i64"},
		{8, 11, "(self: Feet, factor: i64) -> i64"},
	} {
		hover, err := h.Hover(testURI, tc.line, tc.col)
		if err != nil {
			t.Fatalf("Hover: %v", err)
		}
		if hover == nil {
			t.Errorf("line %d: no hover", tc.line)
			continue
		}
		if !strings.Contains(hover.Contents.Value, tc.want) {
			t.Errorf("line %d: hover = %q; want it to contain %q", tc.line, hover.Contents.Value, tc.want)
		}
	}
}
