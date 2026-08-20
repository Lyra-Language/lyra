package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

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

// checkedIntBinaryOps are the third member of that family, and the one that answers
// rather than deciding: `(self: T, other: T) -> Maybe<T>`, `None` where the operation
// would overflow.
//
// The three cover the three sensible reactions to overflow, and having all three is the
// point of trapping by default: `+` traps, which is the safe answer when the author has
// not thought about it; `wrapping_*` says "modular arithmetic is what I meant";
// `saturating_*` says "clamp"; and `checked_*` says "I will handle it", handing back a
// `Maybe` the caller must open. Rust's split is the same, for the same reason.
//
// `checked_div` is in the set although division cannot *overflow* in the intrinsic
// sense: its two failures — a zero divisor, and `INT_MIN / -1` — are exactly the two
// cases `/` traps on, so the name means the same thing (the operation the operator
// would have refused, as a value). It is arguably the most useful of the four, since a
// zero divisor is the overflow case that most often comes from data rather than from a
// bug.
var checkedIntBinaryOps = map[string]string{
	"checked_add": "add",
	"checked_sub": "sub",
	"checked_mul": "mul",
	"checked_div": "div",
}

// isOverflowArithBuiltin reports whether name is one of the integer
// overflow-arithmetic builtins — the union of the two maps above. This is the set
// the newtype method fallback refuses (lyra-E043): these methods are the escape
// hatches of the operators a newtype must opt into, so reaching them through the
// wrapper would hand out the arithmetic the operator rule withholds, mixed
// operands included. The float rounding ops are deliberately not in the set —
// they are `i64(x)`'s alternative, not an operator's, and a conversion already
// flows by the ordinary value rules.
func isOverflowArithBuiltin(name string) bool {
	if intBinaryOps[name] {
		return true
	}
	_, ok := checkedIntBinaryOps[name]
	return ok
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

// floatUnaryMathOps are the transcendental-and-friends builtins: one float in, one float
// of the **same width** out. Unlike the rounding builtins they do not narrow to i64 — a
// log or a root is a float operation whose answer is a float, and narrowing here would
// throw away precision the caller may still want.
//
// **Builtins rather than prelude Lyra, and that is the `random_seed` rule rather than
// the `parse_i64` one.** None of these is expressible in this language — no series, no
// lookup table, and no FFI to reach libm — so they have to be primitive. Parsing and
// formatting are arithmetic and live in the prelude; these cannot.
//
// **One map, because they are one question.** Each is `llvm.<name>.<width>` and each
// returns the receiver's width, so a second table keyed the same way is the drift hazard
// 8 keeps cataloguing. Adding `exp` or `sin` later is a line here and a line in the
// backend's twin, and nothing else.
//
// The three logarithms ship together because smooth mandelbrot coloring is
// `n + 1 - log2(log(|z|))` — `log2` written as `x.log() / 2.0.log()` costs an extra call
// and loses accuracy at exactly the magnitudes shading depends on. Having the trio is
// also what makes the bare name's base unambiguous by contrast: `log` is the one with no
// subscript, which is `e`.
var floatUnaryMathOps = map[string]bool{
	"log":   true,
	"log2":  true,
	"log10": true,
	"sqrt":  true,
}

// builtinMethodSignature returns the LambdaType of the builtin method name for a
// receiver of type recv, specialized to recv's concrete type, or ok=false when
// no builtin of that name applies to that receiver. The signature's parameters
// are the *call* arguments only — the receiver (self) is implicit, so
// `x.wrapping_add(y)` matches a one-parameter signature `(T) -> T`.
//
// An untyped-literal receiver is promoted to its default (e.g. `5.wrapping_add`
// treats 5 as i64), mirroring how an unannotated literal binding is typed.
func (tc *TypeChecker) builtinMethodSignature(recv types.Type, name string, loc ast.Location) (*types.LambdaType, bool) {
	// Array length: `xs.len()` on a fixed-size or dynamic array → i64 element count,
	// no arguments. (i64, not u64, so `xs[i - 1]`-style signed index arithmetic
	// composes without a conversion at every use.)
	if name == "len" && types.IsArray(recv) {
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
		}, true
	}
	// `xs.from_end(k)` / `s.from_end(k)` — the k-th value from the end, 1-based:
	// `from_end(1)` is the last element (rune), `from_end(2)` the one before it.
	// This is the explicit spelling that replaced the negative index (08/12): `s[-1]`
	// used to mean the same thing, and the audit's objection was that it made the
	// most common off-by-one — an index underflowing past zero — a *valid read of
	// the wrong element* instead of a trap, in the language whose thesis is
	// trap-over-silently-wrong. The accessor keeps the performance the negative
	// spelling was justified by: on a string it is the same backward byte walk
	// (O(k) with no decoding, where `s[s.len() - 1]` is two full decode walks), and
	// on an array it is `len - k` with one bounds check. 1-based because "the 1st
	// from the end" is how the phrase reads — and it maps exactly onto the `-1`/`-2`
	// it replaced. Out of range (k < 1, or k > len) traps, like the index it mirrors.
	if name == "from_end" {
		i64t := types.PrimitiveType{Name: types.Int64}
		switch r := recv.(type) {
		case types.StaticArrayType:
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}},
				ReturnType: types.ReturnType{Type: r.ElementType},
			}, true
		case types.DynamicArrayType:
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}},
				ReturnType: types.ReturnType{Type: r.ElementType},
			}, true
		}
		if types.IsString(recv) {
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}},
				ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Rune}},
			}, true
		}
	}
	// `s.encode_utf8()` — a string's bytes, as a `[]u8`.
	//
	// `decode_utf8`'s inverse, and a builtin for the mirror-image reason: **nothing in the
	// language can read a byte out of a string.** `byte_len` measures, `byte_offset` maps
	// a rune position to a byte one and `compare_bytes_at` compares — none of them reads,
	// and `s[i]` is a rune. So the bytes were reachable only by re-encoding each rune by
	// hand, which is a UTF-8 encoder written in user code to recover bytes the string
	// already holds.
	//
	// **Dynamic, not `[N]u8`**: the length is a run-time property of the string, so a
	// fixed size could not be written down. That also makes the result `push`-able, which
	// is what `std.ffi`'s NUL-terminated form needs.
	if name == "encode_utf8" && types.IsString(recv) {
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.DynamicArrayType{
				ElementType: types.PrimitiveType{Name: types.UInt8},
			}},
		}, true
	}
	// `bytes.decode_utf8()` — a `[]u8` or `[N]u8` read as UTF-8 text.
	//
	// **A builtin because it has to be**: it allocates a ref-counted string box and
	// copies into it, and nothing in the language can build a string out of parts.
	// Concatenation is the only construction Lyra code has, so the alternative was
	// `s = s ++ "${rune(b)}"` in a loop — O(n²) in allocations, which is what
	// `examples/zlib.lyra` did to read a six-byte version string.
	//
	// **Named for the interpretation, not for the destination.** `to_string` on a byte
	// array is ambiguous with rendering it — `[104, 105]` as text is "hi" or "[104, 105]"
	// depending on what the reader assumed — and the two are both things a program wants.
	// `decode_utf8` says which, and pairs with the `encode_utf8` that will read bytes back
	// out (todo.md).
	//
	// It does **not** validate, which is a decision rather than an omission: `read_line`
	// already builds a string from libc bytes without validating, and one unvalidated
	// answer is better than two different ones. Both count runes with `lyra_utf8_count`,
	// so both are wrong the same way on malformed input, and both are fixed by the same
	// change.
	if name == "decode_utf8" && isByteArray(recv) {
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.String}},
		}, true
	}
	// `p.offset(n)` — the only pointer arithmetic the language has, and a *method*
	// rather than an operator on purpose.
	//
	// `p[i]` was the obvious spelling and is the wrong one: it is `xs[i]`'s spelling
	// with none of `xs[i]`'s bounds check, so the two would look alike and behave
	// differently in the language whose thesis is that they must not. Written this way
	// the rule stays statable — *pointer arithmetic is a named method, never an
	// operator* — and `^` remains the only load, so `p.offset(3)^` is visibly the two
	// acts it is.
	//
	// Three things it decides, each of which had a plausible other answer:
	//
	//   - **Elements, not bytes.** A `^T` knows its pointee, and the parallel is the
	//     language's own: a string is rune-indexed so `len` and `[i]` agree about the
	//     unit, and `byte_offset` is the precedent for putting the unit in the name when
	//     it differs.
	//   - **Mutability propagates.** `^mut T` in, `^mut T` out — otherwise
	//     `p.offset(n)^ = v` is unwritable and the write direction stays broken.
	//   - **Signed.** A negative offset is meaningful in C, and refusing it buys nothing
	//     when nothing here is checked anyway.
	//
	// Nothing is checked, and nothing can be: a raw pointer carries no length. That is
	// what `std.ffi`'s `CBuffer` is for — a pointer *and* a length can be checked, so
	// the trapping accessor is ordinary Lyra written over this one primitive, and
	// `unsafe` appears once in the standard library instead of at every use.
	if ptr, ok := recv.(types.RawPointerType); ok && name == "offset" {
		return &types.LambdaType{
			Parameters: []types.ParameterType{{Type: types.PrimitiveType{Name: types.Int64}}},
			ReturnType: types.ReturnType{Type: ptr},
		}, true
	}
	// `xs.push(v)` — the growth operation, on a **dynamic** array only. A `[N]T` has
	// its size in its type, so growing one would change what type it is.
	//
	// It returns void and mutates in place rather than returning a new array, because
	// `[]T` already has reference semantics: `let b = a; a[0] = 9` is visible through
	// `b`, so a `push` that produced a new value would be the odd one out — and the
	// representation makes the in-place reading the only sound one anyway (the box
	// pointer is the value, so the elements sit behind an indirection precisely so that
	// growth cannot move it out from under an alias).
	//
	// The receiver must permit interior mutation, checked at the call site by the same
	// predicate `a[0] = v` uses — a plain `let` is deeply immutable, and push is
	// interior mutation with a different spelling.
	if dyn, ok := recv.(types.DynamicArrayType); ok && name == "push" {
		return &types.LambdaType{
			Parameters: []types.ParameterType{{Type: dyn.ElementType}},
			ReturnType: types.ReturnType{Type: types.VoidType{}},
		}, true
	}
	// String methods, both **rune-indexed**, which is not a fresh decision: `s[i]`
	// already yields the i-th *code point* and `for c in s` already walks code
	// points, so a byte-based length would make `for i in 0..<s.len() { s[i] }` —
	// the most obvious loop anyone writes — wrong on the first non-ASCII input. The
	// fat pointer's `len` field stays bytes (STRING_LAYOUT.md); that is the
	// representation, and this is the language.
	//
	//   - `len() -> i64` is the rune count — **O(1)** as of 08/12, a field read of
	//     the count the fat pointer carries (STRING_LAYOUT.md), so agreeing with the
	//     index no longer costs a walk. `for c in s` (or `for i, c in s` with the
	//     index) remains the way to traverse; `s[i]` in a loop still decodes from
	//     the start each time.
	//   - `slice(start, end) -> string` is the half-open rune range `[start, end)`,
	//     matching `..<` and array indexing. It **allocates**, so `noalloc` refuses
	//     it — see lowerStringSlice for why a borrowed slice is not on offer.
	//
	// i64 for both, like the array `len`, so they compose with signed index
	// arithmetic rather than forcing a conversion at every use.
	if types.IsString(recv) {
		switch name {
		case "len":
			return &types.LambdaType{
				ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
			}, true
		case "slice":
			i64t := types.PrimitiveType{Name: types.Int64}
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}, {Type: i64t}},
				ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.String}},
			}, true
		case "compare_bytes":
			// The primitive under the prelude's `impl Ord for string`: negative, zero or
			// positive, memcmp's convention. It returns an `i64` rather than an
			// `Ordering` so the backend needs no knowledge of a prelude type — the same
			// division `random_seed` and `read_line` follow, where the builtin is the
			// part that cannot be written in Lyra and everything shaped on top of it
			// lives in the prelude.
			//
			// **Byte order is code-point order in UTF-8**, by design of the encoding, so
			// this answers exactly what a rune-by-rune walk would — in one memcmp rather
			// than O(n) O(i) index operations. That property is why the builtin is
			// worth having at all.
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: types.PrimitiveType{Name: types.String}}},
				ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
			}, true
		case "byte_len":
			// The **representation's** length, in bytes, O(1) — the fat pointer's own
			// field (STRING_LAYOUT.md); `len()` is the rune-count field beside it,
			// O(1) too as of 08/12.
			//
			// Exposing it is a deliberate crack in "rune-indexed is the language, bytes
			// are the representation", and it is narrow on purpose: it exists so
			// `compare_bytes_at` below has an offset to be given, and the unit is in the
			// name for the reason `wall_clock_nanos` puts its unit there — a length that
			// does not say which one invites the guess that fails silently on the first
			// non-ASCII input. **`len()` remains what ordinary code wants**: it is the
			// one that agrees with `s[i]` and `for c in s`.
			return &types.LambdaType{
				ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: types.Int64}},
			}, true
		case "byte_offset":
			// The byte offset at which rune `i` starts — the rune→byte conversion that
			// `compare_bytes_at` needs and that nothing else could supply, since byte
			// offsets are not otherwise derivable from inside the language.
			//
			// It is what makes "does `sep` occur at rune i" cheap:
			// `s.compare_bytes_at(s.byte_offset(i).unwrap_or(-1), sep) == 0`. The two
			// compose without a `match` precisely because `compare_bytes_at` is total —
			// `unwrap_or(-1)` hands it an offset it already answers negative for — which
			// is the payoff for having made it total rather than trapping.
			//
			// **It maps positions, not elements**, so the end position (`i` == the rune
			// count) is `Some(byte_len)` rather than `None`. That is deliberately
			// `slice`'s rule rather than `s[i]`'s: this exists to convert *bounds*, and a
			// bound may name the end — `s.slice(a, n)` is ordinary, so `byte_offset(n)`
			// must have an answer. A negative `i` is `None` (08/12; it counted from
			// the end before): positions are non-negative, and None matches `index`'s
			// negative offset — a Maybe gives the no-such-position answer a shape.
			//
			// `Maybe<i64>`, not -1, for the reason the prelude's own functions return one;
			// a Maybe of a scalar is an inline union, so it stays `noalloc`. Resolved
			// through the canonical-Maybe accessor `read_line` and `checked_*` use, so a
			// program whose Maybe is named something else gets its own type back.
			maybeName, ok := tc.canonicalTypeName("Maybe", loc)
			if !ok {
				return nil, false
			}
			i64t := types.PrimitiveType{Name: types.Int64}
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}},
				ReturnType: types.ReturnType{Type: types.ParameterizedType{
					Name:          maybeName,
					TypeArguments: []types.Type{i64t},
				}},
			}, true
		case "compare_bytes_at":
			// `compare_bytes` at a byte offset: compares `other`'s bytes against the
			// bytes of `self` starting at `offset`, memcmp's convention, so
			// `s.compare_bytes_at(0, p) == 0` is "p is a prefix of s".
			//
			// **It compares exactly `other`'s length**, which is what separates it from
			// `compare_bytes` and is the whole point: comparing the *rest* of `self`
			// would make a prefix test come out non-zero because `self` is longer.
			// Where `self` has fewer bytes than that — including an out-of-range offset
			// — the answer is negative, the short-operand-sorts-first rule the sibling
			// already follows. Note the order those two rules apply in, which is
			// memcmp's and is easy to guess backwards: **a byte mismatch decides before
			// any shortfall**, so `"hello".compare_bytes_at(4, "lo")` is *positive*
			// ('o' > 'l') rather than negative for the missing byte. Short-sorts-first
			// only settles a range that matched as far as it went. Every predicate built
			// on this asks `== 0`, where the distinction cannot arise.
			//
			// It is the primitive the prelude's `starts_with`/`ends_with` are one line
			// each on top of, and `contains`/`split` will be a loop over. That division
			// is the standing one: a memcmp at an offset cannot be written in Lyra
			// (byte offsets are not addressable from the language), and the predicates
			// built on it are ordinary code.
			//
			// **Rune-correct despite being byte-level**, which is why it can carry a
			// language-level predicate at all: UTF-8 is prefix-free and
			// self-synchronizing, so for well-formed strings a byte-prefix is exactly a
			// rune-prefix and a byte-suffix exactly a rune-suffix. Every Lyra string is
			// well-formed by construction.
			i64t := types.PrimitiveType{Name: types.Int64}
			return &types.LambdaType{
				Parameters: []types.ParameterType{{Type: i64t}, {Type: types.PrimitiveType{Name: types.String}}},
				ReturnType: types.ReturnType{Type: i64t},
			}, true
		}
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
	if _, isChecked := checkedIntBinaryOps[name]; isChecked {
		if !isAnyConcreteInt(p.Name) {
			return nil, false
		}
		// The result is a `Maybe<T>` of the **canonical** Maybe, resolved through the
		// same accessor `read_line` uses — the marker confers that identity, so a
		// program whose Maybe is named something else still gets its own type back.
		// With no canonical Maybe at all there is nothing to return, so the method
		// simply does not exist; the call then reports "no such method" rather than
		// naming a type the program does not have.
		maybeName, ok := tc.canonicalTypeName("Maybe", loc)
		if !ok {
			return nil, false
		}
		return &types.LambdaType{
			Parameters: []types.ParameterType{{Type: recv}},
			ReturnType: types.ReturnType{Type: types.ParameterizedType{
				Name:          maybeName,
				TypeArguments: []types.Type{recv},
			}},
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
	if floatUnaryMathOps[name] {
		if !isAnyConcreteFloat(p.Name) {
			return nil, false
		}
		// The receiver's own width, not a fixed one: `f32.log()` is an f32.
		return &types.LambdaType{
			Parameters: nil,
			ReturnType: types.ReturnType{Type: recv},
		}, true
	}
	return nil, false
}

// builtinMethodAllocates reports whether a resolved builtin method puts a value on
// the heap, so `noalloc` can refuse it.
//
// Almost none of them do — the registry is otherwise arithmetic over a scalar, and
// the 08/05 work that made builtin methods *pure* exists precisely so a PRNG or a
// checksum can be `pure noalloc`. `s.slice(a, b)` is the exception: a substring of a
// ref-counted string cannot borrow its parent's bytes, so it copies into a fresh box
// (backend/llvm/string_methods.go).
//
// Asked here, where the receiver's type is still in hand, and recorded on the call
// (`MethodTable.SetBuiltinMethod`) rather than re-derived downstream from the
// property name. The purity pass carries *three* copies of the "what does this call
// call?" ladder, and every one of them treats a builtin method as effect-free — so
// an allocating builtin is invisible to all three at once, and `noalloc` silently
// accepts a function that allocates on every call. That is the failure this
// parameter exists to prevent; it was live for as long as it took to write the
// prelude's `trim`.
func builtinMethodAllocates(recv types.Type, name string) bool {
	if name == "decode_utf8" && isByteArray(recv) {
		return true
	}
	if name == "encode_utf8" && types.IsString(recv) {
		return true
	}
	if name == "slice" && types.IsString(recv) {
		return true
	}
	// `push` grows the element buffer when it is full. It does not allocate on *every*
	// call — amortized doubling means most pushes are a store — but `noalloc` is a
	// static promise about what a function may do, not a statistical one, so a call
	// that can allocate counts.
	if name == "push" {
		if _, isDyn := recv.(types.DynamicArrayType); isDyn {
			return true
		}
	}
	return false
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

// isBuiltinWallClockFn reports whether name is the compiler-provided
// `wall_clock_nanos`. Resolved exactly like print/panic/read_line/random_seed — by
// name in inferIdentifierCall, after scope resolution misses.
//
// `wall_clock_nanos() -> i64` asks the OS what time it is, in nanoseconds since the
// Unix epoch, and that is all it does. Everything derived from the answer —
// seconds, elapsed durations, formatting — is arithmetic, so it belongs in the
// prelude rather than here; same division of labour as `random_seed` beside the
// prelude's `Rng`, and `read_line` beside `parse_i64`.
//
// It carries EffectTime, so `pure` and `det` both refuse it, for the reason
// `random_seed` carries EffectRand: reading a clock nobody passed in is what makes a
// computation non-reproducible. A *threaded* timestamp — one taken at the edge of the
// program and passed down as a parameter — is ordinary `i64` data carrying no effect
// at all, which is what lets `det` code work with time.
//
// See `pkg/backend/llvm/clock.go` for why the name spells its unit, and why the
// result is signed.
func isBuiltinWallClockFn(name string) bool {
	return name == "wall_clock_nanos"
}

// The terminal builtins — `set_raw_mode`, `read_key`, `terminal_size`. Resolved by name
// in inferIdentifierCall after scope resolution misses, like every builtin above.
//
// These three exist because a TUI's **input** half cannot be written in Lyra and its
// output half already could: `\e` reaches stdout as byte 27, so ANSI colour, cursor
// positioning and the alternate screen buffer were always ordinary `print` calls, while
// `read_line` waits for Enter and no amount of prelude code makes it not. Raw mode is
// `tcsetattr`, a single keypress is `read` on a raw fd, and the window size is an
// `ioctl` — three syscalls, no FFI, so three builtins.
//
// Everything above them is prelude Lyra: decoding an escape sequence into an arrow key,
// colour helpers, box drawing, frame diffing, a status bar. That is the `read_line`
// beside `parse_i64` division exactly, and the line is the same one — the syscall is
// primitive, the interpretation is not.

// isBuiltinSetRawModeFn reports whether name is `set_raw_mode`.
//
// `set_raw_mode(on: bool)` turns off line buffering, echo and signal generation, so a
// keypress is readable the moment it happens. It carries EffectOutput rather than
// EffectInput: it changes the world instead of reading it, and it is *deterministic* —
// which is what lets `det` code enter and leave raw mode while still being refused
// `read_key`. Same classification, and the same reasoning, as `print`.
//
// The original terminal settings are saved by the shim on the first enable and restored
// on disable, so this takes a bool rather than returning a token to hand back. See
// `pkg/backend/llvm/tui.go` for why that is not merely convenient.
func isBuiltinSetRawModeFn(name string) bool {
	return name == "set_raw_mode"
}

// isBuiltinReadKeyFn reports whether name is `read_key`.
//
// `read_key() -> Maybe<rune>` reads one **code point** — not one byte, because `rune`
// means a code point everywhere else in this language. `None` is EOF, which is the same
// reason `read_line` answers a `Maybe`: in raw mode a read blocks until a byte arrives,
// so `None` cannot mean "no key yet" and a bare `rune` would have no way to say the
// input has ended.
//
// An escape sequence arrives as its separate bytes (ESC, `[`, `A` for an arrow key)
// rather than as one decoded key. Assembling those needs a timeout, a table and a policy
// for unknown sequences — none primitive, all expressible — so it is `std.tui`'s job.
//
// EffectInput, like `read_line`, and destructive in the same way: the key is consumed, so
// two identical calls do not return the same thing.
func isBuiltinReadKeyFn(name string) bool {
	return name == "read_key"
}

// isBuiltinTerminalSizeFn reports whether name is `terminal_size`.
//
// `terminal_size() -> (i64, i64)` answers **(columns, rows)** — width first. That is the
// reverse of the `struct winsize` underneath it, deliberately: the tuple is this
// language's API rather than C's, and "size" is width-then-height wherever it is a pair.
//
// EffectInput rather than EffectOutput, and the reason is worth stating because the call
// looks like a query about the program rather than about the world: the window can be
// resized between two calls, so the result depends on external state and is not
// reproducible — which is exactly what EffectInput classifies. A viewer that redraws on
// resize is *built* on that not being deterministic.
//
// It never fails. A closed or absent window answers the 80x24 fallback, so piping the
// output somewhere still renders; see `pkg/backend/llvm/tui.go` for why a defined answer
// beats a plausible one.
func isBuiltinTerminalSizeFn(name string) bool {
	return name == "terminal_size"
}

// isBuiltinWaitForKeyFn reports whether name is `wait_for_key_ms`.
//
// `wait_for_key_ms(timeout: i64) -> bool` waits up to `timeout` milliseconds for input to
// become readable, and answers whether it did. It is the fourth terminal builtin, added
// once `std.tui` proved the other three could not resolve one case: `\e` begins every
// escape sequence, so a decoder must look at what follows it, and with only a *blocking*
// read a lone Escape waits for the next keypress instead of being reported.
//
// **A bool, deliberately, rather than a timed read returning `Maybe<rune>`.** There are
// three outcomes — a key arrived, nothing yet, input ended — and a `Maybe` has two
// answers, so it must conflate two of them. Conflating "nothing yet" with "ended" is the
// exact mistake `read_line`'s `Maybe` exists to avoid, and it makes the natural loop spin
// forever once stdin closes. Splitting the question needs no new type: this says whether
// there is anything to read, and `read_key` then says whether it is a key or the end.
//
// The pairing is a property of `poll` rather than a convention: **a closed descriptor
// reports readable**, so at EOF this answers true and the following `read_key` answers
// `None`. See `pkg/backend/llvm/tui.go` for the timeout's clamping rule.
//
// EffectInput, like `read_key` — the answer depends on state the caller never passed in.
// It consumes no input, but a `det` function that could observe whether a key is waiting
// is not reproducible.
func isBuiltinWaitForKeyFn(name string) bool {
	return name == "wait_for_key_ms"
}

// isPrintableType reports whether print/println can format a value of type t:
// a string, any integer or float, a bool, or a rune. Each has a backend
// formatting path (write for strings, snprintf for numbers, "true"/"false" for
// bools, UTF-8 encoding for runes). Aggregates and functions are not printable
// (no Show/Display trait yet).
func isPrintableType(t types.Type) bool {
	return types.IsString(t) || types.IsNumeric(t) || types.IsBoolean(t) || isRuneType(t)
}

// isByteArray reports whether t is an array of u8, fixed or dynamic — the receiver
// `decode_utf8` takes.
//
// Both flavours, because which one a byte buffer is depends on how it was written rather
// than on what it holds: `[104, 105]` is a `[2]u8` and the same literal under a `[]u8`
// annotation is dynamic, and a reader decoding a buffer does not think of the two as
// different questions.
func isByteArray(t types.Type) bool {
	var elem types.Type
	switch v := t.(type) {
	case types.DynamicArrayType:
		elem = v.ElementType
	case types.StaticArrayType:
		elem = v.ElementType
	default:
		return false
	}
	p, ok := elem.(types.PrimitiveType)
	return ok && p.Name == types.UInt8
}
