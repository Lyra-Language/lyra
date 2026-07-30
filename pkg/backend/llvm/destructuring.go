package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The three destructuring *statements* — all one mechanism, differing only in what
// happens when the pattern doesn't match:
//
//	let (a, b) = pair                        // irrefutable: no failure path exists
//	if let Some(v) = m { … } else { … }       // conditional: bind in the then-branch
//	let Some(v) = m else { return 0 }         // refutable-with-exit: else must diverge
//
// All three reuse the pattern machinery `match` is built on (aggPatternTest /
// aggPatternBind), so a pattern means exactly the same thing in a match arm and in
// an `if let`. That is the point of routing them through one helper rather than
// re-deriving destructuring here: two implementations of "does this value match
// this pattern" would drift.
//
// A destructuring statement has no value, which makes these simpler than a match:
// there is no merge phi to feed, only control flow to join.

// patternMatcher returns the test/bind pair for one pattern against `val` (of Lyra
// type valType) — the same closures the match ladders are driven by.
//
// A `shared` aggregate is a pointer to its ref-counted box, so it is unboxed first:
// the pattern machinery works on the inline value. Reading through the box consumes
// no reference, so there is no ownership action here — the box's own release is the
// ordinary last-use drop of whatever binding held it.
func (l *lowerer) patternMatcher(block *ir.Block, val value.Value, valType types.Type) (
	func(*ir.Block, ast.Pattern) (value.Value, error),
	func(*ir.Block, ast.Pattern) error,
	error,
) {
	if _, ok := valType.(types.DynamicArrayType); ok {
		// An array pattern is a length test plus per-element tests, which the array
		// match driver expresses over its own block structure (match_array.go). It
		// does not fit the single test/bind pair, so it is a loud error rather than a
		// half-working path.
		return nil, nil, fmt.Errorf("llvm: destructuring an array with a pattern is not implemented yet — use `match`")
	}
	scrut, err := l.unboxSharedData(block, val)
	if err != nil {
		return nil, nil, err
	}
	test := func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
		return l.aggPatternTest(b, scrut, pat, valType)
	}
	bind := func(b *ir.Block, pat ast.Pattern) error {
		return l.aggPatternBind(b, scrut, pat, valType)
	}
	return test, bind, nil
}

// destructuringValue lowers the value being destructured and returns it with its
// recorded Lyra type.
func (l *lowerer) destructuringValue(block *ir.Block, d *ast.DestructuringDeclStmt) (value.Value, types.Type, *ir.Block, error) {
	if d.Value == nil {
		return nil, nil, nil, fmt.Errorf("llvm: destructuring declaration has no value")
	}
	valType, ok := l.recordedType(d.Value)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: no type recorded for a destructured value")
	}
	val, end, err := l.lowerExpr(block, d.Value)
	if err != nil {
		return nil, nil, nil, err
	}
	return val, valType, end, nil
}

// lowerDestructuringDecl lowers `let (a, b) = pair` — the irrefutable form.
//
// It binds and nothing else: the pattern must match every value of its type, so
// there is no failure path to generate. A pattern that *does* impose a test
// (`let Some(v) = m`, which can fail) is rejected here rather than silently
// binding on a path where the match may not hold — the `let … else` form is how
// that is written.
func (l *lowerer) lowerDestructuringDecl(block *ir.Block, d *ast.DestructuringDeclStmt) (*ir.Block, error) {
	val, valType, block, err := l.destructuringValue(block, d)
	if err != nil {
		return nil, err
	}
	test, bind, err := l.patternMatcher(block, val, valType)
	if err != nil {
		return nil, err
	}
	cond, err := test(block, d.Pattern)
	if err != nil {
		return nil, err
	}
	if cond != nil {
		return nil, fmt.Errorf("llvm: `let %s = …` can fail to match, so it needs an `else` branch (`let … = … else { … }`)", d.Pattern.GetName())
	}
	if err := bind(block, d.Pattern); err != nil {
		return nil, err
	}
	return block, nil
}

// lowerIfDestructuring lowers `if let pat = v { Then } else { Else }`: test the
// pattern, bind its names on the taken path only, and join.
//
// The bindings are made in the `then` block, which is what scopes them to that
// branch — matching the collector, which pushes a scope around `Then` alone, and
// the backend's own per-block local scoping.
//
// An irrefutable pattern (`if let (a, b) = pair`) has no condition, so the then
// branch is unconditional and the else branch is unreachable. It is still emitted
// (and immediately joined) rather than special-cased: the typechecker warns about
// the pointless form, and generating it keeps this path free of a shape that only
// occurs in odd code.
func (l *lowerer) lowerIfDestructuring(block *ir.Block, s *ast.IfDestructuringStmt) (*ir.Block, error) {
	d := &s.DestructuringStatement
	// A `weak` scrutinee is not destructured but *upgraded*: the question is whether
	// the referent is still alive, not whether it matches a shape. See weak.go.
	if d.Value != nil {
		if t, ok := l.recordedType(d.Value); ok {
			if wt, isWeak := l.stripNewtype(t).(types.WeakType); isWeak {
				return l.lowerWeakUpgrade(block, s, wt)
			}
		}
	}
	val, valType, block, err := l.destructuringValue(block, d)
	if err != nil {
		return nil, err
	}
	test, bind, err := l.patternMatcher(block, val, valType)
	if err != nil {
		return nil, err
	}
	cond, err := test(block, d.Pattern)
	if err != nil {
		return nil, err
	}

	fn := block.Parent
	thenBlock := fn.NewBlock("")
	merge := fn.NewBlock("")
	elseBlock := merge
	if s.Else != nil {
		elseBlock = fn.NewBlock("")
	}
	if cond != nil {
		block.NewCondBr(cond, thenBlock, elseBlock)
	} else {
		block.NewBr(thenBlock)
	}

	// Bindings live in the then-branch: they are only valid where the match held.
	// Hence the branch-scoped local scope — a pattern name must not outlive the
	// branch, and `if let Some(x) = m { … }` where an outer `x` exists otherwise
	// leaves the *pattern's* slot installed under that name for the code after the
	// statement, which then reads the matched payload instead of its own binding.
	// Silently, and with a wrong value rather than an error, since the collector
	// scopes the name to `Then` and only codegen disagreed — the same shape as the
	// match-arm shadowing bug (see pushLocalScope).
	restoreLocals := l.pushLocalScope()
	if err := bind(thenBlock, d.Pattern); err != nil {
		restoreLocals()
		return nil, err
	}
	thenEnd, err := l.lowerForEffect(thenBlock, s.Then)
	restoreLocals()
	if err != nil {
		return nil, err
	}
	if thenEnd.Term == nil {
		thenEnd.NewBr(merge)
	}

	if s.Else != nil {
		elseEnd, err := l.lowerForEffect(elseBlock, s.Else)
		if err != nil {
			return nil, err
		}
		if elseEnd.Term == nil {
			elseEnd.NewBr(merge)
		}
	}
	return merge, nil
}

// lowerElseDestructuring lowers `let pat = v else { Else }`: on a match the names
// bind and control continues; otherwise the else block runs, and it must diverge.
//
// Unlike `if let`, the bindings belong to the *enclosing* scope — they are visible
// after the statement, which is the whole point of the form — so they are bound
// into the continuation block, not a nested one. That is sound precisely because
// Else cannot fall through: the typechecker requires it to diverge (return/break/
// continue), so the only path reaching the continuation is the one where the
// pattern matched. A diverging Else seals its own block, so no join is emitted for
// it; a fall-through would be a use of unbound names, and is a loud error here
// rather than a jump into code that reads them.
func (l *lowerer) lowerElseDestructuring(block *ir.Block, s *ast.ElseDestructuringStmt) (*ir.Block, error) {
	d := &s.DestructuringStatement
	val, valType, block, err := l.destructuringValue(block, d)
	if err != nil {
		return nil, err
	}
	test, bind, err := l.patternMatcher(block, val, valType)
	if err != nil {
		return nil, err
	}
	cond, err := test(block, d.Pattern)
	if err != nil {
		return nil, err
	}

	fn := block.Parent
	cont := fn.NewBlock("")
	if cond == nil {
		// An irrefutable pattern never takes the else path; bind and continue.
		block.NewBr(cont)
	} else {
		elseBlock := fn.NewBlock("")
		block.NewCondBr(cond, cont, elseBlock)
		elseEnd, err := l.lowerForEffect(elseBlock, s.Else)
		if err != nil {
			return nil, err
		}
		if elseEnd.Term == nil {
			return nil, fmt.Errorf("llvm: the `else` branch of a `let … else` must diverge (return, break, or continue)")
		}
	}
	if err := bind(cont, d.Pattern); err != nil {
		return nil, err
	}
	return cont, nil
}
