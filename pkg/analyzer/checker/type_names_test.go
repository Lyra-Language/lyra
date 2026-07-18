package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkTypeNames(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckTypeNames(program)
}

// A SCREAMING_CASE struct name collides with the constant-identifier token, so
// `NAME { … }` construction won't parse — warn at the declaration.
func TestTypeNames_ScreamingCaseStruct_Warns(t *testing.T) {
	for _, name := range []string{"A", "AB", "NODE"} {
		d := checkTypeNames(t, "struct "+name+" { v: u8, }")
		if len(d) != 1 {
			t.Fatalf("%s: want 1 warning, got %d: %v", name, len(d), d)
		}
		if d[0].Severity != diag.SeverityWarning || d[0].Code != diag.CodeScreamingCaseTypeName {
			t.Errorf("%s: want a W009 warning, got %v/%s", name, d[0].Severity, d[0].Code)
		}
		if !strings.Contains(d[0].Message, name) {
			t.Errorf("%s: message should name the type: %q", name, d[0].Message)
		}
	}
}

// A PascalCase struct (a lowercase letter) is fine.
func TestTypeNames_PascalCaseStruct_Ok(t *testing.T) {
	for _, name := range []string{"Node", "Ab", "Pair", "RgbA"} {
		if d := checkTypeNames(t, "struct "+name+" { v: u8, }"); len(d) != 0 {
			t.Errorf("%s: want no warning, got %v", name, d)
		}
	}
}

// Only structs are flagged: a `data` type constructs via its constructors and a
// named tuple via `Name(…)`, so a SCREAMING_CASE name works there.
func TestTypeNames_DataAndTuple_NotFlagged(t *testing.T) {
	if d := checkTypeNames(t, "data W = Empty | Full(i64)"); len(d) != 0 {
		t.Errorf("data W: want no warning, got %v", d)
	}
	if d := checkTypeNames(t, "tuple RGBA(u8, u8, u8, u8)"); len(d) != 0 {
		t.Errorf("tuple RGBA: want no warning, got %v", d)
	}
}
