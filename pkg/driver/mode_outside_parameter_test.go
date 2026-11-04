package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// `ref`/`mut`/`own` on anything that isn't a parameter (lyra-E093). Driver-level, as a return
// type is the collector's refusal and the rest are syntax errors the driver names.
func TestModeOutsideParameter_Refused(t *testing.T) {
	const main = "\nlet main = () -> void => {}\n"
	for _, c := range []struct{ name, src, want string }{
		{"a ref return", "let f = (x: string) -> ref string => x" + main, "a return type takes no `ref`"},
		{"a mut return", "let f = (x: mut []i64) -> mut []i64 => x" + main, "a return type takes no `mut`"},
		{"an own return", "let f = () -> own string => \"a\"" + main, "a return type takes no `own`"},
		{"a function type's return", "let f = (g: () -> ref string) -> void => {}" + main, "a return type takes no `ref`"},
		{"a trait method's return", "trait Getter { get: (Self) -> ref string }" + main, "a return type takes no `ref`"},
		{"an extern's return", "unsafe extern abs: (n: i32) -> own i32" + main, "a return type takes no `own`"},

		{"a local", "let main = () -> void => { let a = 1; let b: mut i64 = a }\n", "`mut` goes on a function's parameter"},
		{"a var", "let main = () -> void => { var b: own string = \"a\" }\n", "`own` goes on a function's parameter"},
		{"a global", "let g: ref string = \"x\"" + main, "`ref` goes on a function's parameter"},
		{"a mode as a binding's name", "let main = () -> void => { let ref x = 1 }\n", "`ref` goes on a function's parameter"},
		{"a field", "struct Holder { x: ref string }" + main, "`ref` goes on a function's parameter"},
		{"an anonymous struct's field", "let f = (x: { a: mut i64 }) -> i64 => 1" + main, "`mut` goes on a function's parameter"},
		{"a payload", "data Holder = Held(ref string) | Empty" + main, "`ref` goes on a function's parameter"},
		{"a later payload", "data Holder = Held(i64, own string) | Empty" + main, "`own` goes on a function's parameter"},
		{"a generic argument", "let f = (x: Maybe<ref string>) -> i64 => 1" + main, "`ref` goes on a function's parameter"},
		{"an array's element", "let f = (x: []mut string) -> i64 => 1" + main, "`mut` goes on a function's parameter"},
		{"a pointer's pointee", "let f = (x: ^ref i64) -> i64 => 1" + main, "`ref` goes on a function's parameter"},
		{"an alias", "alias Held = ref string" + main, "`ref` goes on a function's parameter"},
		{"a nominal type", "type Held = own string" + main, "`own` goes on a function's parameter"},
		{"a tuple in a field", "struct Holder { p: (i64, mut string) }" + main, "`mut` goes on a function's parameter"},
		{"a tuple in a local", "let main = () -> void => { let p: (ref string, i64) = (\"a\", 1) }\n", "`ref` goes on a function's parameter"},
		{"a tuple in a parameter", "let f = (x: (mut i64, f32)) -> i64 => 1" + main, "`mut` goes on a function's parameter"},
		{"a tuple in a return", "let f = () -> (ref string, i64) => (\"a\", 1)" + main, "`ref` goes on a function's parameter"},
	} {
		t.Run(c.name, func(t *testing.T) {
			expectCoded(t, c.src, diag.CodeModeOutsideParameter, c.want)
		})
	}
}

// A mode on a function value's parameter (lyra-E094): a call through one passes every argument
// by value. `(own string) -> i64` leaked, and no value could be a `(mut i64) -> void`.
func TestModeOnFunctionValue_Refused(t *testing.T) {
	const main = "\nlet main = () -> void => {}\n"
	for _, c := range []struct{ name, src, want string }{
		{"a callback parameter", "let f = (g: (mut i64) -> void) -> void => {}" + main, "takes no `mut`"},
		{"a field", "struct Holder { g: (own string) -> i64 }" + main, "takes no `own`"},
		{"a binding", "let g: (ref string) -> i64 = (s) => 1" + main, "takes no `ref`"},
		{"an alias", "alias Callback = (mut i64) -> void" + main, "takes no `mut`"},
		{"a generic argument", "let f = (g: Maybe<(own string) -> i64>) -> void => {}" + main, "takes no `own`"},
		{"a return type's", "let f = () -> (ref string) -> i64 => (s) => 1" + main, "takes no `ref`"},
		{"a callback in a trait method", "trait Runner { run: (mut Self, (mut i64) -> void) -> void }" + main, "takes no `mut`"},
		{"a callback in an extern", "unsafe extern run: (f: (mut i64) -> void) -> void" + main, "takes no `mut`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			expectCoded(t, c.src, diag.CodeModeOnFunctionValue, c.want)
		})
	}
}

// What still takes a mode, and what must not be told about one: a function missing its `=>`
// has a `mut` in its error's text and nothing wrong with it, and `ref` is a name to anything
// but a type.
func TestModeOutsideParameter_NotReported(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"a function's parameters", "let f = (a: mut i64, b: ref string, c: own string) -> void => { a += 1 }\nlet main = () -> void => {}\n"},
		{"a trait method's signature", "trait Sink { put: (mut Self, own string, ref string) -> void }\nlet main = () -> void => {}\n"},
		{"an extern's signature", "unsafe extern fill: (n: mut i32) -> void\nlet main = () -> void => {}\n"},
		{"a function missing its arrow", "let f = (x: mut i64) { x }\nlet main = () -> void => {}\n"},
		{"a stray operator after a parameter", "let f = (x: mut i64) - > void => 1\nlet main = () -> void => {}\n"},
		{"a variable named ref", "let main = () -> void => { let ref = 1; print(ref) }\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, d := range driver.Analyze([]byte(c.src)).Diagnostics {
				if d.Code == diag.CodeModeOutsideParameter || d.Code == diag.CodeModeOnFunctionValue {
					t.Errorf("unexpected %s: %s", d.Code, d.Message)
				}
			}
		})
	}
}

func expectCoded(t *testing.T, src, code, want string) {
	t.Helper()
	res := driver.Analyze([]byte(src))
	for _, d := range res.Errors() {
		if d.Code == code && strings.Contains(d.Message, want) {
			return
		}
	}
	t.Errorf("expected %s containing %q; got %v", code, want, res.Diagnostics)
}
