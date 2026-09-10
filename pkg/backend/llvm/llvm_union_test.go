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
import bindings.raylib.{ Vector2, Rectangle, Color, rgb, rgba, RED, mouse_position,
                         circle_hits_rect, KEY_ESCAPE }
let main = () -> void => {
  let c = rgb(1, 2, 3)
  let t = rgba(9, 8, 7, 6)
  let r = RED
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

// **The audio bindings**, which are where raylib's aggregates get large: a `Sound` is 40
// bytes and a `Wave` 24, so both cross in memory rather than in registers, and both are
// *returned* that way through an `sret` buffer.
//
// It synthesises a WAV rather than shipping one, which is also what
// `examples/raylib/breakout.lyra` does — and it exercises the trap the binding absorbs:
// raylib's `LoadWaveFromMemory` wants the extension **with** a dot and silently answers an
// empty `Wave` without one, so `wave_from_memory` normalises and both spellings are
// checked here.
//
// No device is opened. `LoadWaveFromMemory` is pure decoding, so this runs on a machine
// with no audio output — which is every CI runner.
func TestExec_RaylibAudioBindings(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ wave_from_memory, unload_wave }
let main = () -> void => {
  let bytes = wav_bytes(880, 40)
  var dotted = "none"
  match wave_from_memory(".wav", bytes) {
    Some(w) => { dotted = "${w.frame_count}/${w.sample_rate}"; unload_wave(w) },
    None => {},
  }
  var bare = "none"
  match wave_from_memory("wav", bytes) {
    Some(w) => { bare = "${w.frame_count}/${w.sample_rate}"; unload_wave(w) },
    None => {},
  }
  var junk = "none"
  match wave_from_memory("wav", [1, 2, 3, 4]) {
    Some(w) => { junk = "decoded"; unload_wave(w) },
    None => { junk = "refused" },
  }
  println("${bytes.len()} ${dotted} ${bare} ${junk}")
}
let wav_bytes = pure (freq: i64, ms: i64) -> []u8 => {
  let rate = 22050
  let frames = rate * ms / 1000
  let period = rate / freq
  var s: []u8 = []
  var i = 0
  for i < frames {
    let amp = 70.0 * (1.0 - f64(i) / f64(frames))
    let v = if (i % period) * 2 < period { 128.0 + amp } else { 128.0 - amp }
    s.push(u8(v.round()))
    i += 1
  }
  var out: []u8 = []
  out = ascii(out, "RIFF")
  out = u32le(out, 36 + s.len())
  out = ascii(out, "WAVE")
  out = ascii(out, "fmt ")
  out = u32le(out, 16)
  out = u16le(out, 1)
  out = u16le(out, 1)
  out = u32le(out, rate)
  out = u32le(out, rate)
  out = u16le(out, 1)
  out = u16le(out, 8)
  out = ascii(out, "data")
  out = u32le(out, s.len())
  var j = 0
  for j < s.len() { out.push(s[j]); j += 1 }
  out
}
let ascii = pure (buf: []u8, s: string) -> []u8 => {
  var out = buf
  for b in s.encode_utf8() { out.push(b) }
  out
}
let u32le = pure (buf: []u8, v: i64) -> []u8 => {
  var out = buf
  out.push(u8(v % 256))
  out.push(u8((v / 256) % 256))
  out.push(u8((v / 65536) % 256))
  out.push(u8((v / 16777216) % 256))
  out
}
let u16le = pure (buf: []u8, v: i64) -> []u8 => {
  var out = buf
  out.push(u8(v % 256))
  out.push(u8((v / 256) % 256))
  out
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("running the raylib-audio binary failed: %v", err)
		}
	}
	// 44-byte header + 882 frames; both spellings decode; junk bytes are refused.
	//
	// The last line only: raylib writes its own INFO/WARNING trace to stdout, and the
	// refused case deliberately provokes one of them.
	want := "926 882/22050 882/22050 refused"
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got := strings.TrimSpace(lines[len(lines)-1]); got != want {
		t.Errorf("raylib audio = %q; want %q", got, want)
	}
}

// **The shapes module's four hard crossings**, which are the ones a wrong ABI breaks
// silently rather than loudly:
//
//   - `collision_rect` **returns** a `Rectangle` by value — sixteen bytes of homogeneous
//     float, so the float registers on aarch64 and an `sret` buffer on x86-64;
//   - `lines_intersect` passes four `Vector2` by value *and* a `^mut Vector2`
//     out-parameter, which the binding turns into a `Maybe`;
//   - `point_in_poly` passes a `[]Vector2`'s buffer as `^Vector2` plus a count;
//   - `circle_hits_rect` puts two different aggregates and a scalar in one call.
//
// All four are pure geometry, so this needs **no window and no display** — which is what
// makes it runnable in CI, where the gallery in `examples/raylib/shapes.lyra` is not.
//
// The expected values follow from the geometry rather than from having run raylib: the
// overlap of (0,0,10,10) and (5,5,10,10) is the square (5,5,5,5), and the diagonals of a
// 10x10 square cross at its centre. A value read off Lyra's own output would assert only
// that Lyra agrees with itself.
func TestExec_RaylibShapeGeometry(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ Vector2, vec2, rect, collision_rect, lines_intersect,
                         point_in_poly, circle_hits_rect, rects_overlap }
let main = () -> void => {
  let a = rect(0.0, 0.0, 10.0, 10.0)
  let b = rect(5.0, 5.0, 10.0, 10.0)
  let o = collision_rect(a, b)
  print("${o.x},${o.y},${o.width},${o.height} ")

  var crossed = "none"
  match lines_intersect(vec2(0.0, 0.0), vec2(10.0, 10.0), vec2(0.0, 10.0), vec2(10.0, 0.0)) {
    Some(h) => { crossed = "${h.x},${h.y}" },
    None => {},
  }
  var parallel = "some"
  match lines_intersect(vec2(0.0, 0.0), vec2(10.0, 0.0), vec2(0.0, 5.0), vec2(10.0, 5.0)) {
    Some(_) => {},
    None => { parallel = "none" },
  }
  print("${crossed} ${parallel} ")

  let square: []Vector2 = [vec2(0.0, 0.0), vec2(10.0, 0.0), vec2(10.0, 10.0), vec2(0.0, 10.0)]
  let empty: []Vector2 = []
  print("${point_in_poly(vec2(5.0, 5.0), square)} ${point_in_poly(vec2(50.0, 5.0), square)} ")
  // The binding answers false where data() would trap on the empty buffer.
  print("${point_in_poly(vec2(5.0, 5.0), empty)} ")

  println("${circle_hits_rect(vec2(5.0, 5.0), 1.0, a)} ${rects_overlap(a, b)}")
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the raylib-geometry binary failed: %v", err)
	}
	want := "5,5,5,5 5,5 none true false false true true"
	if got := strings.TrimSpace(string(raw)); got != want {
		t.Errorf("raylib shape geometry = %q; want %q", got, want)
	}
}

// **The texture module's image half**, which is the half a machine can check: an `Image`
// is pixels in ordinary memory, so none of this needs a window or a GL context. The
// texture half does, and is not tested here for that reason — not because it is less
// important.
//
// Four crossings it pins, each one a shape a wrong ABI breaks silently:
//
//   - `Image` **returned** by value — 24 bytes, a pointer and four ints, so a mixed
//     register class on both targets;
//   - `Color` returned by value from GetImageColor;
//   - `^mut Image` as an in-out parameter, which is how every in-place edit works — and
//     the mutation has to reach the *caller's* binding, which is what `self: mut Image`
//     buys and what the resize assertion below actually establishes;
//   - `^Color` back from LoadImageColors, walked with offset.
//
// The expected values follow from the generated image rather than from raylib: a solid
// 8x4 of (200,100,50) has 32 pixels all of that colour, and a 2x2-checked 4x4 has corners
// that differ.
// `bindings/raylib/image.lyra` — editing an image and painting onto one, all of it on CPU
// pixels and so **headless**, which is the reason this family is worth a behavioural test
// where the texture half cannot have one.
//
// Every expectation follows from the numbers rather than from what the bindings printed:
// a copy is independent of its original, an identity convolution changes nothing, an alpha
// border is the rectangle that was painted, a two-colour image has a two-entry palette.
//
// **The text case asserts a `None`**, and it is the interesting one: raylib loads its
// built-in font in `InitWindow`, so with no window `GetFontDefault()` answers a font with
// a null glyph array and `ImageText` walks it — a pure-C caller exits 139. `image_text`
// gates on `font_valid` and answers `None`, so the one call in this module that is not
// headless says so instead of taking the process down.
func TestExec_RaylibImageEditingAndPainting(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ Image, gen_image_color, unload_image, image_color_at, reformat,
                         image_copy, image_region, image_channel, image_text, fill,
                         paint_pixel, paint_rectangle, paint_image, image_palette,
                         image_alpha_border, alpha_crop, convolve, brightness,
                         replace_color, resize_canvas, to_power_of_two, gen_mipmaps,
                         Vector2, rect, RED, GREEN, BLUE, WHITE, BLANK }

// PIXELFORMAT_UNCOMPRESSED_R8G8B8A8. The paint calls need a format with alpha, and
// gen_image_color answers R8G8B8A8 already — this is here so the test says which format
// its expectations are about rather than inheriting one.
const RGBA8: i32 = 7

let px = pure (im: Image, x: i32, y: i32) -> string => {
  let c = image_color_at(im, x, y)
  "${c.r},${c.g},${c.b},${c.a}"
}

let main = () -> void => {
  var base = gen_image_color(8, 8, RED)
  base.reformat(RGBA8)

  // A copy has pixels of its own: filling one leaves the other alone.
  var dup = image_copy(base)
  dup.fill(GREEN)
  print("${px(base, 0, 0)} ${px(dup, 0, 0)} ")

  let reg = image_region(base, rect(0.0, 0.0, 3.0, 2.0))
  print("${reg.width}x${reg.height} ")

  // Painting: inside the rectangle is blue, outside is untouched.
  var canvas = gen_image_color(16, 16, BLANK)
  canvas.reformat(RGBA8)
  canvas.paint_rectangle(rect(2.0, 2.0, 4.0, 4.0), BLUE)
  canvas.paint_pixel(Vector2 { x: 10.0, y: 10.0 }, GREEN)
  print("${px(canvas, 3, 3)} ${px(canvas, 8, 8)} ${px(canvas, 10, 10)} ")

  // A blit of the whole image onto a blank one reproduces it.
  var dst = gen_image_color(8, 8, BLANK)
  dst.reformat(RGBA8)
  dst.paint_image(base, rect(0.0, 0.0, 8.0, 8.0), rect(0.0, 0.0, 8.0, 8.0), WHITE)
  print("${px(dst, 4, 4)} ")

  // Two colours, two palette entries — raylib pads its answer to the limit and the
  // binding trims to the count it reports.
  var two = gen_image_color(4, 4, RED)
  two.reformat(RGBA8)
  two.paint_rectangle(rect(0.0, 0.0, 2.0, 4.0), BLUE)
  print("${image_palette(two, 8).len()} ")

  // The alpha border is exactly the rectangle painted into a transparent canvas, and
  // alpha_crop shrinks the image to it.
  var sparse = gen_image_color(10, 10, BLANK)
  sparse.reformat(RGBA8)
  sparse.paint_rectangle(rect(3.0, 4.0, 2.0, 3.0), RED)
  let b = image_alpha_border(sparse, 0.5)
  var cropped = image_copy(sparse)
  cropped.alpha_crop(0.5)
  print("${b.x},${b.y},${b.width},${b.height} ${cropped.width}x${cropped.height} ")

  // A 3x3 identity kernel leaves every pixel where it was.
  var conv = image_copy(base)
  conv.convolve([0.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 0.0])
  print("${px(conv, 4, 4)} ")

  var bright = image_copy(base)
  bright.brightness(25)
  var swapped = image_copy(base)
  swapped.replace_color(RED, GREEN)
  print("${px(bright, 0, 0)} ${px(swapped, 0, 0)} ")

  // The canvas grows without scaling: the old pixels sit at the offset, the rest is fill.
  var canv = image_copy(base)
  canv.resize_canvas(12, 12, 2, 2, BLANK)
  print("${canv.width}x${canv.height} ${px(canv, 2, 2)} ${px(canv, 0, 0)} ")

  // 5x3 rounds up to 8x4, and an 8x8 image has four mipmap levels (8, 4, 2, 1).
  var pot = gen_image_color(5, 3, RED)
  pot.to_power_of_two(BLANK)
  var mip = gen_image_color(8, 8, RED)
  mip.gen_mipmaps()
  print("${pot.width}x${pot.height} ${mip.mipmaps} ")

  // Channel 0 on its own is a grayscale image (PIXELFORMAT_UNCOMPRESSED_GRAYSCALE = 1).
  let ch = image_channel(base, 0)
  print("${ch.width}x${ch.height},${ch.format} ")

  // The one call here that needs a window, and it says so rather than crashing.
  println("${image_text("hi", 20, RED).is_none()}")

  unload_image(base) ; unload_image(dup) ; unload_image(reg) ; unload_image(canvas)
  unload_image(dst) ; unload_image(two) ; unload_image(sparse) ; unload_image(cropped)
  unload_image(conv) ; unload_image(bright) ; unload_image(swapped) ; unload_image(canv)
  unload_image(pot) ; unload_image(mip) ; unload_image(ch)
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the raylib-image-editing binary failed: %v", err)
	}
	want := strings.Join([]string{
		"230,41,55,255", "0,228,48,255", // a copy is independent
		"3x2",                                      // image_region
		"0,121,241,255", "0,0,0,0", "0,228,48,255", // paint_rectangle, outside, paint_pixel
		"230,41,55,255",  // paint_image
		"2",              // image_palette
		"3,4,2,3", "2x3", // alpha border, alpha_crop
		"230,41,55,255",                 // identity convolution
		"255,66,80,255", "0,228,48,255", // brightness, replace_color
		"12x12", "230,41,55,255", "0,0,0,0", // resize_canvas
		"8x4", "4", // to_power_of_two, gen_mipmaps
		"8x8,1", // image_channel
		"true",  // image_text is None with no window
	}, " ")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got := strings.TrimSpace(lines[len(lines)-1]); got != want {
		t.Errorf("raylib image editing = %q; want %q", got, want)
	}
}

func TestExec_RaylibImageBindings(t *testing.T) {
	t.Parallel()
	libdir := pkgConfigLibDir(t, "raylib")
	src := `
module main
import bindings.raylib.{ gen_image_color, gen_image_checked, image_valid, unload_image,
                         image_color_at, image_colors, resize, crop, grayscale,
                         rect, rgb, RED, BLUE }
let main = () -> void => {
  var img = gen_image_color(8, 4, rgb(200, 100, 50))
  let c = image_color_at(img, 2, 2)
  print("${image_valid(img)} ${c.r},${c.g},${c.b},${c.a} ")

  let colors = image_colors(img)
  print("${colors.len()} ${colors[0].r == 200 && colors[31].r == 200} ")

  // The in-place edits mutate this binding, not a copy.
  img.resize(16, 8)
  print("${img.width}x${img.height} ")
  img.crop(rect(0.0, 0.0, 4.0, 4.0))
  print("${img.width}x${img.height} ")
  img.grayscale()
  let g = image_color_at(img, 1, 1)
  print("${g.r == g.g && g.g == g.b} ")

  var checks = gen_image_checked(4, 4, 2, 2, RED, BLUE)
  println("${image_color_at(checks, 0, 0).r != image_color_at(checks, 3, 0).r}")
  unload_image(checks)
  unload_image(img)
}
`
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+libdir, "-lraylib")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the raylib-image binary failed: %v", err)
	}
	want := "true 200,100,50,255 32 true 16x8 4x4 true true"
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got := strings.TrimSpace(lines[len(lines)-1]); got != want {
		t.Errorf("raylib image bindings = %q; want %q", got, want)
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
