package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Foreign functions: `extern name: (…) -> T`, and a call to one.
//
// **There is almost nothing here, and that is the design working.** An extern is a
// signature standing in for a body someone else supplies, which is a shape this package
// already emits — a function declared before any body exists, so a call can reference it.
// `ExternDeclStmt.Func()` is that body-less function, so declaring one is
// `declareFunctionAs` and calling one is the ordinary call path, reached through the same
// `l.funcs` key any other function is found by. Nothing downstream of this file knows an
// extern exists.
//
// Three things are genuinely different, and each is a decision rather than a detail:
//
//   - **The symbol is the name as written**, not `userSymbol`'s `lyra.<module>.<name>`.
//     A foreign symbol is the linker's, so mangling it would name a function nobody
//     defines — and the failure would be a link error about a symbol the source never
//     mentions.
//   - **Two declarations of one foreign name are one function.** They must be, since the
//     symbol is global; the question is only whether they agree, and a disagreement is an
//     error here rather than two `declare`s of one name, which is invalid IR.
//   - **No ownership crosses.** An extern's parameters and result are FFI-safe by
//     lyra-E063 — scalars, `^T`, `void` — so none of them is reference-counted and there
//     is no retain, release or drop glue to emit. The rule the front end enforces is what
//     makes this file short.

// declareExterns emits a `declare` for every foreign function the program names, before
// any body is lowered, and registers it under the key a call resolves by.
//
// Registered in `l.funcs`, not in a table of its own: a call to an extern is an ordinary
// call by name, and giving it a second lookup would be a second answer to a question
// `funcKey` already answers — including for a private declaration, whose key is
// module-qualified (rule 4).
func (l *lowerer) declareExterns(program *ast.Program) error {
	for _, stmt := range program.Statements {
		ext, ok := stmt.(*ast.ExternDeclStmt)
		if !ok {
			continue
		}
		declared, err := l.declareExtern(ext)
		if err != nil {
			return err
		}
		key := l.funcKey(ext.Name, ext.GetLocation())
		l.funcs[key] = declared
		l.funcParams[key] = ext.Func().Parameters
		// By declaration as well as by name, for the same reason an ordinary function is:
		// a bare call the typechecker resolved past the scope chain reaches its callee by
		// identity, and `ExternDeclStmt.Func()` is the one instance it publishes.
		l.recordByDecl(ext.Func(), declared, ext.Func().Parameters)
	}
	return nil
}

// markNarrowCrossings gives every parameter and return of a foreign declaration narrower
// than 32 bits the extension attribute clang gives it: `zeroext` for `bool`, `u8` and `u16`,
// `signext` for `i8` and `i16`. Mirrored at each call by callWithDeclaredAttrs: LLVM reads
// ABI attributes off the call, not the callee.
//
// **The attribute is the caller's promise that the register's upper bits are clean**, and
// C code compiled by clang relies on it — Apple's arm64 convention makes the caller extend
// (AAPCS64 proper does not), and x86-64 compilers have always assumed it. Without it the
// low byte is right and the rest of the register is whatever the last computation left
// there: `SDL_SetRenderDrawColor(r, g, b, a)` read a computed `u8` component as, say,
// 0x3A0000C8, clamped it to 1.0, and drew cyan where the program asked for grey. It links,
// it runs, and a constant argument hides it, because a freshly materialised constant is
// already clean.
//
// **Signedness comes from the Lyra type**, since `i8` in LLVM says nothing about it — the
// reason this reads the signature rather than the emitted parameters. A newtype over a
// narrow integer crosses as its base, so the type is seen through first, from the extern's
// own module (rule 4).
//
// A planned (aggregate-carrying) signature flattens: an `sret` pointer may lead, and an
// aggregate parameter may become several LLVM parameters. Only a scalar slot is narrow, and
// a scalar slot is always exactly one parameter.
func (l *lowerer) markNarrowCrossings(fn *ir.Func, ext *ast.ExternDeclStmt, plan *externPlan) {
	sig := ext.Signature
	if sig == nil {
		return
	}
	defer l.enterModuleOf(ext.GetLocation())()
	next := 0
	if plan != nil && plan.retIndirect {
		next = 1
	}
	for i, p := range sig.Parameters {
		width := 1
		if plan != nil && i < len(plan.params) {
			width = len(plan.params[i].llTypes)
		}
		if width == 1 && next < len(fn.Params) {
			if zext, sext := l.narrowExtension(p.Type); zext {
				fn.Params[next].Attrs = append(fn.Params[next].Attrs, enum.ParamAttrZeroExt)
			} else if sext {
				fn.Params[next].Attrs = append(fn.Params[next].Attrs, enum.ParamAttrSignExt)
			}
		}
		next += width
	}
	if plan != nil && plan.ret.aggregate {
		return
	}
	if zext, sext := l.narrowExtension(sig.ReturnType.Type); zext {
		fn.ReturnAttrs = append(fn.ReturnAttrs, enum.ReturnAttrZeroExt)
	} else if sext {
		fn.ReturnAttrs = append(fn.ReturnAttrs, enum.ReturnAttrSignExt)
	}
}

// narrowExtension says how a scalar narrower than C's `int` is widened in a register:
// zero-extended (`bool`, `u8`, `u16`), sign-extended (`i8`, `i16`), or neither.
func (l *lowerer) narrowExtension(t types.Type) (zext, sext bool) {
	for range 64 {
		stripped := l.stripNewtype(l.resolveShape(l.stripNewtype(t)))
		if types.TypesEqual(stripped, t) {
			break
		}
		t = stripped
	}
	prim, ok := t.(types.PrimitiveType)
	if !ok {
		return false, false
	}
	switch prim.Name {
	case "bool", "u8", "u16":
		return true, false
	case "i8", "i16":
		return false, true
	}
	return false, false
}

// callWithDeclaredAttrs emits a call to fn carrying the parameter and return attributes
// its declaration has — `zeroext`/`signext` on a narrow integer or `bool` crossing to C, `byval` and `sret` on an
// aggregate the ABI passes in memory — so the caller's side of the convention matches the
// callee's.
//
// **A call site's attributes are the ones that lower the call**, not the declaration's,
// which is the whole reason this function exists and what made dropping `byval` a bug
// rather than untidiness (09/20): with `byval` on the `declare` alone, LLVM passed a
// pointer in a register while the C function read a copy on the stack. On x86-64 that is a
// segmentation fault the first time the callee dereferences a field. **On aarch64 it is
// invisible**, because a MEMORY aggregate is passed there as a plain pointer and clang
// emits no attribute at all — so every machine in this project agreed the formatter worked,
// while it crashed on the architecture CI runs, and the one test that would have said so
// was skipping for want of a library.
func callWithDeclaredAttrs(block *ir.Block, fn *ir.Func, args []value.Value) *ir.InstCall {
	callArgs := make([]value.Value, len(args))
	for i, a := range args {
		callArgs[i] = a
		if i < len(fn.Params) {
			var mirrored []ir.ParamAttribute
			for _, attr := range fn.Params[i].Attrs {
				switch attr.(type) {
				case ir.Byval, ir.SRet, ir.Align:
					mirrored = append(mirrored, attr)
				default:
					if attr == enum.ParamAttrZeroExt || attr == enum.ParamAttrSignExt {
						mirrored = append(mirrored, attr)
					}
				}
			}
			if len(mirrored) > 0 {
				callArgs[i] = ir.NewArg(a, mirrored...)
			}
		}
	}
	call := block.NewCall(fn, callArgs...)
	for _, attr := range fn.ReturnAttrs {
		if attr == enum.ReturnAttrZeroExt || attr == enum.ReturnAttrSignExt {
			call.ReturnAttrs = append(call.ReturnAttrs, attr)
		}
	}
	return call
}

// declareExtern declares one foreign function, or returns the one already declared under
// that symbol.
//
// The same C function may be declared by two modules — each `extern` is private to the
// module that writes it, but the symbol they name is not. So the second declaration is
// not a redefinition to refuse: it is the same function, and refusing it would make a
// library's own use of `strlen` collide with a program's. What *is* refused is two
// declarations that disagree about the signature, because only one of them can describe
// the function that will be linked, and emitting either silently picks a winner.
func (l *lowerer) declareExtern(ext *ast.ExternDeclStmt) (*ir.Func, error) {
	// **Keyed by the C symbol, not the Lyra name.** `@symbol("SDL_PollEvent")` makes the
	// two differ, and it is the symbol that shares a namespace with every other
	// declaration in the module — two Lyra names for one symbol are the same function
	// and must collapse to one `declare`, while two symbols under one Lyra name cannot
	// happen (a name declares once per module).
	symbol := ext.CSymbol()
	if prior, ok := l.externs[symbol]; ok {
		if !types.TypesEqual(prior.signature, ext.Signature) {
			return nil, fmt.Errorf("llvm: the C symbol %q is declared twice with different signatures, "+
				"%s at %s and %s at %s — one C symbol cannot have both",
				symbol, prior.signature, describeLocation(prior.at), ext.Signature,
				describeLocation(ext.NameLocation))
		}
		return prior.fn, nil
	}
	// **An aggregate crossing by value takes its own declaration path.** `declareFunctionAs`
	// is shared with Lyra's own functions, whose calling convention is Lyra's and must not
	// move; a planned signature is built from the ABI classifier instead. A foreign
	// signature with no aggregate in it gets no plan and nothing here changes.
	plan, err := l.planExtern(ext)
	if err != nil {
		return nil, err
	}
	var declared *ir.Func
	if plan != nil {
		declared = l.declareExternWithPlan(symbol, plan)
	} else {
		restore := l.pushExternSignature()
		declared, err = l.declareFunctionAs(symbol, ext.Func())
		restore()
		if err != nil {
			return nil, err
		}
	}
	l.markNarrowCrossings(declared, ext, plan)
	// **Variadic-ness is on the emitted signature, not on the call.** LLVM renders a
	// variadic declaration as `declare i32 @printf(ptr, ...)` and requires every call to
	// it to name that signature explicitly — `call i32 (ptr, ...) @printf(…)` — which llir
	// does off `Sig.Variadic` alone. Setting it here is therefore the whole of the
	// backend's part: the call path needs no case for it, and cannot get it wrong for a
	// symbol declared through this function.
	//
	// Without it the call is emitted at fixed arity, which links and is silently wrong on
	// every target whose variadic convention differs from its ordinary one — Apple aarch64
	// puts variadic arguments on the stack while the fixed convention puts them in
	// registers, so the callee reads whatever the stack happened to hold.
	//
	// **It is stamped before the comparison below, not after** (09/18): `...` is part of
	// the signature the libc table is compared against, and a compiler-declared `ioctl` or
	// `snprintf` is variadic too, so stamping afterwards made a correct variadic extern
	// read as a conflict whenever the runtime had declared the symbol first.
	if ext.Signature != nil && ext.Signature.IsVariadic {
		declared.Sig.Variadic = true
	}
	// The compiler declares libc functions of its own — `write` for `print`, `memcpy`,
	// `realloc` — and a C symbol has one declaration per module, so an extern naming one
	// of them shares that declaration rather than emitting a second. Here there *is* an
	// error to return, so a disagreement is named rather than deferred (declareLibc's
	// side of the same rule has to record it instead).
	if prior, ok := l.libc[symbol]; ok {
		if !lltypes.Equal(prior.Sig, declared.Sig) {
			return nil, fmt.Errorf(
				"llvm: `extern %s` declares the C symbol %q as %s, and the compiler uses it as %s — "+
					"one symbol cannot have both signatures. Rename the extern, or declare it to match",
				ext.Name, symbol, declared.Sig, prior.Sig)
		}
		l.module.Funcs = removeFunc(l.module.Funcs, declared)
		declared = prior
	}
	l.externs[symbol] = externDecl{fn: declared, signature: ext.Signature, at: ext.NameLocation}
	if plan != nil {
		if l.externPlans == nil {
			l.externPlans = map[*ir.Func]*externPlan{}
		}
		l.externPlans[declared] = plan
	}
	return declared, nil
}

// externDecl is a declared foreign function together with what it was declared *as*, so a
// second declaration of the same symbol can be compared against the first and the
// diagnostic can name where that first one is.
type externDecl struct {
	fn        *ir.Func
	signature *types.LambdaType
	at        ast.Location
}

// describeLocation is `file:line:col`, falling back to `line:col` for a program with no
// file (a snippet compiled from a test or an editor buffer). Two declarations of one
// extern are usually in two *files*, so a bare line:col names both of them the same way.
func describeLocation(loc ast.Location) string {
	if loc.File != "" {
		return fmt.Sprintf("%s:%s", loc.File, loc.Pretty())
	}
	return loc.Pretty()
}

// removeFunc drops one declaration from the module, for the case where it turned out to
// name a symbol already declared. Emitting it and taking it back is what keeps
// declareFunctionAs the one place a signature is lowered — the alternative is a second
// path that lowers a signature only to compare it, which is the drift hazard 8 is about.
func removeFunc(funcs []*ir.Func, drop *ir.Func) []*ir.Func {
	for i, fn := range funcs {
		if fn == drop {
			return append(funcs[:i], funcs[i+1:]...)
		}
	}
	return funcs
}
