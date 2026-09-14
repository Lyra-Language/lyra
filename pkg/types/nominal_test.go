package types

import "testing"

// IdentityString spells a keyed nominal type with its module, through every composite a
// type is written out of, and leaves an unkeyed one — a program-wide name — exactly as String
// renders it.
func TestIdentityString(t *testing.T) {
	two := NamedStructType{Name: "Point", Key: "two::Point"}
	entry := DataType{Name: "Shape", Key: "::Shape"}
	wide := NamedStructType{Name: "Pos"}
	for _, c := range []struct {
		t    Type
		want string
	}{
		{two, "two.Point"},
		{entry, "entry.Shape"},
		{wide, "Pos"},
		{DynamicArrayType{ElementType: two}, DynamicArrayType{ElementType: NamedStructType{Name: "two.Point"}}.String()},
		{ParameterizedType{Name: "Opt", TypeArguments: []Type{two, wide}}, ParameterizedType{Name: "Opt", TypeArguments: []Type{NamedStructType{Name: "two.Point"}, wide}}.String()},
	} {
		if got := IdentityString(c.t); got != c.want {
			t.Errorf("IdentityString(%v) = %q; want %q", c.t, got, c.want)
		}
	}
	if two.String() != "Point" {
		t.Errorf("String must stay the bare name for messages, got %q", two.String())
	}
	if TypesEqual(two, NamedStructType{Name: "Point", Key: "one::Point"}) {
		t.Error("two modules' Points with known keys are different types")
	}
	// An unkeyed Point is the program-wide one, which a keyed Point never is.
	if TypesEqual(two, NamedStructType{Name: "Point"}) {
		t.Error("a keyed Point is not the program-wide Point")
	}
	if !TypesEqual(wide, NamedStructType{Name: "Pos"}) {
		t.Error("two unkeyed types of one name are the one program-wide declaration")
	}
}
