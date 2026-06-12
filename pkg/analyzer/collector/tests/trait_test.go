package collector_test

import "testing"

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

func TestCollector_TraitDeclarationForBinaryOperator(t *testing.T) {
	source := `
	trait Eq {
		(_==_): (Self, Self) -> bool,
		(_!=_): (Self, Self) -> bool
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
		show_twice: (Self) -> string = (x) => show(x) + " " + show(x)
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
