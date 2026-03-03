package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollectBasicArrayCompExpr(t *testing.T) {
	source := `let squares = [ 1..=5 -> x, | x * x ]`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	got := captureProgramPrint(program)
	want := `Program(1 statements) {
  VarDeclStmt(squares)
    Keyword: let
    Value: {
      ArrayCompExpr {
        Generators: {
          Generator {
            Value: {
              RangeExpr {
                Start: {
                  IntegerLiteralExpr(1)
                }
                EndOperator: =
                End: {
                  IntegerLiteralExpr(5)
                }
              }
            }
            Identifier: x
          }
        }
        Guards: {
        }
        Result: {
          MathBinaryOpExpr(math_binary_op_expr) {
            Left: {
              IdentifierExpr(x, IsConst: false)
            }
            Operator: *
            Right: {
              IdentifierExpr(x, IsConst: false)
            }
          }
        }
      }
    }
  }
}
`
	if got != want {
		t.Errorf("Print output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestCollectArrayCompExprWithGuard(t *testing.T) {
	source := `let evens = [ 1..=5 -> x, | x % 2 == 0 | x * x ]`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	got := captureProgramPrint(program)
	want := `Program(1 statements) {
  VarDeclStmt(evens)
    Keyword: let
    Value: {
      ArrayCompExpr {
        Generators: {
          Generator {
            Value: {
              RangeExpr {
                Start: {
                  IntegerLiteralExpr(1)
                }
                EndOperator: =
                End: {
                  IntegerLiteralExpr(5)
                }
              }
            }
            Identifier: x
          }
        }
        Guards: {
          Guard: {
            BooleanBinaryOpExpr(boolean_binary_op_expr)
              Left: {
                IdentifierExpr(x, IsConst: false)
              }
              Operator: ==
              Right: {
                IntegerLiteralExpr(0)
              }
          }
        }
        Result: {
          MathBinaryOpExpr(math_binary_op_expr) {
            Left: {
              IdentifierExpr(x, IsConst: false)
            }
            Operator: *
            Right: {
              IdentifierExpr(x, IsConst: false)
            }
          }
        }
      }
    }
  }
}
`
	if got != want {
		t.Errorf("Print output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestCollectArrayCompExprWithMultipleGuardsAndGenerators(t *testing.T) {
	source := `let foo = [ 1..=5 -> x, 1..=5 -> y | odd(x), even(y) | (x, y, x * y) ]`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	got := captureProgramPrint(program)
	goldenPath := filepath.Join("testdata", "array_comp_multiple_guards.golden")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.MkdirAll(filepath.Dir(goldenPath), 0755)
			if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
				t.Fatalf("Write golden file: %v", err)
			}
			t.Fatalf("Wrote initial golden file at %s; re-run test to verify", goldenPath)
		}
		t.Fatalf("Read golden file: %v", err)
	}
	if len(want) == 0 {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("Write golden file: %v", err)
		}
		t.Fatal("Golden file was empty; wrote current output. Re-run test to verify.")
	}
	if got != string(want) {
		t.Errorf("Print output mismatch:\ngot:\n%s\nwant (from %s):\n%s", got, goldenPath, string(want))
	}
}

func TestCollectArrayCompExprWithArrayLiteralGenerator(t *testing.T) {
	source := `let foo = [ [1, 2, 3] -> x | x * x ]`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	got := captureProgramPrint(program)
	want := `Program(1 statements) {
  VarDeclStmt(foo)
    Keyword: let
    Value: {
      ArrayCompExpr {
        Generators: {
          Generator {
            Value: {
              ArrayLiteralExpr(array_literal_expr) {
                  IntegerLiteralExpr(1)
                  IntegerLiteralExpr(2)
                  IntegerLiteralExpr(3)
              }
            }
            Identifier: x
          }
        }
        Guards: {
        }
        Result: {
          MathBinaryOpExpr(math_binary_op_expr) {
            Left: {
              IdentifierExpr(x, IsConst: false)
            }
            Operator: *
            Right: {
              IdentifierExpr(x, IsConst: false)
            }
          }
        }
      }
    }
  }
}
`
	if got != want {
		t.Errorf("Print output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
