package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A union lowers to memory, not to an SSA aggregate: every member sits at offset 0, so
// reading one member of a value written as another is a *reinterpretation* of bytes, and
// LLVM has no way to say that about an SSA value. `alloca`, then bitcast the address to a
// pointer to the member's own type.
//
// The layout itself is proved against C in `TestExec_FFIFixture_UnionLayoutMatchesC`;
// what these prove is the behaviour a program can observe.
func TestExec_Union(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The whole point of the type: write one member, read another. The expected
			// bytes are little-endian's, and identical to what the equivalent C program
			// prints — which is the claim, since a union with any other layout would
			// still "work" and answer differently.
			"one member written, another read",
			`union Bits { whole: u32, bytes: [4]u8 }
			 let main = () -> void => {
			   let b = Bits { whole: 0x01020304 }
			   unsafe { println("${b.bytes[0]} ${b.bytes[1]} ${b.bytes[2]} ${b.bytes[3]}") }
			 }`,
			"4 3 2 1",
		},
		{
			// **The unwritten bytes are zero**, which C does not promise and Lyra does.
			// A C union's unwritten bytes are indeterminate, so reading the wrong member
			// gives whatever the stack held — behaviour that changes with the call that
			// ran before it. Zeroing costs one store and makes a wrong read wrong the
			// *same way every time*.
			"the rest of the storage is zeroed",
			`union U { small: u8, big: u64 }
			 let main = () -> void => {
			   let u = U { small: 255 }
			   unsafe { println("${u.big}") }
			 }`,
			"255",
		},
		{
			// The size is the largest member's, so a small member written into a large
			// union leaves the rest addressable — this is the property SDL_Event's
			// `padding[128]` member exists to pin.
			"a padding member fixes the size",
			`union Ev { kind: u32, padding: [64]u8 }
			 let main = () -> void => {
			   let e = Ev { kind: 9 }
			   unsafe { println("${e.kind} ${e.padding[63]}") }
			 }`,
			"9 0",
		},
		{
			// A union in every position that matters — rule 8's probe, kept as a test.
			//
			// A **closure capture** is deliberately not among them, and not because a
			// union cannot be captured: capturing any named aggregate failed in a file
			// that declares `module` ("cannot lower captured binding"), a struct exactly
			// as much as a union, on `main` before this feature existed. Fixed 09/09 and
			// pinned by TestExec_ClosureOverAModuleType, which is where it belongs —
			// the bug was the lifted-lambda lowering path's, not this feature's.
			"in a struct, an array and a return",
			`union U { a: u32, b: f32 }
			 struct Holder { u: U, n: i64 }
			 let make = pure () -> U => U { a: 9 }
			 let main = () -> void => {
			   let h = Holder { u: U { a: 7 }, n: 1 }
			   let xs: []U = [U { a: 1 }, U { a: 2 }]
			   unsafe { println("${make().a} ${h.u.a} ${xs[1].a}") }
			 }`,
			"9 7 2",
		},
		{
			// A member that is itself a struct, read field by field — the shape every
			// real C union takes, and what `SDL_Event.user.code` is.
			"a struct member is read through the union",
			`struct P { a: u32, b: f64 }
			 union U { kind: u32, p: P }
			 let main = () -> void => {
			   let u = U { p: P { a: 4, b: 1.5 } }
			   unsafe { println("${u.p.a} ${u.p.b} ${u.kind}") }
			 }`,
			"4 1.5 4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "module main\n" + tc.src
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// **SDL3, the second real library the FFI has talked to**, and the one that needed a
// union to be reachable at all.
//
// zlib was the first, and it exercised everything the boundary had except an aggregate.
// `SDL_Event` is the shape it had none of: 128 bytes, read as a `Uint32` to find the tag
// and as a struct to read the payload. A fixture whose both sides we wrote cannot make
// this claim — that Lyra talks to a library nobody wrote for it — which is why this test
// exists beside the hermetic fixture one rather than instead of it.
//
// Skipped where SDL3 is absent, which is CI and the Debian container: `-lSDL3` needs a
// package, exactly as `-lz` did, and the suite's standing rule is that the automated half
// must need none. `examples/sdl3.lyra` is the same program to run by hand.
func TestExec_UnionAgainstSDL3(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "sdl3")
	src := `
module main
const SDL_INIT_EVENTS: u32 = 0x00004000
const SDL_EVENT_USER: u32 = 32768
struct SdlUserEvent {
  kind: u32, reserved: u32, timestamp: u64, window_id: u32, code: i32,
  data1: ^u8, data2: ^u8,
}
union SdlEvent { kind: u32, user: SdlUserEvent, padding: [128]u8 }
@link("SDL3") @symbol("SDL_Init")      unsafe extern sdl_init: (flags: u32) -> u8
@link("SDL3") @symbol("SDL_PushEvent") unsafe extern sdl_push: (event: ^mut SdlEvent) -> u8
@link("SDL3") @symbol("SDL_PollEvent") unsafe extern sdl_poll: (event: ^mut SdlEvent) -> u8
@link("SDL3") @symbol("SDL_Quit")      unsafe extern sdl_quit: () -> void
let main = () -> void => {
  let started = unsafe { sdl_init(SDL_INIT_EVENTS) }
  if started == 0 { println("init failed"); return }
  var push = SdlEvent { user: SdlUserEvent {
    kind: SDL_EVENT_USER, reserved: 0, timestamp: 0, window_id: 0,
    code: 4242, data1: nullptr, data2: nullptr,
  } }
  let pushed = unsafe { sdl_push(&mut push) }
  if pushed == 0 { println("push failed"); return }
  var got = SdlEvent { padding: [0; 128] }
  var polling = unsafe { sdl_poll(&mut got) }
  for polling != 0 {
    let kind = unsafe { got.kind }
    if kind == SDL_EVENT_USER { println("${unsafe { got.user.code }}") }
    polling = unsafe { sdl_poll(&mut got) }
  }
  unsafe { sdl_quit() }
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lSDL3")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("running the SDL3 binary failed: %v", err)
		}
	}
	if got := strings.TrimSpace(string(raw)); got != "4242" {
		t.Errorf("SDL3 round trip = %q; want \"4242\"", got)
	}
}

// **raylib, the library struct-by-value exists for.**
//
// SDL3 never needed it — its render API takes `const SDL_FRect *`, so every aggregate
// crosses as a pointer. raylib passes them by value everywhere, and each of its types
// classifies differently: `Color` is four bytes in one integer register, `Vector2` a
// homogeneous float aggregate, `Rectangle` the largest HFA there is, and
// `GetMousePosition` *returns* one.
//
// This calls the real library rather than a fixture, which is the claim a fixture cannot
// make: that Lyra's classifier agrees with a header nobody wrote for it. `GetMousePosition`
// answers (0, 0) with no window open, which is a definite value rather than a crash — and
// a wrong classification here does not answer 0, it faults or returns rubbish.
//
// Skipped where raylib is absent, as the SDL3 and zlib tests skip: the automated suite must
// need no package on either platform. `examples/raylib.lyra` is the windowed version.
func TestExec_ByValueAgainstRaylib(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
struct Vector2 { x: f32, y: f32 }
struct Color { r: u8, g: u8, b: u8, a: u8 }
@symbol("GetMousePosition") unsafe extern mouse_position: () -> Vector2
@symbol("ColorToInt") unsafe extern color_to_int: (c: Color) -> i32
let main = () -> void => unsafe {
  let m = mouse_position()
  println("${m.x} ${m.y} ${color_to_int(Color { r: 1, g: 2, b: 3, a: 4 })}")
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("running the raylib binary failed: %v", err)
		}
	}
	// ColorToInt packs RGBA big-endian: 0x01020304 = 16909060.
	if got := strings.TrimSpace(string(raw)); got != "0 0 16909060" {
		t.Errorf("raylib by-value = %q; want \"0 0 16909060\"", got)
	}
}

// **The raylib bindings, exercised as a program uses them** — through `bindings/raylib`
// rather than through raw externs.
//
// It is a separate test from the one above on purpose: that one pins the *classification*
// against a header, and this one pins that the binding module built on top of it works —
// wrappers converting C's byte-wide `bool`, colours built by `pure` functions because a
// `const` cannot hold a struct, and `Vector2` returned by value through two layers of Lyra.
//
// Headless, so it runs anywhere raylib is installed: no window is opened, and the calls
// used are the ones that need none.
func TestExec_RaylibBindings(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ Vector2, Rectangle, Color, rgb, rgba, red, mouse_position,
                         circle_hits_rect, KEY_ESCAPE }
let main = () -> void => {
  let c = rgb(1, 2, 3)
  let t = rgba(9, 8, 7, 6)
  let r = red()
  let m = mouse_position()
  // Two aggregates by value in one call, which is the shape a game actually uses:
  // CheckCollisionCircleRec(Vector2, float, Rectangle). A wrong classification here is
  // a ball that passes through bricks rather than a crash.
  let brick = Rectangle { x: 100.0, y: 100.0, width: 72.0, height: 24.0 }
  let hit = circle_hits_rect(Vector2 { x: 136.0, y: 112.0 }, 8.0, brick)
  let miss = circle_hits_rect(Vector2 { x: 136.0, y: 80.0 }, 8.0, brick)
  println("${c.r} ${c.g} ${c.b} ${c.a} ${t.a} ${r.r} ${m.x} ${m.y} ${KEY_ESCAPE} ${hit} ${miss}")
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("running the raylib-bindings binary failed: %v", err)
		}
	}
	if got := strings.TrimSpace(string(raw)); got != "1 2 3 255 6 230 0 0 256 true false" {
		t.Errorf("raylib bindings = %q; want \"1 2 3 255 6 230 0 0 256 true false\"", got)
	}
}

// pkgConfigLibDir asks pkg-config where a library lives, skipping when it is absent.
//
// `@link` emits `-lNAME` and nothing more — a search path is a build-system question the
// language deliberately does not answer (todo.md) — so the directory is passed as a `-L`
// at compile time, which is what a real project would do with a `--cc` wrapper.
func pkgConfigLibDir(t *testing.T, lib string) string {
	t.Helper()
	out, err := exec.Command("pkg-config", "--variable=libdir", lib).Output()
	if err != nil {
		t.Skipf("%s is not installed (pkg-config has no %s)", lib, lib)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Skipf("pkg-config reports no libdir for %s", lib)
	}
	return dir
}
