package typechecker_test

import "testing"

// A trait method's **default body** — `shout: … = (self) => self.name() ++ "!"` — is the
// body an impl inherits by writing nothing. It parsed and collected from the beginning
// and was dispatched to by nobody, the fifth instance of the surface-nothing-reads shape
// this project keeps cataloguing.
//
// It is checked once with `Self` as a type variable bounded by the declaring trait, which
// is what these tests are mostly about: the body is generic code, and the errors it can
// produce are a generic function's errors.

func TestTraitDefault_InheritedByAnImpl(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Named {
		  pure name: (Self) -> string
		  pure shout: (Self) -> string = (self) => self.name() ++ "!"
		}
		struct Cat { n: i64 }
		impl Named for Cat { name = pure (self) => "cat" }
		let describe = pure (c: Cat) -> string => c.shout()
	`, false)
	assertNoErrors(t, res)
}

// An impl providing the method overrides it, and is not reported as declaring something
// the trait does not have.
func TestTraitDefault_OverrideIsNotAnExtraneousMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Named {
		  pure name: (Self) -> string
		  pure shout: (Self) -> string = (self) => self.name() ++ "!"
		}
		struct Fox { n: i64 }
		impl Named for Fox {
		  name = pure (self) => "fox"
		  shout = pure (self) => "FOX!!"
		}
	`, false)
	assertNoErrors(t, res)
}

// The impl-coherence check has always skipped a defaulted method when listing what an
// impl is missing; this pins that it still does now that the default is reachable.
func TestTraitDefault_ImplNeedNotProvideIt(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Named {
		  pure name: (Self) -> string
		  pure shout: (Self) -> string = (self) => self.name() ++ "!"
		}
		struct Cat { n: i64 }
		impl Named for Cat { name = pure (self) => "cat" }
	`, false)
	assertNoErrors(t, res)
	res2 := parseCollectAndCheck(t, `
		trait Named { pure name: (Self) -> string }
		struct Cat { n: i64 }
		impl Named for Cat { }
	`, false)
	assertErrorsAre(t, res2,
		`impl of Named for Cat: missing required method "name"`)
}

// The bound `Self` carries is closed over supertraits, so a default may call a method the
// supertrait declares — the same reach a `where t: B` bound has.
func TestTraitDefault_CallsASupertraitMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Base { pure base: (Self) -> i64 }
		trait Loud: Base { pure boom: (Self) -> i64 = (self) => self.base() * 10 }
		struct Bell { n: i64 }
		impl Base for Bell { base = pure (self) => 7 }
		impl Loud for Bell
	`, false)
	assertNoErrors(t, res)
}

// A generic impl target. It also runs — see the backend suite — but the typing is worth
// pinning here on its own: `Self` joins the impl's own bindings rather than replacing
// them, so `t` survives beside it.
func TestTraitDefault_GenericImplTarget(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Box<t> { v: t }
		trait Sized2 {
		  pure size: (Self) -> i64
		  pure doubled: (Self) -> i64 = (self) => self.size() * 2
		}
		impl Sized2 for Box<t> { size = pure (self) => 3 }
		let go = pure (b: Box<i64>) -> i64 => b.doubled()
	`, false)
	assertNoErrors(t, res)
}

// **The diagnostic `Self`-as-a-type-variable would otherwise produce.** A default body's
// receiver is a variable the compiler introduced, not one the author wrote, so the
// generic advice — "add a `where Self: Trait` bound" — names a clause no program can
// write. The question in a default is which trait declares the method.
func TestTraitDefault_CallingAMethodTheTraitDoesNotDeclare(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Bad {
		  pure ok: (Self) -> i64
		  pure broken: (Self) -> i64 = (self) => self.nonexistent() + 1
		}
	`, false)
	assertErrorsAre(t, res,
		`trait Bad does not declare a method "nonexistent", so a default body cannot call it on `+
			"`self`; declare it in Bad (or in a supertrait) first")
}
