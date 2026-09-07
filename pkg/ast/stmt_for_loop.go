package ast

// Both loop forms hold their body as a **pointer**, not a value, so the
// *BlockExpr identity the collector recorded in the ScopeTable survives into the
// AST — the same reason IfDestructuringStmt.Then/Else are pointers.
//
// A value field copies the block, and the copy has a different address than the
// one the scope was keyed on. Every consumer that recovers a block's scope by
// pointer (the typechecker's enterScope above all) then missed, and enterScope's
// miss path is silent: it runs the body in the *enclosing* scope. The visible
// effect was that a `let` declared inside any loop body was invisible there
// ("undefined identifier"), because the collector had defined it in the body's
// own block scope, which nothing could reach.
type ForLoopExpr struct {
	ExprBase
	Label     string
	Init      *VarDeclStmt
	Condition *Expression
	Post      *Expression
	Body      *BlockExpr
}

func (t *ForLoopExpr) GetName() string { return "for_loop" }

type ForInLoopExpr struct {
	ExprBase
	Label string
	Key   string
	Value string
	// KeyLocation/ValueLocation span just the binding names, for a diagnostic that is
	// about one of them rather than about the loop (lyra-W020). Tagged out of the printer
	// like every other auxiliary position, so goldens stay stable.
	KeyLocation   Location `print:"-"`
	ValueLocation Location `print:"-"`
	Iterable      Expression
	Body          *BlockExpr
}

func (t *ForInLoopExpr) GetName() string { return "for_in_loop" }

// LoopCanExit reports whether a `for` loop can finish — fall through to whatever follows
// it. It can when it has a condition, or when its body contains a `break` that targets it:
// an unlabeled `break` not inside a nested loop, or a labeled one naming this loop. A
// `for { … }` with neither runs until a `return` or a `panic`, so it is a `never`: the
// typechecker lets it stand as the value of a non-void body, and the backend seals its
// exit block as unreachable rather than leaving a block with no terminator.
//
// A lambda is not entered: a `break` cannot cross one.
func LoopCanExit(e *ForLoopExpr) bool {
	if e.Condition != nil {
		return true
	}
	return breaksOutOf(e.Body, e.Label, 0)
}

// breaksOutOf walks body for a `break` that leaves the loop labeled `label`, `depth`
// nested loops up from where the walk began.
func breaksOutOf(body *BlockExpr, label string, depth int) bool {
	if body == nil {
		return false
	}
	found := false
	for _, s := range body.Statements {
		WalkStmt(s, func(st Statement) bool {
			if b, ok := st.(*BreakStmt); ok {
				if (b.Label == "" && depth == 0) || (b.Label != "" && b.Label == label) {
					found = true
				}
			}
			return !found
		}, func(ex Expression) bool {
			if found {
				return false
			}
			switch inner := ex.(type) {
			case *LambdaExpr:
				return false
			case *ForLoopExpr:
				if breaksOutOf(inner.Body, label, depth+1) {
					found = true
				}
				return false
			case *ForInLoopExpr:
				if breaksOutOf(inner.Body, label, depth+1) {
					found = true
				}
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
