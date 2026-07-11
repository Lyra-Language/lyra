package driver

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// EntryPointName is the reserved name of a program's entry function.
const EntryPointName = "main"

// EntryReturn is how a program's entry function reports completion.
type EntryReturn int

const (
	// EntryReturnVoid: `() -> void` (or no return annotation). The backend
	// synthesizes a 0 exit code.
	EntryReturnVoid EntryReturn = iota
	// EntryReturnExitCode: `() -> u8`. The returned value is the process exit
	// code.
	//
	// u8, not a wider int: the OS truncates a process exit code to its low 8
	// bits regardless of what width the language lets you write (verified —
	// even a real C `int main() { return 300; }` exits 44, not 300), so a
	// wider return type doesn't add real range, only the silent-truncation
	// surprise Lyra rejects everywhere else (checked arithmetic, loud
	// narrowing conversions, …). u8 makes the 0-255 constraint visible in the
	// type itself — the same reasoning behind Zig's `pub fn main() u8` and
	// Rust's move to the narrow std::process::ExitCode over a raw wide int.
	EntryReturnExitCode
)

func (r EntryReturn) String() string {
	switch r {
	case EntryReturnExitCode:
		return "u8 exit code"
	default:
		return "void"
	}
}

// EntryPoint is a program's resolved entry function — where a backend begins
// lowering an executable.
type EntryPoint struct {
	Name    string           // always EntryPointName today
	Decl    *ast.VarDeclStmt // the `let main = …` binding
	Lambda  *ast.LambdaExpr  // its function value
	Returns EntryReturn
}

// ResolveEntryPoint finds and validates a program's entry function. To be built
// into an executable, a program must declare a top-level
//
//	let main = () -> u8 => …   // the u8 is the process exit code
//
// or `() -> void` (a missing return annotation is treated as void). It returns
// the resolved entry point, or nil plus diagnostics when main is absent, is not
// a function, takes parameters, or returns something other than u8/void.
//
// This is a *build-time* requirement — a library, or a plain `lyrac check`,
// needs no main — so it is deliberately not part of Analyze (which the LSP and
// every non-executable path run). A caller passes the analyzed Result.
func ResolveEntryPoint(res *Result) (*EntryPoint, []diag.Diagnostic) {
	if res == nil || res.Program == nil {
		return nil, []diag.Diagnostic{{
			Severity: diag.SeverityError,
			Message:  "no program to build",
		}}
	}

	decl, lambda := findTopLevelFunction(res.Program, EntryPointName)
	if decl == nil {
		return nil, []diag.Diagnostic{{
			Severity: diag.SeverityError,
			Message:  fmt.Sprintf("no entry point: define a top-level `let %s = () -> u8` (or `-> void`)", EntryPointName),
		}}
	}
	if lambda == nil {
		return nil, []diag.Diagnostic{{
			Severity: diag.SeverityError,
			Location: decl.GetLocation(),
			Message:  fmt.Sprintf("entry point %q must be a function", EntryPointName),
		}}
	}

	var diags []diag.Diagnostic
	if len(lambda.Parameters) != 0 {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.SeverityError,
			Location: decl.GetLocation(),
			Message:  fmt.Sprintf("entry point %q must take no parameters", EntryPointName),
		})
	}
	ret, ok := entryReturnOf(lambda.ReturnType)
	if !ok {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.SeverityError,
			Location: decl.GetLocation(),
			Message:  fmt.Sprintf("entry point %q must return u8 or void, got %s", EntryPointName, lambda.ReturnType.Type),
		})
	}
	if len(diags) > 0 {
		return nil, diags
	}

	return &EntryPoint{
		Name:    EntryPointName,
		Decl:    decl,
		Lambda:  lambda,
		Returns: ret,
	}, nil
}

// findTopLevelFunction returns the top-level `let name = …` binding and, when its
// value is a function literal, that lambda. decl is nil when no such binding
// exists; lambda is nil when the binding's value is not a function.
func findTopLevelFunction(program *ast.Program, name string) (*ast.VarDeclStmt, *ast.LambdaExpr) {
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.VarDeclStmt)
		if !ok || decl.Name != name {
			continue
		}
		lambda, _ := decl.Value.(*ast.LambdaExpr)
		return decl, lambda
	}
	return nil, nil
}

// entryReturnOf classifies an entry function's declared return type. A nil or
// void type is EntryReturnVoid; u8 is EntryReturnExitCode; anything else is
// rejected (ok=false).
func entryReturnOf(rt types.ReturnType) (EntryReturn, bool) {
	t := rt.Type
	if t == nil {
		return EntryReturnVoid, true
	}
	if _, ok := t.(types.VoidType); ok {
		return EntryReturnVoid, true
	}
	if p, ok := t.(types.PrimitiveType); ok && p.Name == types.UInt8 {
		return EntryReturnExitCode, true
	}
	// Defensive: an unresolved type name (primitives normally parse to
	// PrimitiveType/VoidType, but keep this robust to that not holding).
	switch t.GetName() {
	case "void":
		return EntryReturnVoid, true
	case "u8":
		return EntryReturnExitCode, true
	}
	return EntryReturnVoid, false
}
