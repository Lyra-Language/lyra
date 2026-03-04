package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollector_BasicConstrainedTypeWithoutConstraints(t *testing.T) {
	source := `type Angle = float`
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
	want :=
		`Program(1 statements) {
			TypeDeclStmt(Angle) {
				Type: {
					ConstrainedType {
						Name: Angle
						Type: float
					}
				}
			}
		}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithStrings(t *testing.T) {
	source := `type Color = string where values("red", "green", "blue")`
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
	want :=
		`Program(1 statements) {
			TypeDeclStmt(Color) {
				Type: {
					ConstrainedType {
						Name: Color
						Type: string
						Constraints: {
							LiteralUnionConstraint {
								Value: StringLiteralExpr(red)
								Value: StringLiteralExpr(green)
								Value: StringLiteralExpr(blue)
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

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithNumbers(t *testing.T) {
	source := `type Status = i32 where values(200, 404, 500)`
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
	want :=
		`Program(1 statements) {
			TypeDeclStmt(Status) {
				Type: {
					ConstrainedType {
						Name: Status
						Type: i32
						Constraints: {
							LiteralUnionConstraint {
								Value: IntegerLiteralExpr(200, Base: 10)
								Value: IntegerLiteralExpr(404, Base: 10)
								Value: IntegerLiteralExpr(500, Base: 10)
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

func TestCollector_SimpleRangeConstrainedType(t *testing.T) {
	source := `type Angle = float where range(0..<360)`
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
	want :=
		`Program(1 statements) {
			TypeDeclStmt(Angle) {
				Type: {
					ConstrainedType {
						Name: Angle
						Type: float
						Constraints: {
							RangeConstraint {
								Start: {
									MathConstraintLiteralExpr {
										Value: 0
										Type: {
										 int
										}
									}
								}
								Comparator: <
								End: {
									MathConstraintLiteralExpr {
										Value: 360
										Type: {
										  int 
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

func TestCollector_RangeConstrainedTypeWithConstantMultiplication(t *testing.T) {
	source := `type Radian = float where range(0.0..<PI*2.0)`
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
		TypeDeclStmt(Radian) {
			Type: {
				ConstrainedType {
					Name: Radian
					Type: float
					Constraints: {
						RangeConstraint {
							Start: {
								MathConstraintLiteralExpr {
									Value: 0
									Type: {
										float
									}
								}
							}
							Comparator: <
							End: {
								MathConstraintBinaryOpExpr(PI * 2) {
									Left: {
										MathConstraintIdentifierExpr(PI)
									}
									Operator: *
									Right: {
										MathConstraintLiteralExpr {
											Value: 2
											Type: {
												float
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

func TestCollector_RangeConstrainedTypeWithVariableAdditionAndSubtraction(t *testing.T) {
	source := `
	let pi = 3.14159
	type Radian = float where range(pi-3..<pi+3)
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
	Program(2 statements) {
		VarDeclStmt(pi) {
			Keyword: let
			Value: {
				FloatLiteralExpr(3.14159)
			}
		}
		TypeDeclStmt(Radian) {
			Type: {
				ConstrainedType {
					Name: Radian
					Type: float
					Constraints: {
						RangeConstraint {
							Start: {
								MathConstraintBinaryOpExpr(pi - 3) {
									Left: {
										MathConstraintIdentifierExpr(pi)
									}
									Operator: -
									Right: {
										MathConstraintLiteralExpr {
											Value: 3
												Type: {
													int
												}
											}
										}
									}
								}
								Comparator: <
								End: {
									MathConstraintBinaryOpExpr(pi + 3) {
										Left: {
											MathConstraintIdentifierExpr(pi)
										}
										Operator: +
										Right: {
											MathConstraintLiteralExpr {
												Value: 3
												Type: {
													int
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

func TestCollector_MultipleConstraints(t *testing.T) {
	source := `type Radian = float where range(0..<PI*2), precision(0.01)`
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
		TypeDeclStmt(Radian) {
			Type: {
				ConstrainedType {
					Name: Radian
					Type: float
					Constraints: {
						RangeConstraint {
							Start: {
								MathConstraintLiteralExpr {
									Value: 0
									Type: {
										int
									}
								}
							}
							Comparator: <
							End: {
								MathConstraintBinaryOpExpr(PI * 2) {
									Left: {
										MathConstraintIdentifierExpr(PI)
									}
									Operator: *
									Right: {
										MathConstraintLiteralExpr {
											Value: 2
											Type: {
												int
											}
										}
									}
								}
							}
						}
						PrecisionConstraint(precision(0.01))
							Value: {
								MathConstraintLiteralExpr {
									Value: 0.01
									Type: {
										float
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

func TestCollector_StepConstrainedType(t *testing.T) {
	source := `type CompassHeading = int where range(0..<360), step(15)`
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
		TypeDeclStmt(CompassHeading) {
			Type: {
				ConstrainedType {
					Name: CompassHeading
					Type: int
					Constraints: {
						RangeConstraint {
							Start: {
								MathConstraintLiteralExpr {
									Value: 0
									Type: {
										int
									}
								}
							}
							Comparator: <
							End: {
								MathConstraintLiteralExpr {
									Value: 360
									Type: {
										int
									}
								}
							}
						}
						StepConstraint(step(15))
							Value: {
								MathConstraintLiteralExpr {
									Value: 15
									Type: {
										int
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

func TestCollector_PatternConstrainedType(t *testing.T) {
	source := `type HexStr = string where pattern(r/^#(?:[0-9a-fA-F]{3}){1,2}$/)`
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
		TypeDeclStmt(HexStr) {
			Type: {
				ConstrainedType {
					Name: HexStr
					Type: string
					Constraints: {
						PatternConstraint(pattern(r/^#(?:[0-9a-fA-F]{3}){1,2}$/))
							Pattern: r/^#(?:[0-9a-fA-F]{3}){1,2}$/
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}
