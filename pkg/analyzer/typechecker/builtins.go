package typechecker

import "github.com/Lyra-Language/lyra/pkg/types"

// Builtin (compiler-provided) methods available on primitive receivers, e.g.
// `x.wrapping_add(y)` on integers. They are consulted during member-call
// dispatch (inferMemberCall) only after struct-field and trait-method
// resolution miss, so a user type or a trait impl always shadows a builtin of
// the same name.
//
// This registry is the "somewhere to live" that Pit-of-Success #2 (explicit
// wrapping/saturating arithmetic, versus trapping `+`) calls for — a registry,
// NOT a prelude (canonical identity != stdlib; see todo #1). Registering a
// method here makes `x.wrapping_add(y)` type-check with the right result type
// today; a backend lowers each to the matching machine operation (the wrapping
// ops are two's-complement add/sub/mul; the saturating ops are LLVM's
// llvm.{s,u}{add,sub}.sat intrinsics). The set of names here is exactly the set
// of integer overflow-arithmetic primitives a backend must implement.

// intBinaryOps are the integer overflow-arithmetic builtins. Each takes the
// receiver as the first operand and one argument as the second, and returns the
// same integer type: `(self: T, other: T) -> T` for a concrete integer T.
var intBinaryOps = map[string]bool{
	"wrapping_add":   true,
	"wrapping_sub":   true,
	"wrapping_mul":   true,
	"saturating_add": true,
	"saturating_sub": true,
	"saturating_mul": true,
}

// floatRoundingOps are the explicit float→int rounding builtins — the escape
// hatch the numeric-conversion error (`inferTypeConversion`) points to, since
// `i64(x)` on a float is rejected as lossy. Each takes no arguments and
// returns a fixed i64 (mirrors how an unannotated numeric literal defaults to
// i64/f64, todo.md's promoteToDefault pattern) rather than inferring a
// narrower width from context — that's the same open "return type from
// context" problem the still-unregistered truncate/saturate/narrow builtins
// have (todo.md Pit-of-Success #5). Narrow further with the existing explicit
// int conversion: `i32(x.floor())`.
var floatRoundingOps = map[string]bool{
	"floor": true,
	"ceil":  true,
	"round": true,
}

// builtinMethodSignature returns the LambdaType of the builtin method name for a
// receiver of type recv, specialized to recv's concrete type, or ok=false when
// no builtin of that name applies to that receiver. The signature's parameters
// are the *call* arguments only — the receiver (self) is implicit, so
// `x.wrapping_add(y)` matches a one-parameter signature `(T) -> T`.
//
// An untyped-literal receiver is promoted to its default (e.g. `5.wrapping_add`
// treats 5 as i64), mirroring how an unannotated literal binding is typed.
func builtinMethodSignature(recv types.Type, name string) (*types.LambdaType, bool) {
	recv = promoteToDefault(recv)
	p, ok := recv.(types.PrimitiveType)
	if !ok {
		return nil, false
	}
	if intBinaryOps[name] {
		// Explicit-overflow integer arithmetic: same integer type in and out.
		if !isAnyConcreteInt(p.Name) {
			return nil, false
		}
		return &types.LambdaType{
			Parameters: []types.ParameterType{{Type: recv}}, // the second operand
			ReturnType: types.ReturnType{Type: recv},
		}, true
	}
	if floatRoundingOps[name] {
		if !isAnyConcreteFloat(p.Name) {
			return nil, false
		}
		return &types.LambdaType{
			Parameters: nil,
			ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
		}, true
	}
	return nil, false
}

// builtinFunctionSignature returns the LambdaType of a compiler-provided *free*
// function (not a method) named name, or ok=false when none applies. Like the
// builtin methods above, these are consulted only after normal name resolution
// misses (inferIdentifierCall's scope.Lookup), so a user binding of the same
// name always shadows the builtin.
//
// `print`/`println` write a string to stdout and return void (`(string) ->
// void`); the backend lowers each to a libc `write(1, …)` (STRING_LAYOUT.md).
// Their effect classification lives separately in checker/effects.go's
// builtinEffects (EffectOutput) — allowed in `det`, forbidden in `pure`. Only
// `string` is accepted for now: formatting a non-string value (int/float/bool/
// rune → text) needs the value→string machinery interpolation also waits on.
func builtinFunctionSignature(name string) (*types.LambdaType, bool) {
	switch name {
	case "print", "println":
		return &types.LambdaType{
			Parameters: []types.ParameterType{{Type: types.PrimitiveType{Name: types.String}}},
			ReturnType: types.ReturnType{Type: types.VoidType{}},
		}, true
	}
	return nil, false
}
