package llvm

import "testing"

// A constructor imported under an alias is that constructor under the new name, as a type or
// a function imported so is: constructed bare and applied, matched, unwrapped with `if let`,
// and generic. Until 10/19 the alias bound nothing — `Theirs` was lyra-E035 "undefined
// constructor or type" and `Big(3)` "undefined tuple type". The aliases are erased into the
// constructors' own names before any pass runs (driver, eraseConstructorAliases).
func TestExec_AnAliasedConstructorImport(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"owner.lyra": `module owner
pub data Tool = OfMap | Other | Sized(i64)
pub data Opt<t> = Val(t) | Nothing
`,
		"app.lyra": `import owner.{ OfMap as Theirs, Other as Else, Sized as Big, Val as Just, Nothing as Nada, Tool, Opt }
let score = (t: Tool) -> i64 => match t {
  Theirs => 1,
  Else => 2,
  Big(n) => n,
}
let get = (o: Opt<i64>) -> i64 => match o { Just(v) => v, Nada => 0 }
let main = () -> u8 => {
  let ts = [Theirs, Else, Big(10)]
  var sum = score(ts[0]) + score(ts[1]) + score(ts[2])
  if let Big(k) = ts[2] { sum += k }
  let j: Opt<i64> = Just(7)
  let n: Opt<i64> = Nada
  u8(sum + get(j) + get(n))
}
`,
	})
	// 1 + 2 + 10 + 10 + 7 + 0
	if got != 30 {
		t.Errorf("exit = %d, want 30", got)
	}
}
