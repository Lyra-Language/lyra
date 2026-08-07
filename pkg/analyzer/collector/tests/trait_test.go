package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A trait impl's `where` clause is collected into TraitImplStmt.Constraints:
// each bound variable with its trait bounds. (Regression pin — the collector
// once read the wrong grammar field name and dropped these silently.)
func TestCollector_TraitImplWhereConstraints(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Ord { compare: (Self, Self) -> i64 }
impl Ord<t> for Box<t> where t: Ord {
    compare = (self, other) => 0
}`
	program, _, _, _ := parseAndCollect(t, source)
	var impl *ast.TraitImplStmt
	for _, st := range program.Statements {
		if ti, ok := st.(*ast.TraitImplStmt); ok {
			impl = ti
		}
	}
	if impl == nil {
		t.Fatal("no trait impl collected")
	}
	if len(impl.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d: %#v", len(impl.Constraints), impl.Constraints)
	}
	c := impl.Constraints[0]
	if c.GenericType != "t" {
		t.Errorf("constraint variable = %q, want %q", c.GenericType, "t")
	}
	if len(c.TraitBounds) != 1 || c.TraitBounds[0] != "Ord" {
		t.Errorf("constraint bounds = %#v, want [Ord]", c.TraitBounds)
	}
}

// The trait's type arguments after the trait name (`impl Get<t> for Box<t>`) are
// collected into TraitImplStmt.TraitArgs. (The grammar labels every child of the
// `<…>` list with one field name, so this needs FieldNameForChild iteration, not
// ChildByFieldName — a regression pin for that.)
func TestCollector_TraitImplTraitArgs(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> {
    get = (self) => self.value
}`
	program, _, _, _ := parseAndCollect(t, source)
	var impl *ast.TraitImplStmt
	for _, st := range program.Statements {
		if ti, ok := st.(*ast.TraitImplStmt); ok {
			impl = ti
		}
	}
	if impl == nil {
		t.Fatal("no trait impl collected")
	}
	if len(impl.TraitArgs) != 1 {
		t.Fatalf("expected 1 trait arg, got %d: %#v", len(impl.TraitArgs), impl.TraitArgs)
	}
	if g, ok := impl.TraitArgs[0].(types.GenericType); !ok || g.Name != "t" {
		t.Errorf("trait arg = %#v, want GenericType{t}", impl.TraitArgs[0])
	}
}

// Effect-bound modifiers on a trait method declaration (`pure show: …`) are
// collected onto TraitMethod.IsPure/IsDet/IsNoAlloc.
func TestCollector_TraitMethodEffectBounds(t *testing.T) {
	source := `
trait Show {
    pure show: (Self) -> string,
    det now: (Self) -> i64,
    plain: (Self) -> bool
}`
	_, table, _, _ := parseAndCollect(t, source)
	methods := table.Traits["Show"].Methods
	if len(methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(methods))
	}
	if !methods[0].IsPure || methods[0].IsDet {
		t.Errorf("show: IsPure=%v IsDet=%v, want pure", methods[0].IsPure, methods[0].IsDet)
	}
	if !methods[1].IsDet || methods[1].IsPure {
		t.Errorf("now: IsDet=%v IsPure=%v, want det", methods[1].IsDet, methods[1].IsPure)
	}
	if methods[2].IsPure || methods[2].IsDet || methods[2].IsNoAlloc {
		t.Errorf("plain: expected no bounds, got pure=%v det=%v noalloc=%v",
			methods[2].IsPure, methods[2].IsDet, methods[2].IsNoAlloc)
	}
}

func TestCollector_TraitDeclaration(t *testing.T) {
	source := `
	trait Show {
		show: (Self) -> string
	}
	`
	runGoldenTest(t, source, "trait_declaration")
}

func TestCollector_TraitImplementation(t *testing.T) {
	source := `
	impl Show for i64 {
		show = (n) => int_to_string(n)
	}

	impl Show for string {
		show = (s) => s
	}
	`
	runGoldenTest(t, source, "trait_implementation")
}

func TestCollector_TraitDeclarationWithGenericParameters(t *testing.T) {
	source := `
	trait Converter<from, to> {
		convert: (from) -> to
	}
	`
	runGoldenTest(t, source, "trait_declaration_with_generic_parameters")
}

func TestCollector_TraitDeclarationWithGenericTypeImplementation(t *testing.T) {
	source := `
	trait Functor {
		map: (Self<a>, (a) -> b) -> Self<b>
	}
	`
	runGoldenTest(t, source, "trait_declaration_with_generic_type_implementation")
}

func TestCollector_TraitDeclarationForPrefixOperator(t *testing.T) {
	source := `
	trait Neg {
		(-_): (Self) -> Self
	}
	`
	runGoldenTest(t, source, "trait_declaration_for_prefix_operator")
}

func TestCollector_TraitDeclarationForSuffixOperator(t *testing.T) {
	source := `
	trait Inc {
		(_++): (Self) -> Self
	}
	`
	runGoldenTest(t, source, "trait_declaration_for_suffix_operator")
}

// Operator-named methods still *collect*, which is what this asserts. It used the
// comparison operators until 08/07; those are now refused outright (lyra-E039) because
// the compiler owns them — `==`/`!=` through the prelude's `Eq`, the ordering ones
// through `Ord::compare`. Arithmetic has no canonical trait, so it keeps the syntax and
// merely warns that nothing dispatches to it yet.
func TestCollector_TraitDeclarationForBinaryOperator(t *testing.T) {
	source := `
	trait Arith {
		(_+_): (Self, Self) -> Self,
		(_-_): (Self, Self) -> Self
	}
	`
	runGoldenTest(t, source, "trait_declaration_for_binary_operator")
}

func TestCollector_TraitDeclarationWithMultipleMethodsAndGenericParameters(t *testing.T) {
	source := `
	trait Collection<c, e> {
		add: (c<e>, e) -> c<e>,
		remove: (c<e>, e) -> c<e>,
		contains: (c<e>, e) -> bool,
		size: (c<e>) -> i64
	}
	`
	runGoldenTest(t, source, "trait_declaration_with_multiple_methods_and_generic_parameters")
}

func TestCollector_TraitDeclarationWithSelfAndGenericConstraints(t *testing.T) {
	source := `
	trait Bounded<t>: Show + Eq where t: Ord {
		id: (t) -> t
	}
	`
	runGoldenTest(t, source, "trait_declaration_with_self_and_generic_constraints")
}

func TestCollector_TraitDeclarationWithMultipleGenericConstraints(t *testing.T) {
	source := `
	trait PairOps<a, b> where a: Show + Eq, b: Ord, {
		first: (a, b) -> a
	}
	`
	runGoldenTest(t, source, "trait_declaration_with_multiple_generic_constraints")
}

func TestCollector_TraitWithDefaultMethodImplementation(t *testing.T) {
	source := `
	trait Show {
		show: (Self) -> string,
		show_twice: (Self) -> string = (x) => show(x) ++ " " ++ show(x)
	}
	`
	runGoldenTest(t, source, "trait_with_default_method_implementation")
}

func TestCollector_TraitWithDefaultMethodImplementationWithBlock(t *testing.T) {
	source := `
	trait Describable {
		describe: (Self) -> string = (self) => {
			let s = self.name()
			concat("Item: ", s)
		}
	}
	`
	runGoldenTest(t, source, "trait_with_default_method_implementation_with_block")
}
