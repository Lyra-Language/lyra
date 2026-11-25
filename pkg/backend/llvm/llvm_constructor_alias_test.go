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

// A constructor imported under an alias beside another constructor of its own name — another
// module's, imported bare or under an alias of its own, or this module's — is still the one
// the alias names: built bare and applied, its type inferred, matched, unwrapped with `if
// let`, and each `match` held to its own type's arms. The payloads are alike, so a
// constructor taken for the other would build and match the wrong type's tag in silence.
// Until 11/24 the import was refused (constructorAliasCollisions), since after the alias was
// erased the two were one name; the erased use now records the module it came from, as a
// name written through a namespace does.
func TestExec_AnAliasedConstructorBesideAnotherOfItsName(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"sdl.lyra": `module sdl
pub data Event = KeyDown(i64) | Quit
pub let sdl_code = (e: Event) -> i64 => match e { KeyDown(k) => k, Quit => 0 }
`,
		"ui.lyra": `module ui
pub data Input = KeyDown(i64) | Closed
pub let ui_code = (i: Input) -> i64 => match i { KeyDown(k) => k * 10, Closed => 0 }
`,
		"pad.lyra": `module pad
pub data Button = KeyDown(i64) | Idle
pub let pad_code = (b: Button) -> i64 => match b { KeyDown(k) => k * 100, Idle => 0 }
`,
		"app.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown, Quit, sdl_code }
import ui.{ Input, KeyDown, Closed, ui_code }
import pad.{ Button, KeyDown as PadDown, Idle, pad_code }
let translate = (e: Event) -> Input => match e {
  SdlKeyDown(k) => KeyDown(k + 1),
  Quit => Closed,
}
let press = (e: Event) -> Button => match e {
  SdlKeyDown(k) => PadDown(k),
  Quit => Idle,
}
let main = () -> u8 => {
  let e = SdlKeyDown(2)
  let i = KeyDown(3)
  let b = PadDown(1)
  let events = [SdlKeyDown(4), Quit]
  var sum = sdl_code(e) + ui_code(i) + pad_code(b) + ui_code(translate(e)) + pad_code(press(events[1]))
  if let SdlKeyDown(k) = events[0] { sum += k }
  if let KeyDown(k) = i { sum += k }
  u8(sum)
}
`,
	})
	// 2 + 30 + 100 + 30 + 0 + 4 + 3
	if got != 169 {
		t.Errorf("exit = %d, want 169", got)
	}
}

// The same beside this module's own constructor of the name: the bare name is the module's,
// the alias the import's.
func TestExec_AnAliasedConstructorBesideTheModulesOwnOfItsName(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"sdl.lyra": `module sdl
pub data Event = KeyDown(i64) | Quit
pub let sdl_code = (e: Event) -> i64 => match e { KeyDown(k) => k, Quit => 0 }
`,
		"app.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown, Quit, sdl_code }
data Input = KeyDown(i64) | Closed
let code = (i: Input) -> i64 => match i { KeyDown(k) => k * 10, Closed => 0 }
let translate = (e: Event) -> Input => match e { SdlKeyDown(k) => KeyDown(k), Quit => Closed }
let main = () -> u8 => {
  let e = SdlKeyDown(2)
  let i = KeyDown(3)
  u8(sdl_code(e) + code(i) + code(translate(e)))
}
`,
	})
	// 2 + 30 + 20
	if got != 52 {
		t.Errorf("exit = %d, want 52", got)
	}
}
