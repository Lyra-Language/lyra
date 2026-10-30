package driver_test

import (
	"fmt"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// A syntax error is reported once, as what went wrong, and the passes after parsing say
// nothing about the item it sits in. An unclosed `add(1,` was `unexpected "= add(1, \n
// let y"` followed by "x used before its declaration" and "undefined identifier y"; an
// unclosed `(1 + 2` was `unexpected "2"`, a `missing )` a line later, and "operands must
// be numeric, got integer literal and void" (10/30).
func TestSyntaxErrors_ReportedOnceAsTheMistake(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      []string
	}{
		{"unclosed call",
			"let add = (a: i64, b: i64) -> i64 => a + b\n\nlet main = () => {\n  let x = add(1, \n  let y = x + 1\n  print(y)\n}\n",
			[]string{"4:14 syntax error: unclosed `(`: expected `)` before the `}` at 7:1"}},
		{"unclosed group",
			"let main = () => {\n  let x = (1 + 2\n  print(x)\n}\n",
			[]string{"2:11 syntax error: unclosed `(`: expected `)` before the `}` at 4:1"}},
		{"unclosed at the end of the file",
			"let main = () => {\n  print(1)\n",
			[]string{"1:18 syntax error: unclosed `{`: expected `}` before the end of the file"}},
		{"unmatched closer",
			"let main = () => {\n  print(1))\n}\n",
			[]string{"2:11 syntax error: unmatched `)`: nothing open for it to close"}},
		{"wrong closer on the opener's line",
			"let main = () => {\n  let a = [1, 2)\n}\n",
			[]string{"2:16 syntax error: `)` cannot close the `[` at 2:11; expected `]`"}},
		{"delimiters in strings and comments do not count",
			"let main = () => {\n  let s = \"(( [ {\"\n  // ) ] }\n  print(s\n}\n",
			[]string{"4:8 syntax error: unclosed `(`: expected `)` before the `}` at 5:1"}},
		{"an unterminated interpolation is the one reported",
			"let main = () => {\n  let n = 1\n  println(\"n = ${n + 1\")\n}\n",
			[]string{"3:16 syntax error: unclosed `${`: expected `}` before the end of the file"}},
		{"errors in other items stay",
			"let f = () -> i64 => \"no\"\n\nlet main = () => {\n  print((1)\n}\n\nlet g = () -> i64 => true\n",
			[]string{
				"4:8 syntax error: unclosed `(`: expected `)` before the `}` at 5:1",
				"1:22 f: return type mismatch: expected i64, got string",
				"7:22 g: return type mismatch: expected i64, got bool",
			}},
		{"balanced input keeps tree-sitter's report",
			"let main = () => {\n  let x = 1 + + 2\n  let y: i64 = \"s\"\n}\nlet g = () -> i64 => true\n",
			[]string{
				"2:15 syntax error: unexpected \"+\"",
				"5:22 g: return type mismatch: expected i64, got bool",
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, d := range driver.Analyze([]byte(c.src)).Diagnostics {
				got = append(got, fmt.Sprintf("%d:%d %s", d.Location.StartLine, d.Location.StartCol, d.Message))
			}
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("diagnostics:\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

// A file that does not parse is mid-edit: its warnings go, since an "unused" one there is
// usually a use that landed in an ERROR node. The same file parsing keeps them.
func TestSyntaxErrors_HoldBackWarnings(t *testing.T) {
	t.Parallel()
	const parses = "let main = () => {\n  let unused = 1\n}\n"
	if !hasWarning(driver.Analyze([]byte(parses))) {
		t.Fatalf("the parsing file should warn about `unused`")
	}
	broken := parses + "let g = () => {\n  print(1\n}\n"
	if hasWarning(driver.Analyze([]byte(broken))) {
		t.Errorf("a file with a syntax error kept a warning")
	}
}

func hasWarning(res *driver.Result) bool {
	for _, d := range res.Diagnostics {
		if d.Severity == diag.SeverityWarning {
			return true
		}
	}
	return false
}
