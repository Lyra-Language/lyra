package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_TraitDeclaration(t *testing.T) {
	source := `
	trait Show {
		show: (Self) -> string
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration.golden"))
}

func TestCollector_TraitImplementation(t *testing.T) {
	source := `
	impl Show for int {
		show = (n) => int_to_string(n)
	}

	impl Show for string {
		show = (s) => s
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_implementation.golden"))
}

func TestCollector_TraitDeclarationWithGenericParameters(t *testing.T) {
	source := `
	trait Converter<from, to> {
		convert: (from) -> to
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_with_generic_parameters.golden"))
}

func TestCollector_TraitDeclarationWithGenericTypeImplementation(t *testing.T) {
	source := `
	trait Functor {
		map: (Self<a>, (a) -> b) -> Self<b>
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_with_generic_type_implementation.golden"))
}

func TestCollector_TraitDeclarationForPrefixOperator(t *testing.T) {
	source := `
	trait Neg {
		(-_): (Self) -> Self
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_for_prefix_operator.golden"))
}

func TestCollector_TraitDeclarationForSuffixOperator(t *testing.T) {
	source := `
	trait Inc {
		(_++): (Self) -> Self
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_for_suffix_operator.golden"))
}

func TestCollector_TraitDeclarationForBinaryOperator(t *testing.T) {
	source := `
	trait Eq {
		(_==_): (Self, Self) -> bool,
		(_!=_): (Self, Self) -> bool
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_for_binary_operator.golden"))
}

func TestCollector_TraitDeclarationWithMultipleMethodsAndGenericParameters(t *testing.T) {
	source := `
	trait Collection<c, e> {
		add: (c<e>, e) -> c<e>,
		remove: (c<e>, e) -> c<e>,
		contains: (c<e>, e) -> bool,
		size: (c<e>) -> int
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_with_multiple_methods_and_generic_parameters.golden"))
}

func TestCollector_TraitDeclarationWithSelfAndGenericConstraints(t *testing.T) {
	source := `
	trait Bounded<t>: Show + Eq where t: Ord {
		id: (t) -> t
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_with_self_and_generic_constraints.golden"))
}

func TestCollector_TraitDeclarationWithMultipleGenericConstraints(t *testing.T) {
	source := `
	trait PairOps<a, b> where a: Show + Eq, b: Ord, {
		first: (a, b) -> a
	}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "trait_declaration_with_multiple_generic_constraints.golden"))
}