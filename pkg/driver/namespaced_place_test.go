package driver_test

import (
	"strings"
	"testing"
)

// A write through a namespace asks the binding's own rule: interior writes need a `var` or a
// `let mut`, a whole reassignment a `var` (11/11). Every one of them said
// `undefined identifier "lib"` until then, whatever the binding was.
func TestNamespacedPlace_TheBindingDecides(t *testing.T) {
	lib := `module lib
pub var queue: []i64 = [0]
pub let fixed: []i64 = [1]
pub let mut grow: []i64 = [1]
`
	cases := []struct{ name, body, want string }{
		{"element of a var", "lib.queue[0] = 2", ""},
		{"element of a let", "lib.fixed[0] = 2", "lib.fixed: `let` binding is deeply immutable"},
		{"push onto a let", "lib.fixed.push(2)", "lib.fixed: `let` binding is deeply immutable"},
		{"let mut reassigned", "lib.grow = [2]", "lib.grow: 'let' binding is immutable"},
		{"let mut element", "lib.grow[0] = 2", ""},
		{"a let to mut", "bump(lib.fixed)", `cannot pass immutable binding "lib.fixed" to a ` + "`mut`"},
		{"a var to mut", "bump(lib.queue)", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeModules(t, map[string]string{
				"lib.lyra": lib,
				"main.lyra": "module main\nimport lib\n" +
					"let bump = (xs: mut []i64) -> void => xs.push(1)\n" +
					"let main = () -> void => {\n  " + c.body + "\n}\n",
			})
			var msgs []string
			for _, e := range res.Errors() {
				msgs = append(msgs, e.Message)
			}
			joined := strings.Join(msgs, "\n")
			if strings.Contains(joined, "undefined identifier") {
				t.Fatalf("the namespace was read as a value:\n%s", joined)
			}
			if c.want == "" {
				if len(msgs) != 0 {
					t.Fatalf("want a clean analysis, got:\n%s", joined)
				}
				return
			}
			if len(msgs) != 1 || !strings.Contains(msgs[0], c.want) {
				t.Fatalf("want one error containing %q, got:\n%s", c.want, joined)
			}
		})
	}
}

// `==` on a type holding a function value is refused where it is written, naming the path to
// the function (11/11). Only the backend refused it before, with no position.
func TestFunctionEquality_IsRefusedWhereWritten(t *testing.T) {
	cases := []struct{ name, expr, want string }{
		{"a field", "w == w", "Work holds a function value (run: () -> void)"},
		{"through an array", "q != q", "Queue holds a function value (jobs[…].run: () -> void)"},
		{"a function", "f == f", "a function value has no equality ((i64) -> i64)"},
		{"a Maybe's payload", "m == None", "(Some.0: () -> void)"},
		{"a constructor's payload", "j == Idle", "Job holds a function value (Runs.1: (i64) -> i64)"},
		{"none held", "p == p", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeModules(t, map[string]string{
				"main.lyra": `module main
struct Work { name: string, run: () -> void }
struct Queue { jobs: []Work }
data Job = Idle | Runs(i64, (i64) -> i64)
struct Plain { name: string, n: i64 }
let main = () -> void => {
  let w = Work { name: "a", run: () => println("a") }
  let q = Queue { jobs: [w] }
  let f = (x: i64) -> i64 => x
  let m: Maybe<() -> void> = None
  let j = Runs(1, f)
  let p = Plain { name: "p", n: 1 }
  if ` + c.expr + ` { println("") }
}
`,
			})
			var msgs []string
			for _, e := range res.Errors() {
				msgs = append(msgs, e.Message)
			}
			joined := strings.Join(msgs, "\n")
			if c.want == "" {
				if len(msgs) != 0 {
					t.Fatalf("want a clean analysis, got:\n%s", joined)
				}
				return
			}
			if len(msgs) != 1 || !strings.Contains(msgs[0], c.want) {
				t.Fatalf("want one error containing %q, got:\n%s", c.want, joined)
			}
		})
	}
}
