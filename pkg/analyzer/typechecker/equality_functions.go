package typechecker

import (
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A function value has no equality: two closures are equal neither by their code nor by what
// they captured in any sense a program could rely on. `==` on a type holding one was accepted
// here and refused by the backend alone — "structural equality on () -> void is not
// implemented", with no position (11/02, Vega's `Work`). The structural comparison it would
// lower (backend equality.go) walks every field, element and payload, and never a nested
// type's `Eq` impl, so the question is whether a function is reachable that way at all.

// reportFunctionEquality refuses `a == b` on t when a function value is part of it, naming
// the path to the first one, and reports whether it did. An `Eq` impl for t itself has been
// tried first (dispatchEq).
func (tc *TypeChecker) reportFunctionEquality(expr *ast.BooleanBinaryOpExpr, t types.Type) bool {
	path, fn, ok := tc.functionHeldBy(t, expr.GetLocation())
	if !ok {
		return false
	}
	if path == "" {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: a function value has no equality (%s); compare what it computes instead",
			expr.Operator, fn)
		return true
	}
	advice := "compare the fields that have one, or implement Eq for " + types.Source(t)
	if resolved, _ := tc.layoutResolver(expr.GetLocation())(t); resolved != nil {
		if data, isData := resolved.(types.DataType); isData {
			advice = "test which variant it is with `match`"
			if data.Name == "Maybe" {
				advice = "test which it is with `is_some()` or `is_none()`"
			}
		}
	}
	tc.addError(expr.GetLocation(), SeverityError,
		"operator %s: %s holds a function value (%s: %s), and a function has no equality — %s",
		expr.Operator, t, path, fn, advice)
	return true
}

// functionHeldBy finds a function value t holds by value — itself, a field, an element, a
// tuple position or a constructor's payload — resolving each aggregate from its declaration
// as layout does (layoutResolver), so a generic one is seen instantiated. The path is how the
// program would reach it (`run`, `jobs[…].run`, `Some.0`), empty for t itself.
func (tc *TypeChecker) functionHeldBy(t types.Type, loc ast.Location) (string, types.Type, bool) {
	return functionHeldWith(t, tc.layoutResolver(loc), map[string]bool{})
}

func functionHeldWith(t types.Type, resolve types.Resolver, seen map[string]bool) (string, types.Type, bool) {
	if t == nil {
		return "", nil, false
	}
	t, inner := resolve(t)
	if t == nil {
		return "", nil, false
	}
	if fn, ok := t.(*types.LambdaType); ok {
		return "", fn, true
	}
	// A recursive type is walked once: `JsonValue` holds `[]JsonValue`.
	switch t.(type) {
	case types.NamedStructType, types.DataType:
		key := t.String()
		if seen[key] {
			return "", nil, false
		}
		seen[key] = true
	}
	within := func(step string, component types.Type) (string, types.Type, bool) {
		path, fn, ok := functionHeldWith(component, inner, seen)
		if !ok {
			return "", nil, false
		}
		switch {
		case path == "":
			return step, fn, true
		case path[0] == '[':
			return step + path, fn, true
		}
		return step + "." + path, fn, true
	}
	switch v := t.(type) {
	case types.NamedStructType:
		for _, f := range v.Fields {
			if path, fn, ok := within(f.Name, f.Type); ok {
				return path, fn, true
			}
		}
	case types.AnonymousStructType:
		for _, f := range v.Fields {
			if path, fn, ok := within(f.Name, f.Type); ok {
				return path, fn, true
			}
		}
	case types.TupleType:
		for i, e := range v.Elements {
			if path, fn, ok := within(strconv.Itoa(i), e); ok {
				return path, fn, true
			}
		}
	case types.StaticArrayType:
		return within("[…]", v.ElementType)
	case types.DynamicArrayType:
		return within("[…]", v.ElementType)
	case types.DataType:
		for _, c := range v.Constructors {
			for i, f := range c.FieldTypes() {
				if path, fn, ok := within(c.Name+"."+strconv.Itoa(i), f); ok {
					return path, fn, true
				}
			}
		}
	}
	return "", nil, false
}
