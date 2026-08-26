package llvm

import (
	"fmt"
	"slices"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerStructMatch lowers a `match` on a struct value via the shared aggregate
// ladder: a struct pattern `{ x, y }` binds fields (structPatternTest returns nil
// → unconditional), while a literal field sub-pattern (`{ x: 0, y }`) makes it
// conditional on that field.
func (l *lowerer) lowerStructMatch(block *ir.Block, e *ast.MatchExpr, st types.NamedStructType) (value.Value, *ir.Block, error) {
	whole, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	scrut, err := l.unboxSharedData(block, whole) // a `shared` struct is a box pointer
	if err != nil {
		return nil, nil, err
	}
	if _, ok := scrut.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: struct match scrutinee did not lower to a struct (%s)", scrut.Type())
	}
	return l.lowerMatchLadder(block, e, whole,
		func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
			return l.aggPatternTest(b, scrut, pat, st)
		},
		func(b *ir.Block, pat ast.Pattern) error {
			return l.aggPatternBind(b, scrut, pat, st)
		})

}

// lowerMatchLadder lowers a `match` as an if-else ladder driven by the `test` and
// `bind` closures. It is the one ladder in the backend: a struct or tuple scrutinee
// (single shape, no variant tag), a `data` scrutinee whose payload value test rules
// out the tag `switch` (its closures encode the tag check in `test`), and a **scalar**
// — bool, integer, float or string — whose `test` is a comparison and whose `bind`
// does nothing, since the only name a scalar arm introduces is a catch-all identifier
// this driver binds itself.
//
// A `_`/identifier arm is an unconditional catch-all (an identifier binds the whole
// value). Every other arm calls `test` for its condition in the current block (nil →
// the pattern always matches) and, on the taken path, `bind` for its sub-pattern
// bindings; the scrutinee-specific pattern shape lives entirely in those two closures.
// An `if` guard (any arm, catch-all included) is a further test after binding — when it
// fails, control falls through to the next arm, so a guarded arm never seals the ladder
// (lowerGuardedArmBody). Arms feed a merge phi, so the match is a value like `if`; the
// unmatched fall-through is sealed by sealMatchFallthrough.
//
// `whole` is the value an identifier catch-all binds to (the whole scrutinee) — the
// inline aggregate for a `stack` value, but the *box pointer* for a `shared` scrutinee,
// whose bound name has the box-pointer type, not the unboxed union. The per-arm pattern
// shape (which references the unboxed value) lives in the `test` and `bind` closures,
// so this driver only needs `whole`.
//
// Scalar matches had a second copy of all of this until 08/05 — merge block, incoming
// and phi bookkeeping, per-arm scope reset, catch-all handling, seal — differing only
// in the two closures. Array matches still have their own (match_array.go): their
// pattern test spans several blocks rather than yielding one condition in the current
// block, which is a different shape, not a different filling.
func (l *lowerer) lowerMatchLadder(
	block *ir.Block, e *ast.MatchExpr, whole value.Value,
	test func(b *ir.Block, pat ast.Pattern) (value.Value, error),
	bind func(b *ir.Block, pat ast.Pattern) error,
) (value.Value, *ir.Block, error) {
	fn := block.Parent
	merge := newMatchMerge(fn)

	lowerArmInto := func(b *ir.Block, body ast.Expression) error {
		// lowerBranchValue, not lowerExpr: an arm body is value-*optional*, exactly as
		// an `if` branch is. A block body whose last statement is an assignment has no
		// value, and requiring one made a `match` used as a statement fail to lower.
		val, end, err := l.lowerBranchValue(b, body)
		if err != nil {
			return err
		}
		merge.arm(val, end)
		return nil
	}

	current := block
	sealed := false
	// Per-arm binding scope — see lowerScalarMatch.
	armScope := l.pushLocalScope()
	defer armScope()
	for _, arm := range e.MatchArms {
		armScope()
		if names, isCatchAll := matchCatchAll(arm.Pattern); isCatchAll {
			if len(names) > 0 { // a catch-all that binds: `x => …`, `all @ _ => …`
				slot := fn.Blocks[0].NewAlloca(whole.Type())
				current.NewStore(whole, slot)
				for _, name := range names {
					l.locals[name] = slot
				}
			}
			if arm.Guard == nil {
				if err := lowerArmInto(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
				break
			}
			// A guarded catch-all may fail, so it doesn't seal the ladder.
			next := fn.NewBlock("")
			if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerArmInto); err != nil {
				return nil, nil, err
			}
			current = next
			continue
		}
		cond, err := test(current, arm.Pattern)
		if err != nil {
			return nil, nil, err
		}
		if cond == nil { // no literal sub-pattern → the arm's pattern always matches
			if err := bind(current, arm.Pattern); err != nil {
				return nil, nil, err
			}
			if arm.Guard == nil {
				if err := lowerArmInto(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
				break
			}
			// The pattern matched unconditionally, but a guard can still fail — bind
			// its variables (done above) then test the guard, falling to `next` if false.
			next := fn.NewBlock("")
			if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerArmInto); err != nil {
				return nil, nil, err
			}
			current = next
			continue
		}
		bodyBlock := fn.NewBlock("")
		nextBlock := fn.NewBlock("")
		current.NewCondBr(cond, bodyBlock, nextBlock)
		if err := bind(bodyBlock, arm.Pattern); err != nil {
			return nil, nil, err
		}
		if err := l.lowerGuardedArmBody(bodyBlock, arm.Guard, arm.Body, nextBlock, lowerArmInto); err != nil {
			return nil, nil, err
		}
		current = nextBlock
	}
	if !sealed {
		l.sealMatchFallthrough(current)
	}
	val, end := merge.value()
	return val, end, nil
}

// matchCatchAll reports whether a pattern is an unconditional catch-all (a
// wildcard or an identifier). The returned *IdentifierPattern is non-nil only for
// a real binding identifier (name != "_") — the name the whole scrutinee value
// binds to — and nil for a wildcard or `_`.
func matchCatchAll(pat ast.Pattern) ([]string, bool) {
	// **Peeled, and the names collected on the way down.** `all @ _` catches everything `_`
	// does and binds `all` besides, so a wrapper neither disqualifies an arm from being a
	// catch-all nor is free to be dropped. Returning names rather than the identifier node
	// is what lets both come back: the name a binding pattern introduces lives on the
	// wrapper, not on a sub-pattern there is one of.
	var names []string
	for {
		bp, isBinding := pat.(*ast.BindingPattern)
		if !isBinding {
			break
		}
		if bp.Name != "_" {
			names = append(names, bp.Name)
		}
		pat = bp.Pattern
	}
	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return names, true
	case *ast.IdentifierPattern:
		if p.Name != "_" {
			names = append(names, p.Name)
		}
		return names, true
	}
	return nil, false
}

// aggPatternTest builds the i1 condition that a first-class struct/tuple value
// `val` (of Lyra type valType) matches `pat` — the AND of a scalar comparison per
// literal/range sub-pattern, recursing into nested struct/tuple sub-patterns via
// `extractvalue` (safe on a single-shape aggregate, no tag/branch needed). Returns
// nil when the pattern imposes no test (all identifier/wildcard/shorthand
// bindings). A nested `data` sub-pattern contributes its tag check plus a test for
// each value-testing payload field (`Some(0)`), computed branchlessly and ANDed.
func (l *lowerer) aggPatternTest(block *ir.Block, val value.Value, pat ast.Pattern, valType types.Type) (value.Value, error) {
	switch p := pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		return nil, nil // binding leaves impose no test
	case *ast.LiteralPattern, *ast.RangePattern:
		prim, ok := valType.(types.PrimitiveType)
		if !ok {
			return nil, fmt.Errorf("llvm: literal pattern on non-scalar value of type %s", valType)
		}
		return l.scalarMatchTest(block, val, pat, prim.Name == types.Boolean, IsSignedInt(prim.Name))
	case *ast.StructPattern:
		st, ok := l.resolveStructType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: struct pattern on non-struct value of type %s", valType)
		}
		var cond value.Value
		for _, f := range p.Fields {
			if isBindingLeaf(f.Pattern) {
				continue
			}
			idx, ftype, ok := structFieldIndexAndType(st, f.Name)
			if !ok {
				return nil, fmt.Errorf("llvm: struct %s has no field %q", st.Name, f.Name)
			}
			c, err := l.aggPatternTest(block, block.NewExtractValue(val, uint64(idx)), f.Pattern, ftype)
			if err != nil {
				return nil, err
			}
			cond = andConds(block, cond, c)
		}
		return cond, nil
	case *ast.TuplePattern:
		tt, ok := l.resolveTupleType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: tuple pattern on non-tuple value of type %s", valType)
		}
		var cond value.Value
		for i, el := range p.Elements {
			if isBindingLeaf(el) {
				continue
			}
			if i >= len(tt.Elements) {
				return nil, fmt.Errorf("llvm: tuple pattern element %d out of range", i)
			}
			c, err := l.aggPatternTest(block, block.NewExtractValue(val, uint64(i)), el, tt.Elements[i])
			if err != nil {
				return nil, err
			}
			cond = andConds(block, cond, c)
		}
		return cond, nil
	case *ast.DataPattern:
		// A `data` sub-pattern's test is its tag check (`extractvalue`-the-tag ==
		// the variant index), ANDed with a value test for each payload field that
		// imposes one (`Some(0)`, or a nested data pattern). The payload test is
		// computed unconditionally and ANDed after the tag check: when the tag
		// doesn't match, the payload blob reinterpreted as this variant is
		// meaningless, but the tag comparison has already forced the whole
		// condition false — so reading those bits is harmless (they stay within the
		// union's own stack blob, sized to the largest variant).
		dt, ok := l.resolveDataType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: data pattern on non-data value of type %s", valType)
		}
		ctor, idx, ok := findConstructor(dt, p.Name)
		if !ok {
			return nil, fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
		}
		unionSt, ok := val.Type().(*lltypes.StructType)
		if !ok {
			return nil, fmt.Errorf("llvm: data value did not lower to a struct (%s)", val.Type())
		}
		tagTy := unionSt.Fields[0].(*lltypes.IntType)
		tag := block.NewExtractValue(val, 0)
		cond := value.Value(block.NewICmp(enum.IPredEQ, tag, constant.NewInt(tagTy, int64(idx))))

		fieldPatterns, err := payloadFieldPatterns(p, ctor)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(fieldPatterns, patternHasTest) {
			payload, err := l.extractDataPayload(block, val, ctor)
			if err != nil {
				return nil, err
			}
			fieldTypes := ctor.FieldTypes()
			for i, fp := range fieldPatterns {
				c, err := l.aggPatternTest(block, block.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i])
				if err != nil {
					return nil, err
				}
				cond = andConds(block, cond, c)
			}
		}
		return cond, nil
	case *ast.BindingPattern:
		// The name binds unconditionally, so the test is entirely the inner pattern's.
		return l.aggPatternTest(block, val, p.Pattern, valType)
	default:
		return nil, fmt.Errorf("llvm: match sub-pattern %T not implemented yet", pat)
	}
}

// aggPatternBind binds the identifier leaves of a struct/tuple pattern into
// l.locals for the arm body, recursing into nested struct/tuple sub-patterns via
// `extractvalue`. The alloca type is the extracted value's own LLVM type, so nested
// aggregate fields bind correctly. Literal/range/wildcard sub-patterns bind
// nothing; a nested `data` sub-pattern is deferred.
func (l *lowerer) aggPatternBind(block *ir.Block, val value.Value, pat ast.Pattern, valType types.Type) error {
	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name != "_" {
			l.bindValue(block, p.Name, val)
		}
		return nil
	case *ast.BindingPattern:
		// `name @ pattern` binds the name to the **whole** value at this position and then
		// binds whatever the inner pattern does — the same shape the typechecker gives it.
		if p.Name != "_" {
			l.bindValue(block, p.Name, val)
		}
		return l.aggPatternBind(block, val, p.Pattern, valType)
	case nil, *ast.WildcardPattern, *ast.LiteralPattern, *ast.RangePattern:
		return nil // no binding
	case *ast.StructPattern:
		st, ok := l.resolveStructType(valType)
		if !ok {
			return fmt.Errorf("llvm: struct pattern on non-struct value of type %s", valType)
		}
		for _, f := range p.Fields {
			idx, ftype, ok := structFieldIndexAndType(st, f.Name)
			if !ok {
				return fmt.Errorf("llvm: struct %s has no field %q", st.Name, f.Name)
			}
			fieldVal := block.NewExtractValue(val, uint64(idx))
			if f.Pattern == nil { // shorthand `{ x }` binds x
				l.bindValue(block, f.Name, fieldVal)
				continue
			}
			if err := l.aggPatternBind(block, fieldVal, f.Pattern, ftype); err != nil {
				return err
			}
		}
		return nil
	case *ast.TuplePattern:
		tt, ok := l.resolveTupleType(valType)
		if !ok {
			return fmt.Errorf("llvm: tuple pattern on non-tuple value of type %s", valType)
		}
		for i, el := range p.Elements {
			if i >= len(tt.Elements) {
				return fmt.Errorf("llvm: tuple pattern element %d out of range", i)
			}
			if err := l.aggPatternBind(block, block.NewExtractValue(val, uint64(i)), el, tt.Elements[i]); err != nil {
				return err
			}
		}
		return nil
	case *ast.DataPattern:
		// Reached only on the taken path (aggPatternTest's tag check already held),
		// so bind the payload: reinterpret it as the variant's payload struct and
		// recurse into each field's sub-pattern.
		dt, ok := l.resolveDataType(valType)
		if !ok {
			return fmt.Errorf("llvm: data pattern on non-data value of type %s", valType)
		}
		ctor, _, ok := findConstructor(dt, p.Name)
		if !ok {
			return fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
		}
		fieldPatterns, err := payloadFieldPatterns(p, ctor)
		if err != nil {
			return err
		}
		if len(fieldPatterns) == 0 {
			return nil // nullary variant
		}
		payload, err := l.extractDataPayload(block, val, ctor)
		if err != nil {
			return err
		}
		fieldTypes := ctor.FieldTypes()
		for i, fp := range fieldPatterns {
			if err := l.aggPatternBind(block, block.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i]); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("llvm: match sub-pattern %T binding not implemented yet", pat)
	}
}

// bindValue stores val into a fresh entry-block alloca and records it under name
// in l.locals, so the arm body reads the binding like any local.
func (l *lowerer) bindValue(block *ir.Block, name string, val value.Value) {
	slot := block.Parent.Blocks[0].NewAlloca(val.Type())
	block.NewStore(val, slot)
	l.locals[name] = slot
}

// andConds combines two optional i1 conditions (nil = "always true"), returning
// nil when both are nil, the other when one is nil, or their `and`.
func andConds(block *ir.Block, a, b value.Value) value.Value {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return block.NewAnd(a, b)
	}
}

// isBindingLeaf reports whether a sub-pattern only binds (or ignores) and imposes
// no test: a shorthand field (nil), a wildcard, or an identifier.
func isBindingLeaf(pat ast.Pattern) bool {
	switch pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		return true
	}
	return false
}

// lowerTupleMatch lowers a `match` on a tuple value via the shared aggregate
// ladder — the positional counterpart to lowerStructMatch: a tuple pattern
// `(a, b)` binds elements by position, and a literal element (`(0, b)`) makes the
// arm conditional on that position.
func (l *lowerer) lowerTupleMatch(block *ir.Block, e *ast.MatchExpr, tt types.TupleType) (value.Value, *ir.Block, error) {
	whole, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	scrut, err := l.unboxSharedData(block, whole) // a `shared` tuple is a box pointer
	if err != nil {
		return nil, nil, err
	}
	if _, ok := scrut.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: tuple match scrutinee did not lower to a struct (%s)", scrut.Type())
	}
	return l.lowerMatchLadder(block, e, whole,
		func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
			return l.aggPatternTest(b, scrut, pat, tt)
		},
		func(b *ir.Block, pat ast.Pattern) error {
			return l.aggPatternBind(b, scrut, pat, tt)
		})

}

// dataMatchHasPayloadTest reports whether any arm of a `data` match imposes a
// value test on a payload sub-pattern (`Some(0)`, `Some(Wrapped(0))`) — i.e. a
// test beyond the variant tag. Such a match can't lower to a single tag `switch`
// (two same-tag arms need distinct payload tests) and instead uses the if-else
// ladder. A non-data or unknown-constructor arm contributes no payload test here.
func dataMatchHasPayloadTest(e *ast.MatchExpr, dt types.DataType) bool {
	for _, arm := range e.MatchArms {
		// Unwrapped, or an `@` hides the test from the routing decision: `w @ Box(0)` would
		// take the compact tag switch, which cannot express "this tag *and* this value",
		// and the literal would then reach a path that refuses it. A binding contributes no
		// test of its own, so what an arm tests is always what is inside the wrapper.
		dp, ok := ast.UnwrapBinding(arm.Pattern).(*ast.DataPattern)
		if !ok {
			continue
		}
		ctor, _, ok := findConstructor(dt, dp.Name)
		if !ok {
			continue
		}
		fps, err := payloadFieldPatterns(dp, ctor)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(fps, patternHasTest) {
			return true
		}
	}
	return false
}

// lowerDataMatch lowers a `match` on a `data` value: store the scrutinee, load
// its tag, and `switch` on it to one block per arm (DATA_LAYOUT.md). Each data
// pattern's arm reinterprets the payload blob as its variant's payload struct and
// binds the fields; a wildcard/identifier arm is the switch default. The arms
// feed a merge phi, so the match is a value (like `if`). The front-end guarantees
// exhaustiveness (lyra-E009), so a match with no catch-all gets an `unreachable`
// default.
func (l *lowerer) lowerDataMatch(block *ir.Block, e *ast.MatchExpr, dt types.DataType) (value.Value, *ir.Block, error) {
	whole, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	// A `shared` data value is a pointer to its ref-counted box; load the inline
	// union out of it so the tag/payload logic below (and aggPattern*) sees a
	// first-class struct. `whole` stays the box pointer — the value an identifier
	// catch-all binds — while `scrut` is the unboxed union. (For an inline value
	// unboxSharedData is the identity, so whole == scrut.)
	scrut, err := l.unboxSharedData(block, whole)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data match scrutinee did not lower to a struct (%s)", scrut.Type())
	}

	// The compact tag `switch` below routes each tag to exactly one block, so it
	// can't express a match where two arms share a tag but differ — a value-testing
	// payload sub-pattern (`Some(0)` vs `Some(x)`), or a guard that may fail and
	// fall through to a following same-tag arm. Either case falls back to the
	// if-else ladder shared with struct/tuple matches, where each arm's condition is
	// the tag check ANDed with its payload tests (aggPatternTest) and then its
	// guard, first-match-wins preserved.
	if dataMatchHasPayloadTest(e, dt) || matchHasGuard(e) {
		return l.lowerMatchLadder(block, e, whole,
			func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
				return l.aggPatternTest(b, scrut, pat, dt)
			},
			func(b *ir.Block, pat ast.Pattern) error {
				return l.aggPatternBind(b, scrut, pat, dt)
			})

	}

	fn := block.Parent

	// Store the scrutinee so a variant's payload can be reinterpreted out of the
	// blob (the mirror of construction), and load its tag.
	slot := fn.Blocks[0].NewAlloca(unionTy)
	block.NewStore(scrut, slot)
	// The slot an identifier catch-all binds: the union slot for an inline value,
	// but a box-pointer slot for a `shared` scrutinee (so the bound name keeps the
	// box-pointer type). Built lazily only when an arm actually binds the whole value.
	wholeSlot := slot
	if whole != scrut && matchBindsWhole(e) {
		wholeSlot = fn.Blocks[0].NewAlloca(whole.Type())
		block.NewStore(whole, wholeSlot)
	}
	tagTy := unionTy.Fields[0].(*lltypes.IntType)
	tagPtr := block.NewGetElementPtr(unionTy, slot, i32c(0), i32c(0))
	tag := block.NewLoad(tagTy, tagPtr)

	// Perceus reuse (FBIP): if the scrutinee is an owned `shared data` binding at its
	// last use (the ownership pass decided), reclaim its box via `lyra_rc_drop_reuse`
	// — the box when unique, else null — and hand that token to the arms. A reuse
	// target arm writes the new value into the reclaimed box (no allocation); any
	// other arm frees the token. Retiring the scrutinee's slot suppresses its ordinary
	// last-use drop / frame release, since the token now owns the box's disposal.
	var reuseToken value.Value
	if reuseName, ok := l.ownership().ReuseScrutinee(e); ok {
		l.ensureRCRuntime()
		reuseToken = block.NewCall(l.rcDropReuse, block.NewBitCast(whole, lltypes.NewPointer(lltypes.I8)))
		if slot, found := l.locals[reuseName]; found {
			l.retireManagedSlot(slot)
		}
	}

	merge := newMatchMerge(fn)
	var cases []*ir.Case
	var defaultBlock *ir.Block

	// Per-arm binding scope — see lowerScalarMatch.
	armScope := l.pushLocalScope()
	defer armScope()
	// A tag already claimed by an earlier arm. Reaching this function at all means every
	// arm is a bind-only `DataPattern` with no guard (line ~484 routes a payload test or a
	// guard to the ladder instead), so an arm repeating a constructor is **unreachable** —
	// the earlier one matches that tag unconditionally. First-match-wins is what `match`
	// already means, so dropping the later arm is the semantics rather than a choice.
	//
	// Emitting it produced two `i8 0` cases in one `switch`, which llir builds happily and
	// clang refuses: *"duplicate case value in switch"*. That is a compile failure pointing
	// at generated IR, on a program `lyrac check` passed clean — the front end does not yet
	// report an unreachable match arm (see todo.md), so nothing upstream refuses it either.
	unreachableArms := unreachableDataArms(e.MatchArms)
	for armIdx, arm := range e.MatchArms {
		if unreachableArms[armIdx] {
			continue
		}
		armScope()
		armBlock := fn.NewBlock("")
		// **`name @ pattern` binds the whole scrutinee and then matches what is inside.**
		// Peeled here rather than given a case of its own, because the wrapper contributes
		// a binding and no test: after peeling, the arm dispatches exactly as the pattern
		// it wraps. Written as a loop since `a @ b @ p` parses.
		armPattern := arm.Pattern
		for {
			bp, isBinding := armPattern.(*ast.BindingPattern)
			if !isBinding {
				break
			}
			if bp.Name != "_" {
				l.locals[bp.Name] = wholeSlot
			}
			armPattern = bp.Pattern
		}
		switch p := armPattern.(type) {
		case *ast.DataPattern:
			idx := -1
			for i, c := range dt.Constructors {
				if c.Name == p.Name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return nil, nil, fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
			}
			if err := l.bindDataPayload(armBlock, p, dt.Constructors[idx], slot, unionTy); err != nil {
				return nil, nil, err
			}
			cases = append(cases, ir.NewCase(constant.NewInt(tagTy, int64(idx)), armBlock))
		case *ast.WildcardPattern:
			defaultBlock = armBlock
		case *ast.IdentifierPattern:
			if p.Name != "_" {
				// Bind the whole scrutinee value (the box pointer for a `shared` value).
				l.locals[p.Name] = wholeSlot
			}
			defaultBlock = armBlock
		default:
			return nil, nil, fmt.Errorf("llvm: match pattern %T not implemented for a data scrutinee", arm.Pattern)
		}

		// Make the reuse token available to this arm's reuse-target construction (if
		// any). Reset per arm — arms are alternative paths, so each independently
		// consumes the token (a construction writes into the reclaimed box) or, failing
		// to consume it, frees it below.
		l.reuseToken = reuseToken
		val, end, err := l.lowerBranchValue(armBlock, arm.Body)
		if err != nil {
			return nil, nil, err
		}
		if reuseToken != nil && l.reuseToken != nil && end.Term == nil {
			// This arm didn't reuse the reclaimed box → free it (`free(NULL)` is a valid
			// no-op, so this is safe whether the token is a box or null).
			end.NewCall(l.free, reuseToken)
		}
		l.reuseToken = nil
		merge.arm(val, end)
	}

	if defaultBlock == nil {
		// Exhaustive over the constructors (lyra-E009 is a hard error for `data`), so
		// this default is unreachable in a well-typed program — trap rather than
		// `unreachable` anyway, so a gap in that enforcement aborts cleanly instead of
		// running off into undefined behavior.
		defaultBlock = fn.NewBlock("")
		l.sealMatchFallthrough(defaultBlock)
	}
	block.NewSwitch(tag, defaultBlock, cases...)

	phi, mergeBlock := merge.value()
	if phi == nil {
		return nil, mergeBlock, nil // every arm diverged (e.g. all `return`)
	}
	end, err := l.dropReclaimedPayload(mergeBlock, reuseToken, scrut, dt)
	if err != nil {
		return nil, nil, err
	}
	return phi, end, nil
}

// dropReclaimedPayload releases what a drop-reused value's *old* payload owned —
// its string fields, its `shared` tail — after the arms have run.
//
// lyra_rc_drop_reuse deliberately leaves the payload alone: an arm binds a field by
// reading it straight out of the box, taking no reference of its own, so dropping
// at reclaim time would free a field the arm is about to use (an arm duplicates
// only when it actually consumes the binding, which happens later, while lowering
// the body). Emitting the drop here — at the merge, past every arm — puts it after
// all of those duplications.
//
// It runs only when the token came back non-null, i.e. the box was *unique* and its
// shell was reclaimed (whether an arm then rewrote it or freed it). A null token
// means shared-and-decremented, or pinned: the box is still alive and still owns its
// fields, so touching them would be a double free. The old field values are read
// from `scrut`, the union unboxed *before* the shell could be overwritten.
//
// Returns the block control ends in — no-op (the same block) when there's no reuse
// token or the payload owns nothing.
func (l *lowerer) dropReclaimedPayload(block *ir.Block, token value.Value, scrut value.Value, dt types.DataType) (*ir.Block, error) {
	payloadType := types.WithAllocation(dt, types.Stack)
	if token == nil || !l.needsDrop(payloadType) {
		return block, nil
	}
	fn := block.Parent
	dropBlock := fn.NewBlock("")
	after := fn.NewBlock("")
	reclaimed := block.NewICmp(enum.IPredNE, token, constant.NewNull(lltypes.NewPointer(lltypes.I8)))
	block.NewCondBr(reclaimed, dropBlock, after)

	end, err := l.emitDropValue(dropBlock, scrut, payloadType)
	if err != nil {
		return nil, err
	}
	end.NewBr(after)
	return after, nil
}

// bindDataPayload binds a data pattern's payload sub-patterns into l.locals for
// the arm body. It reinterprets the union's payload blob as the variant's payload
// struct (bitcast + load) and binds each field's sub-pattern via aggPatternBind,
// so a payload that is (or contains) a struct/tuple is destructured recursively
// (`W((a, b))`, `Some({ x, y })`). A *value-testing* payload sub-pattern (a
// literal, or a nested data pattern) is deferred: this arm was already selected by
// the tag switch, which can't also test the payload and fall through to another
// arm (see patternHasTest).
func (l *lowerer) bindDataPayload(armBlock *ir.Block, p *ast.DataPattern, ctor types.DataTypeConstructor, slot value.Value, unionTy *lltypes.StructType) error {
	fieldPatterns, err := payloadFieldPatterns(p, ctor)
	if err != nil {
		return err
	}
	if len(fieldPatterns) == 0 {
		return nil // nullary variant — nothing to bind
	}
	for _, fp := range fieldPatterns {
		if patternHasTest(fp) {
			return fmt.Errorf("llvm: a value-testing payload sub-pattern (%T) in a `data` match arm is not implemented yet", fp)
		}
	}
	payloadStructTy, err := l.dataPayloadStructType(ctor)
	if err != nil {
		return err
	}
	blobPtr := armBlock.NewGetElementPtr(unionTy, slot, i32c(0), i32c(1))
	typedPtr := armBlock.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
	payload := armBlock.NewLoad(payloadStructTy, typedPtr)

	fieldTypes := ctor.FieldTypes()
	for i, fp := range fieldPatterns {
		if err := l.aggPatternBind(armBlock, armBlock.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i]); err != nil {
			return err
		}
	}
	return nil
}

// extractDataPayload reinterprets a first-class data value's payload blob as the
// given variant's payload struct, returning the loaded payload struct value. It
// goes through memory (alloca + store + bitcast + load), the mirror of
// construction — the same move as lowerDataMatch, but on an arbitrary value rather
// than the match scrutinee's pre-stored slot, so it works for a data value nested
// inside another pattern.
func (l *lowerer) extractDataPayload(block *ir.Block, val value.Value, ctor types.DataTypeConstructor) (value.Value, error) {
	payloadStructTy, err := l.dataPayloadStructType(ctor)
	if err != nil {
		return nil, err
	}
	unionSt, ok := val.Type().(*lltypes.StructType)
	if !ok {
		return nil, fmt.Errorf("llvm: data value did not lower to a struct (%s)", val.Type())
	}
	slot := block.Parent.Blocks[0].NewAlloca(unionSt)
	block.NewStore(val, slot)
	blobPtr := block.NewGetElementPtr(unionSt, slot, i32c(0), i32c(1))
	typedPtr := block.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
	return block.NewLoad(payloadStructTy, typedPtr), nil
}

// patternHasTest reports whether a pattern imposes a runtime value test (rather
// than only binding/ignoring): a literal or range, a data pattern (a tag check),
// or any aggregate containing one. Used to defer a payload sub-pattern that a
// tag-switch `data` arm can't test-and-fall-through on.
func patternHasTest(pat ast.Pattern) bool {
	switch p := pat.(type) {
	case *ast.LiteralPattern, *ast.RangePattern, *ast.DataPattern:
		return true
	case *ast.StructPattern:
		return slices.ContainsFunc(p.Fields, func(f ast.StructPatternField) bool {
			return patternHasTest(f.Pattern)
		})
	case *ast.TuplePattern:
		return slices.ContainsFunc(p.Elements, patternHasTest)
	case *ast.BindingPattern:
		// See through it: `whole @ Box(n)` tests exactly what `Box(n)` tests. Missing here
		// and the test is never *emitted* — the arm would be taken whatever the value is,
		// which is a wrong program rather than a failed build.
		return patternHasTest(p.Pattern)
	}
	return false
}

// payloadFieldPatterns returns the flat list of sub-patterns, one per payload
// field, for a data pattern — or an error for a form the backend doesn't bind
// yet. Flat positional (`Rect(w, h)` against `[i64, i64]`, `Circle(r)` against
// `[i64]`) and bare single (`Some x`) are supported; tuple-payload destructuring
// (`MkPair((x, y))`) is deferred.
func payloadFieldPatterns(p *ast.DataPattern, ctor types.DataTypeConstructor) ([]ast.Pattern, error) {
	flat := ctor.FieldTypes()
	if p.Pattern == nil {
		if len(flat) != 0 {
			return nil, fmt.Errorf("llvm: constructor %q has a payload but the pattern binds none", p.Name)
		}
		return nil, nil
	}
	if tp, ok := p.Pattern.(*ast.TuplePattern); ok {
		if len(tp.Elements) == len(flat) {
			return tp.Elements, nil
		}
		return nil, fmt.Errorf("llvm: tuple-payload destructuring for %q not implemented yet", p.Name)
	}
	if len(flat) == 1 {
		return []ast.Pattern{p.Pattern}, nil
	}
	// A bare `_` standing for a whole multi-field payload (`Rect _`) expands to one
	// wildcard per field. A wildcard binds nothing and tests nothing, so the expansion
	// is exact rather than an approximation — `Rect _` and `Rect(_, _)` describe the
	// same set of values, and only the second used to lower.
	//
	// Fresh nodes rather than the same one repeated: nothing downstream keys on a
	// pattern's identity today, but sharing one node across field positions is the kind
	// of aliasing that makes a later map-by-pointer quietly wrong.
	if _, isWildcard := p.Pattern.(*ast.WildcardPattern); isWildcard {
		out := make([]ast.Pattern, len(flat))
		for i := range out {
			out[i] = &ast.WildcardPattern{PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: p.GetLocation()}}}
		}
		return out, nil
	}
	// What is left is a single *binding* for a multi-field payload (`Rect pair`), which
	// would bind the payload tuple as one value. That is a real feature and not this
	// one, so it keeps the honest error rather than being guessed at.
	return nil, fmt.Errorf("llvm: binding a whole multi-field payload as one value (%q) is not implemented yet; "+
		"name the fields instead, as %s(…)", p.Name, p.Name)
}

// unreachableDataArms reports which arms repeat a constructor an earlier arm already
// claimed, so the caller can skip them before it builds a block for one.
//
// A pre-pass rather than a check inside the lowering loop: the block is created before the
// pattern kind is known, and an empty block with no terminator is invalid IR — so the
// alternative was to create one and pop it back off `fn.Blocks`, which is correct only
// while nothing else appends a block in between.
func unreachableDataArms(arms []ast.MatchArm) map[int]bool {
	claimed := map[string]bool{}
	dead := map[int]bool{}
	for i, arm := range arms {
		// Unwrapped: `w @ Box(n)` claims Box's tag exactly as `Box(n)` does, and missing
		// that emits two cases for one tag — IR llir builds happily and clang refuses.
		p, ok := ast.UnwrapBinding(arm.Pattern).(*ast.DataPattern)
		if !ok {
			continue
		}
		if claimed[p.Name] {
			dead[i] = true
			continue
		}
		claimed[p.Name] = true
	}
	return dead
}
