package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// roundingIntrinsicOps maps a Lyra builtin rounding method name
// (typechecker/builtins.go's floatRoundingOps) to the LLVM intrinsic that
// implements it. `round` uses llvm.round (half away from zero, C/Rust-style),
// not llvm.rint/nearbyint (round-to-even, mode-dependent).
var roundingIntrinsicOps = map[string]string{
	"floor": "floor",
	"ceil":  "ceil",
	"round": "round",
}

// lowerBuiltinMethodCall lowers a call whose callee is a MemberExpr resolved
// by the typechecker to a builtin method (builtins.go) rather than a struct
// field, trait method, or user function: the float rounding builtins
// (`x.floor()`/`.ceil()`/`.round()`) and the integer wrapping/saturating
// overflow-arithmetic builtins (`x.wrapping_add(y)` etc., wrapping.go).
func (l *lowerer) lowerBuiltinMethodCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	// `math.double(21)` is not a method call at all — the "receiver" is an imported
	// module namespace. Checked first, because everything below assumes the object is
	// a value and would try to lower a namespace as one.
	if fn, ok := l.namespaceCallee(member); ok {
		return l.lowerNamespaceCall(block, call, member, fn)
	}
	if m, ok := intOverflowMethods[member.Property.Name]; ok {
		return l.lowerIntOverflowMethod(block, call, member, m)
	}
	if member.Property.Name == "len" {
		return l.lowerArrayLen(block, call, member)
	}
	if member.Property.Name == "weak" {
		return l.lowerWeakDowngrade(block, call, member)
	}
	op, ok := roundingIntrinsicOps[member.Property.Name]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: unsupported method call %q", member.Property.Name)
	}
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: %s() expects 0 arguments, got %d", member.Property.Name, len(call.Arguments))
	}
	recvT, ok := l.recordedType(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for %s receiver", member.Property.Name)
	}
	recvP, ok := recvT.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-primitive receiver %s not implemented", member.Property.Name, recvT)
	}
	suffix, ok := floatIntrinsicSuffix(recvP.Name)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-float receiver %s not implemented", member.Property.Name, recvT)
	}
	recv, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	fT, ok := recv.Type().(*lltypes.FloatType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() lowered receiver is not a float value (%s)", member.Property.Name, recv.Type())
	}
	rounded := block.NewCall(l.roundingIntrinsicFunc(op, suffix, fT), recv)
	// The builtin's fixed return type is i64 (builtins.go's floatRoundingOps);
	// narrow further with an explicit int conversion, e.g. i32(x.floor()).
	return block.NewFPToSI(rounded, lltypes.I64), block, nil
}

// roundingIntrinsicFunc lazily declares `llvm.<op>.<suffix>` (e.g.
// `llvm.floor.f64`), caching it on the lowerer — the same lazy-declare-and-
// cache shape as memcmpFunc (strings.go), keyed by name in a map since there
// are up to 9 of these (3 ops x 3 float widths) instead of just one.
func (l *lowerer) roundingIntrinsicFunc(op, suffix string, fT *lltypes.FloatType) *ir.Func {
	name := "llvm." + op + "." + suffix
	if fn, ok := l.roundingIntrinsics[name]; ok {
		return fn
	}
	fn := l.module.NewFunc(name, fT, ir.NewParam("", fT))
	l.roundingIntrinsics[name] = fn
	return fn
}

// namespaceCallee resolves `alias.name(…)` to the emitted function, when alias names an
// imported module rather than a value.
//
// The membership check mirrors the typechecker's: names are program-wide unique today
// (a cross-module duplicate is rejected), so a bare lookup would happily resolve
// `math.thing` to some *other* module's `thing`. Rejecting here as well means the
// backend cannot emit a call the front end did not sanction, which is the standing rule
// that it errors rather than guessing.
func (l *lowerer) namespaceCallee(member *ast.MemberExpr) (*ir.Func, bool) {
	id, ok := member.Object.(*ast.IdentifierExpr)
	if !ok {
		return nil, false
	}
	if _, isLocal := l.locals[id.Name]; isLocal {
		return nil, false // a value shadows the namespace
	}
	st := l.res.SymbolTable
	imp, ok := st.NamespaceImport(member.GetLocation().File, id.Name)
	if !ok {
		return nil, false
	}
	name := member.Property.Name
	if st.DeclaringModule(name) != imp.Path {
		return nil, false
	}
	fn, ok := l.funcs[name]
	return fn, ok
}

// lowerNamespaceCall emits the call once the callee is known. Split out so the
// parameter list comes from the callee's own declaration, which is what the argument
// coercion in lowerDirectCall reads.
func (l *lowerer) lowerNamespaceCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, fn *ir.Func) (value.Value, *ir.Block, error) {
	var params []ast.Parameter
	if lam, ok := l.res.SymbolTable.LookupFunction(member.Property.Name); ok {
		params = lam.Parameters
	}
	return l.lowerDirectCall(block, call, fn, params)
}
