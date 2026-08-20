package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Builtin **method** calls: `x.floor()`, `xs.len()`, `s.slice(a, b)`, `p.offset(n)` — a
// call whose callee is a MemberExpr the typechecker resolved to a compiler-provided
// method (typechecker/builtins.go) rather than to a struct field, a trait method or a
// user function. This file is that registry's back half, and is named for it.
//
// It lived in `rounding.go` until 08/19, for the ordinary reason: rounding was the only
// builtin method there was when the dispatcher was written (07/17), and each of the
// dozen added since — `wrapping_*`, `len`, `slice`, `from_end`, `push`, the byte-level
// string methods, `weak`, `offset`, `decode_utf8`, `encode_utf8` — landed in the file
// that already had the switch. The name stopped describing the contents after the second
// one, and a reader looking for `push`'s dispatch had no reason to open a file about
// floats.
//
// **Every case delegates.** That was not true before the move: rounding and float-math
// were handled *inline* at the end of the dispatcher, because that tail was the original
// function the file was named for — so the one case a reader would expect to find there
// was the one case that broke the shape. `lowerFloatMathMethod` (rounding.go) is now a
// callee like the rest.
//
// **Order is resolution order**, and it is the typechecker's, not this file's: a
// namespace call first (its "receiver" is not a value at all), then a resolved trait
// method, then the builtins — so a user's own impl always beats a compiler-provided
// method of the same name. Reordering these silently changes which of two callees a
// program means.

// lowerBuiltinMethodCall dispatches one builtin-method call to its lowering, in the order
// above.
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
	if member.Property.Name == "encode_utf8" {
		return l.lowerEncodeUTF8(block, call, member)
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
	// Rounding and the unary float-math builtins (rounding.go), which are the same
	// shape — one float in, no arguments — and differ only in what comes out. Last
	// because they are the fallthrough: a name matching none of the arms above is
	// either one of these or nothing at all.
	return l.lowerFloatMathMethod(block, call, member)
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
