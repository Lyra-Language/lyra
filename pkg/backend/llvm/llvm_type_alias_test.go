package llvm

import (
	"strings"
	"testing"
)

// A `type` alias is transparent, so by the time codegen runs there should be nothing
// left of it. These are the exec cases that prove the alias reaches the same machine
// code the spelled-out type does.
func TestExec_TypeAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The case aliases exist for: a function type, where the double parens (a
			// single *tuple* parameter) cannot be spelled away, only named.
			"alias of a function type",
			`type Op = ((i64, i64)) -> i64
			 let apply = (g: Op, p: (i64, i64)) -> i64 => g(p)
			 let main = () -> u8 => u8(apply(((a, b)) => a * b, (3, 4)))`,
			12,
		},
		{
			"alias of a primitive",
			`type Id = i64
			 let double = (n: Id) -> Id => n * 2
			 let main = () -> u8 => u8(double(21))`,
			42,
		},
		{
			// An alias holds the aliased type *itself*, so a struct alias is the case
			// where the backend would declare and define Pt's LLVM struct a second
			// time under the name Point if lowerTypeDecl did not skip aliases.
			"alias of a struct",
			`struct Pt { x: i64, y: i64 }
			 type Point = Pt
			 let sum = (p: Point) -> i64 => p.x + p.y
			 let main = () -> u8 => {
			   let p = Pt { x: 3, y: 4 }
			   u8(sum(p))
			 }`,
			7,
		},
		{
			"alias in return position",
			`type Op = (i64) -> i64
			 let mk = (n: i64) -> Op => (x: i64) -> i64 => x + n
			 let main = () -> u8 => {
			   let f = mk(1)
			   u8(f(6))
			 }`,
			7,
		},
		{
			// A chain: the backend expands one hop and recurses, since the alias's own
			// recorded type comes back as another named type.
			"alias chain",
			`struct Pt { x: i64 }
			 type A = Pt
			 type B = A
			 let get = (p: B) -> i64 => p.x
			 let main = () -> u8 => {
			   let p = Pt { x: 9 }
			   u8(get(p))
			 }`,
			9,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_StructAliasEmitsOneType: an alias must not duplicate the type it names.
// A struct alias holds the very NamedStructType the struct's own declaration holds,
// so without the IsAlias skip in lowerTypeDecl/lowerTypeDef the module would carry
// two definitions of the same layout — which llir would emit under two names, and
// which would then disagree at any boundary that crossed them.
func TestEmit_StructAliasEmitsOneType(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `struct Pt { x: i64, y: i64 }
	 type Point = Pt
	 let sum = (p: Point) -> i64 => p.x + p.y
	 let main = () -> u8 => {
	   let p = Pt { x: 1, y: 2 }
	   u8(sum(p))
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, "%Pt = type"); n != 1 {
		t.Errorf("want exactly 1 definition of %%Pt, got %d:\n%s", n, got)
	}
	if strings.Contains(got, "%Point = type") {
		t.Errorf("the alias emitted a type of its own:\n%s", got)
	}
}
