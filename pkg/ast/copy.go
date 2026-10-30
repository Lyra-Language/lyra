package ast

import "reflect"

// CopyBindingFree is a deep copy of expr when nothing inside it binds a name — no statement,
// no pattern, no lambda, no comprehension — and expr itself, with false, otherwise.
//
// **For a struct field's default, filled into each literal that omits the field**
// (typechecker.applyDefaultFields). A generic struct's default is typed per instantiation —
// `slots: []Maybe<e> = [None]` is `[]Maybe<i64>` in one literal and `[]Maybe<string>` in the
// next — and one node records one type, so each literal needs nodes of its own (10/27 copied a
// leaf; 10/30 a whole binding-free tree).
//
// **Binding-free is the whole of what makes a copy sound.** A name a node binds is registered
// in the collector's scope table under that node, and its type recorded by its source
// location, so a copy of a lambda or a block shares both with the original: a copy is safe
// only where there is nothing of the kind. This compiler otherwise substitutes rather than
// clones (the monomorphizer's note says why), and this copier copies nothing else.
//
// The walk is by reflection over the node's exported fields, as pkg/printer's: a node kind
// added later is copied with no case to add. A type (pkg/types) is shared, never copied — a
// type is a value nothing records against.
func CopyBindingFree(expr Expression) (Expression, bool) {
	if expr == nil || !bindsNothing(expr) {
		return expr, false
	}
	return copyNode(reflect.ValueOf(expr)).Interface().(Expression), true
}

// bindsNothing reports whether no node in expr binds a name.
func bindsNothing(expr Expression) bool {
	free := true
	visit := func(e Expression) bool {
		switch e.(type) {
		case *LambdaExpr, *ArrayCompExpr:
			free = false
		}
		if len(PatternsOf(e)) > 0 {
			free = false
		}
		return free
	}
	if !visit(expr) {
		return false
	}
	WalkExprChildren(expr, func(Statement) bool {
		free = false
		return false
	}, visit)
	return free
}

var astPackage = reflect.TypeFor[Location]().PkgPath()

// copyNode copies v, recursing into this package's structs (through pointers, interfaces and
// slices) and sharing everything else.
func copyNode(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || v.Type().Elem().Kind() != reflect.Struct || v.Type().Elem().PkgPath() != astPackage {
			return v
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(copyNode(v.Elem()))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(copyNode(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c.Index(i).Set(copyNode(v.Index(i)))
		}
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		c.Set(v) // every field, unexported ones included
		if v.Type().PkgPath() != astPackage {
			return c
		}
		for i := range v.NumField() {
			if f := c.Field(i); f.CanSet() {
				f.Set(copyNode(v.Field(i)))
			}
		}
		return c
	}
	return v
}
