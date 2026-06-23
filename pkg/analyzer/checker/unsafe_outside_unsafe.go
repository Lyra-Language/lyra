package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// UnsafeOutsideUnsafeError reports a raw-pointer operation (`&x`, `*p`, a write
// through a pointer) or a call to an `unsafe` function that appears outside an
// `unsafe` block or `unsafe` function body.
type UnsafeOutsideUnsafeError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e UnsafeOutsideUnsafeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckUnsafeOutsideUnsafe walks the program AST and reports unsafe operations
// that are not enclosed by an `unsafe { ... }` block or an `unsafe` function
// body: taking a raw pointer (`&x`), dereferencing one (`p^` / a pointer write
// `p^ = v`), and calling an `unsafe` function. The unsafe surface must be explicit so it
// stays greppable and loud, mirroring CheckAwaitOutsideAsync's context walk.
//
// The unsafe context does NOT leak across a function boundary: a non-unsafe
// lambda declared inside an `unsafe` block is its own safe context (just as
// `inAsync` resets per-lambda in the await checker).
func CheckUnsafeOutsideUnsafe(program *ast.Program) []UnsafeOutsideUnsafeError {
	c := &unsafeChecker{unsafeFns: unsafeFunctions(program)}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// Top level is a safe context: inUnsafe == false.
			ast.WalkStmt(stmt, c.stmtVisitor(false), c.exprVisitor(false))
		}
	}
	return c.errors
}

type unsafeChecker struct {
	errors []UnsafeOutsideUnsafeError
	// unsafeFns names the top-level functions declared `unsafe`, so a call to one
	// from a safe context can be reported even though it isn't a raw-pointer op.
	unsafeFns map[string]bool
}

func (c *unsafeChecker) report(loc ast.Location, format string, args ...any) {
	c.errors = append(c.errors, UnsafeOutsideUnsafeError{
		Code:     diag.CodeUnsafeOutsideUnsafe,
		Message:  fmt.Sprintf(format, args...),
		Location: loc,
	})
}

func (c *unsafeChecker) stmtVisitor(inUnsafe bool) func(ast.Statement) bool {
	return func(stmt ast.Statement) bool {
		// A pointer write (`p^ = v`) is unsafe. The walker descends into the
		// target's *operand*, not the DerefExpr node itself, so the deref is not
		// otherwise visited as an expression — flag it here.
		if s, ok := stmt.(*ast.DerefAssignmentStmt); ok && !inUnsafe {
			c.report(s.GetLocation(),
				"writing through a raw pointer requires an `unsafe` block or function")
		}
		return true
	}
}

func (c *unsafeChecker) exprVisitor(inUnsafe bool) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.AddressOfExpr:
			if !inUnsafe {
				c.report(e.GetLocation(),
					"taking a raw pointer with `&` requires an `unsafe` block or function")
			}

		case *ast.DerefExpr:
			if !inUnsafe {
				c.report(e.GetLocation(),
					"dereferencing a raw pointer with `^` requires an `unsafe` block or function")
			}

		case *ast.FunctionCallExpr:
			if !inUnsafe {
				if name := calleeName(e.Function); name != "" && c.unsafeFns[name] {
					c.report(e.GetLocation(),
						"calling unsafe function %q requires an `unsafe` block or function", name)
				}
			}

		case *ast.UnsafeBlockExpr:
			// The block body is an unsafe context. Recurse manually so the
			// elevated context is threaded through, then stop the walker.
			for _, stmt := range e.Body.Statements {
				ast.WalkStmt(stmt, c.stmtVisitor(true), c.exprVisitor(true))
			}
			return false

		case *ast.LambdaExpr:
			// Default values run at the call site, in the *enclosing* context.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(inUnsafe), c.exprVisitor(inUnsafe))
			}
			// A function boundary resets the unsafe context: only an `unsafe`
			// function body is unsafe; unsafe-ness does not leak in from an
			// enclosing block.
			childUnsafe := e.IsUnsafe
			walkLambdaBodies(e, c.stmtVisitor(childUnsafe), c.exprVisitor(childUnsafe))
			return false
		}
		return true
	}
}

// unsafeFunctions maps the names of top-level `let`/`var name = <lambda>`
// bindings whose lambda is declared `unsafe`. Unlike impurity, unsafe-ness is
// not transitive: a safe function that calls an unsafe one is fine *if* the call
// sits in an `unsafe` block, which is exactly what this checker enforces.
func unsafeFunctions(program *ast.Program) map[string]bool {
	unsafe := map[string]bool{}
	for name, lam := range topLevelFunctions(program) {
		if lam.IsUnsafe {
			unsafe[name] = true
		}
	}
	return unsafe
}
