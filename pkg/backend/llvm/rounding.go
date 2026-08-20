package llvm

import (
	"fmt"
	"github.com/Lyra-Language/lyra/pkg/typetable"
	"math"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
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

// floatMathIntrinsicOps maps the unary float-math builtins
// (typechecker/builtins.go's floatUnaryMathOps) to their LLVM intrinsics. They share the
// rounding builtins' shape — one float in, no arguments — and differ in what comes out: a
// float of the receiver's own width, so the result is the intrinsic's and there is no
// conversion to guard.
//
// The Lyra name and the intrinsic name coincide for every one of them, which is why this
// is a set spelled as a map: it stays a map so a future builtin whose spellings differ
// (`ln` over `llvm.log`, say) needs no restructuring.
//
// LLVM lowers each to the libm call of the same name, which is why `lyrac build` links
// `-lm` unconditionally — `sqrt` is the one that often becomes a hardware instruction
// instead, on every target this compiles for.
var floatMathIntrinsicOps = map[string]string{
	"log":   "log",
	"log2":  "log2",
	"log10": "log10",
	"sqrt":  "sqrt",
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
	if fn, params, ok := l.namespaceCallee(member, call); ok {
		return l.lowerDirectCall(block, call, fn, params)
	}
	// A resolved trait-impl method. Checked before the builtins so a user's own impl
	// wins over a compiler-provided method of the same name — matching the
	// typechecker's resolution order, which consults builtins last.
	if fn, isTraitCall, err := l.traitMethodCallee(call); err != nil {
		return nil, nil, err
	} else if isTraitCall {
		// No resolution in hand here: this path found the callee through the table, so
		// methodParams falls back to reading the same table for its modes.
		return l.lowerTraitMethodCall(block, call, member, fn, typetable.Resolution{})
	}
	if m, ok := intOverflowMethods[member.Property.Name]; ok {
		return l.lowerIntOverflowMethod(block, call, member, m)
	}
	if op, ok := checkedIntOps[member.Property.Name]; ok {
		return l.lowerCheckedIntMethod(block, call, member, op)
	}
	// `len` is two methods sharing a name, told apart by the receiver: an array's is
	// a field read of its box, a string's of its fat pointer (string_methods.go);
	// both O(1) since the rune count began riding the value (08/12). Dispatch
	// on the recorded receiver type rather than on the lowered value, so an
	// unrecorded receiver is an error here instead of silently taking the array path.
	if member.Property.Name == "len" || member.Property.Name == "slice" {
		recvT, ok := l.recordedType(member.Object)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for %s() receiver", member.Property.Name)
		}
		if types.IsString(recvT) {
			if member.Property.Name == "len" {
				return l.lowerStringLen(block, call, member)
			}
			return l.lowerStringSlice(block, call, member)
		}
		if member.Property.Name == "len" {
			return l.lowerArrayLen(block, call, member)
		}
	}
	// `from_end` is two methods sharing a name, told apart exactly as `len` is: a
	// string's is the backward byte walk, an array's is `len - k` with one check.
	if member.Property.Name == "from_end" {
		recvT, ok := l.recordedType(member.Object)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for from_end() receiver")
		}
		if types.IsString(recvT) {
			return l.lowerStringFromEnd(block, call, member)
		}
		return l.lowerArrayFromEnd(block, call, member, recvT)
	}
	if member.Property.Name == "push" {
		recvT, ok := l.recordedType(member.Object)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for push() receiver")
		}
		dyn, ok := l.resolveForLayout(recvT).(types.DynamicArrayType)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: push() on a non-dynamic-array receiver (%s)", recvT)
		}
		return l.lowerDynArrayPush(block, call, member, dyn)
	}
	// `p.offset(n)` — pointer arithmetic, the one form of it the language has
	// (pointers.go). Dispatched on the recorded receiver type, because `offset` is a
	// perfectly ordinary method name a user type may also have.
	if member.Property.Name == "offset" {
		recvT, ok := l.recordedType(member.Object)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for offset() receiver")
		}
		if ptrT, isPtr := l.resolveForLayout(recvT).(types.RawPointerType); isPtr {
			return l.lowerPointerOffset(block, call, member, ptrT)
		}
	}
	if member.Property.Name == "decode_utf8" {
		recvT, ok := l.recordedType(member.Object)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for decode_utf8() receiver")
		}
		return l.lowerDecodeUTF8(block, call, member, l.resolveForLayout(recvT))
	}
	if member.Property.Name == "compare_bytes" {
		return l.lowerStringCompareBytes(block, call, member)
	}
	if member.Property.Name == "compare_bytes_at" {
		return l.lowerStringCompareBytesAt(block, call, member)
	}
	if member.Property.Name == "byte_len" {
		return l.lowerStringByteLen(block, call, member)
	}
	if member.Property.Name == "byte_offset" {
		return l.lowerStringByteOffset(block, call, member)
	}
	if member.Property.Name == "weak" {
		return l.lowerWeakDowngrade(block, call, member)
	}
	// A call dispatched through a `where` bound (`v.show()` on a bounded type
	// parameter) resolves *abstractly* in the typechecker — to a trait and a method
	// name, not to an impl — because the concrete impl is only known once a
	// specialization fixes the parameter. The backend has no path from that to a
	// callee yet, so say what is missing rather than reporting the method as
	// unsupported: the program is well-typed and the bound is satisfied, and the
	// author would otherwise go looking for a mistake in code that has none.
	if ref, ok := l.res.MethodTable.GetBound(call); ok {
		return l.lowerBoundMethodCall(block, call, member, ref)
	}
	op, isRounding := roundingIntrinsicOps[member.Property.Name]
	if mathOp, isMath := floatMathIntrinsicOps[member.Property.Name]; isMath {
		op = mathOp
	} else if !isRounding {
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
	result := block.NewCall(l.roundingIntrinsicFunc(op, suffix, fT), recv)
	if !isRounding {
		// A unary float-math builtin answers a float of the receiver's own width — the
		// intrinsic's result, unconverted. Outside its domain each gives IEEE's value
		// rather than trapping (`log(0)` is -inf, `log(-1)` and `sqrt(-1)` are NaN),
		// which is the answer the float operators already give; feeding either to an
		// integer conversion is what traps, and that is the right place for it.
		return result, block, nil
	}
	block = l.guardFloatToInt(block, result, fT)
	// The builtin's fixed return type is i64 (builtins.go's floatRoundingOps);
	// narrow further with an explicit int conversion, e.g. i32(x.floor()).
	return block.NewFPToSI(result, lltypes.I64), block, nil
}

// guardFloatToInt traps unless v is a number an i64 can hold, and returns the block to
// carry on in. Emitted before every `fptosi`, which is poison out of range rather than
// saturating (see panicFloatToIntFunc).
//
// **The bounds are `-2^63 <= v < 2^63`, and the asymmetry is not a slip.** i64's minimum
// is exactly -2^63 and representable as a float; its *maximum* is 2^63-1, which is **not**
// representable in binary64 — the nearest float above 2^63-1 is 2^63 itself. So a `<=`
// against a float spelled `9223372036854775807` would compare against 2^63 and admit a
// value one past the end. The exclusive upper bound is exact in every float width.
//
// **A NaN traps here too, and for free.** The check is written as "trap unless in range"
// using *ordered* comparisons, which are false for a NaN — so it takes the trap edge
// without a test of its own. Written the other way round (`trap if v < lo || v >= hi`,
// with unordered compares) a NaN would slip through to the conversion, which is poison for
// it as well.
func (l *lowerer) guardFloatToInt(block *ir.Block, v value.Value, fT *lltypes.FloatType) *ir.Block {
	// 2^63 exactly, in the receiver's own width. f16 cannot reach it (its maximum is
	// 65504) and f32 represents it exactly, being a power of two — so one constant
	// serves all three widths without a per-width table.
	limit := constant.NewFloat(fT, math.Ldexp(1, 63))
	negLimit := constant.NewFloat(fT, -math.Ldexp(1, 63))
	aboveMin := block.NewFCmp(enum.FPredOGE, v, negLimit)
	belowMax := block.NewFCmp(enum.FPredOLT, v, limit)
	inRange := block.NewAnd(aboveMin, belowMax)
	outOfRange := block.NewXor(inRange, constant.NewInt(lltypes.I1, 1))
	return l.emitTrapIf(block, outOfRange, l.panicFloatToIntFunc())
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
// The membership check mirrors the typechecker's: a bare lookup would happily resolve
// `math.thing` to some *other* module's `thing`. Rejecting here as well means the
// backend cannot emit a call the front end did not sanction, which is the standing rule
// that it errors rather than guessing.
//
// **Both halves of it ask about a module, and both used to ask about a name.** The
// membership test went through DeclaringModule, a last-writer-wins map, and the callee
// came out of l.funcs under the bare name. That was sound only while a top-level name
// was program-wide unique — the premise a module's own declaration of an imported name
// retires (08/08). With it gone, a file declaring its own `tally` beside
// `import util.seq` made `seq.tally(…)` fail *in the backend*, with
// `unsupported method call`, on a program the front end had checked clean: DeclaringModule
// answered with the entry file's declaration, the membership test rejected the call, and
// it fell out of this path entirely. Both now go through the module, and the key is taken
// from the **declaration** resolved rather than from the name.
// It returns the callee's parameter list alongside the function, because that is what the
// argument coercion in lowerDirectCall reads — and for a specialization the parameters
// come from the instantiation rather than from the generic declaration.
func (l *lowerer) namespaceCallee(member *ast.MemberExpr, call *ast.FunctionCallExpr) (*ir.Func, []ast.Parameter, bool) {
	id, ok := member.Object.(*ast.IdentifierExpr)
	if !ok {
		return nil, nil, false
	}
	if _, isLocal := l.locals[id.Name]; isLocal {
		return nil, nil, false // a value shadows the namespace
	}
	st := l.res.SymbolTable
	imp, ok := st.NamespaceImport(member.GetLocation().File, id.Name)
	if !ok {
		return nil, nil, false
	}
	name := member.Property.Name
	if !st.ModuleDeclares(imp.Path, name) {
		return nil, nil, false
	}
	// A *generic* callee resolves to the specialization the typechecker solved for this
	// call site, not to the generic name — which has no emitted body, since a type
	// variable has no representation. Checked before l.funcs for the same reason the
	// by-name path checks it first (lowerIdentifierCall), and it is not optional here:
	// l.funcs holds only functions emitted as themselves, so `maybe.map(m, f)` found
	// nothing, fell out of this path entirely, and died as `unsupported method call`.
	if fn, params, ok := l.specializedFuncFor(call); ok {
		return fn, params, true
	}
	lam, ok := st.LookupFunctionIn(imp.Path, name)
	if !ok {
		return nil, nil, false
	}
	// Keyed from the *declaration's* own location, not from the call's: funcKey resolves
	// a name as the file at that location sees it, and the asking file is precisely the
	// one that may have declared its own.
	fn, ok := l.funcs[l.funcKey(name, lam.GetLocation())]
	if !ok {
		return nil, nil, false
	}
	return fn, lam.Parameters, true
}
