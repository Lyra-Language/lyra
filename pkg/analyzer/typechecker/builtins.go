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
	// Array length: `xs.len()` on a fixed-size or dynamic array → i64 element count,
	// no arguments. (i64, not u64, so it composes with signed index arithmetic — a
	// negative index counts from the end.)
	if name == "len" && types.IsArray(recv) {
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
		}, true
	}
	// `x.weak()` on a `shared T` → a non-owning `weak T`. A method rather than new
	// syntax: `weak` is only a *type* in the grammar, and there was previously no
	// expression that produced one at all, so a `weak` field could be declared but
	// never constructed.
	//
	// The receiver must be `shared`: a weak reference is a reference to a
	// ref-counted box, and that is what `shared` means. Downgrading a stack value
	// would have nothing to point at, and downgrading a string or a `[]T` — also
	// boxed — is deliberately not offered: those have no identity a user can observe,
	// so a weak one would only be a way to write a dangling read.
	if name == "weak" && types.AllocationOf(recv) == types.Shared {
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.WeakType{Inner: types.WithAllocation(recv, types.Stack)}},
		}, true
	}
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

// isBuiltinPrintFn reports whether name is one of the compiler-provided output
// builtins (print/println). They are free functions, not methods, resolved by
// name in inferIdentifierCall only after scope resolution misses — so a user
// binding of the same name always shadows the builtin. Their effect
// classification lives separately in checker/effects.go's builtinEffects
// (EffectOutput) — allowed in `det`, forbidden in `pure`.
func isBuiltinPrintFn(name string) bool {
	return name == "print" || name == "println"
}

// isBuiltinPanicFn reports whether name is the compiler-provided `panic`. Resolved
// exactly like print/println — by name in inferIdentifierCall, only after scope
// resolution misses, so a user binding of the same name shadows it.
//
// `panic(msg: string) -> never` is the *only* way a Lyra program can reach the trap
// machinery deliberately. Every other trap (overflow, divide by zero, a bounds
// check, a non-exhaustive match) is emitted by the compiler on a condition it
// checks; this is the one the programmer writes. It shares their exit code and
// their stderr convention, because a panic the programmer wrote and a panic the
// compiler inserted are the same event and should be indistinguishable to whatever
// is watching the process.
func isBuiltinPanicFn(name string) bool {
	return name == "panic"
}

// isBuiltinReadLineFn reports whether name is the compiler-provided `read_line`.
// Resolved exactly like print/panic — by name in inferIdentifierCall, only after
// scope resolution misses, so a user binding of the same name shadows it.
//
// `read_line() -> Maybe<string>` reads one line from stdin with the line
// terminator removed, and is the program's only way to get input. Two decisions
// are worth stating because both had a cheaper alternative:
//
//   - **It returns `Maybe<string>`, not `string`.** EOF has to be distinguishable
//     from a blank line, and with a bare `string` it is not — the two are both
//     `""`. That is not a theoretical loss: the natural shape for reading input is
//     a loop, and a loop that cannot see EOF spins forever the moment stdin
//     closes. `None` at EOF makes the terminating case the one the reader has to
//     handle, which is the whole argument for having `Maybe` at all.
//   - **It is a builtin rather than a prelude function**, unlike `parse_i64`,
//     which is written in Lyra. The line has to come from libc, and Lyra has no
//     FFI — so unlike parsing, this genuinely cannot be expressed in the language.
//     Anything that *can* be belongs in the prelude, where it is readable and
//     testable; keeping the builtin surface to what is actually primitive is what
//     stops this registry from growing a standard library inside the compiler.
func isBuiltinReadLineFn(name string) bool {
	return name == "read_line"
}

// isBuiltinRandomSeedFn reports whether name is the compiler-provided
// `random_seed`. Resolved exactly like print/panic/read_line — by name in
// inferIdentifierCall, after scope resolution misses.
//
// `random_seed() -> u64` draws one word of **entropy from the operating system**,
// and that is all it does. It is deliberately not a random-number *generator*:
// the generator (`Rng`, `next_u64`, `below`, `random_below`) is ordinary Lyra in
// `std/prelude.lyra`, because a PRNG is arithmetic and arithmetic is expressible.
// Only the entropy is primitive — nothing in the language can ask the OS for a
// random word — so only the entropy is a builtin. Same division of labour as
// `read_line` (primitive, must be a builtin) beside `parse_i64` (expressible, so
// it is not).
//
// Keeping the *seed* as the primitive is also what makes the determinism story
// work. A seeded generator is pure arithmetic over its state, so `rng.below(100)`
// carries only EffectMut and stays legal in `det`; it is reaching for a seed
// nobody supplied that is non-deterministic, and that is exactly where EffectRand
// is charged. Had the builtin been `random_below` instead, every draw would carry
// the Rand bit and `det` code could not use randomness at all.
func isBuiltinRandomSeedFn(name string) bool {
	return name == "random_seed"
}

// isPrintableType reports whether print/println can format a value of type t:
// a string, any integer or float, a bool, or a rune. Each has a backend
// formatting path (write for strings, snprintf for numbers, "true"/"false" for
// bools, UTF-8 encoding for runes). Aggregates and functions are not printable
// (no Show/Display trait yet).
func isPrintableType(t types.Type) bool {
	return types.IsString(t) || types.IsNumeric(t) || types.IsBoolean(t) || isRuneType(t)
}
