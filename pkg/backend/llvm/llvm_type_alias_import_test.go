package llvm

import "testing"

// A type imported under an alias is the same type wherever the alias is written, a
// declaration's members included: a struct's field, a `data` payload, a nominal type's base,
// a generic type's argument. Until 10/21 a written alias resolved in an annotation and failed
// inside a declaration — `unknown type "B"`, reported at the *imported* type's declaration —
// because a declaration's member names are stamped with the declaration they mean
// (`stampNames`) and then resolved from that declaration's module, which has no `B`.
// Found writing `ui.sdl3`, which imports SDL's `Renderer` beside its own trait of that name.
func TestExec_AnAliasedTypeImportInsideADeclaration(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"owner.lyra": `module owner
pub struct Box { n: i64 }
pub type Handle = i64
pub struct Pair<t> { a: t, b: t }
pub let handle = pure (n: i64) -> Handle => Handle(n)
`,
		"app.lyra": `import owner.{ Box as B, Handle as H, Pair as P, handle }
struct Holder { b: B, h: H, p: P<i64>, bs: []B }
data Slot = Full(B) | Empty
type Id = H
let total = (s: Slot) -> i64 => match s { Full(b) => b.n, Empty => 0 }
let main = () -> u8 => {
  let x = Holder { b: B { n: 3 }, h: handle(4), p: P { a: 5, b: 6 }, bs: [B { n: 7 }] }
  let id: Id = Id(handle(8))
  u8(x.b.n + i64(x.h) + x.p.a + x.p.b + x.bs[0].n + total(Full(B { n: 9 })) + i64(i64(id)))
}
`,
	})
	// 3 + 4 + 5 + 6 + 7 + 9 + 8
	if got != 42 {
		t.Errorf("exit = %d, want 42", got)
	}
}
