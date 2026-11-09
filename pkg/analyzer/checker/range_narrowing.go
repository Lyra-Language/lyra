package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
)

// isBuiltinNarrowing reports the shape evalNarrowing handles: a no-argument `.truncate()`,
// `.saturate()` or `.narrow()` on an integer, resolved to no declaration. A method of that
// name on some other type, or one the program declared, is an ordinary call.
func (c *rangeChecker) isBuiltinNarrowing(call *ast.FunctionCallExpr) bool {
	member, ok := call.Function.(*ast.MemberExpr)
	if !ok || len(call.Arguments) != 0 {
		return false
	}
	switch member.Property.Name {
	case "truncate", "saturate", "narrow":
	default:
		return false
	}
	if fn, resolved := c.tt.Callee(call); resolved && fn != nil {
		return false
	}
	_, _, known := c.intBoundsOf(member.Object)
	return known
}

// evalNarrowing answers what a narrowing method's result can be, from what its receiver can
// be. A receiver proven to fit is the same value out as in, whichever method it was, and a
// `narrow` then has nothing to check. Otherwise only `saturate` still says anything: every
// value it answers is in the receiver's range clamped to the target's, while a `truncate`'s
// is any bit pattern of the target and a `narrow`'s is the part of the receiver that fits.
func (c *rangeChecker) evalNarrowing(st rangeEnv, call *ast.FunctionCallExpr) (interval, bool, rangeEnv) {
	member := call.Function.(*ast.MemberExpr)
	recv, tracked, st := c.eval(st, member.Object)
	lo, hi, known := c.intBoundsOf(call)
	if !known {
		return interval{}, false, st
	}
	if !tracked {
		recv, tracked, _ = c.typeIntervalIn(member.Object, st)
	}
	if !tracked {
		return interval{lo, hi}, true, st
	}
	// +∞ is a `u64`'s top, which no signed target holds however it compares.
	if recv.lo >= lo && recv.hi <= hi && recv.hi != posInf {
		if member.Property.Name == "narrow" && !c.silent {
			c.safe.noNarrowTrap[call] = true
		}
		return recv, true, st
	}
	switch member.Property.Name {
	case "saturate", "narrow":
		clamped := interval{max(recv.lo, lo), min(recv.hi, hi)}
		if clamped.empty() {
			// Every value is out of range: a `saturate` answers one end, a `narrow` traps.
			return interval{lo, hi}, true, st
		}
		return clamped, true, st
	}
	return interval{lo, hi}, true, st
}
