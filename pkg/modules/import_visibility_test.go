package modules_test

import "testing"

// An import's member list restricts visibility (08/18). Until then the rule was "any
// `pub` name of any module you imported at all": `import std.tui.{ bg }` admitted `grey`,
// `rgb` and `bold` too, and the member list drove only the namespace binding and the
// unused-import warning.
//
// It was architectural rather than a missing check. Every `pub` declaration went into one
// global scope that sat on every module's parent chain, so *exported* and *visible* were
// the same thing and no per-reference check was consulted. Each module now has an imports
// scope between its own and the prelude's, and resolution stops at the prelude.

const visLib = `module lib
pub struct Point { x: i64 }
pub trait Shown { pure show2: (Self) -> i64 }
pub let listed = pure () -> i64 => 1
pub let unlisted = pure () -> i64 => 2
let hidden = pure () -> i64 => 3`

func visTree(t *testing.T, app string) map[string]string {
	t.Helper()
	return map[string]string{"app.lyra": app, "lib.lyra": visLib}
}

// A listed member resolves; an unlisted one does not, even though it is `pub`.
func TestImportVisibility_UnlistedNameIsRefused(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => u8(listed() + unlisted())`)))
	if !errorsContaining(res, `undefined function "unlisted"`) {
		t.Errorf("an unlisted export must not resolve bare; got %v", res.Errors())
	}
}

// **A type takes the same rule**, and needed its own gate to get it: a value resolves
// through the scope chain, while a type goes through the Types map keyed by declKey,
// which answers "whose declaration is this" and says nothing about who may see it.
func TestImportVisibility_UnlistedTypeIsRefused(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => {
  let p: Point = Point { x: 1 }
  u8(p.x + listed())
}`)))
	if !errorsContaining(res, `undefined struct type "Point"`) {
		t.Errorf("an unlisted exported type must not resolve; got %v", res.Errors())
	}
}

func TestImportVisibility_ListedTypeResolves(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ Point }
let main = () -> u8 => {
  let p: Point = Point { x: 1 }
  u8(p.x)
}`)))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a listed type should resolve; got %v", errs)
	}
}

// **A namespace import admits no bare names.** If it did, the two import forms would mean
// the same thing and the member list would be decoration.
func TestImportVisibility_NamespaceImportAdmitsNoBareNames(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib
let main = () -> u8 => u8(listed())`)))
	if !errorsContaining(res, `undefined function "listed"`) {
		t.Errorf("a namespace import must not admit a bare name; got %v", res.Errors())
	}
}

// …and the qualified form it *does* bind still works.
func TestImportVisibility_NamespaceImportResolvesQualified(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib
let main = () -> u8 => u8(lib.listed())`)))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a namespace import should still resolve qualified; got %v", errs)
	}
}

// An alias binds only its local name.
func TestImportVisibility_AliasBindsOnlyTheLocalName(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed as renamed }
let main = () -> u8 => u8(renamed())`)))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("an alias should bind its local name; got %v", errs)
	}
	res2 := analyze(t, buildTree(t, visTree(t, `import lib.{ listed as renamed }
let main = () -> u8 => u8(listed())`)))
	if !errorsContaining(res2, `undefined function "listed"`) {
		t.Errorf("an alias must not also bind the source name; got %v", res2.Errors())
	}
}

// **The diagnostic names the fix.** "Undefined" is the wrong word for by far the
// commonest new failure: the name exists, it is `pub`, and this file simply did not ask
// for it. GlobalScope is what can answer that — it is off every module's parent chain now
// and serves as the program-wide registry of exported names.
func TestImportVisibility_DiagnosticNamesTheImport(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => u8(unlisted())`)))
	if !errorsContaining(res, "add `import lib.{ unlisted }`") {
		t.Errorf("the diagnostic should name the import that fixes it; got %v", res.Errors())
	}
}

// And it must not be confused with the privacy diagnostic, whose fix is the opposite —
// `pub` on the declaration. Before imports restricted visibility the two could not be told
// apart at the type lookup, since an exported type always resolved.
func TestImportVisibility_ExportedButUnimportedIsNotReportedPrivate(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => u8(unlisted())`)))
	if errorsContaining(res, "is private to module") {
		t.Errorf("an exported name must not be reported private; got %v", res.Errors())
	}
}

// A genuinely private name still gets the privacy message, not the import one.
func TestImportVisibility_PrivateStillSaysPrivate(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => u8(hidden())`)))
	if !errorsContaining(res, `hidden is private to module "lib"`) {
		t.Errorf("a private name should still report as private; got %v", res.Errors())
	}
}

// **UFCS stays exempt.** `b.doubled()` names a method the receiver's type already
// justifies; requiring the free function to be imported by name would make method syntax
// mean something different from what it desugars to.
func TestImportVisibility_UFCSDoesNotRequireImportingTheMethod(t *testing.T) {
	res := analyze(t, buildTree(t, map[string]string{
		"app.lyra": `import boxes.{ Box }
let main = () -> u8 => {
  let b = Box { v: 21 }
  u8(b.doubled())
}`,
		"boxes.lyra": `module boxes
pub struct Box { v: i64 }
pub let doubled = pure (self: Box) -> i64 => self.v * 2`,
	}))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a method call should not require importing the free function; got %v", errs)
	}
}

// The prelude is ambient and unaffected — it sits under every module's imports scope.
func TestImportVisibility_PreludeStaysAmbient(t *testing.T) {
	res := analyze(t, buildTree(t, visTree(t, `import lib.{ listed }
let main = () -> u8 => u8(listed())`)))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("expected a clean program; got %v", errs)
	}
}

// An `as`-aliased **type** import binds the local name to the source declaration, so the
// alias works everywhere the type's own name would — including as an annotation. Under
// the pre-08/27 key scheme the alias never reached the Types map, so `mk(7)` resolved
// while `let p: Pt = mk(7)` failed with "cannot assign Point to Pt".
func TestImportVisibility_AliasedTypeResolvesAsAnnotation(t *testing.T) {
	res := analyze(t, buildTree(t, map[string]string{
		"app.lyra": `import shapes.{ Point as Pt, mk }
let main = () -> u8 => {
  let p: Pt = mk(7)
  u8(p.x)
}`,
		"shapes.lyra": `module shapes
pub struct Point { x: i64 }
pub let mk = pure (n: i64) -> Point => Point { x: n }`,
	}))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("an aliased type import must resolve as an annotation; got %v", errs)
	}
}

// A type the file never imported still resolves where a *value* carried it in — a
// constructor payload, a listed function's return — because the value's type must be
// usable wherever the value flows. What the boundary polices is the *written* name:
// the same file spelling `Point` in an annotation is refused (see
// TestImportVisibility_UnlistedTypeIsRefused).
func TestImportVisibility_ValueCarriedTypeStaysUsable(t *testing.T) {
	res := analyze(t, buildTree(t, map[string]string{
		"app.lyra": `import shapes.{ mk }
let main = () -> u8 => u8(mk(9).x)`,
		"shapes.lyra": `module shapes
pub struct Point { x: i64 }
pub let mk = pure (n: i64) -> Point => Point { x: n }`,
	}))
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a value-carried type must stay usable without an import; got %v", errs)
	}
}
