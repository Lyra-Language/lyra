package types

import (
	"fmt"
	"strings"
)

type LambdaType struct {
	Parameters []ParameterType
	ReturnType ReturnType
	// The effect bounds a *declared* function type carries: `f: pure () -> t`. They are
	// part of the type, not of the value, which is what lets a signature constrain the
	// callbacks it accepts instead of only describing its own body — see isAssignable,
	// where a stronger guarantee is assignable to a weaker slot and never the reverse.
	//
	// Bools rather than the checker's Effect bitmask because pkg/types cannot import the
	// checker (the dependency runs the other way), and because these mirror the three
	// modifiers the grammar spells one-for-one. Absent all three, a type promises nothing
	// and accepts anything, which is what every function type meant before they existed.
	IsPure    bool
	IsDet     bool
	IsNoAlloc bool
}

func (*LambdaType) typeNode() {}

func (t *LambdaType) GetName() string {
	paramTypeNames := make([]string, len(t.Parameters))
	for i, param := range t.Parameters {
		paramTypeNames[i] = param.String()
	}
	return fmt.Sprintf("%s(%s) -> %s", t.EffectPrefix(),
		strings.Join(paramTypeNames, ", "), t.ReturnType.String())
}

// EffectPrefix renders the declared bounds the way they are written, so a diagnostic
// about a mismatch shows the difference rather than printing two identical-looking types.
func (t *LambdaType) EffectPrefix() string {
	var parts []string
	if t.IsPure {
		parts = append(parts, "pure")
	}
	if t.IsDet {
		parts = append(parts, "det")
	}
	if t.IsNoAlloc {
		parts = append(parts, "noalloc")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// EffectBoundsSatisfy reports whether a function type carrying these bounds can be used
// where `want` is required: every guarantee the slot demands must be one this type makes.
// Extra guarantees are fine — a `pure noalloc` function is usable wherever a `pure` one is.
//
// `pure` also satisfies a `det` requirement, because the ladder is `pure` ⊆ `det` ⊆
// unannotated (checker/effects.go: DetEffects ⊆ PurityEffects). Without that, the stricter
// annotation would be rejected by the looser slot, which is exactly backwards.
func (t *LambdaType) EffectBoundsSatisfy(want *LambdaType) bool {
	if want == nil {
		return true
	}
	if want.IsPure && !t.IsPure {
		return false
	}
	if want.IsDet && !(t.IsDet || t.IsPure) {
		return false
	}
	if want.IsNoAlloc && !t.IsNoAlloc {
		return false
	}
	return true
}

func (t *LambdaType) String() string {
	return t.GetName()
}

type ParameterType struct {
	Modifier AllocationModifier
	// Borrow is the `ref`/`mut`/`own` axis, written on a parameter of a function *type*
	// (`(mut Self, own i64) -> void`) — distinct from Modifier, which is the
	// `stack`/`shared` allocation flavor. A function type is where a trait method's
	// parameter modes live, since an impl binds patterns rather than typed parameters.
	Borrow       TypeModifier
	Type         Type
	DefaultValue any
}

func (p ParameterType) GetName() string {
	modifier := ""
	if p.Borrow != TypeModifier("") {
		modifier = string(p.Borrow) + " "
	}
	if p.Modifier != AllocationModifier("") {
		modifier += string(p.Modifier) + " "
	}
	if p.Type != nil {
		return fmt.Sprintf("%s%s", modifier, p.Type.String())
	}
	return modifier
}

func (p ParameterType) String() string {
	return p.GetName()
}
