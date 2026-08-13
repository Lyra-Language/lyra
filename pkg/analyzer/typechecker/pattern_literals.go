package typechecker

import (
	"math"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkPatternLiterals value-checks every integer literal a match pattern carries
// against the type it will be compared to — the fix for the truncation family the
// 08/12 audit's second sweep found (lyra-E048). Until then a pattern literal was
// checked for *kind* (an int where an int belongs) but never for *value*, and the
// backend lowered the constant at the scrutinee's width: `match x { 300 => … }` on a
// u8 silently matched 44, `{ -1 => … }` matched 255 — the negative-indexing bug's
// spirit reborn in pattern position, hours after it was removed from indexing — and
// `Some(300)` on a `Maybe<u8>` matched `Some(44)`. A wrong *value* in a pattern is
// worse than in an expression: it redirects control flow.
//
// Everything here is a compile-time constant by construction (the grammar admits only
// literals in these positions), so the standing ladder collapses to its first rung:
// provable → compile error, and there is no runtime half. Rust draws the same line
// ("range endpoint is out of range" on `200..=300u8`) for the same reason: an
// out-of-range bound is a bug in what the author wrote, not a clamp request.
//
// **This is a conservative mirror of walkDestructuredPattern's pairing, not a copy of
// its checking.** The pairing (which sub-pattern meets which type) must agree with
// that walk, and the shapes are shared where they exist as helpers
// (resolveToDataType, ctor.FieldTypes, arrayElementType, structFieldTypes). It cannot
// simply live inside that walk: withPatternBindings runs the walk with its errors
// *discarded* (deliberately — the arm-kind checks own those reports), which is
// exactly where these reports must not vanish. Any pairing this mirror does not
// recognize is skipped silently — the authoritative shape errors live in the walk and
// the per-kind arm checks, so a miss here degrades to a lost diagnostic, never a
// false one.
func (tc *TypeChecker) checkPatternLiterals(pat ast.Pattern, t types.Type) {
	if pat == nil || t == nil {
		return
	}
	t = tc.resolveTypeIfKnown(t, pat.GetLocation())

	switch p := pat.(type) {
	case *ast.BindingPattern:
		tc.checkPatternLiterals(p.Pattern, t)

	case *ast.LiteralPattern:
		s, isStr := p.Value.(string)
		if !isStr {
			return // a rune pattern; runes carry no width to violate
		}
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return // not an integer literal (string/bool/float), or wider than int64
		}
		tc.checkPatternIntFits(p.GetLocation(), s, v, t)

	case *ast.RangePattern:
		// Both bounds are number literals by grammar; a bound outside the scrutinee's
		// type is refused rather than clamped — the author meant *something*, and
		// `200..<=300` on a u8 is a bug to fix (write `..<=255`), not a range to
		// quietly shrink. An open bound (nil) means the type's own edge and needs no
		// check. An **exclusive** end names a position, not a value — `0..<256` on a
		// u8 is exactly `0..<=255`, the full range, and the exhaustiveness analysis
		// already reads it that way — so what must fit is the last *included* value,
		// the bound minus one (the same one-past-the-end grace `slice`'s end and
		// `byte_offset(n)` carry).
		if v, ok := extractIntFromExpr(p.Start); ok {
			tc.checkPatternIntFits(p.Start.GetLocation(), formatPatternInt(v), v, t)
		}
		if p.End != nil {
			if v, ok := extractIntFromExpr(p.End); ok {
				if types.RangeExcludesEnd(p.EndOperator) {
					if v == math.MinInt64 {
						return // `..< min` is empty; v-1 would wrap
					}
					tc.checkPatternIntFits(p.End.GetLocation(), formatPatternInt(v), v-1, t)
				} else {
					tc.checkPatternIntFits(p.End.GetLocation(), formatPatternInt(v), v, t)
				}
			}
		}

	case *ast.TuplePattern:
		tt, ok := t.(types.TupleType)
		if !ok || len(p.Elements) != len(tt.Elements) {
			return
		}
		for i, el := range p.Elements {
			if _, isRest := el.(*ast.RestPattern); isRest {
				continue
			}
			tc.checkPatternLiterals(el, tt.Elements[i])
		}

	case *ast.ArrayPattern:
		elemType := arrayElementType(t)
		if elemType == nil {
			return
		}
		for _, el := range p.Elements {
			if _, isRest := el.(*ast.RestPattern); isRest {
				continue
			}
			tc.checkPatternLiterals(el, elemType)
		}

	case *ast.StructPattern:
		fields := structFieldTypes(t)
		if fields == nil {
			return
		}
		for _, f := range p.Fields {
			if f.Pattern == nil {
				continue
			}
			if fieldType, ok := fields[f.Name]; ok {
				tc.checkPatternLiterals(f.Pattern, fieldType)
			}
		}

	case *ast.DataPattern:
		dt, ok := tc.resolveToDataType(t, p.GetLocation())
		if !ok || p.Pattern == nil {
			return
		}
		var ctor *types.DataTypeConstructor
		for i := range dt.Constructors {
			if dt.Constructors[i].Name == p.Name {
				ctor = &dt.Constructors[i]
				break
			}
		}
		if ctor == nil {
			return // unknown constructor — checkDataMatchArm owns that report
		}
		flat := ctor.FieldTypes()
		// The three payload shapes bindDataPatternPayload pairs, mirrored:
		// flat positional, a sole tuple-typed param destructured whole, and the
		// bare single-payload `Some 300`.
		if tp, isTuple := p.Pattern.(*ast.TuplePattern); isTuple {
			switch {
			case len(tp.Elements) == len(flat):
				for i, el := range tp.Elements {
					tc.checkPatternLiterals(el, flat[i])
				}
			case len(tp.Elements) == 1 && len(ctor.Params) == 1:
				tc.checkPatternLiterals(tp.Elements[0], ctor.Params[0])
			}
			return
		}
		if len(flat) == 1 {
			tc.checkPatternLiterals(p.Pattern, flat[0])
		}
	}
}

// checkPatternIntFits reports an integer pattern value that its comparison type
// cannot hold — the leaf the walk above exists to reach. Two flavors of impossible:
// the value does not fit the *width* (`300` on u8, `-1` on any unsigned), or it is
// outside a newtype's *range constraint* (`200` on `Percent = u8 where
// range(0..<=100)`) — the constraint half of "the check follows the type" (08/12),
// which had covered every expression position and no pattern position.
func (tc *TypeChecker) checkPatternIntFits(loc ast.Location, text string, v int64, t types.Type) {
	if ct, isCT := t.(*types.ConstrainedType); isCT {
		reported := false
		for _, c := range ct.Constraints {
			rc, isRange := c.(*types.RangeConstraint)
			if !isRange {
				continue
			}
			if intOutsideRangeConstraint(v, rc) {
				tc.addErrorCode(loc, SeverityError, diag.CodePatternOutOfRange,
					"pattern %s is outside the range %s of %s, so this arm can never match",
					text, rangeConstraintString(rc), ct.Name)
				reported = true
			}
		}
		if reported {
			return // the constraint is a subset of the base; one report per mistake
		}
		t = tc.resolveTypeIfKnown(ct.Type, loc)
	}
	p, isPrim := t.(types.PrimitiveType)
	if !isPrim || !isAnyConcreteInt(p.Name) {
		return
	}
	if !integerFitsInType(v, p.Name) {
		tc.addErrorCode(loc, SeverityError, diag.CodePatternOutOfRange,
			"pattern %s does not fit the scrutinee type %s, so this arm can never match",
			text, p.Name)
	}
}

// intOutsideRangeConstraint is checkIntRange's judgment as a pure predicate, shared
// so a pattern value and an expression value cannot come to disagree about one
// constraint. Bounds that do not fold (a named constant, arithmetic) leave that side
// unenforced — conservative, like everything in this family.
func intOutsideRangeConstraint(v int64, rc *types.RangeConstraint) bool {
	if rc.Start != nil {
		if lo, ok := foldConstraintInt(rc.Start); ok && v < lo {
			return true
		}
	}
	if rc.End != nil {
		if hi, ok := foldConstraintInt(rc.End); ok {
			if rc.Comparator == "<" {
				return v >= hi
			}
			return v > hi
		}
	}
	return false
}

// formatPatternInt renders a folded bound for the diagnostic, matching how the
// author wrote a plain decimal (the only spelling extractIntFromExpr folds).
func formatPatternInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
