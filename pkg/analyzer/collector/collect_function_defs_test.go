package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollector_SimpleFunctionDefinition(t *testing.T) {
	source := `pub def sum<int>: (int, int) -> int = (a, b) => a + b`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	got := captureProgramPrint(program)
	want := `
	Program(1 statements) {
		FunctionDefStmt(sum) {
			GenericParams: [int]
			Signature: (int, int) -> int
			IsPublic: true
			Clauses: {
				FunctionClause {
					Parameters: {
						Parameter(a)
						Parameter(b)
					}
					Guard: nil
					Body: {
						MathBinaryOpExpr(math_binary_op_expr) {
							Left: {
								IdentifierExpr(a, IsConst: false)
							}
							Operator: +
							Right: {
								IdentifierExpr(b, IsConst: false)
							}
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_FunctionDefinitionWithGenericParams(t *testing.T) {
	source := `pub def sum<t>: (t, t) -> t = (a, b) => a + b`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// Check symbol table lookup
	_, ok := table.Functions["sum"]
	if !ok {
		t.Fatalf("\"sum\" not found in functions")
	}

	got := captureProgramPrint(program)
	want := `
	Program(1 statements) {
		FunctionDefStmt(sum) {
			GenericParams: [t]
			Signature: (t, t) -> t
			IsPublic: true
			Clauses: {
				FunctionClause {
					Parameters: {
						Parameter(a)
						Parameter(b)
					}
					Guard: nil
					Body: {
						MathBinaryOpExpr(math_binary_op_expr) {
							Left: {
								IdentifierExpr(a, IsConst: false)
							}
							Operator: +
							Right: {
								IdentifierExpr(b, IsConst: false)
							}
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_FunctionDefinitionWithMultipleClausesAndGuard(t *testing.T) {
	source := `
		def fib: (int) -> int = {
			(n) if n < 2 => n,
			(n) => fib(n-2) + fib(n-1),
		}
	`

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
	want := `
	Program(1 statements) {
		FunctionDefStmt(fib) {
			Signature: (int) -> int
			Clauses: {
				FunctionClause {
					Parameters: {
						Parameter(n)
					}
					Guard: {
						GuardExpr(boolean_binary_op_expr) {
							Condition: {
								BooleanBinaryOpExpr(boolean_binary_op_expr) {
									Left: {
										IdentifierExpr(n, IsConst: false)
									}
									Operator: <
									Right: {
										IntegerLiteralExpr(2, Base: 10)
									}
								}
							}
						}
					}
					Body: {
						IdentifierExpr(n, IsConst: false)
					}
				}
				FunctionClause {
					Parameters: {
						Parameter(n)
					}
					Guard: nil
					Body: {
						MathBinaryOpExpr(math_binary_op_expr) {
							Left: {
								FunctionCallExpr {
									Function: {
										IdentifierExpr(fib, IsConst: false)
									}
									Arguments: {
										ArgumentList(math_binary_op_expr) {
											MathBinaryOpExpr(math_binary_op_expr) {
												Left: {
													IdentifierExpr(n, IsConst: false)
												}
												Operator: -
												Right: {
													IntegerLiteralExpr(2, Base: 10)
												}
											}
										}
									}
								}
							}
							Operator: +
							Right: {
								FunctionCallExpr {
									Function: {
										IdentifierExpr(fib, IsConst: false)
									}
									Arguments: {
										ArgumentList(math_binary_op_expr) {
											MathBinaryOpExpr(math_binary_op_expr) {
												Left: {
													IdentifierExpr(n, IsConst: false)
												}
												Operator: -
												Right: {
													IntegerLiteralExpr(1, Base: 10)
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_FunctionDefinitionWithModifiedParameters(t *testing.T) {
	source := `def add: (mut Point, ref Vec3) -> Point = (point, vec) => point + vec`

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
	want := `
	Program(1 statements) {
		FunctionDefStmt(add) {
			Signature: (mut Point, ref Vec3) -> Point
			Clauses: {
				FunctionClause {
					Parameters: {
						Parameter(point)
						Parameter(vec)
					}
					Guard: nil
					Body: {
						MathBinaryOpExpr(math_binary_op_expr) {
							Left: {
								IdentifierExpr(point, IsConst: false)
							}
							Operator: +
							Right: {
								IdentifierExpr(vec, IsConst: false)
							}
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_FunctionDefinitionWithDefaultParameters(t *testing.T) {
	source := `def add: (int, int) -> int = (a, b = 0) => a + b`

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
	want := `
	Program(1 statements) {
		FunctionDefStmt(add) {
			Signature: (int, int) -> int
			Clauses: {
				FunctionClause {
					Parameters: {
						Parameter(a)
						Parameter(b, DefaultValue: IntegerLiteralExpr(0, Base: 10))
					}
					Guard: nil
					Body: {
						MathBinaryOpExpr(math_binary_op_expr) {
							Left: {
								IdentifierExpr(a, IsConst: false)
							}
							Operator: +
							Right: {
								IdentifierExpr(b, IsConst: false)
							}
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}
