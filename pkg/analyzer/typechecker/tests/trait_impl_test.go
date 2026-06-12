package typechecker_test

import "testing"

func TestTraitImpl_UnknownTrait(t *testing.T) {
	res := parseCollectAndCheck(t, `
	impl Ghost for i64 {
		foo = (n) => n
	}
	`, false)
	assertErrorsAre(t, res, `impl: unknown trait "Ghost"`)
}

func TestTraitImpl_AllRequiredMethodsProvided(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (n) => "x"
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MissingRequiredMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: missing required method "show"`)
}

func TestTraitImpl_DefaultMethodNotRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Describable {
		describe: (Self) -> string = (self) => "thing"
	}

	impl Describable for i64 {
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MissingOneOfMultipleRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Eq {
		(_==_): (Self, Self) -> bool,
		(_!=_): (Self, Self) -> bool
	}

	impl Eq for i64 {
		(_==_) = (a, b) => true
	}
	`, false)
	assertErrorsAre(t, res, `impl of Eq for i64: missing required method "!="`)
}

func TestTraitImpl_ExtraneousMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (n) => "x",
		extra = (n) => "y"
	}
	`, false)
	assertWarningsAre(t, res, `impl of Show for i64: method "extra" is not declared in trait`)
}

func TestTraitImpl_WrongArity(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = () => "x"
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: method "show" has wrong number of parameters: expected 1, got 0`)
}

func TestTraitImpl_WrongArityTooMany(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (a, b) => "x"
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: method "show" has wrong number of parameters: expected 1, got 2`)
}

func TestTraitImpl_MixedDefaultAndRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string,
		show_twice: (Self) -> string = (x) => "twice"
	}

	impl Show for i64 {
		show = (n) => "x"
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MultipleMissingMethods(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Collection {
		add: (Self, i64) -> Self,
		remove: (Self, i64) -> Self,
		size: (Self) -> i64
	}

	impl Collection for i64 {
	}
	`, false)
	if len(res.errors) != 3 {
		t.Errorf("expected 3 errors, got %d:", len(res.errors))
		for _, e := range res.errors {
			t.Errorf("  %s", e.Message)
		}
	}
}
