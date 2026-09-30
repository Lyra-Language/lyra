package checker

import (
	"fmt"
	"sort"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/target"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckTarget reports what the program's own code does that cannot work on tgt — the
// console a `lyra.toml` names (see package target):
//
//   - lyra-E084: a call that reaches outside Lyra (EffectHost — an `extern`, or a builtin
//     such as `println` that the backend lowers to libc) where there is no operating
//     system;
//   - lyra-E085: a heap allocation (EffectAlloc) where there is no heap — at the form
//     that allocates, or at the call to a function that does;
//   - lyra-W026: arithmetic on a type the CPU can only emulate (`i64`, `f64` on a 68000),
//     once per function and type.
//
// **It reads the purity pass's one walk** (bodyEffects, through callable.onCharge) rather
// than walking bodies itself: which call resolves to what, and which form allocates, are
// that walk's questions, and a second copy of the ladder is the drift rule 8 warns of.
// Each site reports its *own* charge — a callback argument's effect is reported inside
// the callback, not again at the call it is passed to.
//
// inScope says which files are the program's own: the standard library and the
// bindings are not reported (a prelude function allocating inside is reported where the
// game calls it), since their authors did not write them for this target.
func CheckTarget(program *ast.Program, symTable *symbols.SymbolTable, scopeTable *symbols.ScopeTable, typeTable *typetable.TypeTable, methodTable *typetable.MethodTable, caps *captures.Table, tgt target.Target, inScope func(file string) bool) []diag.Diagnostic {
	if tgt.Host && tgt.Heap && len(tgt.Emulated) == 0 {
		return nil
	}
	inf, base, defs := runInference(program, symTable, scopeTable, typeTable, methodTable, caps)
	var diags []diag.Diagnostic
	reported := map[ast.Location]bool{}
	sink := func(site ast.Expression, effect Effect, callee string) {
		loc := site.GetLocation()
		if reported[loc] {
			return
		}
		switch {
		case !tgt.Host && effect.Has(EffectHost):
			reported[loc] = true
			diags = append(diags, diag.Diagnostic{
				Location: loc,
				Severity: diag.SeverityError,
				Code:     diag.CodeTargetHost,
				Message: fmt.Sprintf("%s needs an operating system — it reaches code outside Lyra — and %s has none",
					describeCallee(callee), tgt.Display),
			})
		case !tgt.Heap && effect.Has(EffectAlloc):
			reported[loc] = true
			what := describeAllocation(site)
			if callee != "" {
				what = fmt.Sprintf("%s allocates", describeCallee(callee))
			}
			diags = append(diags, diag.Diagnostic{
				Location: loc,
				Severity: diag.SeverityError,
				Code:     diag.CodeTargetAlloc,
				Message: fmt.Sprintf("%s, and %s has no heap; a fixed array (`#[…]`), a plain struct and a string literal need none",
					what, tgt.Display),
			})
		}
	}
	for lam, capture := range defs {
		if lam.IsExtern || !inScope(lam.GetLocation().File) {
			continue
		}
		cb := lambdaCallable(lam, capture, inf)
		cb.onCharge = sink
		bodyEffects(cb, inf)
		diags = append(diags, emulatedArithmetic(lam, typeTable, tgt)...)
	}
	for _, m := range collectMethodImpls(program) {
		if !inScope(m.Clause.Body.GetLocation().File) {
			continue
		}
		cb := methodCallable(m, base, inf)
		cb.onCharge = sink
		bodyEffects(cb, inf)
	}
	// defs is a map: sort, so the same program reports in the same order every time.
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i].Location, diags[j].Location
		if a.File != b.File {
			return a.File < b.File
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartCol < b.StartCol
	})
	return diags
}

// describeCallee names a callee for a target diagnostic: `println`, or the method name.
func describeCallee(name string) string {
	if name == "" {
		return "this call"
	}
	return fmt.Sprintf("`%s`", name)
}

// emulatedArithmetic warns once per type for arithmetic in lam's own body (not its nested
// lambdas, which are visited as lambdas of their own) on a type tgt's CPU emulates.
func emulatedArithmetic(lam *ast.LambdaExpr, typeTable *typetable.TypeTable, tgt target.Target) []diag.Diagnostic {
	if len(tgt.Emulated) == 0 || typeTable == nil {
		return nil
	}
	var diags []diag.Diagnostic
	warned := map[string]bool{}
	walkLambdaBodies(lam, nil, func(e ast.Expression) bool {
		if _, nested := e.(*ast.LambdaExpr); nested {
			return false
		}
		op, isMath := e.(*ast.MathBinaryOpExpr)
		if !isMath {
			return true
		}
		t, ok := typeTable.Get(op)
		if !ok {
			return true
		}
		prim, isPrim := types.StripNewtype(t).(types.PrimitiveType)
		if !isPrim {
			return true
		}
		name := string(prim.Name)
		if !tgt.Emulates(name) || warned[name] {
			return true
		}
		warned[name] = true
		diags = append(diags, diag.Diagnostic{
			Location: op.GetLocation(),
			Severity: diag.SeverityWarning,
			Code:     diag.CodeTargetEmulated,
			Message:  emulatedMessage(name, tgt),
		})
		return true
	})
	return diags
}

// emulatedMessage says why arithmetic on `name` is slow on tgt, and what to use instead.
func emulatedMessage(name string, tgt target.Target) string {
	switch name {
	case "f32", "f64":
		return fmt.Sprintf("the %s has no floating point: each `%s` operation here is a library call; use integers, or fixed point for fractions",
			tgt.CPU, name)
	}
	return fmt.Sprintf("`%s` arithmetic is emulated on the %s — several instructions or a call for each operation; use `i32`, or `i16` where the values fit (a loop variable over literals is `%s` unless typed: `for i: i16 in …`)",
		name, tgt.CPU, name)
}
