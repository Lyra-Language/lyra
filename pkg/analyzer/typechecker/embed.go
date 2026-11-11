package typechecker

import (
	"os"
	"path/filepath"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `embed("path")` is a file's bytes as a `const`'s value, read when the program is
// compiled —
//
//	const THEME_PCM: [1048576]u8 = embed("art/theme.pcm")
//
// — for data too large to write as a `#[…]` table: a megabyte of bytes as hex is six of
// source, and took lyrac 12 s and 1.2 GB to compile (11/11, Vega's music on the Genesis).
// The bytes go straight to a constant global (the backend's const_data.go), never through
// the parser.
//
// **Only a whole `const`'s initializer**: what it makes is static data, so a `let`, an
// argument or a part of a bigger literal is refused. **The path is relative to the file
// the call is in**, as an import is to its root — not to the directory lyrac runs in, which
// would make a build's answer depend on where it was started. Its type is `[N]u8`, N the
// file's length, so an annotation of another length is the ordinary mismatch. A missing or
// unreadable file is an error here, not a trap at run time.

// embedCall is expr as an `embed(...)` call nothing in scope shadows.
func (tc *TypeChecker) embedCall(expr ast.Expression) (*ast.FunctionCallExpr, bool) {
	call, ok := expr.(*ast.FunctionCallExpr)
	if !ok {
		return nil, false
	}
	ident, ok := call.Function.(*ast.IdentifierExpr)
	if !ok || ident.Name != "embed" {
		return nil, false
	}
	if _, shadowed := tc.lookupAt(ident.Name, ident.GetLocation()); shadowed {
		return nil, false
	}
	return call, true
}

// EmbedPath is where `embed(literal)`, in a file at file, reads from: literal relative to
// file's directory unless it is absolute. The backend reads the bytes from the same place.
func EmbedPath(file, literal string) string {
	if filepath.IsAbs(literal) {
		return literal
	}
	return filepath.Join(filepath.Dir(file), literal)
}

// inferEmbedCall types `embed("path")`: one string literal, as a whole `const`'s
// initializer, naming a readable file — `[N]u8`, N its length.
func (tc *TypeChecker) inferEmbedCall(call *ast.FunctionCallExpr) types.Type {
	u8 := types.PrimitiveType{Name: types.UInt8}
	if !tc.embedConsts[call] {
		tc.addError(call.GetLocation(), SeverityError,
			"embed: only a whole `const`'s value can be a file's bytes (`const DATA: [N]u8 = embed(\"file\")`)")
		return nil
	}
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"embed: expected 1 argument (the file's path, a string literal), got %d", len(call.Arguments))
		return nil
	}
	lit, ok := call.Arguments[0].(*ast.StringLiteralExpr)
	if !ok {
		tc.addError(call.Arguments[0].GetLocation(), SeverityError,
			"embed: the path must be a string literal, known when the program is compiled")
		return nil
	}
	tc.typeTable.Set(lit, types.PrimitiveType{Name: types.String})
	path := EmbedPath(call.GetLocation().File, lit.Value)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		tc.addError(lit.GetLocation(), SeverityError, "embed: cannot read %s", path)
		return nil
	}
	return types.StaticArrayType{ElementType: u8, Size: int(info.Size())}
}
