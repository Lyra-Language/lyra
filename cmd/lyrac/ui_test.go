package main

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// `ui.sdl3` draws every op of a `ui.draw` display list, checked a pixel at a time in the
// frame `examples/ui/shapes.lyra` writes headless: rounded fills with crisp straight edges
// and clear corners, a hairline and a thick border, a half see-through fill blended over
// what is beneath, a clip that keeps a larger fill to its rectangle, a picture's texels and
// a run of glyphs tinted by their colour. It is the GUI's phase-1 gate — a window draws a
// display list — and the first program to draw through `SDL_RenderGeometry`.
//
// **Edges are checked for being there, not for their exact shade.** SDL's software renderer,
// the one a headless test has, truncates every vertex to a whole pixel, so the band that
// softens an edge (half a pixel either side of it) lands half a pixel out and a hairline
// shows as two half-strength pixels. A GPU renderer keeps the half pixels.
func TestRun_UiDrawsADisplayList(t *testing.T) {
	t.Parallel()
	frame := drawUiExample(t, filepath.Join(repoRoot(t), "examples", "ui", "shapes.lyra"))
	if b := frame.Bounds(); b.Dx() != 360 || b.Dy() != 240 {
		t.Fatalf("the frame is %dx%d, want 360x240 (one pixel a unit)", b.Dx(), b.Dy())
	}
	background := [3]int{242, 242, 245}
	for _, c := range []struct {
		x, y int
		want [3]int
		what string
	}{
		{100, 40, [3]int{255, 255, 255}, "the card"},
		{24, 80, [3]int{255, 255, 255}, "just inside the card's border"},
		{16, 80, background, "just outside the card"},
		{20, 20, background, "the card's rounded corner"},
		{84, 108, [3]int{60, 120, 216}, "the button"},
		{160, 150, [3]int{233, 153, 154}, "half see-through red over the background"},
		{160, 100, [3]int{240, 160, 160}, "half see-through red over the card"},
		{243, 70, [3]int{46, 139, 87}, "the thick ring's band"},
		{290, 70, background, "inside the ring"},
		{290, 180, [3]int{240, 200, 90}, "the clipped fill, inside its clip"},
		{230, 180, background, "the clipped fill, left of its clip"},
		{290, 145, background, "the clipped fill, above its clip"},
		{108, 178, [3]int{32, 32, 32}, "a glyph, tinted"},
		{118, 178, background, "between the glyphs"},
	} {
		if got := rgbAt(frame, c.x, c.y); !near(got, c.want, 3) {
			t.Errorf("%s: (%d, %d) is %v, want %v", c.what, c.x, c.y, got, c.want)
		}
	}
	// Hairlines: darker than both sides, over the pixel or two the renderer puts them in.
	for _, c := range []struct {
		x0, x1, y int
		what      string
	}{{19, 20, 80, "the card's border"}, {3, 4, 120, "the window's frame"}} {
		a, b := rgbAt(frame, c.x0, c.y), rgbAt(frame, c.x1, c.y)
		if min(a[0], b[0]) > 235 {
			t.Errorf("%s does not show: (%d, %d) is %v and (%d, %d) %v", c.what, c.x0, c.y, a, c.x1, c.y, b)
		}
	}
	// The picture: an 8×8 checkerboard 8 units a texel, uploaded `pixelated`, so a texel is
	// its own colour right up to its edge — the last pixel of one and the first of the next
	// (x 27 and 28, y 167 and 168) — where smooth sampling would blend the two into grey.
	for _, c := range []struct {
		x, y  int
		black bool
	}{
		{24, 164, true}, {32, 164, false}, {24, 172, false}, {80, 220, true},
		{27, 164, true}, {28, 164, false}, {24, 167, true}, {24, 168, false},
	} {
		got := rgbAt(frame, c.x, c.y)
		if (c.black && got[0] > 8) || (!c.black && got[0] < 247) {
			t.Errorf("the checkerboard at (%d, %d) is %v, want it %s", c.x, c.y, got,
				map[bool]string{true: "black", false: "white"}[c.black])
		}
	}
}

// The geometry under a display list, which no renderer is needed to check: corners fitted
// to their rectangle as CSS fits them (every radius scaled by one factor until each side
// holds its two), intersection and containment, and the meshes `ui.tess` makes — a fill is
// two contours and a fan, a border four contours and three bands, every index in range,
// and nothing at all for an empty rectangle, a clear colour or a border of no width.
func TestRun_UiGeometry(t *testing.T) {
	t.Parallel()
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.draw.{ Rect, Radii, Point, Color, rgb, rgba, radius }
import ui.tess.{ Mesh, fill, border, quad }

let main = () -> u8 => {
  let r = Radii { top_left: 30.0, top_right: 30.0, bottom_right: 5.0, bottom_left: 0.0 }.fitted(40.0, 100.0)
  println("${r.top_left} ${r.top_right} ${r.bottom_right} ${r.bottom_left}")
  let a = Rect { x: 0.0, y: 0.0, w: 10.0, h: 10.0 }
  let b = Rect { x: 5.0, y: -5.0, w: 10.0, h: 10.0 }
  let i = a.intersect(b)
  println("${i.x} ${i.y} ${i.w} ${i.h} ${a.intersect(Rect { x: 20.0, y: 0.0, w: 1.0, h: 1.0 }).is_empty()}")
  println("${a.contains(Point { x: 0.0, y: 9.5 })} ${a.contains(Point { x: 10.0, y: 5.0 })}")
  let c = rgba(0x11223344)
  println("${c.r} ${c.g} ${c.b} ${c.a} ${rgb(0xFF8000).a}")
  var m = Mesh {}
  fill(m, Rect { x: 0.0, y: 0.0, w: 50.0, h: 30.0 }, radius(8.0), rgb(0x000000))
  let per_fill = m.vertices.len()
  println("fill ${per_fill} ${m.indices.len()} ${in_range(m)}")
  border(m, Rect { x: 0.0, y: 0.0, w: 50.0, h: 30.0 }, radius(8.0), 2.0, rgb(0x000000))
  println("border ${m.vertices.len() - per_fill} ${in_range(m)}")
  var none = Mesh {}
  fill(none, Rect { x: 0.0, y: 0.0, w: 0.0, h: 30.0 }, radius(8.0), rgb(0x000000))
  fill(none, Rect { x: 0.0, y: 0.0, w: 50.0, h: 30.0 }, radius(8.0), Color { r: 0, g: 0, b: 0, a: 0 })
  border(none, Rect { x: 0.0, y: 0.0, w: 50.0, h: 30.0 }, radius(8.0), 0.0, rgb(0x000000))
  println("none ${none.vertices.len()} ${none.indices.len()}")
  quad(none, Rect { x: 1.0, y: 2.0, w: 3.0, h: 4.0 }, Rect { x: 0.0, y: 0.0, w: 1.0, h: 1.0 }, rgb(0xFFFFFF))
  let v = none.vertices[2]
  println("quad ${none.vertices.len()} ${none.indices.len()} ${v.x} ${v.y} ${v.u} ${v.v}")
  0
}

let in_range = (m: Mesh) -> bool =>
  m.indices.len() % 3 == 0 && m.indices.all((i) => i >= 0 && i64(i) < m.vertices.len())
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code != 0 {
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	// The left side, 100 tall, holds 30 + 0; the top, 40 wide, needs 30 + 30, so every
	// radius is two thirds. A corner of 8 is six steps (one every 2.5 pixels of its outer
	// edge), seven points, so a contour is 28: a fill is two contours (56 vertices), a fan of
	// 26 triangles and a band of 56 (246 indices); a border four contours (112).
	want := `20 20 3.3333335 0
5 0 5 5 true
true false
17 34 51 68 255
fill 56 246 true
border 112 true
none 0 0
quad 4 6 4 6 1 1
`
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
}

// drawUiExample builds the `ui` program at src and runs it headless for one frame,
// answering the frame it wrote — skipping without SDL3, or where SDL cannot open its dummy
// window.
func drawUiExample(t *testing.T, src string) image.Image {
	t.Helper()
	frame, _ := drawUiExampleWith(t, src)
	return frame
}

// drawUiExampleWith is drawUiExample with more arguments for the program, answering what it
// printed as well — skipping too where it found no font ("no font").
func drawUiExampleWith(t *testing.T, src string, extra ...string) (image.Image, string) {
	t.Helper()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "ui")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.png")
	cmd := command(t, bin, append([]string{"--frames", "1", "--png", shot}, extra...)...)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("drawing: %v\n%s", err, out)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the frame is not a PNG: %v", err)
	}
	return frame, string(out)
}

func rgbAt(img image.Image, x, y int) [3]int {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

// near reports whether two colours are within tolerance in every channel — a renderer's
// blend rounds as it likes.
func near(a, b [3]int, tolerance int) bool {
	for i := range a {
		if d := a[i] - b[i]; d > tolerance || d < -tolerance {
			return false
		}
	}
	return true
}

// skipWithoutText skips where HarfBuzz or FreeType is not installed: `ui.layout` links both
// through `ui.text`, whether or not a program draws any text.
func skipWithoutText(t *testing.T) {
	t.Helper()
	if err := command(t, "pkg-config", "--exists", "harfbuzz", "freetype2").Run(); err != nil {
		t.Skip("no HarfBuzz or FreeType")
	}
}

// runUiProgram runs a `ui` program written here and answers what it printed.
func runUiProgram(t *testing.T, program string) string {
	t.Helper()
	skipWithoutText(t)
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code != 0 {
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	return stdout
}

// `ui.layout`'s constraints protocol, with no text so no font is needed: a row's growing
// child takes what its fixed ones leave (and two share it by their factors); a column's
// children placed by each main-axis alignment and each cross-axis one, inside padding; a row
// in a column fills its width; a stack sized by its unpositioned child, a positioned one fixed
// by two edges and another by its right and bottom; a node's own padding added to a padding
// element's; centring; a row's child overflowing its width but held to its height; and a
// scroll moving its content up by the offset, clipped, never past the content's end.
func TestRun_UiLayout(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.draw.{ Size }
import ui.text.{ fonts_new }
import ui.style.{ Style, insets }
import ui.layout.{
  Node, NoEvent, Laid, Edges, Constraints, CENTER, Vertical,
  MainStart, MainCenter, MainEnd, SpaceBetween, SpaceAround, SpaceEvenly,
  CrossStart, CrossCenter, CrossEnd, CrossStretch,
  row, column, expanded, stack, positioned, padding, align, sized, scroll,
  layout, tight, loose,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  let box = (w: f32, h: f32) -> Node<NoEvent> => sized(Some(w), Some(h))
  let open = (w: Maybe<f32>, h: Maybe<f32>) -> Node<NoEvent> => sized(w, h)
  let screen = loose(Size { w: 300.0, h: 200.0 })
  // A row: two boxes 10 apart, the middle one growing into the rest.
  show("row", layout(row([box(40.0, 20.0), expanded(box(0.0, 10.0)), box(30.0, 30.0)]).with_style(Style { gap: Some(10.0) }), screen, fonts))
  // Flex factors share the free space 1:3.
  show("flex", layout(row([expanded(box(0.0, 10.0)), expanded(box(0.0, 10.0), 3.0)]), tight(Size { w: 200.0, h: 50.0 }), fonts))
  for along in [MainStart, MainCenter, MainEnd, SpaceBetween, SpaceAround, SpaceEvenly] {
    show("main", layout(column([box(10.0, 20.0), box(10.0, 20.0)]).aligned(along, CrossStart), screen, fonts))
  }
  for cross in [CrossStart, CrossCenter, CrossEnd, CrossStretch] {
    show("cross", layout(column([box(10.0, 20.0), open(None, Some(20.0))]).aligned(MainStart, cross).with_style(Style { padding: Some(insets(5.0)) }), tight(Size { w: 100.0, h: 100.0 }), fonts))
  }
  // A row in a column: unbounded nowhere, so it fills the width.
  show("nest", layout(column([row([box(10.0, 10.0)])]), screen, fonts))
  // Stack: the largest unpositioned child sizes it; positioned ones by their edges.
  show("stack", layout(stack([
    box(100.0, 60.0),
    positioned(Edges { left: Some(10.0), right: Some(20.0), top: Some(5.0), height: Some(8.0) }, open(None, None)),
    positioned(Edges { right: Some(0.0), bottom: Some(0.0) }, box(15.0, 15.0)),
  ]), screen, fonts))
  show("padding", layout(padding(insets(7.0), box(10.0, 10.0)).with_style(Style { padding: Some(insets(3.0)) }), screen, fonts))
  show("align", layout(align(CENTER, box(20.0, 10.0)), tight(Size { w: 100.0, h: 50.0 }), fonts))
  // A row's child is as wide as it likes (it overflows) and only as tall as the row may be.
  show("overflow", layout(row([box(500.0, 500.0)]), screen, fonts))
  // A scroll: its child as tall as it likes, moved up by the offset, never past its end.
  show("scroll", layout(scroll(Vertical, 50.0, column([box(10.0, 80.0), box(10.0, 80.0)])), loose(Size { w: 100.0, h: 100.0 }), fonts))
  show("scrollend", layout(scroll(Vertical, 500.0, column([box(10.0, 80.0), box(10.0, 80.0)])), loose(Size { w: 100.0, h: 100.0 }), fonts))
  let c = Constraints { min_w: 10.0, max_w: 20.0, min_h: 0.0, max_h: 5.0 }
  let s = c.constrain(Size { w: 50.0, h: -3.0 })
  println("constrain ${s.w} ${s.h} ${c.deflate(insets(4.0)).min_w} ${c.deflate(insets(4.0)).max_h}")
  0
}

/// A laid tree on one line: each node's size, and in brackets its children at their offsets.
let show = (name: string, laid: Laid<NoEvent>) -> void => println("${name} ${shape(laid)}")

let shape = pure (laid: Laid<NoEvent>) -> string => {
  var out = "${laid.size.w}x${laid.size.h}"
  if laid.clips { out = out ++ " clip" }
  if laid.children.len() > 0 {
    let inner = [p in laid.children | "${p.at.x},${p.at.y}:${shape(p.laid)}"]
    out = out ++ " [" ++ inner.join(" ") ++ "]"
  }
  out
}
`)
	want := `row 300x30 [0,5:40x20 50,10:210x10 [0,0:210x10] 270,0:30x30]
flex 200x50 [0,20:50x10 [0,0:50x10] 50,20:150x10 [0,0:150x10]]
main 10x200 [0,0:10x20 0,20:10x20]
main 10x200 [0,80:10x20 0,100:10x20]
main 10x200 [0,160:10x20 0,180:10x20]
main 10x200 [0,0:10x20 0,180:10x20]
main 10x200 [0,40:10x20 0,140:10x20]
main 10x200 [0,53.333332:10x20 0,126.66666:10x20]
cross 100x100 [5,5:10x20 5,25:0x20]
cross 100x100 [45,5:10x20 50,25:0x20]
cross 100x100 [85,5:10x20 95,25:0x20]
cross 100x100 [5,5:90x20 5,25:90x20]
nest 300x200 [0,0:300x10 [0,0:10x10]]
stack 100x60 [0,0:100x60 10,5:70x8 [0,0:70x8] 85,45:15x15 [0,0:15x15]]
padding 30x30 [10,10:10x10]
align 100x50 [40,20:20x10]
overflow 300x200 [0,0:500x200]
scroll 10x100 clip [0,-50:10x160 [0,0:10x80 0,80:10x80]]
scrollend 10x100 clip [0,-60:10x160 [0,0:10x80 0,80:10x80]]
constrain 20 0 2 0
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The cascade's order (`ui.style`): within a layer the more specific rule wins and then the
// later; a higher layer beats a lower one whatever its specificity — the theme's elements, the
// global sheets (outermost first), a component's scoped sheet, part rules, inline style. A
// component's sheet reaches a component inside it only at its top-level elements, as Vue's
// scoped styles do — beneath that component's own sheet, and no deeper — and its parts through
// its part rules. Colour, font and opacity are inherited (opacity multiplied), the background is not,
// and a themed subtree takes its theme's text colour. Each rule sets a different gray, so the
// gray an element ends with names the rule that won.
func TestRun_UiCascade(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.draw.{ Size, Color }
import ui.text.{ fonts_new }
import ui.style.{ Style, Sheet, Rule, Theme, light_theme, dark_theme, specificity }
import ui.layout.{ Node, NoEvent, Laid, column, text, themed, component, layout, loose }

let main = () -> u8 => {
  println("specificity ${specificity("Text", "Text", "a b")} ${specificity(".b", "Text", "a b")} ${specificity("Text.a", "Text", "a b")} ${specificity("Row", "Text", "a")} ${specificity(".c", "Text", "a b")} ${specificity("X::p", "Text", "")}")
  var fonts = fonts_new()
  let theme = Theme {
    light_theme() |
    elements: Sheet { rules: [
      rule("Text.layer", 1),
      rule("Card::label", 1),
    ] },
  }
  let globals = Sheet { rules: [
    // Within a layer: a class beats a tag, written before or after it.
    rule(".spec", 2),
    rule("Text", 3),
    // Among equals, the later.
    rule(".order", 4),
    rule(".order", 5),
    // A lower layer's more specific rule loses to this plain tag.
    rule("Text", 6),
    rule(".outer", 7),
    rule("Card::label", 8),
  ] }
  let inner = Sheet { rules: [rule(".outer", 9)] }
  let card_sheet = Sheet { rules: [rule("Text", 10), rule("Badge::dot", 11)] }
  let badge_sheet = Sheet { rules: [rule(".dot", 12), rule(".own", 14)] }
  let tree: Node<NoEvent> = themed(theme, globals, [
    column([
      text("").with_class("spec"),
      text("").with_class("order"),
      text("").with_class("layer"),
      themed(theme, inner, [text("").with_class("outer")]),
      component("Card", card_sheet, [
        // The scoped sheet beats the global sheet's class.
        text("").with_class("scoped spec"),
        // A part rule beats the scoped sheet; the global one, later than the theme's, wins.
        text("").with_class("part").with_part("label"),
        // Inline beats everything.
        text("").with_class("inline").with_part("label").with_style(Style { color: Some(gray(13)) }),
        component("Badge", badge_sheet, [
          // The card's sheet reaches the badge's top-level elements, and no deeper (the global
          // Text rule does); the card's part rule for the badge's dot beats the badge's own.
          text("").with_class("nested"),
          // The badge's own sheet beats the card's, there too.
          text("").with_class("nested own"),
          column([text("").with_class("deeper")]),
          text("").with_class("dot").with_part("dot"),
        ]),
      ]),
      // Inherited: colour, font and opacity; not the background. (Columns, which no rule
      // matches.)
      column([
        blank().with_class("child"),
        column([blank().with_class("grandchild").with_style(Style { opacity: Some(0.5) })])
          .with_style(Style { font_size: Some(30.0) }),
      ]).with_class("parent").with_style(Style {
        color: Some(gray(20)),
        opacity: Some(0.5),
        background: Some(gray(21)),
      }),
      // A theme's subtree takes its text colour again.
      themed(dark_theme(), Sheet {}, [blank().with_class("dark")]),
    ]),
  ])
  report(layout(tree, loose(Size { w: 100.0, h: 100.0 }), fonts))
  0
}

/// An empty column, which no rule matches.
let blank = pure () -> Node<NoEvent> => column([])

/// A rule setting the colour gray n, so which rule won can be read back.
let rule = pure (selector: string, n: u8) -> Rule => Rule { selector: selector, style: Style { color: Some(gray(n)) } }

let gray = pure (n: u8) -> Color => Color { r: n, g: n, b: n }

/// Each element with a class: its colour's gray, font size, opacity and whether it has a
/// background.
let report = (laid: Laid<NoEvent>) -> void => {
  if laid.class.len() > 0 {
    let s = laid.style
    println("${laid.class}: ${s.color.r} ${s.font.size} ${s.opacity} ${s.background.is_some()}")
  }
  for child in laid.children { report(child.laid) }
}
`)
	want := `specificity 1 10 11 0 0 0
spec: 2 14 1 false
order: 5 14 1 false
layer: 6 14 1 false
outer: 9 14 1 false
scoped spec: 10 14 1 false
part: 8 14 1 false
inline: 13 14 1 false
nested: 10 14 1 false
nested own: 14 14 1 false
deeper: 6 14 1 false
dot: 11 14 1 false
parent: 20 14 0.5 true
child: 20 14 0.5 false
grandchild: 20 30 0.25 false
dark: 242 14 1 false
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The phase-2 gate, a static screen laid out (`examples/ui/settings.lyra`), checked where the
// program says it put each element (`--boxes`) and a pixel at a time in its frame — in terms
// that hold for any font: the title's glyphs are inside its box, and a heading's end nowhere
// past its own width; a card is its surface colour inside its border and the background
// outside it; each setting's value ends at its row's end; the toggle's knob is white on the
// accent; a paragraph narrower than its text breaks into lines; and the dark-themed card is
// that theme's surface with light text on it.
func TestRun_UiLaysOutAScreen(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	frame, out := drawUiExampleWith(t, filepath.Join(repoRoot(t), "examples", "ui", "settings.lyra"), "--boxes")
	if b := frame.Bounds(); b.Dx() != 420 || b.Dy() != 560 {
		t.Fatalf("the frame is %dx%d, want 420x560", b.Dx(), b.Dy())
	}
	boxes := map[string][]uiBox{}
	line := regexp.MustCompile(`^(.+) (\S+) (\S+) (\S+) (\S+)$`)
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("not a box: %q", l)
		}
		var v [4]float64
		for i := range v {
			f, err := strconv.ParseFloat(m[i+2], 64)
			if err != nil {
				t.Fatalf("not a box: %q", l)
			}
			v[i] = f
		}
		boxes[m[1]] = append(boxes[m[1]], uiBox{v[0], v[1], v[2], v[3]})
	}
	one := func(class string, n int) uiBox {
		t.Helper()
		if len(boxes[class]) <= n {
			t.Fatalf("no box %d of class %q in\n%s", n, class, out)
		}
		return boxes[class][n]
	}
	background := [3]int{242, 242, 245}
	// The darkest red channel in a box: text is ink, a blank box is its background.
	darkest := func(b uiBox) int {
		least := 255
		for y := int(b.y); y < int(b.y+b.h); y++ {
			for x := int(b.x); x < int(b.x+b.w); x++ {
				least = min(least, rgbAt(frame, x, y)[0])
			}
		}
		return least
	}
	title := one("title", 0)
	if d := darkest(title); d > 80 {
		t.Errorf("the title %v has no glyphs in it (darkest %d)", title, d)
	}
	for x := int(title.x); x < int(title.x+title.w); x++ {
		if got := rgbAt(frame, x, int(title.y)-4); !near(got, background, 3) {
			t.Fatalf("above the title, (%d, %d) is %v: a glyph outside its box", x, int(title.y)-4, got)
		}
	}
	card := one("card", 0)
	mid := int(card.y + card.h/2)
	if got := rgbAt(frame, int(card.x)+4, mid); !near(got, [3]int{255, 255, 255}, 3) {
		t.Errorf("inside the card, (%d, %d) is %v, want its white surface", int(card.x)+4, mid, got)
	}
	if got := rgbAt(frame, int(card.x)-4, mid); !near(got, background, 3) {
		t.Errorf("outside the card, (%d, %d) is %v, want the background", int(card.x)-4, mid, got)
	}
	if a, b := rgbAt(frame, int(card.x), mid), rgbAt(frame, int(card.x)+1, mid); min(a[0], b[0]) > 240 {
		t.Errorf("the card's border does not show at x %d: %v %v", int(card.x), a, b)
	}
	for i, value := range boxes["value"] {
		row := one("setting", i)
		if end, want := value.x+value.w, row.x+row.w; end < want-0.01 || end > want+0.01 {
			t.Errorf("value %d ends at %v, want its row's end %v", i, end, want)
		}
		if d := darkest(value); d > 200 {
			t.Errorf("value %d %v has no glyphs in it (darkest %d)", i, value, d)
		}
	}
	knob := one("knob", 0)
	kx, ky := int(knob.x+knob.w/2), int(knob.y+knob.h/2)
	if got := rgbAt(frame, kx, ky); !near(got, [3]int{255, 255, 255}, 3) {
		t.Errorf("the knob's middle (%d, %d) is %v, want white", kx, ky, got)
	}
	if got := rgbAt(frame, int(knob.x)-6, ky); !near(got, [3]int{10, 132, 255}, 8) {
		t.Errorf("left of the knob, (%d, %d) is %v, want the accent", int(knob.x)-6, ky, got)
	}
	paragraph := one("muted", 0)
	third := paragraph.h / 3
	for _, part := range []uiBox{
		{paragraph.x, paragraph.y, paragraph.w, third},
		{paragraph.x, paragraph.y + 2*third, paragraph.w, third},
	} {
		if d := darkest(part); d > 200 {
			t.Errorf("the paragraph %v is not on three lines: nothing in %v (darkest %d)", paragraph, part, d)
		}
	}
	dark := one("card", 1)
	if got := rgbAt(frame, int(dark.x)+20, int(dark.y+dark.h)-6); !near(got, [3]int{44, 44, 46}, 3) {
		t.Errorf("inside the dark card, (%d, %d) is %v, want its surface", int(dark.x)+20, int(dark.y+dark.h)-6, got)
	}
	heading := one("heading", 0)
	brightest := 0
	for y := int(heading.y); y < int(heading.y+heading.h); y++ {
		for x := int(heading.x); x < int(heading.x+heading.w); x++ {
			brightest = max(brightest, rgbAt(frame, x, y)[0])
		}
		for x := int(heading.x+heading.w) + 2; x < int(dark.x+dark.w)-20; x++ {
			if got := rgbAt(frame, x, y); !near(got, [3]int{44, 44, 46}, 3) {
				t.Fatalf("past the heading's end, (%d, %d) is %v: a glyph outside its box", x, y, got)
			}
		}
	}
	if brightest < 200 {
		t.Errorf("the heading %v has no light glyphs in it (brightest %d)", heading, brightest)
	}
}

// uiBox is where `--boxes` says an element was laid out.
type uiBox struct{ x, y, w, h float64 }

// `bindings.freetype` reads a face's glyph slot and the slot's bitmap at fixed offsets, which
// FreeType's public layout fixes for an LP64 machine; this holds them to a C compiler's
// `offsetof` against the FreeType installed here.
func TestFreetypeOffsets(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	cc := os.Getenv("LYRA_CC")
	if cc == "" {
		cc = "clang"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no %s", cc)
	}
	flags, err := command(t, "pkg-config", "--cflags", "freetype2").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "offsets.c")
	if err := os.WriteFile(src, []byte(`#include <stdio.h>
#include <stddef.h>
#include <ft2build.h>
#include FT_FREETYPE_H
int main(void) {
  printf("FACE_GLYPH %zu\n", offsetof(FT_FaceRec, glyph));
  printf("SLOT_BITMAP %zu\n", offsetof(FT_GlyphSlotRec, bitmap));
  printf("SLOT_BITMAP_LEFT %zu\n", offsetof(FT_GlyphSlotRec, bitmap_left));
  printf("SLOT_BITMAP_TOP %zu\n", offsetof(FT_GlyphSlotRec, bitmap_top));
  return 0;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "offsets")
	args := append(strings.Fields(string(flags)), src, "-o", bin)
	if out, err := command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compiling: %v\n%s", err, out)
	}
	measured, err := command(t, bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := os.ReadFile(filepath.Join(repoRoot(t), "bindings", "freetype.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimSpace(string(measured)), "\n") {
		name, value, _ := strings.Cut(l, " ")
		want := fmt.Sprintf("const %s: u64 = %s\n", name, value)
		if !strings.Contains(string(binding), want) {
			t.Errorf("bindings/freetype.lyra lacks %q: FreeType has it at %s here", strings.TrimSpace(want), value)
		}
	}
}

// `ui.interact` and `ui.app`, driven by scripted input with no window (`ui.script`) and a
// renderer that draws nothing. **Hit-testing**: a click reaches the control under it through a
// scroll's offset (after scrolling it to its end), never what the scroll clips away, and a press
// released over another control presses nothing. **Identity**: a keyed field keeps its focus and
// caret when another is inserted before it, where an unkeyed one's pass to whatever now stands
// at its index; and a field leaving the tree takes its focus and its state with it. **The
// keyboard**: Tab and Shift-Tab through the controls in order, wrapping; Enter and Space press
// a button and tick a box; the arrows step a slider, Home and End take it to its ends. **A text
// field**, an edit at a time: typing, a word back, selecting to the start and deleting it, the
// clipboard's copy, cut and paste, undo twice, deleting a word either way, the selection
// extended a rune at a time — and text input asked for while the field has the focus. **The
// pointer's shape**: an I-beam over a text field and while a selection is dragged out of one,
// the pointing hand over a button, the arrow anywhere else. **A checkbox**: its box bordered ticked or not, and when ticked
// holding the accent a gap inside the border, its corners less round by the inset. **Roles and labels**: each control's role, and its
// label its visible text unless it was given one.
func TestRun_UiInteraction(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, run }
import ui.script.{ ScriptPlatform, script_platform, parse_script }
import ui.interact.{ ui_new, find }
import ui.backend.{ Renderer, Arrow, IBeam, Hand }
import ui.draw.{ Point, Size, Color, TextureId }
import std.collections
import ui.text.{ fonts_new, Fonts }
import ui.layout.{
  Node,
  Laid,
  Role,
  NoRole,
  GroupRole,
  TextRole,
  ImageRole,
  ButtonRole,
  CheckboxRole,
  SliderRole,
  TextFieldRole,
  ScrollRole,
  ComboBoxRole,
  ListBoxRole,
  OptionRole,
  TabListRole,
  TabRole,
  FieldSpec,
  SliderSpec,
  Vertical,
  column,
  sized,
  scroll,
  text,
  button,
  checkbox,
  slider,
  text_input,
}

/// A renderer that draws nothing: the tests read state, not pixels.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Pick(string) | Toggle | Slide(f32) | Typed(string)

struct State {
  picks: []string = [],
  on: bool = false,
  level: f32 = 5.0,
  value: string = "",
}

let main = () -> u8 => {
  var fonts = fonts_new()
  hits(fonts)
  identity(fonts)
  keyboard(fonts)
  editing(fonts)
  cursors(fonts)
  checks(fonts)
  roles(fonts)
  0
}

/// The state a script leaves a view in, run as a program.
let play = (view: (State) -> Node<Ev>, script: string, fonts: mut Fonts) -> (State, ScriptPlatform) => {
  let Ok(inputs) = parse_script(script) else { panic("bad script: ${script}") }
  var platform = script_platform(inputs, Size { w: 300.0, h: 200.0 })
  var renderer = Blank {}
  let app = sandbox(view, update, Color { r: 0, g: 0, b: 0 })
  let last = run(app, State {}, platform, renderer, fonts)
  (last, platform)
}

let update = pure (s: State, e: Ev) -> State => match e {
  Pick(name) => State { s | picks: [...s.picks, name] },
  Toggle => State { s | on: !s.on, picks: [...s.picks, "toggle"] },
  Slide(v) => State { s | level: v },
  Typed(v) => State { s | value: v },
}

/// Hit-testing: a click reaches the control under it, through a scroll's offset, and never
/// what the scroll clips away.
let hits = (fonts: mut Fonts) -> void => {
  let view = (_: State) -> Node<Ev> => column([
    sized(Some(100.0), Some(30.0), [button("A", Pick("a"))]),
    sized(Some(100.0), Some(50.0), [
      scroll(Vertical, 0.0, column([
        sized(Some(100.0), Some(30.0), [button("B", Pick("b"))]),
        sized(Some(100.0), Some(30.0), [button("C", Pick("c"))]),
      ])),
    ]),
  ])
  // A; C at 70 (40 into the scroll); C's rest at 85 is clipped away; scrolled to its end
  // (10), 40 is B and 75 is C; a press on A released over B presses nothing.
  let (s, _) = play(view, "click:10,10 click:10,70 click:10,85 move:10,50 scroll:1 click:10,40 click:10,75 down:10,10 up:10,40", fonts)
  println("hits ${s.picks.join(",")}")
}

/// Identity: a keyed field keeps its focus and caret when one is inserted before it, an
/// unkeyed one's pass to whatever now stands at its index; and an element leaving the tree
/// takes its state with it. (Each field's label is its placeholder, its name.)
let identity = (fonts: mut Fonts) -> void => {
  for keyed in [true, false] {
    var ui = ui_new()
    var platform = script_platform([], Size { w: 300.0, h: 200.0 })
    let size = Size { w: 300.0, h: 200.0 }
    var laid = ui.frame(fields(["b", "c"], keyed), size, fonts)
    let Ok(keys) = parse_script("Tab Tab End Left") else { panic("script") }
    for input in keys {
      let _ = ui.handle(laid, input, platform)
      laid = ui.frame(fields(["b", "c"], keyed), size, fonts)
    }
    let focused = ui.state.focus
    laid = ui.frame(fields(["a", "b", "c"], keyed), size, fonts)
    let now = find(laid, ui.state.focus, Point {}).map((f) => f.laid.label) ?? "none"
    let caret = ui.state.widgets.get(ui.state.focus).map((w) => w.caret) ?? -1
    laid = ui.frame(fields(["a", "b"], keyed), size, fonts)
    let kept = ui.state.widgets.contains_key(focused)
    println("identity keyed=${keyed} ${focused} -> ${now} caret ${caret}; removed: focus '${ui.state.focus}' state kept ${kept}")
  }
}

/// A field for each of the names, its placeholder (its label) the name, keyed by it or not.
let fields = pure (names: []string, keyed: bool) -> Node<Ev> =>
  column([n in names | field(n, keyed)])

let field = pure (name: string, keyed: bool) -> Node<Ev> => {
  let f = text_input(FieldSpec { value: "${name}123", placeholder: name, on_edit: Typed })
  if keyed { f.keyed(name) } else { f }
}

/// The keyboard: Tab and Shift-Tab through the controls in order, wrapping; Enter and Space
/// press; the arrows move a slider by its step, Home and End to its ends.
let keyboard = (fonts: mut Fonts) -> void => {
  let view = (s: State) -> Node<Ev> => column([
    button("A", Pick("a")),
    checkbox("On", s.on, Toggle),
    slider(SliderSpec { value: s.level, min: 0.0, max: 10.0, step: 1.0, on_change: Slide }),
    text_input(FieldSpec { value: s.value, on_edit: Typed }),
    button("B", Pick("b")),
  ])
  let (s, _) = play(view, "Tab Enter Tab Space Tab Right Right Left Shift+Tab Space Tab End Tab Tab Enter Tab Space Shift+Tab Shift+Tab Shift+Tab Home Up", fonts)
  println("keys ${s.picks.join(",")} on ${s.on} level ${s.level}")
}

/// A text field's editing, from the keyboard alone.
let editing = (fonts: mut Fonts) -> void => {
  let view = (s: State) -> Node<Ev> => column([text_input(FieldSpec { value: s.value, on_edit: Typed })])
  let steps = [
    "Tab 'hello world'",
    "Alt+Left 'big '",
    "Shift+Home Backspace",
    "End ' wide'",
    "Primary+a Primary+c End Primary+v",
    "Primary+z",
    "Alt+Backspace",
    "Home Delete Right Shift+Right Shift+Right Primary+x",
    "Primary+z Primary+z",
    "Left Left Ctrl+Delete",
  ]
  var script = ""
  for step in steps {
    script = if script.len() == 0 { step } else { "${script} ${step}" }
    let (s, platform) = play(view, script, fonts)
    println("edit [${step}] '${s.value}' clipboard '${platform.clip}' typing ${platform.typing.is_some()}")
  }
}

/// The pointer's shape: an I-beam over a text field and while dragging a selection out of one,
/// the pointing hand over a button, the arrow over anything else.
let cursors = (fonts: mut Fonts) -> void => {
  let view = (s: State) -> Node<Ev> => column([
    sized(Some(100.0), Some(30.0), [text_input(FieldSpec { value: s.value, on_edit: Typed })]),
    sized(Some(100.0), Some(30.0), [button("B", Pick("b"))]),
  ])
  var shapes: []string = []
  for script in ["move:10,10", "move:10,10 move:10,40", "move:10,40 move:10,10 move:10,100", "move:10,10 down:10,10 move:10,40"] {
    let (_, platform) = play(view, script, fonts)
    shapes.push(if platform.shape == IBeam { "ibeam" } else if platform.shape == Hand { "hand" } else if platform.shape == Arrow { "arrow" } else { "other" })
  }
  println("cursors ${shapes.join(",")}")
}

/// A checkbox's box keeps its border ticked or not, and when ticked holds the theme's accent a
/// gap inside it: the fill inset by the border and the gap, its corners less round by as much.
let checks = (fonts: mut Fonts) -> void => {
  var ui = ui_new()
  for on in [true, false] {
    let view: Node<Ev> = checkbox("Loud", on, Toggle)
    let laid = ui.frame(view, Size { w: 300.0, h: 200.0 }, fonts)
    let b = laid.children[0].laid.children[0].laid.children[0].laid
    let face = b.style.background ?? Color { r: 0, g: 0, b: 0, a: 0 }
    var line = "check ${on} ${b.size.w}x${b.size.h} face ${face.r},${face.g},${face.b} border ${b.style.border_width}"
    for inner in b.children {
      let fill = inner.laid.style.background ?? Color { r: 0, g: 0, b: 0, a: 0 }
      line = "${line}; fill at ${inner.at.x},${inner.at.y} ${inner.laid.size.w}x${inner.laid.size.h} ${fill.r},${fill.g},${fill.b} radius ${inner.laid.style.radius}"
    }
    println(line)
  }
}

/// Every element's role and label, as assistive technology would read them.
let roles = (fonts: mut Fonts) -> void => {
  var ui = ui_new()
  let view: Node<Ev> = column([
    text("Title"),
    button("Save", Pick("save")),
    checkbox("Loud", true, Toggle),
    slider(SliderSpec { value: 1.0, on_change: Slide }).labelled("Volume"),
    text_input(FieldSpec { value: "x", placeholder: "Name", on_edit: Typed }),
    scroll(Vertical, 0.0, sized(Some(10.0), Some(10.0))),
  ])
  let laid = ui.frame(view, Size { w: 300.0, h: 200.0 }, fonts)
  var out: []string = []
  describe(laid, out)
  println("roles ${out.join(" | ")}")
}

let describe = (laid: Laid<Ev>, out: mut []string) -> void => {
  if laid.role != NoRole { out.push("${role_name(laid.role)} '${laid.label}'") }
  for child in laid.children { describe(child.laid, out) }
}

let role_name = pure (role: Role) -> string => match role {
  NoRole => "none",
  GroupRole => "group",
  TextRole => "text",
  ImageRole => "image",
  ButtonRole => "button",
  CheckboxRole => "checkbox",
  SliderRole => "slider",
  TextFieldRole => "field",
  ScrollRole => "scroll",
  ComboBoxRole => "combo box",
  ListBoxRole => "list box",
  OptionRole => "option",
  TabListRole => "tab list",
  TabRole => "tab",
}
`)
	want := `hits a,c,b,c
identity keyed=true #/:c -> c caret 3; removed: focus '' state kept false
identity keyed=false #/1 -> b caret 3; removed: focus '#/1' state kept true
keys a,toggle,toggle,b,a on false level 1
edit [Tab 'hello world'] 'hello world' clipboard '' typing true
edit [Alt+Left 'big '] 'hello big world' clipboard '' typing true
edit [Shift+Home Backspace] 'world' clipboard '' typing true
edit [End ' wide'] 'world wide' clipboard '' typing true
edit [Primary+a Primary+c End Primary+v] 'world wideworld wide' clipboard 'world wide' typing true
edit [Primary+z] 'world wide' clipboard 'world wide' typing true
edit [Alt+Backspace] 'world ' clipboard 'world wide' typing true
edit [Home Delete Right Shift+Right Shift+Right Primary+x] 'od ' clipboard 'rl' typing true
edit [Primary+z Primary+z] 'world ' clipboard 'rl' typing true
edit [Left Left Ctrl+Delete] ' ' clipboard 'rl' typing true
cursors ibeam,hand,arrow,ibeam
check true 18x18 face 255,255,255 border 1.5; fill at 3.5,3.5 11x11 10,132,255 radius 1.5
check false 18x18 face 255,255,255 border 1.5
roles group '' | text 'Title' | button 'Save' | text 'Save' | checkbox 'Loud' | group '' | text 'Loud' | slider 'Volume' | group '' | field 'Name' | scroll ''
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Phase 3's gate: a form that works from the keyboard alone (`examples/ui/form.lyra`, played
// by its own `--script`) — Tab into the name field and type, Tab to the email and type, Tab to
// the checkbox and tick it with Space, Tab to the slider and step it twice, Tab to Submit and
// press Enter — leaves the state a user would have, and the last frame is drawn.
func TestRun_UiFormFromTheKeyboard(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "form")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "form.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "form.png")
	cmd := command(t, bin, "--script",
		"Tab 'Ada' Tab 'ada@example.com' Tab Space Tab Right Right Tab Enter", "--png", shot)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	want := `name: Ada
email: ada@example.com
newsletter: true
volume: 7
result: Thanks, Ada: ada@example.com, with the newsletter, volume 7.
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the frame is not a PNG: %v", err)
	}
	if b := frame.Bounds(); b.Dx() != 420 || b.Dy() != 460 {
		t.Fatalf("the frame is %dx%d, want 420x460", b.Dx(), b.Dy())
	}
	// The ticked checkbox, wherever the font put it: the first accent pixel down the form's
	// left edge is its fill's corner. The fill's middle is the accent, and just left of it —
	// between the fill and the box's border — is the box's white surface: the gap.
	accent := [3]int{10, 132, 255}
	left, top := -1, -1
	for y := 0; y < 460 && top < 0; y++ {
		for x := 20; x < 45; x++ {
			if near(rgbAt(frame, x, y), accent, 8) {
				left, top = x, y
				break
			}
		}
	}
	if top < 0 {
		t.Fatal("no accent down the form's left edge: the checkbox is not ticked, or not drawn")
	}
	right, bottom := left, top
	for near(rgbAt(frame, right+1, top+2), accent, 8) {
		right++
	}
	for near(rgbAt(frame, left+2, bottom+1), accent, 8) {
		bottom++
	}
	midX, midY := (left+right)/2, (top+bottom)/2
	if got := rgbAt(frame, midX, midY); !near(got, accent, 8) {
		t.Errorf("the checkbox's fill middle (%d, %d) is %v, want the accent", midX, midY, got)
	}
	gap := false
	for d := 1; d <= 3; d++ {
		gap = gap || near(rgbAt(frame, left-d, midY), [3]int{255, 255, 255}, 12)
	}
	if !gap {
		t.Errorf("left of the checkbox's fill at (%d, %d), no white surface before the border: %v %v %v",
			left, midY, rgbAt(frame, left-1, midY), rgbAt(frame, left-2, midY), rgbAt(frame, left-3, midY))
	}
	if w := right - left + 1; w < 9 || w > 13 {
		t.Errorf("the checkbox's fill is %d pixels wide, want about 11 (18 less the border and gap each side)", w)
	}
}

// Commands and subscriptions (`ui.app`), played on a script's clock with no window. **A timer**
// (`After`) fires once at its moment, among the ticks of an `Every` that runs only while the
// state subscribes to it. **The window's size and the system's look** are heard while
// subscribed and not after. **A file written** comes back as its event, whose update reads it
// back; **the clipboard** is written and read. **A frame subscription** delivers a frame each
// pass while on, their times adding up to the time passed. **`Node.map`** lifts a component's
// own event into the program's. **`QuitApp`** ends the program before the script does, and a
// web address asked for is opened.
func TestRun_UiCommandsAndSubscriptions(t *testing.T) {
	t.Parallel()
	setenv(t, "UI_TMP", t.TempDir())
	got := runUiProgram(t, `import ui.app.{
  App,
  Command,
  Subscription,
  WriteFile,
  ReadFile,
  After,
  CopyText,
  PasteText,
  OpenUrl,
  QuitApp,
  EveryFrame,
  Every,
  OnResize,
  OnAppearance,
  run,
}
import ui.script.{ timed_platform, parse_steps }
import ui.backend.{ Renderer, Appearance, DarkMode }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, column, button }
import std.env.{ lookup }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Msg =
  Start
  | Stop
  | Tick
  | Delayed
  | Save
  | Saved(bool)
  | Loaded(Maybe<string>)
  | Copy
  | Pasted(string)
  | Animate
  | Frame(f32)
  | Resize(Size)
  | Look(Appearance)
  | Child(Poke)
  | Bye

/// A component with events of its own, lifted into the program's by map.
data Poke = Poked

struct State {
  running: bool = false,
  animating: bool = false,
  frames: i64 = 0,
  elapsed: f32 = 0.0,
  log: []string = [],
  file: string,
}

let main = () -> u8 => {
  let file = "${lookup("UI_TMP") ?? "/tmp"}/saved.txt"
  // Buttons in order: start, stop, save, copy, animate, child's poke, quit — Tab reaches
  // the first, and each further Tab the next. The last Tab and Enter never play: quit ends
  // the program first.
  let script = [
    "Tab Enter",
    "wait:1000",
    "resize:320,240 dark",
    "Tab Enter",
    "wait:1000 light",
    "Tab Enter",
    "Tab Enter",
    "Tab Enter wait:100 Enter",
    "Tab Enter",
    "Tab Enter Tab Enter",
  ].join(" ")
  let Ok(steps) = parse_steps(script) else { panic("script") }
  var platform = timed_platform(steps, Size { w: 300.0, h: 200.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = App { view: view, update: update, subscriptions: subscriptions, background: Color { r: 0, g: 0, b: 0 } }
  let last = run(app, State { file: file }, platform, renderer, fonts)
  for line in last.log { println(line) }
  println("frames ${last.frames} over ${last.elapsed}ms; clock ${platform.clock}; script ${platform.next} of ${platform.steps.len()}; opened ${platform.opened.join(",")}")
  0
}

let view = pure (_: State) -> Node<Msg> => column([
  button("start", Start),
  button("stop", Stop),
  button("save", Save),
  button("copy", Copy),
  button("animate", Animate),
  child().map(Child),
  button("quit", Bye),
])

/// A part of the interface written for its own events.
let child = pure () -> Node<Poke> => column([button("poke", Poked)])

let update = pure (s: State, msg: Msg) -> (State, []Command<Msg>) => match msg {
  Start => (said(State { s | running: true }, "start"), [After(500, Delayed)]),
  Stop => (said(State { s | running: false }, "stop"), []),
  Tick => (said(s, "tick"), []),
  Delayed => (said(s, "delayed"), []),
  Save => (said(s, "save"), [WriteFile(s.file, "hello", Saved)]),
  Saved(ok) => (said(s, "saved ${ok}"), [ReadFile(s.file, Loaded)]),
  Loaded(text) => (said(s, "loaded ${text ?? "nothing"}"), []),
  Copy => (said(s, "copy"), [CopyText("clipped"), PasteText(Pasted)]),
  Pasted(text) => (said(s, "pasted ${text}"), []),
  Animate => (said(State { s | animating: !s.animating }, "animate ${!s.animating}"), []),
  Frame(ms) => (State { s | frames: s.frames + 1, elapsed: s.elapsed + ms }, []),
  Resize(size) => (said(s, "resized ${size.w}x${size.h}"), []),
  Look(look) => (said(s, "look ${if look == DarkMode { "dark" } else { "light" }}"), []),
  Child(Poked) => (said(s, "child poked"), []),
  Bye => (said(s, "bye"), [OpenUrl("https://example.com"), QuitApp]),
}

let said = pure (s: State, what: string) -> State => State { s | log: [...s.log, what] }

/// Ticks every 200 ms, the window's size and the system's look while running; frames while
/// animating.
let subscriptions = pure (s: State) -> []Subscription<Msg> => {
  var subs: []Subscription<Msg> = []
  if s.running {
    subs.push(Every(200, Tick))
    subs.push(OnResize(Resize))
    subs.push(OnAppearance(Look))
  }
  if s.animating { subs.push(EveryFrame(Frame)) }
  subs
}
`)
	want := `start
tick
tick
delayed
tick
tick
tick
resized 320x240
look dark
stop
save
saved true
loaded hello
copy
pasted clipped
animate true
animate false
child poked
bye
frames 8 over 100ms; clock 2100; script 21 of 23; opened https://example.com
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Phase 4's gate: the bindings.imgui.ui example's interface, ported to the ui target as
// markup (`examples/ui/components_view.lui`), runs from the keyboard with its commands and
// timer: a slot loaded, the name typed over and saved — a file written, its result an event —
// the slider stepped, a swatch picked, Sound turned off, a reset asked for and confirmed, and
// the notice it showed cleared by its timer two seconds on.
func TestRun_UiComponentsPort(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "components")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "components.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	cmd := command(t, bin, "--script",
		"Tab Tab Tab Enter Tab Tab Tab 'Ada' Tab Enter Tab Right Right Tab Tab Enter Tab Tab Space Tab Tab Enter Shift+Tab Shift+Tab Enter wait:2500",
		"--png", filepath.Join(dir, "end.png"), "--save-to", filepath.Join(dir, "name.txt"))
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	want := `name: Ada (saved: Ada, 1 commits)
sound: false, music: false, volume: 0.6
resets: 1, loaded: 1, accent: 3050327
notice: 
quit: false
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, "name.txt")); err != nil || string(saved) != "Ada" {
		t.Errorf("name.txt is %q (%v), want the saved name", saved, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "end.png")); err != nil {
		t.Errorf("no last frame: %v", err)
	}
}

// Phase 5's layout cache and redraw on demand. **The cache**: five elements laid out afresh,
// then — the view unchanged — the root given back whole; one paragraph changed, and only its
// path and itself laid out, its siblings given back; another theme, which is in the key, and
// everything laid out once; then that again, given back whole. **An idle host** draws once and
// then waits through fifteen seconds drawing nothing, until a key — and draws once more.
func TestRun_UiCacheAndIdle(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, Command, Subscription, host_of }
import ui.script.{ timed_platform, Waited, Played }
import ui.backend.{ Renderer, KeyPressed, Tab, Modifiers }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Theme, Sheet, light_theme, dark_theme }
import ui.layout.{
  Node,
  NoEvent,
  LayoutCache,
  layout_cache,
  layout_cached,
  loose,
  interaction_new,
  column,
  text,
  themed,
  button,
}

/// A renderer that draws nothing, counting the frames it is given.
struct Blank {
  textures: i64 = 0,
  drawn: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => { self.drawn += 1 }
}

let main = () -> u8 => {
  cached()
  idle()
  0
}

/// Five elements — a theme, a column, three paragraphs — laid out five times: afresh; the same
/// again (the theme given back whole); one paragraph changed (its path and itself, its two
/// siblings given back); in another theme (everything, the theme being in the key); and that
/// again (given back whole).
let cached = () -> void => {
  var fonts = fonts_new()
  var cache: LayoutCache<NoEvent> = layout_cache()
  let size = loose(Size { w: 200.0, h: 200.0 })
  for (theme, words) in [
    (light_theme(), ["a", "b", "c"]),
    (light_theme(), ["a", "b", "c"]),
    (light_theme(), ["a", "B", "c"]),
    (dark_theme(), ["a", "B", "c"]),
    (dark_theme(), ["a", "B", "c"]),
  ] {
    let _ = layout_cached(view(theme, words), size, fonts, interaction_new(), cache)
    println("cache laid out ${cache.laid_out}, given back ${cache.reused}")
  }
}

let view = pure (theme: Theme, words: []string) -> Node<NoEvent> =>
  themed(theme, Sheet {}, [column([w in words | text(w)])])

data Ev = Pressed

/// An idle host draws once and then waits — however long — drawing nothing until an input
/// comes; then once more.
let idle = () -> void => {
  var platform = timed_platform(
    [Waited(5000), Waited(5000), Played(KeyPressed(Tab, Modifiers {})), Waited(5000)],
    Size { w: 200.0, h: 200.0 }
  )
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = App { view: idle_view, update: idle_update, subscriptions: no_subs, background: Color { r: 0, g: 0, b: 0 } }
  var host = host_of(app, 0, platform)
  var passes = 0
  while host.step(platform, renderer, fonts) { passes += 1 }
  println("idle: ${passes} passes over ${platform.clock} ms, ${renderer.drawn} frames drawn, ${host.layouts} layouts")
}

let idle_view = pure (n: i64) -> Node<Ev> => column([text("${n}"), button("go", Pressed)])

let idle_update = pure (n: i64, _: Ev) -> (i64, []Command<Ev>) => (n + 1, [])

let no_subs = pure (_: i64) -> []Subscription<Ev> => []
`)
	want := `cache laid out 5, given back 0
cache laid out 0, given back 1
cache laid out 3, given back 2
cache laid out 5, given back 0
cache laid out 0, given back 1
idle: 4 passes over 15000 ms, 2 frames drawn, 3 layouts
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Phase 5's render batching (`ui.sdl3`): ten boxes each with a label are two geometry calls,
// the boxes and the labels; inside a scroll's clip, still two; and a box drawn over a label
// stays over it — a third call, not merged into the box under the label. The pixel tests above
// hold that batching draws what it drew before.
func TestRun_UiBatchesGeometryCalls(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	setenv(t, "SDL_RENDER_DRIVER", "software")
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.sdl3
import ui.sdl3.{ geometry_calls }
import ui.draw.{ Point, Size, Color, DrawOp, Rect, Fill, radius, rgb }
import ui.text.{ fonts_new, glyph_atlas }
import ui.style.{ Style, insets }
import ui.layout.{ Node, NoEvent, column, row, text, scroll, Vertical, layout, loose, paint }

/// Ten boxes, each with a label in it: two calls, the boxes and the labels; the same in a
/// scroll, whose clip begins and ends a run of calls; and two boxes overlapping a label, the
/// second drawn over it — which must stay over it, so it cannot join the first.
let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() {
    println("no font")
    return 1
  }
  let Ok(window) = sdl3.open("batch", 400.0, 600.0) else {
    println("SDL could not open a window")
    return 2
  }
  var platform = window.platform
  var renderer = window.renderer
  var atlas = glyph_atlas()
  let boxes: Node<NoEvent> = column([i in 0..<10 | labelled_box("item ${i}")])
  let scrolled: Node<NoEvent> = scroll(Vertical, 0.0, boxes)
  for (name, view) in [("boxes", boxes), ("scrolled", scrolled)] {
    let laid = layout(view, loose(Size { w: 400.0, h: 600.0 }), fonts)
    let ops = paint(laid, Point {}, 1.0, fonts, atlas, renderer)
    println("${name}: ${ops.len()} ops, ${geometry_calls(renderer, rgb(0xFFFFFF), ops)} calls")
  }
  // A box, a label over it, and a box over the label: the second box may not join the first.
  let laid = layout(labelled_box("under"), loose(Size { w: 400.0, h: 600.0 }), fonts)
  let over = Fill(Rect { x: 0.0, y: 0.0, w: 60.0, h: 30.0 }, radius(0.0), rgb(0xFF0000))
  let ops = [...paint(laid, Point {}, 1.0, fonts, atlas, renderer), over]
  println("overlapping: ${ops.len()} ops, ${geometry_calls(renderer, rgb(0xFFFFFF), ops)} calls")
  sdl3.close(platform, renderer)
  fonts.close()
  0
}

let labelled_box = pure (label: string) -> Node<NoEvent> =>
  row([text(label)]).with_style(Style {
    background: Some(rgb(0xDDE6F5)),
    padding: Some(insets(8.0)),
    radius: Some(6.0),
  })
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code == 1 && strings.HasPrefix(stdout, "no font") {
		t.Skip("no font here")
	}
	if code == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", stdout)
	}
	if code != 0 {
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	want := `boxes: 20 ops, 2 calls
scrolled: 22 ops, 2 calls
overlapping: 3 ops, 3 calls
`
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
}

// Phase 5's accessibility tree (`ui.access`), built from a form as the host hands it to the
// platform: a node for each paragraph and control — none for the rows and columns between —
// a field labelled by its `label`, a checkbox ticked or not, a slider's number and range, and
// the actions each offers (1 focus, 2 click, 4 set value, 8/16 step). Then what VoiceOver
// does, as `AccessAction` inputs found by label: the name set, the box ticked, the slider
// stepped twice and set outright (as a number, and as text, which is stepped too and ignored
// when it is no number), Send pressed — the events the keyboard would raise — and an action
// for an element no longer there ignored.
func TestRun_UiAccessibilityTree(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, host_of }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ AxWindow, AxGroup, AxLabel, AxImage, AxButton, AxCheckbox, AxSlider, AxTextInput, AxScroll, AxComboBox, AxListBox, AxOption, AxTabList, AxTab, AxContainer, Renderer, InputEvent, AccessAction, AccessTree, AccessNode, AxSetValue, AxClick, AxIncrement, AxFocus, AxSetNumber }
import ui.access.{ access_node }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new, Fonts }
import ui.layout.{ Node, FieldSpec, SliderSpec, column, row, text, text_input, checkbox, slider, button }
import std.collections.{ HashMap, hashmap_new }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

struct Form {
  name: string = "",
  ticked: bool = false,
  volume: f32 = 5.0,
  sent: string = "",
}

data Ev = Named(string) | Ticked | Moved(f32) | Sent

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  // The tree, as the form first shows.
  let first = play([], fonts)
  print_tree(first.0, first.0.root, 0)
  // Each control by its label, as VoiceOver would find it.
  var keys: HashMap<string, string> = hashmap_new()
  for node in first.0.nodes { keys.insert(node.label, node.key) }
  let name = keys.get("Name") ?? ""
  let box = keys.get("Tick me") ?? ""
  let volume = keys.get("Volume") ?? ""
  let send = keys.get("Send") ?? ""
  let (tree, form) = play([
    AccessAction(name, AxFocus),
    AccessAction(name, AxSetValue("Ada")),
    AccessAction(box, AxClick),
    AccessAction(volume, AxIncrement),
    AccessAction(volume, AxIncrement),
    AccessAction(send, AxClick),
  ], fonts)
  println("after: name ${form.name}, ticked ${form.ticked}, volume ${form.volume}, sent ${form.sent}")
  println("focus on Name: ${access_node(tree, name).map((n) => n.id) == Some(tree.focus)}")
  let (_, set) = play([AccessAction(volume, AxSetNumber(9.4)), AccessAction("nothing", AxClick)], fonts)
  println("set number: volume ${set.volume}")
  let (_, as_text) = play([AccessAction(volume, AxSetValue(" 6.6 "))], fonts)
  let (_, not_a_number) = play([AccessAction(volume, AxSetValue("loud"))], fonts)
  println("set as text: volume ${as_text.volume}, and as text that is no number ${not_a_number.volume}")
  fonts.close()
  0
}

/// The form after these inputs, and the tree it last gave the platform.
let play = (inputs: []InputEvent, fonts: mut Fonts) -> (AccessTree, Form) => {
  var platform = script_platform(inputs, Size { w: 300.0, h: 300.0 })
  var renderer = Blank {}
  let app = sandbox(view, update, Color { r: 255, g: 255, b: 255 })
  var host = host_of(app, Form {}, platform)
  while host.step(platform, renderer, fonts) {}
  (platform.access, host.state)
}

let view = pure (form: Form) -> Node<Ev> => column([
  text("Sign up"),
  column([text("Your name"), Node { text_input(FieldSpec { value: form.name, placeholder: "Ada", on_edit: Named }) | label: "Name" }]),
  checkbox("Tick me", form.ticked, Ticked),
  row([text("Volume"), Node { slider(SliderSpec { value: form.volume, min: 0.0, max: 10.0, step: 1.0, on_change: Moved }) | label: "Volume" }]),
  button("Send", Sent),
])

let update = pure (form: Form, ev: Ev) -> Form => match ev {
  Named(n) => Form { form | name: n },
  Ticked => Form { form | ticked: !form.ticked },
  Moved(v) => Form { form | volume: v },
  Sent => Form { form | sent: "yes" },
}

let print_tree = (tree: AccessTree, id: u64, depth: i64) -> void => {
  for node in tree.nodes {
    if node.id != id { continue }
    var indent = ""
    for _ in 0..<depth { indent = "${indent}  " }
    var line = "${indent}${role_name(node)}"
    if node.label.len() > 0 { line = "${line} label=${node.label}" }
    if node.value.len() > 0 { line = "${line} value=${node.value}" }
    if let Some(t) = node.toggled { line = "${line} toggled=${t}" }
    if let Some((v, a, b, s)) = node.numeric { line = "${line} numeric=${v} in ${a}..${b} by ${s}" }
    if node.actions != 0 { line = "${line} actions=${node.actions}" }
    // Where it is depends on the font; that it is somewhere does not.
    if node.bounds.w <= 0.0 || node.bounds.h <= 0.0 { line = "${line} NO BOUNDS" }
    println(line)
    for child in node.children { print_tree(tree, child, depth + 1) }
  }
}

let role_name = pure (node: AccessNode) -> string => match node.role {
  AxWindow => "window",
  AxGroup => "group",
  AxLabel => "label",
  AxImage => "image",
  AxButton => "button",
  AxCheckbox => "checkbox",
  AxSlider => "slider",
  AxTextInput => "text input",
  AxScroll => "scroll",
  AxComboBox => "combo box",
  AxListBox => "list box",
  AxOption => "option",
  AxTabList => "tab list",
  AxTab => "tab",
  AxContainer => "container",
}
`)
	want := `window
  label value=Sign up
  label value=Your name
  text input label=Name actions=5
  checkbox label=Tick me toggled=false actions=3
  label value=Volume
  slider label=Volume numeric=5 in 0..10 by 1 actions=29
  button label=Send actions=3
after: name Ada, ticked true, volume 7, sent yes
focus on Name: true
set number: volume 9
set as text: volume 7, and as text that is no number 5
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// AccessKit's macOS adapter takes a node's bounds in the window's pixels and divides by the
// backing scale itself, so ui.sdl3 scales the tree's logical bounds at the seam: at scale 2 a
// node's position and size double, at 1 they stay. Given points instead, VoiceOver's
// highlight was half size on a Retina display (10/21).
func TestRun_UiAccessBoundsArePixels(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.sdl3.{ access_bounds }
import ui.draw.{ Rect }

let main = () -> u8 => {
  let node = Rect { x: 24.0, y: 60.5, w: 300.0, h: 30.0 }
  let scales: []f32 = [1.0, 2.0, 1.5]
  for scale in scales {
    let at = access_bounds(node, scale)
    println("at ${scale}: ${at.x},${at.y} ${at.w}x${at.h}")
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code != 0 {
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	want := "at 1: 24,60.5 300x30\nat 2: 48,121 600x60\nat 1.5: 36,90.75 450x45\n"
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
}

// ui.layout's flex container against CSS: each line is what a browser lays out for the same
// boxes (worked by hand from CSS Flexbox §9). Justify's six values sharing 120 over three
// items; grow from a zero basis with one item held to its max-width, the other taking the
// rest; shrink with one held at its min-width and one not shrinking; shrink in proportion to
// the base; wrap with gaps, lines stretched and centred by align-content in a fixed height,
// and wrap-reverse; order; row-reverse; a column of fixed height whose item grows; align-items
// center; and the automatic least size — an item no narrower than its min-content.
func TestRun_UiFlex(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.draw.{ Size }
import ui.text.{ fonts_new, Fonts }
import ui.style.{
  Style,
  FlexRowReverse,
  FlexColumn,
  Wrap,
  WrapReverse,
  Justify,
  JustifyStart,
  JustifyEnd,
  JustifyCenter,
  JustifyBetween,
  JustifyAround,
  JustifyEvenly,
  AlignCenter,
  BasisLength,
}
import ui.layout.{ Node, NoEvent, Laid, flex, sized, layout, loose }

let main = () -> u8 => {
  var fonts = fonts_new()
  // Justify: 50 + 80 + 50 in 300 leaves 120.
  for (name, how) in [
    ("start", JustifyStart),
    ("end", JustifyEnd),
    ("center", JustifyCenter),
    ("between", JustifyBetween),
    ("around", JustifyAround),
    ("evenly", JustifyEvenly),
  ] {
    show(name, flex([block("a", 50.0, 20.0), block("b", 80.0, 20.0), block("c", 50.0, 20.0)]).with_style(Style { justify_content: Some(how) }), fonts)
  }
  // Grow from a basis of 0, the second held to 50: the first takes the rest.
  show("grow clamped", flex([
    grown("a", 1.0, Style {}),
    grown("b", 1.0, Style { max_width: Some(50.0) }),
    block("c", 100.0, 20.0),
  ]), fonts)
  // 500 into 300, c not shrinking: a held to 150 by its least, b takes the rest of the cut.
  show("shrink clamped", flex([
    block("a", 200.0, 20.0).with_style(Style { min_width: Some(150.0) }),
    block("b", 200.0, 20.0),
    block("c", 100.0, 20.0).with_style(Style { shrink: Some(0.0) }),
  ]), fonts)
  // Shrinking is in proportion to the base: 100 and 300 give up 25 and 75.
  show("shrink weighted", flex([block("a", 100.0, 20.0), block("b", 300.0, 20.0)]), fonts)
  let five = [i in 0..<5 | block("${i}", 100.0, 20.0)]
  show("wrap", flex(five).with_style(Style { flex_wrap: Some(Wrap), gap: Some(10.0) }), fonts)
  show("wrap stretched lines", flex(five).with_style(Style { flex_wrap: Some(Wrap), gap: Some(10.0), height: Some(140.0) }), fonts)
  show("wrap centred lines", flex(five).with_style(Style { flex_wrap: Some(Wrap), gap: Some(10.0), height: Some(140.0), align_content: Some(JustifyCenter) }), fonts)
  show("wrap-reverse", flex(five).with_style(Style { flex_wrap: Some(WrapReverse), gap: Some(10.0) }), fonts)
  show("order", flex([
    block("a", 10.0, 20.0).with_style(Style { order: Some(2) }),
    block("b", 20.0, 20.0),
    block("c", 30.0, 20.0).with_style(Style { order: Some(1) }),
  ]), fonts)
  show("row-reverse", flex([block("a", 50.0, 20.0), block("b", 80.0, 20.0)]).with_style(Style { flex_direction: Some(FlexRowReverse) }), fonts)
  // A column 200 tall: a grows into what b leaves, both as wide as the column.
  show("column", flex([
    sized::<NoEvent>(None, None).keyed("a").with_style(Style { height: Some(50.0), grow: Some(1.0) }),
    sized::<NoEvent>(None, None).keyed("b").with_style(Style { height: Some(30.0) }),
  ]).with_style(Style { flex_direction: Some(FlexColumn), height: Some(200.0) }), fonts)
  show("align center", flex([block("a", 50.0, 20.0), block("b", 50.0, 40.0)]).with_style(Style { height: Some(100.0), align_items: Some(AlignCenter) }), fonts)
  // An item no narrower than its min-content: a wrapping row of 60 and 40 shrinks to 60, not 30.
  show("automatic least", flex([
    flex([block("w1", 60.0, 10.0), block("w2", 40.0, 10.0)]).keyed("words").with_style(Style { flex_wrap: Some(Wrap) }),
    block("x", 50.0, 10.0).with_style(Style { shrink: Some(0.0) }),
  ]).with_style(Style { width: Some(80.0) }), fonts)
  0
}

let block = pure (key: string, w: f32, h: f32) -> Node<NoEvent> =>
  sized::<NoEvent>(None, None).keyed(key).with_style(Style { width: Some(w), height: Some(h) })

let grown = pure (key: string, grow: f32, style: Style) -> Node<NoEvent> =>
  sized::<NoEvent>(None, None).keyed(key).with_style(Style { style | grow: Some(grow), basis: Some(BasisLength(0.0)), height: Some(20.0) })

let show = (name: string, view: Node<NoEvent>, fonts: mut Fonts) -> void => {
  let laid = layout(view, loose(Size { w: 300.0, h: 400.0 }), fonts)
  var line = "${name} (${laid.size.w}x${laid.size.h}):"
  for p in laid.children {
    let parts = p.laid.id.split(":")
    let key = parts[parts.len() - 1]
    line = "${line} ${key}@${p.at.x},${p.at.y} ${p.laid.size.w}x${p.laid.size.h}"
  }
  println(line)
}
`)
	want := `start (300x20): a@0,0 50x20 b@50,0 80x20 c@130,0 50x20
end (300x20): a@120,0 50x20 b@170,0 80x20 c@250,0 50x20
center (300x20): a@60,0 50x20 b@110,0 80x20 c@190,0 50x20
between (300x20): a@0,0 50x20 b@110,0 80x20 c@250,0 50x20
around (300x20): a@20,0 50x20 b@110,0 80x20 c@230,0 50x20
evenly (300x20): a@30,0 50x20 b@110,0 80x20 c@220,0 50x20
grow clamped (300x20): a@0,0 150x20 b@150,0 50x20 c@200,0 100x20
shrink clamped (300x20): a@0,0 150x20 b@150,0 50x20 c@200,0 100x20
shrink weighted (300x20): a@0,0 75x20 b@75,0 225x20
wrap (300x80): 0@0,0 100x20 1@110,0 100x20 2@0,30 100x20 3@110,30 100x20 4@0,60 100x20
wrap stretched lines (300x140): 0@0,0 100x20 1@110,0 100x20 2@0,50 100x20 3@110,50 100x20 4@0,100 100x20
wrap centred lines (300x140): 0@0,30 100x20 1@110,30 100x20 2@0,60 100x20 3@110,60 100x20 4@0,90 100x20
wrap-reverse (300x80): 0@0,60 100x20 1@110,60 100x20 2@0,30 100x20 3@110,30 100x20 4@0,0 100x20
order (300x20): b@0,0 20x20 c@20,0 30x20 a@50,0 10x20
row-reverse (300x20): a@250,0 50x20 b@170,0 80x20
column (300x200): a@0,0 300x170 b@0,170 300x30
align center (300x100): a@0,40 50x20 b@50,30 50x40
automatic least (80x20): words@0,0 60x20 x@60,0 50x10
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// ui.layout's grid against CSS (worked by hand from CSS Grid §8 and §12): px and fr columns
// with a gap; an auto track grown to its max-content, a min-content one, fr taking the rest;
// spans placed by the sparse cursor; a hole left by a span, filled when the flow is dense;
// items with lines given beside auto-placed ones; minmax(100px, 1fr) wide, and narrower than
// its least; a column flow; an item centred in its cell; and fr rows sharing a fixed height.
func TestRun_UiGrid(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.draw.{ Size }
import ui.text.{ fonts_new, Fonts }
import ui.style.{
  Style,
  Track,
  GridSpan,
  Wrap,
  FlowColumn,
  FlowRowDense,
  AlignCenter,
  TrackPx,
  TrackFr,
  px,
  fr,
  auto_track,
  min_content,
  minmax,
  repeat_tracks,
}
import ui.layout.{ Node, NoEvent, flex, grid, sized, layout, loose }

let main = () -> u8 => {
  var fonts = fonts_new()
  show("px and fr", grid([cell("a"), cell("b"), cell("c")]).with_style(Style {
    grid_columns: Some([px(100.0), fr(1.0), fr(2.0)]),
    column_gap: Some(10.0),
  }), 300.0, fonts)
  // auto grows to its max-content (100), min-content stays at 60, fr takes the rest.
  show("auto and min-content", grid([words("a"), words("b"), block("c", 30.0, 20.0)]).with_style(Style {
    grid_columns: Some([auto_track(), min_content(), fr(1.0)]),
  }), 300.0, fonts)
  let thirds = Some(repeat_tracks(3, [fr(1.0)]))
  show("spans", grid([
    cell("a").with_style(Style { grid_column: Some(GridSpan { span: 2 }) }),
    cell("b"),
    cell("c"),
    cell("d").with_style(Style { grid_column: Some(GridSpan { span: 2 }) }),
    cell("e"),
  ]).with_style(Style { grid_columns: thirds }), 300.0, fonts)
  let holes = [
    cell("a"),
    cell("b").with_style(Style { grid_column: Some(GridSpan { span: 3 }) }),
    cell("c"),
  ]
  show("sparse", grid(holes).with_style(Style { grid_columns: thirds }), 300.0, fonts)
  show("dense", grid(holes).with_style(Style { grid_columns: thirds, grid_flow: Some(FlowRowDense) }), 300.0, fonts)
  show("placed", grid([
    cell("a").with_style(Style { grid_column: Some(GridSpan { start: 3 }), grid_row: Some(GridSpan { start: 1 }) }),
    cell("b").with_style(Style { grid_column: Some(GridSpan { start: 1, span: 2 }), grid_row: Some(GridSpan { start: 2 }) }),
    cell("c"),
    cell("d"),
  ]).with_style(Style { grid_columns: Some(repeat_tracks(3, [px(50.0)])) }), 300.0, fonts)
  let floor = Style { grid_columns: Some([minmax(TrackPx(100.0), TrackFr(1.0)), px(50.0)]) }
  show("minmax", grid([cell("a"), cell("b")]).with_style(floor), 300.0, fonts)
  show("minmax narrow", grid([cell("a"), cell("b")]).with_style(floor), 120.0, fonts)
  show("column flow", grid([block("a", 30.0, 20.0), block("b", 30.0, 20.0), block("c", 30.0, 20.0)]).with_style(Style {
    grid_rows: Some([px(20.0), px(20.0)]),
    grid_flow: Some(FlowColumn),
  }), 300.0, fonts)
  show("centred", grid([block("a", 40.0, 20.0)]).with_style(Style {
    grid_columns: Some([px(100.0)]),
    grid_rows: Some([px(50.0)]),
    justify_items: Some(AlignCenter),
    align_items: Some(AlignCenter),
  }), 300.0, fonts)
  show("fr rows", grid([sized::<NoEvent>(None, None).keyed("a"), sized::<NoEvent>(None, None).keyed("b")]).with_style(Style {
    grid_rows: Some([fr(1.0), fr(3.0)]),
    height: Some(100.0),
  }), 300.0, fonts)
  0
}

/// A cell 20 tall, as wide as its column.
let cell = pure (key: string) -> Node<NoEvent> =>
  sized::<NoEvent>(None, None).keyed(key).with_style(Style { height: Some(20.0) })

let block = pure (key: string, w: f32, h: f32) -> Node<NoEvent> =>
  sized::<NoEvent>(None, None).keyed(key).with_style(Style { width: Some(w), height: Some(h) })

/// Two "words", 60 and 40 wide: min-content 60, max-content 100.
let words = pure (key: string) -> Node<NoEvent> =>
  flex([block("w1", 60.0, 10.0), block("w2", 40.0, 10.0)]).keyed(key).with_style(Style { flex_wrap: Some(Wrap) })

let show = (name: string, view: Node<NoEvent>, width: f32, fonts: mut Fonts) -> void => {
  let laid = layout(view, loose(Size { w: width, h: 400.0 }), fonts)
  var line = "${name} (${laid.size.w}x${laid.size.h}):"
  for p in laid.children {
    let parts = p.laid.id.split(":")
    line = "${line} ${parts[parts.len() - 1]}@${p.at.x},${p.at.y} ${p.laid.size.w}x${p.laid.size.h}"
  }
  println(line)
}
`)
	want := `px and fr (300x20): a@0,0 100x20 b@110,0 60x20 c@180,0 120x20
auto and min-content (300x20): a@0,0 100x20 b@100,0 60x20 c@160,0 30x20
spans (300x60): a@0,0 200x20 b@200,0 100x20 c@0,20 100x20 d@100,20 200x20 e@0,40 100x20
sparse (300x60): a@0,0 100x20 b@0,20 300x20 c@0,40 100x20
dense (300x40): a@0,0 100x20 b@0,20 300x20 c@100,0 100x20
placed (300x40): a@100,0 50x20 b@0,20 100x20 c@0,0 50x20 d@50,0 50x20
minmax (300x20): a@0,0 250x20 b@250,0 50x20
minmax narrow (120x20): a@0,0 100x20 b@100,0 50x20
column flow (300x40): a@0,0 30x20 b@0,20 30x20 c@150,0 30x20
centred (300x50): a@30,15 40x20
fr rows (300x100): a@0,0 300x25 b@0,25 300x75
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Media rules and selector lists in the cascade. A sheet's grid has 1, 2 or 4 columns by
// min-width breakpoints. A class rule under a landscape condition beats a later tag rule by
// its specificity, applied or not by the orientation. A not and a comma (any of) combine.
// Then a host resized: a height change crossing no breakpoint gives the grid back from the
// cache (only the root and column are laid out), and crossing min-width 500 or 800 relays the
// whole grid out at its new columns.
func TestRun_UiMediaQueries(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ host_of, sandbox }
import ui.script.{ timed_platform, Played }
import ui.backend.{ Renderer, Resized, DarkMode }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Style, Sheet, Rule, Media, Viewport, Landscape, light_theme, fr, repeat_tracks }
import ui.layout.{ Node, NoEvent, Laid, column, grid, sized, themed, layout_with, loose, interaction_new }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

let main = () -> u8 => {
  cascade()
  resizing()
  0
}

/// The sheet: four columns from 800, two from 500, one below; a class's colour by a media
/// rule against a tag rule, and a selector list.
let sheet = Sheet { rules: [
  Rule { selector: "Grid", style: Style { grid_columns: Some([fr(1.0)]) } },
  Rule { selector: "Grid", style: Style { grid_columns: Some(repeat_tracks(2, [fr(1.0)])) }, media: [Media { min_width: Some(500.0) }] },
  Rule { selector: "Grid", style: Style { grid_columns: Some(repeat_tracks(4, [fr(1.0)])) }, media: [Media { min_width: Some(800.0) }] },
  // .wide beats the later tag rule by specificity, whether or not its media rule applies.
  Rule { selector: ".wide, Grid.big", style: Style { height: Some(30.0) }, media: [Media { orientation: Some(Landscape) }] },
  Rule { selector: "SizedBox", style: Style { height: Some(10.0) } },
  Rule { selector: "SizedBox", style: Style { height: Some(20.0) }, media: [Media { max_width: Some(499.0), not: true }, Media { min_height: Some(900.0) }] },
] }

let view = pure (_: i64) -> Node<NoEvent> => themed(light_theme(), sheet, [
  column([grid([cell(), cell().with_class("wide"), cell(), cell()])]),
])

let cell = pure () -> Node<NoEvent> => sized::<NoEvent>(None, None)

/// What the sheet makes at each viewport: the grid's columns and each cell's height.
let cascade = () -> void => {
  var fonts = fonts_new()
  let sizes: [](f32, f32) = [(900.0, 400.0), (600.0, 400.0), (400.0, 1000.0), (400.0, 300.0)]
  for (w, h) in sizes {
    var ui = interaction_new()
    ui.viewport = Viewport { size: Size { w: w, h: h } }
    let laid = layout_with(view(0), loose(Size { w: w, h: h }), fonts, ui)
    println("${w}x${h}: ${describe(the_grid(laid))}")
  }
}

/// A host resized: within a breakpoint (the grid given back), across one, and back.
let resizing = () -> void => {
  var platform = timed_platform([
    Played(Resized(Size { w: 900.0, h: 500.0 })),
    Played(Resized(Size { w: 600.0, h: 500.0 })),
    Played(Resized(Size { w: 650.0, h: 500.0 })),
    Played(Resized(Size { w: 300.0, h: 500.0 })),
  ], Size { w: 900.0, h: 400.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  var host = host_of(sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 }), 0, platform)
  while host.step(platform, renderer, fonts) {
    let size = platform.window_size()
    println("resized to ${size.w}x${size.h}: ${describe(the_grid(host.shown[0]))}, laid out ${host.cache.laid_out}, given back ${host.cache.reused}")
  }
}

let the_grid = pure (laid: Laid<NoEvent>) -> Laid<NoEvent> => laid.children[0].laid.children[0].laid

/// How many columns a grid's cells make, and their heights.
let describe = pure (g: Laid<NoEvent>) -> string => {
  var xs: []f32 = []
  for p in g.children { if !xs.any((x) => x == p.at.x) { xs.push(p.at.x) } }
  "${xs.len()} columns, heights ${[p in g.children | "${p.laid.size.h}"].join(" ")}"
}
`)
	want := `900x400: 4 columns, heights 20 30 20 20
600x400: 2 columns, heights 20 30 20 20
400x1000: 1 columns, heights 20 20 20 20
400x300: 1 columns, heights 10 30 10 10
resized to 900x500: 4 columns, heights 20 30 20 20, laid out 2, given back 1
resized to 600x500: 2 columns, heights 20 30 20 20, laid out 7, given back 0
resized to 650x500: 2 columns, heights 20 30 20 20, laid out 7, given back 0
resized to 300x500: 1 columns, heights 10 10 10 10, laid out 7, given back 0
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// App's min_size and max_size reach the platform (Platform.set_size_limits) on the first
// pass, and ScriptPlatform holds its resize steps to them as a window would be held, each
// side on its own: below the least width and above the greatest, then within both.
func TestRun_UiWindowSizeLimits(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, host_of, sandbox }
import ui.script.{ timed_platform, Played }
import ui.backend.{ Renderer, Resized }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, NoEvent, sized }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

/// A window opened below its least size grows to it; resized below or above its limits, it
/// stops at them, a side at a time; within them, it is as asked.
let main = () -> u8 => {
  var platform = timed_platform([
    Played(Resized(Size { w: 200.0, h: 900.0 })),
    Played(Resized(Size { w: 1200.0, h: 100.0 })),
    Played(Resized(Size { w: 500.0, h: 600.0 })),
  ], Size { w: 300.0, h: 300.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = App {
    sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 }) |
    min_size: Size { w: 360.0, h: 480.0 },
    max_size: Size { w: 1000.0 },
  }
  var host = host_of(app, 0, platform)
  while host.step(platform, renderer, fonts) {
    let size = platform.window_size()
    println("${size.w}x${size.h}")
  }
  0
}

let view = pure (_: i64) -> Node<NoEvent> => sized::<NoEvent>(None, None)
`)
	want := "360x900\n1000x480\n500x600\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// App's resize_x and resize_y hold a side that may not resize to the size the window opened
// at (within min_size and max_size), through the same limits: one side, then both.
// Under `--live` the host follows App.window — lui's `window_spec`, whose body a reload
// replaces — onto the open window: a new title and width are made to it, then a window that
// may no longer resize is held at its size. (Here the description reads top-level variables
// the loop changes, in place of a reload; `live` is set by hand.) A window opened before a
// reload kept its first description until 10/22.
func TestRun_UiWindowFollowsItsDescription(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, WindowSpec, host_of, sandbox }
import ui.script.{ timed_platform, Waited }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, NoEvent, sized }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

var width: f32 = 200.0
var named: string = "first"
var fixed: bool = false

let spec = () -> WindowSpec => WindowSpec {
  title: named,
  size: Size { w: width, h: 64.0 },
  resize_x: !fixed,
  resize_y: !fixed,
}

let main = () -> u8 => {
  var platform = timed_platform([Waited(10), Waited(10), Waited(10)], Size { w: 200.0, h: 64.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = App { sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 }) | window: Some(spec) }
  var host = host_of(app, 0, platform)
  host.live = true
  var pass = 0
  while pass < 3 && host.step(platform, renderer, fonts) {
    let size = platform.window_size()
    println("pass ${pass}: ${size.w}x${size.h} '${platform.title}', least ${platform.least.w}x${platform.least.h}, most ${platform.most.w}x${platform.most.h}")
    pass += 1
    if pass == 1 {
      width = 320.0
      named = "second"
    }
    if pass == 2 { fixed = true }
  }
  0
}

let view = pure (_: i64) -> Node<NoEvent> => sized::<NoEvent>(None, None)
`)
	want := `pass 0: 200x64 '', least 0x0, most 0x0
pass 1: 320x64 'second', least 0x0, most 0x0
pass 2: 320x64 'second', least 320x64, most 320x64
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRun_UiWindowResizeAxes(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, host_of, sandbox }
import ui.script.{ timed_platform, Played }
import ui.backend.{ Renderer, Resized }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, NoEvent, sized }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

/// Opened at 300x300 below a least width of 360: across, it is held at 360 and only its
/// height follows; held both ways, it stays as it opened.
let main = () -> u8 => {
  let blank = App { sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 }) | min_size: Size { w: 360.0 } }
  show(App { blank | resize_x: false })
  println("--")
  show(App { blank | resize_x: false, resize_y: false })
  0
}

let show = (app: App<i64, NoEvent>) -> void => {
  var platform = timed_platform([
    Played(Resized(Size { w: 500.0, h: 600.0 })),
    Played(Resized(Size { w: 100.0, h: 100.0 })),
  ], Size { w: 300.0, h: 300.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  var host = host_of(app, 0, platform)
  while host.step(platform, renderer, fonts) {
    let size = platform.window_size()
    println("${size.w}x${size.h}")
  }
}

let view = pure (_: i64) -> Node<NoEvent> => sized::<NoEvent>(None, None)
`)
	want := "360x600\n360x100\n--\n360x300\n360x300\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The Layouts demo's view resized 1,700 times — every 0.75 of a unit from 1300 wide to 300,
// then back and forth across its 560 and 860 breakpoints by fractions — must settle every
// pass, and within a minute: a layout loop that does not end fails here rather than hanging
// a window (the demo froze while its edge was dragged, 10/21). And controls that fill their
// width are measured at CONTROL_WIDTH in a flex row, never an unbounded width.
func TestRun_UiResizeSettles(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	view, err := os.ReadFile(filepath.Join(root, "examples", "ui", "layouts_view.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "layouts_view.lyra"), view, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{"stress": `import ui.app.{ host_of, sandbox }
import ui.script.{ timed_platform, Played, Step }
import ui.backend.{ Renderer, Resized }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import layouts_view.{ State, view, update }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

let main = () -> u8 => {
  var steps: []Step = []
  var w: f32 = 1300.0
  while w > 300.0 {
    steps.push(Played(Resized(Size { w: w, h: 700.0 - (w % 37.0) })))
    w -= 0.75
  }
  for k in 0..<400 {
    let x = if k % 2 == 0 { 559.5 + f32(k % 7) * 0.25 } else { 860.25 - f32(k % 5) * 0.5 }
    steps.push(Played(Resized(Size { w: x, h: 420.0 + f32(k) })))
  }
  var platform = timed_platform(steps, Size { w: 900.0, h: 700.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var host = host_of(sandbox(view, update, Color { r: 0, g: 0, b: 0 }), State { cards: 9 }, platform)
  var passes = 0
  while host.step(platform, renderer, fonts) {
    passes += 1
    if passes % 500 == 0 {
      let s = platform.window_size()
      println("${passes} passes, at ${s.w}x${s.h}")
    }
  }
  println("done: ${passes} passes")
  0
}
`, "controls": `import ui.draw.{ Size }
import ui.text.{ fonts_new }
import ui.style.{ Style }
import ui.layout.{ Node, NoEvent, SliderSpec, FieldSpec, flex, sized, slider, text_input, layout, loose }

data Ev = Moved(f32) | Typed(string)

/// Controls that fill their width, in a flex row too narrow for them: measured at
/// CONTROL_WIDTH, not an unbounded width, so the line's arithmetic stays finite and settles.
let main = () -> u8 => {
  var fonts = fonts_new()
  let row: Node<Ev> = flex([
    slider(SliderSpec { value: 0.5, on_change: Moved }),
    text_input(FieldSpec { value: "", on_edit: Typed }),
    sized::<Ev>(None, None).with_style(Style { width: Some(100.0), height: Some(20.0) }),
  ])
  for width in [300.0, 1000.0] {
    let laid = layout(row, loose(Size { w: f32(width), h: 200.0 }), fonts)
    println("${width}: ${[p in laid.children | "${p.at.x}+${p.laid.size.w}"].join(" ")}")
  }
  0
}
`} {
		path := filepath.Join(dir, name+".lyra")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, name)
		if _, stderr, code := captureRunUnoptimized(t, "build", path, "-o", bin); code != 0 {
			t.Fatalf("building %s exited %d: %s", name, code, stderr)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := commandContext(t, ctx, filepath.Join(dir, "stress")).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("resizing the demo did not settle within a minute:\n%s", out)
	}
	if err != nil || !strings.Contains(string(out), "done: 1734 passes") {
		t.Errorf("resizing the demo: %v\n%s", err, out)
	}
	out, err = command(t, filepath.Join(dir, "controls")).CombinedOutput()
	if want := "300: 0+160 160+160 320+0\n1000: 0+160 160+160 320+100\n"; err != nil || string(out) != want {
		t.Errorf("controls in a flex row: %v\ngot\n%s\nwant\n%s", err, out, want)
	}
}

// Shadows and gradients through SDL's 2D renderer, which approximates them: a card's shadow
// darkens the white below it and is gone a little further down; a left-to-right gradient
// is red at its left, blue at its right and purple between; a radial one is white in its
// middle. (The exact falloff is ui.gpu's, tested against the Gaussian in gpu_test.go.)
func TestRun_UiFallbackShadowsAndGradients(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	setenv(t, "SDL_RENDER_DRIVER", "software")
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.sdl3
import ui.sdl3.{ capture }
import ui.draw.{ Point, Size, Color, Image, rgb, rgba, BoxShadow, Gradient, ColorStop, LinearGradient, RadialGradient }
import ui.text.{ fonts_new, glyph_atlas }
import ui.style.{ Style }
import ui.layout.{ Node, NoEvent, sized, stack, positioned, Edges, layout, loose, paint }

/// A card with a shadow under it, a left-to-right gradient, and a radial one, drawn by SDL's
/// 2D renderer: the pixels where a shadow falls, either side of a stop, and well clear.
let main = () -> u8 => {
  var fonts = fonts_new()
  let Ok(window) = sdl3.open("fx", 300.0, 220.0) else { return 2 }
  var platform = window.platform
  var renderer = window.renderer
  var atlas = glyph_atlas()
  let view: Node<NoEvent> = stack([
    sized::<NoEvent>(Some(300.0), Some(220.0)).with_style(Style { background: Some(rgb(0xFFFFFF)) }),
    positioned(Edges { left: Some(20.0), top: Some(20.0) }, box(Style {
      background: Some(rgb(0xFFFFFF)),
      box_shadow: Some([BoxShadow { y: 10.0, blur: 12.0, color: rgba(0x00000080) }]),
    })),
    positioned(Edges { left: Some(160.0), top: Some(20.0) }, box(Style {
      background_gradient: Some(Gradient { kind: LinearGradient(90.0), stops: [ColorStop { color: rgb(0xFF0000) }, ColorStop { color: rgb(0x0000FF) }] }),
    })),
    positioned(Edges { left: Some(160.0), top: Some(120.0) }, box(Style {
      background_gradient: Some(Gradient { kind: RadialGradient(true), stops: [ColorStop { color: rgb(0xFFFFFF) }, ColorStop { color: rgb(0x000000) }] }),
    })),
  ])
  let laid = layout(view, loose(Size { w: 300.0, h: 220.0 }), fonts)
  let ops = paint(laid, Point {}, 1.0, fonts, atlas, renderer)
  let Some(frame) = capture(renderer, rgb(0xFFFFFF), ops) else { return 3 }
  let at = (x: i64, y: i64) -> string => {
    let p = frame.argb[y * frame.width + x]
    "${(p >> 16) & 0xFF},${(p >> 8) & 0xFF},${p & 0xFF}"
  }
  println("card middle ${at(70, 50)}, shadow under ${at(70, 92)}, shadow fading ${at(70, 100)}, clear ${at(70, 130)}")
  println("linear left ${at(162, 50)}, linear middle ${at(210, 50)}, linear right ${at(258, 50)}")
  println("radial centre ${at(210, 150)}, corner ${at(162, 122)}")
  sdl3.close(platform, renderer)
  0
}

/// A box 100 × 60, rounded.
let box = pure (style: Style) -> Node<NoEvent> =>
  sized::<NoEvent>(Some(100.0), Some(60.0)).with_style(Style { style | radius: Some(10.0) })
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", stdout)
	}
	if code != 0 {
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	pixel := func(name string) [3]int {
		m := regexp.MustCompile(regexp.QuoteMeta(name) + ` (\d+),(\d+),(\d+)`).FindStringSubmatch(stdout)
		if m == nil {
			t.Fatalf("no %q in:\n%s", name, stdout)
		}
		var p [3]int
		for i := range p {
			p[i], _ = strconv.Atoi(m[i+1])
		}
		return p
	}
	near := func(name string, want [3]int, by int) {
		got := pixel(name)
		for i := range got {
			if d := got[i] - want[i]; d > by || d < -by {
				t.Errorf("%s: got %v, want %v within %d", name, got, want, by)
				return
			}
		}
	}
	near("card middle", [3]int{255, 255, 255}, 0)
	near("shadow under", [3]int{200, 200, 200}, 40)
	near("clear", [3]int{255, 255, 255}, 0)
	near("linear left", [3]int{255, 0, 0}, 25)
	near("linear middle", [3]int{128, 0, 128}, 25)
	near("linear right", [3]int{0, 0, 255}, 25)
	near("radial centre", [3]int{255, 255, 255}, 15)
}

// Effects in the cascade and the display list: an element with every one paints, in CSS's
// order, a layer for its filter, its outer shadows (the first on top, so last), its backdrop,
// its colour, its gradient, its inset shadow, its border, and the layer put down. A compound
// selector (.card:hover, 20) beats a single class (.card, 10) though written before it,
// replacing the whole shadow list; and hovering the element inside the second card hovers the
// card, as CSS's :hover reaches ancestors.
func TestRun_UiEffectsInTheCascade(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.draw.{
  Point,
  Size,
  Rect,
  rgb,
  rgba,
  DrawOp,
  Fill,
  Border,
  Shadow,
  GradientFill,
  Backdrop,
  PushLayer,
  PopLayer,
  BoxShadow,
  Gradient,
  ColorStop,
  LinearGradient,
}
import ui.text.{ fonts_new, glyph_atlas }
import ui.backend.{ Renderer }
import ui.draw.{ TextureId }
import ui.style.{ Style, Sheet, Rule, light_theme }
import ui.layout.{ Node, NoEvent, Laid, column, sized, themed, layout_with, loose, interaction_new, paint }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

/// The ops an element with every effect paints, in order; a compound selector against a
/// single class, either order; and a card hovered through the element under the pointer.
let main = () -> u8 => {
  var fonts = fonts_new()
  let card = sized::<NoEvent>(Some(100.0), Some(50.0)).with_class("card")
  let view: Node<NoEvent> = themed(light_theme(), sheet, [column([card, sized::<NoEvent>(Some(10.0), Some(10.0), [sized::<NoEvent>(Some(4.0), Some(4.0))]).with_class("card")])])
  var ui = interaction_new()
  for hovered in ["", "#/0/1/0"] {
    ui.hover = hovered
    let laid = layout_with(view, loose(Size { w: 200.0, h: 200.0 }), fonts, ui)
    println("hover ${if hovered.len() == 0 { "none" } else { hovered }}:")
    var renderer = Blank {}
    var atlas = glyph_atlas()
    for op in paint(laid, Point {}, 1.0, fonts, atlas, renderer) { println("  ${name_of(op)}") }
  }
  0
}

let sheet = Sheet { rules: [
  Rule { selector: ".card:hover", style: Style { box_shadow: Some([BoxShadow { y: 9.0, blur: 20.0, color: rgba(0x00000040) }]) } },
  Rule { selector: ".card", style: Style {
    background: Some(rgb(0xFFFFFF)),
    border_color: Some(rgb(0xCCCCCC)),
    border_width: Some(1.0),
    box_shadow: Some([
      BoxShadow { y: 2.0, blur: 4.0, color: rgba(0x00000033) },
      BoxShadow { blur: 3.0, color: rgba(0xFFFFFF80), inset: true },
      BoxShadow { y: 6.0, blur: 12.0, color: rgba(0x0000001A) },
    ]),
    background_gradient: Some(Gradient { kind: LinearGradient(90.0), stops: [ColorStop { color: rgb(0xFF0000) }, ColorStop { color: rgb(0x0000FF) }] }),
    backdrop_blur: Some(8.0),
    filter_blur: Some(1.5),
  } },
] }

let name_of = pure (op: DrawOp) -> string => match op {
  Fill(at, _, _) => "fill ${at.w}x${at.h}",
  Border(_, _, w, _) => "border ${w}",
  Shadow(_, _, s) => "shadow y ${s.y} blur ${s.blur}${if s.inset { " inset" } else { "" }}",
  GradientFill(_, _, g) => "gradient of ${g.stops.len()} stops",
  Backdrop(_, _, r) => "backdrop blur ${r}",
  PushLayer(at, e) => "layer blur ${e.blur} over ${at.w}x${at.h}",
  PopLayer => "layer done",
  _ => "other",
}
`)
	want := `hover none:
  layer blur 1.5 over 109x59
  shadow y 6 blur 12
  shadow y 2 blur 4
  backdrop blur 8
  fill 100x50
  gradient of 2 stops
  shadow y 0 blur 3 inset
  border 1
  layer done
  layer blur 1.5 over 19x19
  shadow y 6 blur 12
  shadow y 2 blur 4
  backdrop blur 8
  fill 10x10
  gradient of 2 stops
  shadow y 0 blur 3 inset
  border 1
  layer done
hover #/0/1/0:
  layer blur 1.5 over 109x59
  shadow y 6 blur 12
  shadow y 2 blur 4
  backdrop blur 8
  fill 100x50
  gradient of 2 stops
  shadow y 0 blur 3 inset
  border 1
  layer done
  layer blur 1.5 over 19x19
  shadow y 9 blur 20
  backdrop blur 8
  fill 10x10
  gradient of 2 stops
  border 1
  layer done
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A see-through window on the GPU (ui.gpu's open with see_through, macOS): a frame over a clear
// background keeps its alpha — the clear background none, a half-clear fill half, its colour
// premultiplied as the window's layer composites it — nothing of its content opaque once SDL's
// content view has updated its layer, which painted it black under the Metal layer, and its
// title bar on an opaque backing, as an opaque window's; its Metal layer pinned, the GPU's
// claim having made it after the platform pinned what there was (a resize showed the last
// frame stretched); and the blur behind it is taken by a
// backdrop layer inside the window, at the radius asked (the window server's blur, the fallback,
// showed past the window's rounded corners), gone again at 0; a frame's backdrops are regions of
// the desktop blurred (one clipped away is none), and opacity again is taken, after which a
// frame has none.
func TestRun_UiGpuSeeThrough(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("ui.gpu's shaders are Metal's")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.gpu
import ui.gpu.{ keep_frames, read_frame }
import ui.backend.{ Renderer, Platform }
import ui.draw.{ Color, Rect, rgba, radius, Fill, DrawOp, Backdrop, PushClip, PopClip }
import bindings.gpu.{ see_through_state, backdrop_state, backdrop_regions_state, pinned_state }

let main = () -> u8 => {
  let Ok(window) = gpu.open("see", 300.0, 220.0, true) else { return 2 }
  var platform = window.platform
  var renderer = window.renderer
  let Some(g) = renderer.gpu else {
    println("no gpu")
    return 3
  }
  keep_frames(g)
  let ops: []DrawOp = [Fill(Rect { x: 20.0, y: 20.0, w: 100.0, h: 60.0 }, radius(0.0), rgba(0xFF000080))]
  for _ in 0..<3 { renderer.draw(Color { r: 0, g: 0, b: 0, a: 0 }, ops) }
  let Some(frame) = read_frame(g) else {
    println("no frame")
    return 4
  }
  let s = f32(frame.width) / 300.0
  let at = (x: f32, y: f32) -> string => {
    let p = frame.argb[i64((y * s).round()) * frame.width + i64((x * s).round())]
    "a ${(p >> 24) & 0xFF} r ${(p >> 16) & 0xFF}"
  }
  println("the clear background: ${at(200.0, 150.0)}")
  println("a half-clear red: ${at(70.0, 50.0)}")
  println("the content see-through, the title bar opaque: ${see_through_state(platform.window)}")
  println("the Metal layer pinned for a resize: ${pinned_state(platform.window)}")
  println("blur taken: ${platform.set_backdrop_blur(20.0)}, by the backdrop: ${backdrop_state(platform.window)}")
  println("blur gone: ${platform.set_backdrop_blur(0.0)}, ${backdrop_state(platform.window)}")
  // Two frosted panels, the second clipped away entirely: one region of the desktop blurred.
  let frosted: []DrawOp = [
    Backdrop(Rect { x: 20.0, y: 20.0, w: 100.0, h: 60.0 }, radius(8.0), 12.0),
    PushClip(Rect { x: 0.0, y: 0.0, w: 300.0, h: 100.0 }, radius(0.0)),
    Backdrop(Rect { x: 20.0, y: 150.0, w: 100.0, h: 40.0 }, radius(8.0), 12.0),
    PopClip,
  ]
  renderer.draw(Color { r: 0, g: 0, b: 0, a: 0 }, frosted)
  println("regions blurred: ${backdrop_regions_state(platform.window)}")
  println("opaque again: ${platform.set_see_through(false)}")
  renderer.draw(Color { r: 0, g: 0, b: 0, a: 0 }, frosted)
  println("regions on an opaque window: ${backdrop_regions_state(platform.window)}")
  gpu.close(platform, renderer)
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	switch code {
	case 2, 3, 4:
		t.Skipf("no GPU frame here (%d): %s", code, stdout)
	case 0:
	default:
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	want := "the clear background: a 0 r 0\na half-clear red: a 128 r 128\nthe content see-through, the title bar opaque: true\nthe Metal layer pinned for a resize: true\nblur taken: true, by the backdrop: 20\nblur gone: true, -1\nregions blurred: 1\nopaque again: true\nregions on an opaque window: 0\n"
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
}

// ui.gpu's frame read back from the GPU (macOS with a GPU; skipped elsewhere): a card's shadow
// fades smoothly below it — darker nearer, half its strength at the shadow's edge, gone three
// deviations out; a three-stop gradient is each stop's colour at its place; and a black box
// in a layer blurred by 8 is grey at its edge, symmetric either side, black within, white
// clear of it.
func TestRun_UiGpuReadback(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("ui.gpu's shaders are Metal's")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import ui.gpu
import ui.gpu.{ keep_frames, read_frame }
import ui.backend.{ Renderer }
import ui.draw.{ Color, Point, Rect, rgb, rgba, radius, Fill, Shadow, GradientFill, PushLayer, PopLayer, BoxShadow, Gradient, ColorStop, LinearGradient, LayerEffect, DrawOp }

let main = () -> u8 => {
  let Ok(window) = gpu.open("gpu", 300.0, 220.0) else { return 2 }
  var platform = window.platform
  var renderer = window.renderer
  let Some(g) = renderer.gpu else {
    println("no gpu")
    return 3
  }
  keep_frames(g)
  let ops: []DrawOp = [
    Shadow(Rect { x: 20.0, y: 20.0, w: 100.0, h: 60.0 }, radius(10.0), BoxShadow { y: 10.0, blur: 12.0, color: rgba(0x00000080) }),
    Fill(Rect { x: 20.0, y: 20.0, w: 100.0, h: 60.0 }, radius(10.0), rgb(0xFFFFFF)),
    GradientFill(Rect { x: 160.0, y: 20.0, w: 100.0, h: 60.0 }, radius(0.0), Gradient { kind: LinearGradient(90.0), stops: [ColorStop { color: rgb(0xFF0000) }, ColorStop { color: rgb(0x00FF00), at: Some(0.5) }, ColorStop { color: rgb(0x0000FF) }] }),
    PushLayer(Rect { x: 0.0, y: 110.0, w: 300.0, h: 110.0 }, LayerEffect { blur: 8.0 }),
    Fill(Rect { x: 100.0, y: 140.0, w: 100.0, h: 50.0 }, radius(0.0), rgb(0x000000)),
    PopLayer,
  ]
  // A couple of frames, so the swapchain is up.
  for _ in 0..<3 { renderer.draw(rgb(0xFFFFFF), ops) }
  let Some(frame) = read_frame(g) else {
    println("no frame")
    return 4
  }
  let s = f32(frame.width) / 300.0
  let at = (x: f32, y: f32) -> string => {
    let p = frame.argb[i64((y * s).round()) * frame.width + i64((x * s).round())]
    "${(p >> 16) & 0xFF},${(p >> 8) & 0xFF},${p & 0xFF}"
  }
  println("size ${frame.width}x${frame.height}")
  println("card ${at(70.0, 50.0)}")
  let ys: []f32 = [82.0, 86.0, 90.0, 94.0, 98.0, 102.0, 110.0]
  for y in ys { println("shadow at ${y}: ${at(70.0, y)}") }
  println("gradient ${at(162.0, 50.0)} | ${at(210.0, 50.0)} | ${at(258.0, 50.0)}")
  let xs: []f32 = [90.0, 96.0, 100.0, 104.0, 110.0, 150.0]
  for x in xs { println("blurred edge at ${x}: ${at(x, 165.0)}") }
  gpu.close(platform, renderer)
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	switch code {
	case 2, 3, 4:
		t.Skipf("no GPU frame here (%d): %s", code, stdout)
	case 0:
	default:
		t.Fatalf("exited %d\nstderr: %s", code, stderr)
	}
	grey := func(name string) int {
		m := regexp.MustCompile(regexp.QuoteMeta(name) + `: (\d+),\d+,\d+`).FindStringSubmatch(stdout)
		if m == nil {
			t.Fatalf("no %q in:\n%s", name, stdout)
		}
		v, _ := strconv.Atoi(m[1])
		return v
	}
	last := 0
	for _, y := range []string{"82", "86", "90", "94", "98", "102", "110"} {
		v := grey("shadow at " + y)
		if v < last {
			t.Errorf("the shadow darkens again at %s: %d after %d", y, v, last)
		}
		last = v
	}
	if v := grey("shadow at 90"); v < 170 || v > 215 {
		t.Errorf("the shadow at its edge: %d, want about half its strength (170–215)", v)
	}
	if v := grey("shadow at 110"); v != 255 {
		t.Errorf("the shadow three deviations out: %d, want 255", v)
	}
	if !regexp.MustCompile(`gradient (2[2-5]\d),\d{1,2},\d{1,2} \| \d{1,2},(2[3-5]\d),\d{1,2} \| \d{1,2},\d{1,2},(2[2-5]\d)`).MatchString(stdout) {
		t.Errorf("the gradient's stops are not red, green and blue:\n%s", stdout)
	}
	if v := grey("blurred edge at 100"); v < 100 || v > 150 {
		t.Errorf("the blurred edge: %d, want about half (100–150)", v)
	}
	if a, b := 255-grey("blurred edge at 96"), grey("blurred edge at 104"); a-b > 15 || b-a > 15 {
		t.Errorf("the blur is not symmetric about the edge: %d in, %d out", a, b)
	}
	if v := grey("blurred edge at 110"); v > 15 {
		t.Errorf("inside the blurred box: %d, want near black", v)
	}
	if v := grey("blurred edge at 90"); v < 240 {
		t.Errorf("clear of the blurred box: %d, want near white", v)
	}
}

// A live resize, as ScriptPlatform plays one (drag:W,H): the platform calls the hook
// ui.app's follow_resizes gave it from inside its wait, as macOS does while a window's edge is
// dragged, and the running window draws a frame laid out at the new size there and then — the
// same size twice draws once — with no input reaching the loop.
func TestRun_UiLiveResize(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, running, follow_resizes, step_running }
import ui.script.{ timed_platform, Dragged, Waited, Step }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, NoEvent, row, sized }

/// A renderer that draws nothing, counting the frames and the sizes they were laid out at.
struct Blank {
  textures: i64 = 0,
  drawn: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => { self.drawn += 1 }
}

/// A window dragged while its loop is held: each step at a new size draws a frame laid out at
/// it from inside the wait — the same size twice draws once — and no input reaches the loop.
let main = () -> u8 => {
  let steps: []Step = [
    Dragged(Size { w: 600.0, h: 400.0 }),
    Dragged(Size { w: 600.0, h: 400.0 }),
    Dragged(Size { w: 520.0, h: 300.0 }),
    Waited(10),
  ]
  var platform = timed_platform(steps, Size { w: 800.0, h: 400.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 })
  let window = running(app, 0, platform, renderer, fonts)
  follow_resizes(window)
  var passes = 0
  while step_running(window) {
    passes += 1
    let at = window.host.drawn_at
    println("pass ${passes}: ${window.renderer.drawn} frames, last at ${at.w}x${at.h}, root ${window.host.shown[0].size.w} wide, ${window.platform.dragged} drags")
  }
  0
}

let view = pure (_: i64) -> Node<NoEvent> => row([sized::<NoEvent>(None, Some(20.0))])
`)
	want := `pass 1: 2 frames, last at 600x400, root 600 wide, 1 drags
pass 2: 2 frames, last at 600x400, root 600 wide, 2 drags
pass 3: 3 frames, last at 520x300, root 520 wide, 3 drags
pass 4: 3 frames, last at 520x300, root 520 wide, 3 drags
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Overflow: scrollers' ports and content, hit-testing through a clip, the wheel on both
// axes bubbling from the innermost scroller that can move to the window, a thumb dragged,
// a track paged, the keyboard, Tab revealing what it focuses, and a clamp on resize.
func TestRun_UiOverflow(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, running, step_running }
import ui.script.{ timed_platform, parse_steps }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Style, Overflow, OverflowAuto, OverflowScroll, OverflowHidden }
import ui.layout.{ Node, Laid, column, row, sized, button, CrossStretch, MainStart }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Pressed(i64)

/// A page 300 wide in a window 300 × 400: a list scrolled down (10 rows of 30 in 100), a row
/// scrolled across (600 in 300), a box scrolled both ways (400 × 300 in 100 high, stretched
/// across the page, bars always), one hidden (100 in 40), and one scrolled down holding one scrolled across.
let view = pure (_: i64) -> Node<Ev> => column([
  column([i in 0..<10 | sized(None, Some(30.0), [button("", Pressed(i))])]).aligned(MainStart, CrossStretch).keyed("down").with_style(Style { height: Some(100.0), overflow_y: Some(OverflowAuto) }),
  row([i in 0..<6 | block(100.0, 40.0)]).keyed("across").with_style(Style { overflow_x: Some(OverflowAuto) }),
  column([block(400.0, 300.0)]).keyed("both").with_style(Style { width: Some(150.0), height: Some(100.0), overflow_x: Some(OverflowScroll), overflow_y: Some(OverflowScroll) }),
  column([block(100.0, 100.0)]).keyed("hidden").with_style(Style { height: Some(40.0), overflow_y: Some(OverflowHidden) }),
  column([
    row([i in 0..<6 | block(100.0, 50.0)]).keyed("inner").with_style(Style { overflow_x: Some(OverflowAuto) }),
    block(10.0, 250.0),
  ]).aligned(MainStart, CrossStretch).keyed("outer").with_style(Style { height: Some(100.0), overflow_y: Some(OverflowAuto) }),
  block(10.0, 400.0),
]).aligned(MainStart, CrossStretch)

let block = pure (w: f32, h: f32) -> Node<Ev> => sized(Some(w), Some(h))

let main = () -> u8 => {
  for (name, script) in [
    ("start", ""),
    ("a click on a row presses it", "click:50,15"),
    ("a click below the list is not a hidden row's", "click:50,120"),
    ("wheel down", "move:50,50 scroll:0,3"),
    ("wheel past the end", "move:50,50 scroll:0,10"),
    ("then bubbles to the window", "move:50,50 scroll:0,10 scroll:0,1"),
    ("wheel across", "move:50,120 scroll:2,0"),
    ("both, either axis", "move:40,180 scroll:1,1"),
    ("hidden passes to the window", "move:50,260 scroll:0,1"),
    ("nested: across stays in, down goes out", "move:50,300 scroll:1,0 scroll:0,1"),
    ("drag the list's thumb", "down:294,10 move:294,30 up:294,30"),
    ("click the list's track pages", "click:294,90"),
    ("End, Home, Page Down over the list", "move:50,50 End Home PageDown"),
    ("Tab to the eighth row reveals it", "Tab Tab Tab Tab Tab Tab Tab Tab"),
    ("a window scrolled to its end, then taller, clamps", "move:200,395 scroll:0,40 resize:300,600"),
  ] {
    let steps = match parse_steps(script) {
      Ok(s) => s,
      Err(why) => {
        println("${name}: ${why}")
        continue
      },
    }
    var platform = timed_platform(steps, Size { w: 300.0, h: 400.0 })
    var renderer = Blank {}
    var fonts = fonts_new()
    let window = running(sandbox(view, pressed, Color { r: 0, g: 0, b: 0 }), -1, platform, renderer, fonts)
    while step_running(window) {}
    let state = window.host.state
    println("${name}: ${offsets(window.host.shown[0])}${if state >= 0 { "; pressed ${state}" } else { "" }}")
    if name == "start" { println("  sizes: ${sizes(window.host.shown[0])}") }
  }
  0
}

let pressed = pure (_: i64, ev: Ev) -> i64 => match ev {
  Pressed(i) => i,
}

/// Each scroller's port and content, by its key.
let sizes = (laid: Laid<Ev>) -> string => {
  var out: []string = []
  each_size(laid, out)
  out.join(", ")
}

let each_size = (laid: Laid<Ev>, out: mut []string) -> void => {
  if let Some(sc) = laid.scroller {
    let parts = laid.id.split(":")
    let name = if laid.id == "#" { "window" } else { parts[parts.len() - 1] }
    out.push("${name} ${sc.port.w}x${sc.port.h} of ${sc.content.w}x${sc.content.h}")
  }
  for c in laid.children { each_size(c.laid, out) }
}

/// Each scroller's offset, by its key — the window's as "window".
let offsets = (laid: Laid<Ev>) -> string => {
  var out: []string = []
  collect(laid, out)
  out.join(", ")
}

let collect = (laid: Laid<Ev>, out: mut []string) -> void => {
  if let Some(sc) = laid.scroller {
    let parts = laid.id.split(":")
    let name = if laid.id == "#" { "window" } else { parts[parts.len() - 1] }
    if sc.offset.x > 0.0 || sc.offset.y > 0.0 { out.push("${name} ${sc.offset.x},${sc.offset.y}") }
  }
  for c in laid.children { collect(c.laid, out) }
}
`)
	want := `start: 
  sizes: window 300x400 of 300x780, down 300x100 of 300x300, across 300x40 of 600x40, both 300x100 of 400x300, hidden 300x40 of 300x100, outer 300x100 of 300x300, inner 300x50 of 600x50
a click on a row presses it: ; pressed 0
a click below the list is not a hidden row's: 
wheel down: down 0,120
wheel past the end: down 0,200
then bubbles to the window: window 0,40, down 0,200
wheel across: across 80,0
both, either axis: both 40,40
hidden passes to the window: window 0,40
nested: across stays in, down goes out: outer 0,40, inner 40,0
drag the list's thumb: down 0,62.5
click the list's track pages: down 0,60
End, Home, Page Down over the list: down 0,60
Tab to the eighth row reveals it: down 0,140
a window scrolled to its end, then taller, clamps: window 0,180
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The window widens its content only to the width below which it overflows: a flex row of a
// growing field and a button in a window narrower than the field's own 160 overflows and
// scrolls, as in a browser, until the field asks min-width: 0 — then the row fits the window
// and the field takes what the button leaves. 7GUIs' counter is the case.
func TestRun_UiWindowFitsAShrinkingFlexItem(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, running, step_running }
import ui.script.{ timed_platform }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Style, insets }
import ui.layout.{ Node, FieldSpec, flex, text_input, button }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Go | Typed(string)

/// The field's least width: its content's (None), or what it asks for.
let view = pure (least: Maybe<f32>) -> Node<Ev> => flex([
  text_input(FieldSpec { value: "0", on_edit: Typed }).with_style(Style { grow: Some(1.0), min_width: least }),
  button("Count", Go),
]).with_style(Style { padding: Some(insets(16.0)), gap: Some(8.0) })

let main = () -> u8 => {
  let cases: []Maybe<f32> = [None, Some(0.0)]
  for least in cases {
    var platform = timed_platform([], Size { w: 200.0, h: 64.0 })
    var renderer = Blank {}
    var fonts = fonts_new()
    let window = running(sandbox(view, (s: Maybe<f32>, _: Ev) => s, Color { r: 0, g: 0, b: 0 }), least, platform, renderer, fonts)
    while step_running(window) {}
    let laid = window.host.shown[0]
    let Some(sc) = laid.scroller else { return 1 }
    let field = laid.children[0]
    let count = laid.children[1]
    println("min-width ${least ?? -1.0}: window ${sc.port.w} of ${sc.content.w}; field ${field.at.x} to ${field.at.x + field.laid.size.w}, button ${count.at.x} to ${count.at.x + count.laid.size.w}")
  }
  0
}
`)
	// The port is the window inside the row's padding, 168. Without min-width the field keeps
	// its 160 and the row overflows to 196, scrolled; with it, the row is 168 and the field
	// takes what the button (its padding alone: no font is loaded) leaves.
	want := `min-width -1: window 168 of 196; field 16 to 176, button 184 to 212
min-width 0: window 168 of 168; field 16 to 148, button 156 to 184
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Transitions, frame by frame at clock times set by hand: CSS's timing curves; colours mixed
// premultiplied; nothing moving on the first frame; an opacity fading its child with it; a
// background named in the transition moving while a radius not named jumps; padding moving
// after its delay and laid out again each frame it moves, while paint-only properties lay
// nothing out; all moving every property; idle once arrived; and a change back mid-flight
// starting from what is shown.
func TestRun_UiTransitions(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.draw.{ Size, Color }
import ui.text.{ Fonts, fonts_new }
import ui.style.{
  Style,
  Rule,
  Sheet,
  Insets,
  Transition,
  TimingLinear,
  TimingEase,
  TimingEaseIn,
  TimingEaseOut,
  TimingEaseInOut,
  TimingSteps,
  TimingBezier,
  light_theme,
}
import ui.motion.{ progress, mix_color }
import ui.layout.{ Node, NoEvent, Laid, LayoutCache, layout_cache, column, sized, themed, tight }

/// Transitions frame by frame, at clock times set by hand.
let main = () -> u8 => {
  // Timing functions at a few points, as CSS's curves give them.
  for (name, t) in [("linear", TimingLinear), ("ease", TimingEase), ("ease-in", TimingEaseIn), ("ease-out", TimingEaseOut), ("ease-in-out", TimingEaseInOut), ("steps(4)", TimingSteps(4, false)), ("steps(4, jump-start)", TimingSteps(4, true)), ("cubic-bezier(0, 1, 0, 1)", TimingBezier(0.0, 1.0, 0.0, 1.0))] {
    println("${name}: ${[x in [0.0, 0.25, 0.5, 0.75, 1.0] | round3(progress(t, f32(x)))].join(" ")}")
  }
  // Premultiplied: opaque red into transparent blue keeps red's hue.
  let half = mix_color(Color { r: 255, g: 0, b: 0 }, Color { r: 0, g: 0, b: 255, a: 0 }, 0.5)
  println("red into transparent blue, halfway: ${half.r} ${half.g} ${half.b} ${half.a}")

  var ui = ui_new()
  var fonts = fonts_new()
  var cache: LayoutCache<NoEvent> = layout_cache()
  let _ = at(ui, cache, fonts, 0, false)
  println("first frame moves nothing: ${ui.animating()}")
  for now in [0, 50, 100, 150, 250, 400] {
    let laid = at(ui, cache, fonts, now, true)
    println("t=${now} ${describe(laid)}; laid out ${cache.laid_out}; moving ${ui.animating()}")
  }
  // Back before arriving: from where it is, not from the end.
  for now in [500, 600] { let _ = at(ui, cache, fonts, now, false) }
  for now in [600, 700, 1100] {
    let laid = at(ui, cache, fonts, now, true)
    println("again t=${now} ${describe(laid)}")
  }
  for now in [1150, 1250] {
    let laid = at(ui, cache, fonts, now, false)
    println("back t=${now} ${describe(laid)}")
  }
  0
}

/// The frame at now, each box on or off.
let at = (ui: mut Ui, cache: mut LayoutCache<NoEvent>, fonts: mut Fonts, now: i64, on: bool) -> Laid<NoEvent> => {
  ui.now = now
  ui.frame_within(view(on), tight(Size { w: 300.0, h: 300.0 }), fonts, cache)
}

let round3 = pure (x: f32) -> f32 => f32((x * 1000.0).round()) / 1000.0

/// A box fading (and its child with it), a box whose background is named in its transition
/// and whose radius is not, a box whose padding moves after a delay, and one with all.
let view = pure (on: bool) -> Node<NoEvent> => themed::<NoEvent>(light_theme(), sheet, [column([
  sized::<NoEvent>(Some(100.0), Some(20.0), [sized::<NoEvent>(Some(10.0), Some(10.0)).keyed("child")]).with_class(if on { "fade on" } else { "fade" }).keyed("fade"),
  sized::<NoEvent>(Some(100.0), Some(20.0)).with_class(if on { "named on" } else { "named" }).keyed("named"),
  column([sized::<NoEvent>(Some(10.0), Some(10.0))]).with_class(if on { "pad on" } else { "pad" }).keyed("pad"),
  sized::<NoEvent>(Some(100.0), Some(20.0)).with_class(if on { "every on" } else { "every" }).keyed("every"),
])])

let sheet = Sheet { rules: [
  Rule { selector: ".fade", style: Style { opacity: Some(1.0), transition: Some([Transition { property: "opacity", duration: 200.0, timing: TimingLinear }]) } },
  Rule { selector: ".fade.on", style: Style { opacity: Some(0.0) } },
  Rule { selector: ".named", style: Style { background: Some(Color { r: 0, g: 0, b: 0 }), radius: Some(0.0), transition: Some([Transition { property: "background-color", duration: 200.0, timing: TimingLinear }]) } },
  Rule { selector: ".named.on", style: Style { background: Some(Color { r: 200, g: 100, b: 0 }), radius: Some(10.0) } },
  Rule { selector: ".pad", style: Style { padding: Some(Insets {}), transition: Some([Transition { property: "padding", duration: 100.0, timing: TimingLinear, delay: 100.0 }]) } },
  Rule { selector: ".pad.on", style: Style { padding: Some(Insets { top: 20.0, right: 20.0, bottom: 20.0, left: 20.0 }) } },
  Rule { selector: ".every", style: Style { opacity: Some(1.0), filter_blur: Some(0.0), transition: Some([Transition { property: "all", duration: 200.0, timing: TimingLinear }]) } },
  Rule { selector: ".every.on", style: Style { opacity: Some(0.5), filter_blur: Some(8.0) } },
] }

/// What each box draws with.
let describe = pure (laid: Laid<NoEvent>) -> string => {
  let fade = find(laid, "fade")
  let child = find(laid, "child")
  let named = find(laid, "named")
  let pad = find(laid, "pad")
  let every = find(laid, "every")
  let bg = named.style.background ?? Color { r: 0, g: 0, b: 0, a: 0 }
  "fade ${round3(fade.style.opacity)} (child ${round3(child.style.opacity)}), named ${bg.r},${bg.g},${bg.b} radius ${named.style.radius}, pad ${pad.size.h} tall, every ${round3(every.style.opacity)} blur ${round3(every.style.filter_blur)}"
}

let find = pure (laid: Laid<NoEvent>, key: string) -> Laid<NoEvent> => {
  if laid.id.ends_with(key) { return laid }
  for c in laid.children {
    let f = find(c.laid, key)
    if f.id.ends_with(key) { return f }
  }
  laid
}
`)
	want := `linear: 0 0.25 0.5 0.75 1
ease: 0 0.409 0.802 0.96 1
ease-in: 0 0.093 0.315 0.622 1
ease-out: 0 0.378 0.685 0.907 1
ease-in-out: 0 0.129 0.5 0.871 1
steps(4): 0 0.25 0.5 0.75 1
steps(4, jump-start): 0.25 0.5 0.75 1 1
cubic-bezier(0, 1, 0, 1): 0 0.949 0.991 0.999 1
red into transparent blue, halfway: 255 0 0 128
first frame moves nothing: false
t=0 fade 1 (child 1), named 0,0,0 radius 10, pad 10 tall, every 1 blur 0; laid out 4; moving true
t=50 fade 0.75 (child 0.75), named 50,25,0 radius 10, pad 10 tall, every 0.875 blur 2; laid out 0; moving true
t=100 fade 0.5 (child 0.5), named 100,50,0 radius 10, pad 10 tall, every 0.75 blur 4; laid out 0; moving true
t=150 fade 0.25 (child 0.25), named 150,75,0 radius 10, pad 30 tall, every 0.625 blur 6; laid out 4; moving true
t=250 fade 0 (child 0), named 200,100,0 radius 10, pad 50 tall, every 0.5 blur 8; laid out 4; moving false
t=400 fade 0 (child 0), named 200,100,0 radius 10, pad 50 tall, every 0.5 blur 8; laid out 3; moving false
again t=600 fade 0.5 (child 0.5), named 100,50,0 radius 10, pad 50 tall, every 0.75 blur 4
again t=700 fade 0.25 (child 0.25), named 150,75,0 radius 10, pad 50 tall, every 0.625 blur 6
again t=1100 fade 0 (child 0), named 200,100,0 radius 10, pad 50 tall, every 0.5 blur 8
back t=1150 fade 0 (child 0), named 200,100,0 radius 0, pad 50 tall, every 0.5 blur 8
back t=1250 fade 0.5 (child 0.5), named 100,50,0 radius 0, pad 50 tall, every 0.75 blur 4
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A host draws a frame a pass while a transition moves and none once it has arrived, however
// long the wait: 15 frames across a second holding a 200 ms fade. Leaving mid-way eases back.
func TestRun_UiTransitionsDrawOnlyWhileMoving(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, running, step_running }
import ui.script.{ timed_platform, parse_steps }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Style, Rule, Sheet, Transition, TimingLinear, light_theme }
import ui.layout.{ Node, NoEvent, Laid, column, sized, themed }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

/// A box fading on hover over 200 ms: the host draws a frame a pass while it moves and none
/// once it has arrived, however long the wait; leaving mid-way eases back from where it is.
let main = () -> u8 => {
  let Ok(steps) = parse_steps("move:50,50 wait:1000 move:50,300 wait:100 move:50,50 wait:2000") else { return 1 }
  var platform = timed_platform(steps, Size { w: 200.0, h: 400.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let window = running(sandbox(view, (n: i64, _: NoEvent) => n, Color { r: 0, g: 0, b: 0 }), 0, platform, renderer, fonts)
  var said = -1
  while step_running(window) {
    let c = window.platform.clock
    if c != said && (c == 1000 || c == 1100 || c == 3100) {
      said = c
      println("t=${c}: ${window.host.frames} frames, opacity ${find(window.host.shown[0], "box").style.opacity}")
    }
  }
  println("end: ${window.host.frames} frames")
  0
}

let sheet = Sheet { rules: [
  Rule { selector: ".box", style: Style { opacity: Some(1.0), transition: Some([Transition { property: "opacity", duration: 200.0, timing: TimingLinear }]) } },
  Rule { selector: ".box:hover", style: Style { opacity: Some(0.2) } },
] }

let view = pure (_: i64) -> Node<NoEvent> => themed::<NoEvent>(light_theme(), sheet, [column([sized::<NoEvent>(Some(100.0), Some(100.0)).with_class("box").keyed("box")])])

let find = pure (laid: Laid<NoEvent>, key: string) -> Laid<NoEvent> => {
  if laid.id.ends_with(key) { return laid }
  for c in laid.children {
    let f = find(c.laid, key)
    if f.id.ends_with(key) { return f }
  }
  laid
}
`)
	want := `t=1000: 15 frames, opacity 0.2
t=1100: 22 frames, opacity 0.584
t=3100: 37 frames, opacity 0.2
end: 37 frames
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// States as CSS's pseudo-classes: a compound's and a part rule's specificity, a class of a
// state's name no longer matching it; :hover and :active reaching every element round the one
// they name; :focus from the pointer without :focus-visible, and from Tab with it; :focus-within
// on the card around the focus; :checked; and a disabled button :disabled, skipped by Tab and
// neither pressed nor focused by a click.
func TestRun_UiPseudoClasses(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.script.{ script_platform }
import ui.backend.{ PointerMoved, PointerDown, PointerUp, KeyPressed, Tab, Primary, Modifiers }
import ui.draw.{ Point, Size, Color }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ Style, Rule, Sheet, light_theme, specificity, part_specificity }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, sized, button, checkbox, themed, tight }

data Ev = Go | Off | Tick

/// Each state sets a property of its own — none that moves anything — so one line shows every
/// state an element is in.
let main = () -> u8 => {
  println("Button.primary:hover on a hovered primary button: ${specificity("Button.primary:hover", "Button", "primary :hover")}")
  println(":hover alone on hovered text: ${specificity(":hover", "Text", ":hover")}")
  println("Button:hover on a button not hovered: ${specificity("Button:hover", "Button", "primary")}")
  println("Button.hover (a class) on a hovered button: ${specificity("Button.hover", "Button", ":hover")}")
  println("Slider:hover::thumb:active on a held thumb of a hovered slider: ${part_specificity("Slider:hover::thumb:active", "Slider", ":hover", "thumb", ":active")}")
  println("Slider::thumb:active on a thumb not held: ${part_specificity("Slider::thumb:active", "Slider", ":hover", "thumb", ":hover")}")
  var ui = ui_new()
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var cache: LayoutCache<Ev> = layout_cache()
  var platform = script_platform([], Size { w: 300.0, h: 300.0 })
  let none = Modifiers {}
  for (name, input) in [
    ("at rest", PointerMoved(Point { x: 290.0, y: 290.0 })),
    ("pointer over go", PointerMoved(Point { x: 20.0, y: 10.0 })),
    ("go held", PointerDown(Primary, Point { x: 20.0, y: 10.0 }, 1)),
    ("go let go", PointerUp(Primary, Point { x: 20.0, y: 10.0 })),
    ("Tab", KeyPressed(Tab, none)),
    ("off held", PointerDown(Primary, Point { x: 20.0, y: 50.0 }, 1)),
  ] {
    let laid = frame(ui, cache, fonts)
    let events = ui.handle(laid, input, platform)
    let after = frame(ui, cache, fonts)
    println("${name}: ${events.len()} events")
    for key in ["card", "go", "off", "tick"] { println("  ${key}: ${states(find(after, key))}") }
  }
  0
}

let frame = (ui: mut Ui, cache: mut LayoutCache<Ev>, fonts: mut Fonts) -> Laid<Ev> =>
  ui.frame_within(view(), tight(Size { w: 300.0, h: 300.0 }), fonts, cache)

let view = pure () -> Node<Ev> => themed::<Ev>(light_theme(), sheet, [column([
  column([
    sized::<Ev>(Some(100.0), Some(30.0), [button("Go", Go).keyed("go")]),
    sized::<Ev>(Some(100.0), Some(30.0), [button("Off", Off).disabled_if(true).keyed("off")]),
    checkbox("Tick", true, Tick).keyed("tick"),
  ]).keyed("card").with_class("card"),
])])

let sheet = Sheet { rules: [
  Rule { selector: ":hover", style: Style { opacity: Some(0.5) } },
  Rule { selector: ":active", style: Style { radius: Some(7.0) } },
  Rule { selector: ":focus", style: Style { filter_blur: Some(2.0) } },
  Rule { selector: ":focus-visible", style: Style { backdrop_blur: Some(9.0) } },
  Rule { selector: ".card:focus-within", style: Style { border_color: Some(Color { r: 1, g: 2, b: 3 }) } },
  Rule { selector: ":checked", style: Style { background: Some(Color { r: 11, g: 22, b: 33 }) } },
  Rule { selector: ":disabled", style: Style { background: Some(Color { r: 44, g: 55, b: 66 }) } },
  Rule { selector: ":enabled", style: Style { color: Some(Color { r: 0, g: 99, b: 0 }) } },
] }

/// The states laid shows, by the property each sets.
let states = pure (laid: Laid<Ev>) -> string => {
  let s = laid.style
  var out: []string = []
  if s.own_opacity < 1.0 { out.push("hover") }
  if s.radius > 6.0 { out.push("active") }
  if s.filter_blur > 1.0 { out.push("focus") }
  if s.backdrop_blur > 8.0 { out.push("focus-visible") }
  if s.border_color == Some(Color { r: 1, g: 2, b: 3 }) { out.push("focus-within") }
  if s.background == Some(Color { r: 11, g: 22, b: 33 }) { out.push("checked") }
  if s.background == Some(Color { r: 44, g: 55, b: 66 }) { out.push("disabled") }
  if s.color.g == 99 { out.push("enabled") }
  if out.len() == 0 { "-" } else { out.join(" ") }
}

let find = pure (laid: Laid<Ev>, key: string) -> Laid<Ev> => {
  if laid.id.ends_with(":${key}") { return laid }
  for c in laid.children {
    let f = find(c.laid, key)
    if f.id.ends_with(":${key}") { return f }
  }
  laid
}
`)
	want := `Button.primary:hover on a hovered primary button: 21
:hover alone on hovered text: 10
Button:hover on a button not hovered: 0
Button.hover (a class) on a hovered button: 0
Slider:hover::thumb:active on a held thumb of a hovered slider: 21
Slider::thumb:active on a thumb not held: 0
at rest: 0 events
  card: -
  go: enabled
  off: disabled
  tick: checked enabled
pointer over go: 0 events
  card: hover
  go: hover enabled
  off: disabled
  tick: checked enabled
go held: 0 events
  card: hover active focus-within
  go: hover active focus enabled
  off: disabled
  tick: checked enabled
go let go: 1 events
  card: hover focus-within
  go: hover focus enabled
  off: disabled
  tick: checked enabled
Tab: 0 events
  card: hover focus-within
  go: hover enabled
  off: disabled
  tick: focus focus-visible checked enabled
off held: 0 events
  card: hover
  go: hover enabled
  off: disabled
  tick: checked enabled
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Edit's items handed to the focused field (Ui.edit_focused, Command.EditField), as the Mac's
// bar takes their keys before a field hears them: all of a colour's text selected and copied,
// another pasted over it — the field's edit raised — and nothing with no field focused.
func TestRun_UiEditFocused(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, FieldSelectAll, FieldCopy, FieldPaste }
import ui.script.{ script_platform }
import ui.backend.{ PointerMoved, PointerDown, PointerUp, Primary, Platform }
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, text_input, FieldSpec, tight }

data Ev = Edited(string)

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var ui = ui_new()
  var cache: LayoutCache<Ev> = layout_cache()
  var platform = script_platform([], Size { w: 300.0, h: 100.0 })
  let unfocused = ui.edit_focused(frame(ui, cache, fonts), FieldCopy, platform)
  println("nothing focused: ${unfocused.len()} events, the clipboard '${platform.clipboard()}'")
  let laid = frame(ui, cache, fonts)
  let field = laid.children[0]
  let at = Point { x: field.at.x + field.laid.size.w / 2.0, y: field.at.y + field.laid.size.h / 2.0 }
  for input in [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)] {
    let _ = ui.handle(frame(ui, cache, fonts), input, platform)
  }
  let _ = ui.edit_focused(frame(ui, cache, fonts), FieldSelectAll, platform)
  let _ = ui.edit_focused(frame(ui, cache, fonts), FieldCopy, platform)
  println("all selected and copied: the clipboard '${platform.clipboard()}'")
  platform.set_clipboard("#ffffff")
  let _ = ui.edit_focused(frame(ui, cache, fonts), FieldSelectAll, platform)
  let pasted = ui.edit_focused(frame(ui, cache, fonts), FieldPaste, platform)
  let texts = [e in pasted | match e { Edited(t) => t }]
  println("pasted over it: [${texts.join(", ")}]")
  0
}

let frame = (ui: mut Ui, cache: mut LayoutCache<Ev>, fonts: mut Fonts) -> Laid<Ev> =>
  ui.frame_within(view(), tight(Size { w: 300.0, h: 100.0 }), fonts, cache)

let view = pure () -> Node<Ev> => column([text_input(FieldSpec { value: "#1a2b3c", on_edit: Edited })])
`)
	want := "nothing focused: 0 events, the clipboard ''\nall selected and copied: the clipboard '#1a2b3c'\npasted over it: [#ffffff]\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A read-only field (read_only_input, lui's readonly): it takes the focus, moves its caret,
// selects and copies, and raises no edit for typing, deleting, pasting, cutting or undo; the
// cascade sees it :read-only.
// A double click in a text field selects the word under the pointer — on the right half of a
// word's last letter too, which is nearer the gap after it — or the run of spaces or
// punctuation it is on, so `34` alone in `12.34`; a
// triple click selects the whole text; and a drag after a double click extends by whole
// words either way, after a triple keeps the whole. Positions are read from the laid-out
// field, so no font's widths are written here; the script's `double:X,Y` is checked too.
func TestRun_UiFieldSelectsAWordOnADoubleClick(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.script.{ script_platform, parse_script }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary }
import std.collections
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new, caret_x }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, text_input, FieldSpec, Prose, tight }

data Ev = Edited(string)

const TEXT: string = "hello brave  world 12.34"

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  // Where rune k's left edge is, and the field's middle, from a first layout.
  var ui = ui_new()
  var cache: LayoutCache<Ev> = layout_cache()
  let laid = frame(ui, cache, fonts)
  let field = laid.children[0]
  let Prose(block) = field.laid.content else { return 2 }
  let left = field.at.x + field.laid.content_at.x
  let y = field.at.y + field.laid.size.h / 2.0
  // A point a fraction of the way across rune k.
  let on = (k: i64, part: f32) -> Point => {
    let a = caret_x(block, k)
    let b = caret_x(block, k + 1)
    Point { x: left + a + (b - a) * part, y: y }
  }
  let double = (at: Point) -> []InputEvent => [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at), PointerDown(Primary, at, 2)]
  let triple = (at: Point) -> []InputEvent => [...double(at), PointerUp(Primary, at), PointerDown(Primary, at, 3)]
  let cases: [](string, []InputEvent) = [
    ("double in brave", [...double(on(8, 0.5)), PointerUp(Primary, on(8, 0.5))]),
    ("double on hello's last letter, right half", [...double(on(4, 0.9)), PointerUp(Primary, on(4, 0.9))]),
    ("double on the two spaces", [...double(on(12, 0.5)), PointerUp(Primary, on(12, 0.5))]),
    ("double on 34 in 12.34", [...double(on(23, 0.5)), PointerUp(Primary, on(23, 0.5))]),
    ("double on its point", [...double(on(21, 0.5)), PointerUp(Primary, on(21, 0.5))]),
    ("double on 12", [...double(on(19, 0.5)), PointerUp(Primary, on(19, 0.5))]),
    ("triple", [...triple(on(8, 0.5)), PointerUp(Primary, on(8, 0.5))]),
    ("double on brave, dragged to world", [...double(on(8, 0.5)), PointerMoved(on(15, 0.5)), PointerUp(Primary, on(15, 0.5))]),
    ("double on brave, dragged to hello", [...double(on(8, 0.5)), PointerMoved(on(1, 0.5)), PointerUp(Primary, on(1, 0.5))]),
    ("triple, dragged", [...triple(on(8, 0.5)), PointerMoved(on(1, 0.5)), PointerUp(Primary, on(1, 0.5))]),
  ]
  for (name, inputs) in cases { show(name, inputs, fonts) }
  let at = on(15, 0.5)
  let Ok(scripted) = parse_script("double:${at.x.round()},${at.y.round()}") else { return 3 }
  show("script double:X,Y in world", scripted, fonts)
  0
}

/// The field's selection after these inputs, from nothing focused.
let show = (name: string, inputs: []InputEvent, fonts: mut Fonts) -> void => {
  var ui = ui_new()
  var cache: LayoutCache<Ev> = layout_cache()
  var platform = script_platform([], Size { w: 300.0, h: 100.0 })
  for input in inputs {
    let laid = frame(ui, cache, fonts)
    let _ = ui.handle(laid, input, platform)
  }
  let laid = frame(ui, cache, fonts)
  let id = laid.children[0].laid.id
  let state = ui.state.widgets.get(id)
  let anchor = state.map((w) => w.anchor) ?? -1
  let caret = state.map((w) => w.caret) ?? -1
  let low = anchor.min(caret)
  let high = anchor.max(caret)
  println("${name}: '${TEXT.slice(low, high)}' (anchor ${anchor}, caret ${caret})")
}

let frame = (ui: mut Ui, cache: mut LayoutCache<Ev>, fonts: mut Fonts) -> Laid<Ev> =>
  ui.frame_within(view(), tight(Size { w: 300.0, h: 100.0 }), fonts, cache)

let view = pure () -> Node<Ev> => column([text_input(FieldSpec { value: TEXT, on_edit: Edited })])
`)
	want := `double in brave: 'brave' (anchor 6, caret 11)
double on hello's last letter, right half: 'hello' (anchor 0, caret 5)
double on the two spaces: '  ' (anchor 11, caret 13)
double on 34 in 12.34: '34' (anchor 22, caret 24)
double on its point: '.' (anchor 21, caret 22)
double on 12: '12' (anchor 19, caret 21)
triple: 'hello brave  world 12.34' (anchor 0, caret 24)
double on brave, dragged to world: 'brave  world' (anchor 6, caret 18)
double on brave, dragged to hello: 'hello brave' (anchor 11, caret 0)
triple, dragged: 'hello brave  world 12.34' (anchor 0, caret 24)
script double:X,Y in world: 'world' (anchor 13, caret 18)
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Each corner's radius (`Style.corners`, CSS's border-*-radius): an element's corners where a
// rule set them, whatever another rule's `radius`; `radius` alone otherwise; and a rule's
// `radius` after another's `corners` leaving the corners (the longhands are kept, as fields).
func TestRun_UiCornerRadii(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.draw.{ Size, Radii }
import ui.text.{ fonts_new }
import ui.style.{ light_theme, Sheet, Rule, Style }
import ui.layout.{ Node, Laid, column, text, themed, tight, layout_cache }

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var ui = ui_new()
  var cache = layout_cache::<i64>()
  let sheet = Sheet {
    rules: [
      Rule { selector: ".round", style: Style { radius: Some(6.0) } },
      Rule { selector: ".top", style: Style { corners: Some(Radii { top_left: 6.0, top_right: 6.0 }) } },
      Rule { selector: ".top.later", style: Style { radius: Some(9.0) } },
    ],
  }
  let view = themed::<i64>(light_theme(), sheet, [
    column([
      text::<i64>("a").with_class("round"),
      text::<i64>("b").with_class("top round"),
      text::<i64>("c").with_class("top later"),
    ]),
  ])
  let laid = ui.frame_within(view, tight(Size { w: 200.0, h: 200.0 }), fonts, cache)
  show(laid)
  0
}

let show = (laid: Laid<i64>) -> void => {
  if laid.class.len() > 0 {
    let corners = match laid.style.corners {
      Some(r) => "${r.top_left} ${r.top_right} ${r.bottom_right} ${r.bottom_left}",
      None => "none",
    }
    println("${laid.class}: radius ${laid.style.radius}, corners ${corners}")
  }
  for child in laid.children { show(child.laid) }
}
`)
	want := `round: radius 6, corners none
top round: radius 6, corners 6 6 0 0
top later: radius 9, corners 6 6 0 0
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The window's place on its screens (ui.sdl3, under SDL's dummy driver): where it is, moved,
// and a place no display shows — one saved while another screen was plugged in — ignored, since
// SDL's display for a point is merely the nearest.
func TestRun_UiWindowPosition(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	setenv(t, "SDL_RENDER_DRIVER", "software")
	got := runUiProgram(t, `import ui.sdl3
import ui.backend.{ Platform }
import ui.draw.{ Point }

let main = () -> u8 => {
  let window = match sdl3.open("Place", 300.0, 200.0) {
    Ok(w) => w,
    Err(why) => {
      println("no window: ${why}")
      return 0
    },
  }
  var p = window.platform
  println("placed: ${p.window_position().is_some()}")
  p.set_window_position(Point { x: 120.0, y: 80.0 })
  println("moved: ${describe(p.window_position())}")
  p.set_window_position(Point { x: 100000.0, y: 100000.0 })
  println("off every display: ${describe(p.window_position())}")
  sdl3.close(p, window.renderer)
  0
}

let describe = pure (at: Maybe<Point>) -> string => match at {
  Some(pt) => "${pt.x},${pt.y}",
  None => "unknown",
}
`)
	if strings.HasPrefix(got, "no window") {
		t.Skipf("SDL could not open a dummy window here: %s", got)
	}
	want := "placed: true\nmoved: 120,80\noff every display: 120,80\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Pictures the program names, kept by the host (ui.layout's picture and PaintPicture,
// ui.text's GlyphAtlas): uploaded the first frame a key is drawn, not again while its
// revision holds, written in place when the revision moves at the same size, released and
// uploaded anew at another, and released the frame no view names it; a painter's drawn at
// its opacity, into its place, from the texels it names.
func TestRun_UiPictures(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.backend.{ Renderer }
import ui.draw.{ Size, Point, Rect, Image, TextureId, PictureSource, Picture }
import ui.text.{ fonts_new, glyph_atlas }
import ui.interact.{ ui_new }
import ui.style.{ Style }
import ui.layout.{ Node, LayoutCache, layout_cache, column, picture, painted, PaintArea, PaintOp, PaintPicture, PaintDraw, paint, tight, picture_fill, FitTile, FitStretch, FitCover, Fit }
import ui.draw.{ radius, PushClip }

/// A renderer that counts what it is asked to do with textures.
struct Counting {
  uploads: i64 = 0,
  updates: i64 = 0,
  releases: i64 = 0,
  next: i64 = 0,
}

impl Renderer for Counting {
  upload = (self, image) => {
    self.uploads += 1
    self.next += 1
    Some(TextureId(self.next - 1))
  }
  update = (self, id, at, image) => {
    self.updates += 1
    true
  }
  release = (self, id) => { self.releases += 1 }
  draw = (self, background, ops) => {}
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var ui = ui_new()
  var cache: LayoutCache<i64> = layout_cache()
  var atlas = glyph_atlas()
  var r = Counting {}
  let small = Image { width: 2, height: 2, argb: [0xFF0000FF, 0xFF00FF00, 0xFFFF0000, 0xFFFFFFFF] }
  let large = Image { width: 3, height: 1, argb: [0xFF000000, 0xFF000000, 0xFF000000] }
  let frames: [](string, Image, i64, bool) = [
    ("first", small, 1, true),
    ("the same revision", small, 1, true),
    ("a new revision, same size", small, 2, true),
    ("a new revision, a new size", large, 3, true),
    ("not named", large, 3, false),
  ]
  for (name, image, revision, named) in frames {
    let view = column::<i64>(if named { [picture::<i64>("art", image, revision, Size { w: 20.0, h: 20.0 })] } else { [] })
    let laid = ui.frame_within(view, tight(Size { w: 100.0, h: 100.0 }), fonts, cache)
    let _ = paint(laid, Point {}, 1.0, fonts, atlas, r)
    println("${name}: ${r.uploads} uploads, ${r.updates} updates, ${r.releases} releases, ${atlas.picture_count()} kept")
  }
  // A painter's: half the image's texels, into its place moved to the painter's, half opaque.
  let canvas = PictureSource { key: "canvas", image: small, revision: 1 }
  let paints = (_: PaintArea) -> []PaintOp =>
    [PaintPicture(canvas, Rect { w: 1.0, h: 2.0 }, Rect { x: 5.0, y: 5.0, w: 10.0, h: 20.0 }, 0.5)]
  let view = column::<i64>([sized_painter(paints)])
  let laid = ui.frame_within(view, tight(Size { w: 100.0, h: 100.0 }), fonts, cache)
  for op in paint(laid, Point {}, 1.0, fonts, atlas, r) {
    if let Picture(_, texels, at, opacity) = op {
      println("painted: texels ${texels.w}x${texels.h} into ${at.x},${at.y} ${at.w}x${at.h} at ${opacity}")
    }
  }
  // The fills: a 4x2 picture stretched, cut to cover a square, and tiled at two pixels a point;
  // a single pixel tiled, its tile doubled until there are few enough.
  let wide = PictureSource { key: "wide", image: Image { width: 4, height: 2, argb: [0, 0, 0, 0, 0, 0, 0, 0] } }
  let dot = PictureSource { key: "dot", image: Image { width: 1, height: 1, argb: [0] } }
  fill("stretched", picture_fill(wide, FitStretch, Rect { w: 100.0, h: 50.0 }, radius(8.0), 1.0, 2.0))
  fill("covering a square", picture_fill(wide, FitCover, Rect { w: 100.0, h: 100.0 }, radius(8.0), 1.0, 2.0))
  fill("tiled", picture_fill(wide, FitTile, Rect { w: 100.0, h: 50.0 }, radius(0.0), 1.0, 2.0))
  fill("a pixel tiled", picture_fill(dot, FitTile, Rect { w: 400.0, h: 300.0 }, radius(0.0), 1.0, 2.0))
  0
}

/// How many pictures a fill draws, the first one's texels and place, and whether it is clipped.
let fill = (name: string, ops: []PaintOp) -> void => {
  var pictures = 0
  var first = ""
  var clipped = false
  for op in ops {
    match op {
      PaintPicture(_, texels, at, _) => {
        if pictures == 0 { first = "texels ${texels.x},${texels.y} ${texels.w}x${texels.h} into ${at.w}x${at.h}" }
        pictures += 1
      },
      PaintDraw(PushClip(_, corners)) => { clipped = corners.top_left > 0.0 },
      _ => {},
    }
  }
  println("${name}: ${pictures}, the first ${first}, rounded ${clipped}")
}

let sized_painter = pure (paints: (PaintArea) -> []PaintOp) -> Node<i64> =>
  painted::<i64>(paints).with_style(Style { width: Some(50.0), height: Some(50.0) })
`)
	want := `first: 1 uploads, 0 updates, 0 releases, 1 kept
the same revision: 1 uploads, 0 updates, 0 releases, 1 kept
a new revision, same size: 1 uploads, 1 updates, 0 releases, 1 kept
a new revision, a new size: 2 uploads, 1 updates, 1 releases, 1 kept
not named: 2 uploads, 1 updates, 2 releases, 0 kept
painted: texels 1x2 into 5,5 10x20 at 0.5
stretched: 1, the first texels 0,0 4x2 into 100x50, rounded true
covering a square: 1, the first texels 1,0 2x2 into 100x100, rounded true
tiled: 2500, the first texels 0,0 4x2 into 2x1, rounded false
a pixel tiled: 1900, the first texels 0,0 1x1 into 8x8, rounded false
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A select's options each in their own font family (SelectSpec.option_families, a font list
// showing each font in itself): in the open list, and on the select for the chosen one; an
// option given none, the select's own. The families need not be loaded: matching falls back.
// The select is mapped to another event type, as a component's page is: the list is made at
// layout from the spec, which mapping copies field by field, and Termina's had lost them.
func TestRun_UiSelectOptionFamilies(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary }
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, select, SelectSpec, themed, tight }

struct Run {
  ui: Ui,
  cache: LayoutCache<i64>,
  platform: ScriptPlatform,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: 300.0, h: 400.0 }) }
  let at = middle_of(laid_out(run, fonts), Point {}, "Select")
  for input in [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)] {
    let _ = run.ui.handle(laid_out(run, fonts), input, run.platform)
  }
  var faces: []string = []
  var options: []string = []
  families(laid_out(run, fonts), false, faces, options)
  println("the list: ${options.join(", ")}")
  println("the select, Menlo chosen: ${faces.join(", ")}")
  0
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<i64> =>
  run.ui.frame_within(view(), tight(Size { w: 300.0, h: 400.0 }), fonts, run.cache)

data Picked = Picked(i64)

let view = pure () -> Node<i64> => themed::<i64>(light_theme(), Sheet {}, [
  column([select(SelectSpec { options: ["Plain", "Menlo", "Courier"], selected: 1, on_change: Picked, option_families: ["", "Menlo", "Courier"] }).map(unpicked)]),
])

let unpicked = pure (p: Picked) -> i64 => match p { Picked(k) => k }

/// The family each text was laid in: an option's, and the select face's seen one.
let families = (laid: Laid<i64>, in_option: bool, faces: mut []string, options: mut []string) -> void => {
  let inside = in_option || laid.tag == "Option"
  if laid.tag == "Text" {
    let family = if laid.style.font.family == "" { "the select's" } else { laid.style.font.family }
    if inside { options.push(family) } else if laid.style.opacity > 0.5 { faces.push(family) }
  }
  for child in laid.children { families(child.laid, inside, faces, options) }
}

let middle_of = (laid: Laid<i64>, origin: Point, tag: string) -> Point => {
  if laid.tag == tag { return Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 } }
  for child in laid.children {
    let p = middle_of(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, tag)
    if p.x >= 0.0 { return p }
  }
  Point { x: -1.0, y: -1.0 }
}
`)
	want := "the list: the select's, Menlo, Courier\nthe select, Menlo chosen: Menlo\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A select's separators (ui/WINDOW.md §7, Termina's saved themes after its own): a rule
// before each option listed, between the options in the list; each option keeps its index, so
// a click below a rule chooses the option there, and the keys walk the options over it.
func TestRun_UiSelectSeparators(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, Found }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, Modifiers, ArrowDown, Enter, Key }
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, select, SelectSpec, themed, tight }

struct Run {
  ui: Ui,
  cache: LayoutCache<i64>,
  platform: ScriptPlatform,
  chosen: i64 = 1,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: 300.0, h: 400.0 }) }
  play(run, fonts, click(where_is(run, fonts, "Select")))
  println("open: ${rows(laid_out(run, fonts))}")
  play(run, fonts, click(where_is(run, fonts, "Option", 2)))
  println("clicked the third: ${run.chosen}")
  play(run, fonts, click(where_is(run, fonts, "Select")))
  play(run, fonts, click(where_is(run, fonts, "Option", 1)))
  println("clicked the second: ${run.chosen}")
  play(run, fonts, [...click(where_is(run, fonts, "Select")), ...key(ArrowDown), ...key(Enter)])
  println("Down from the second, over the rule: ${run.chosen}")
  0
}

let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) { run.chosen = e }
  }
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<i64> =>
  run.ui.frame_within(view(run), tight(Size { w: 300.0, h: 400.0 }), fonts, run.cache)

let view = pure (run: Run) -> Node<i64> => themed::<i64>(light_theme(), Sheet {}, [
  column([select(SelectSpec { options: ["Dark", "Light", "Mine"], selected: run.chosen, on_change: (k: i64) -> i64 => k, separators: [2] })]),
])

/// The list's rows top to bottom: each option by its text, a rule as ---.
let rows = pure (laid: Laid<i64>) -> string => {
  var out: []string = []
  gather(laid, out)
  out.join(" ")
}

let gather = (laid: Laid<i64>, out: mut []string) -> void => {
  if laid.tag == "MenuSeparator" { out.push("---") }
  if laid.tag == "Option" { out.push(laid.id.split("/:")[laid.id.split("/:").len() - 1]) }
  for child in laid.children { gather(child.laid, out) }
}

/// The middle of element nth of those tagged tag.
let where_is = (run: mut Run, fonts: mut Fonts, tag: string, nth: i64 = 0) -> Point => {
  var found: []Point = []
  find_all(laid_out(run, fonts), Point {}, tag, found)
  if nth < found.len() { found[nth] } else { Point { x: -1.0, y: -1.0 } }
}

let find_all = (laid: Laid<i64>, origin: Point, tag: string, out: mut []Point) -> void => {
  if laid.tag == tag { out.push(Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 }) }
  for child in laid.children { find_all(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, tag, out) }
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]
`)
	want := `open: 0 1 --- 2
clicked the third: 2
clicked the second: 1
Down from the second, over the rule: 2
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A select (HTML's <select>): a click opens its list over what is below it — a click where
// the list covers a button chooses the option there, not the button — and a click on an
// option chooses it, closes the list and leaves the focus on the select; a click elsewhere
// only closes it; closed, Down and Up choose outright; open, Space opens it, Down moves the
// option under the keys and Enter chooses it, and Escape closes it without a choice. Choosing
// the chosen option raises nothing. Assistive technology hears a combo box whose value is
// the chosen option, and, while it is open, a list box of options.
func TestRun_UiSelect(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find, control_at }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, Key, Modifiers, Enter, Escape, ArrowDown, ArrowUp, Character, AxComboBox, AxListBox, AxOption }
import ui.access.{ access_tree }
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, button, select, SelectSpec, themed, tight, Choosing, option_id, option_of }

data Ev = Chose(i64) | Pressed

const OPTIONS: []string = ["one-way flight", "return flight", "round the world"]

/// The framework's state, the program's — the option chosen, the button's presses — and the
/// events since the last line said.
struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  chosen: i64 = 0,
  pressed: i64 = 0,
  events: []string = [],
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: 400.0, h: 300.0 }) }
  let first = laid_out(run, fonts)
  let sel = select_id(first)
  let the_select = middle(first, sel)
  let the_button = middle(first, button_id(first))

  play(run, fonts, click(the_select))
  say(run, fonts, "click the select")
  play(run, fonts, click(middle(laid_out(run, fonts), option_id(sel, 2))))
  say(run, fonts, "click option 2")
  play(run, fonts, click(the_select))
  let under = control_at(laid_out(run, fonts), the_button).map((f) => option_of(sel, f.laid.id) ?? -1) ?? -1
  println("  under the button's place while open: option ${under}")
  play(run, fonts, click(the_button))
  say(run, fonts, "click where the list covers the button")
  play(run, fonts, click(the_select))
  play(run, fonts, click(Point { x: 390.0, y: 290.0 }))
  say(run, fonts, "click elsewhere")
  play(run, fonts, click(the_select))
  play(run, fonts, click(middle(laid_out(run, fonts), option_id(sel, run.chosen))))
  say(run, fonts, "click the chosen option")
  play(run, fonts, [...key(Escape), ...key(ArrowDown)])
  say(run, fonts, "Down, closed")
  play(run, fonts, key(ArrowUp))
  say(run, fonts, "Up, closed")
  play(run, fonts, [...key(Character(' ')), ...key(ArrowDown), ...key(ArrowDown)])
  say(run, fonts, "Space, Down, Down")
  play(run, fonts, key(Enter))
  say(run, fonts, "Enter")
  play(run, fonts, [...key(Character(' ')), ...key(ArrowUp), ...key(Escape)])
  say(run, fonts, "Space, Up, Escape")
  play(run, fonts, key(Character(' ')))
  let heard = access_tree(laid_out(run, fonts), run.ui.state.focus)
  for n in heard.nodes {
    match n.role {
      AxComboBox => println("  heard: combo box '${n.value}', ${n.children.len()} child"),
      AxListBox => println("  heard: list box of ${n.children.len()}"),
      AxOption => println("  heard: option '${n.label}'"),
      _ => {},
    }
  }
  0
}

/// Each input played against a fresh layout, its events applied.
let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) {
      match e {
        Chose(i) => {
          run.chosen = i
          run.events.push("Chose(${i})")
        },
        Pressed => {
          run.pressed += 1
          run.events.push("Pressed")
        },
      }
    }
  }
}

/// A line for the step just played: its events, the option chosen, and the select's state.
let say = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let id = select_id(laid_out(run, fonts))
  println("${name}: [${run.events.join(", ")}] chosen ${run.chosen}, open ${run.ui.state.open == id}, focus on it ${run.ui.state.focus == id}")
  run.events = []
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run.chosen), tight(Size { w: 400.0, h: 300.0 }), fonts, run.cache)

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]

/// The middle of the element identified as id.
let middle = pure (laid: Laid<Ev>, id: string) -> Point => {
  let Some(f) = find(laid, id, Point {}) else { return Point { x: -1.0, y: -1.0 } }
  Point { x: f.origin.x + f.laid.size.w / 2.0, y: f.origin.y + f.laid.size.h / 2.0 }
}

let view = pure (chosen: i64) -> Node<Ev> => themed::<Ev>(light_theme(), Sheet {}, [
  column([
    select(SelectSpec { options: OPTIONS, selected: chosen, on_change: Chose }),
    button("Book", Pressed),
  ]),
])

/// The select's identity, and the button's.
let select_id = pure (laid: Laid<Ev>) -> string => first_of(laid, true)
let button_id = pure (laid: Laid<Ev>) -> string => first_of(laid, false)

let first_of = pure (laid: Laid<Ev>, choosing: bool) -> string => {
  let found = match laid.kind {
    Choosing(_) => choosing,
    _ => !choosing && laid.tag == "Button",
  }
  if found { return laid.id }
  for child in laid.children {
    let id = first_of(child.laid, choosing)
    if id.len() > 0 { return id }
  }
  ""
}
`)
	under := ""
	for line := range strings.SplitSeq(got, "\n") {
		if rest, ok := strings.CutPrefix(line, "  under the button's place while open: option "); ok {
			under = rest
		}
	}
	if under == "" || under == "-1" {
		t.Fatalf("the open list did not cover the button:\n%s", got)
	}
	want := `click the select: [] chosen 0, open true, focus on it true
click option 2: [Chose(2)] chosen 2, open false, focus on it true
  under the button's place while open: option ` + under + `
click where the list covers the button: [Chose(` + under + `)] chosen ` + under + `, open false, focus on it true
click elsewhere: [] chosen ` + under + `, open false, focus on it false
click the chosen option: [] chosen ` + under + `, open false, focus on it true
Down, closed: [Chose(1)] chosen 1, open false, focus on it true
Up, closed: [Chose(0)] chosen 0, open false, focus on it true
Space, Down, Down: [] chosen 0, open true, focus on it true
Enter: [Chose(2)] chosen 2, open false, focus on it true
Space, Up, Escape: [] chosen 2, open false, focus on it true
  heard: option 'one-way flight'
  heard: option 'return flight'
  heard: option 'round the world'
  heard: list box of 3
  heard: combo box 'round the world', 1 child
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A popup (WINDOW.md §1): its content floats below its anchor, flipped above it at the
// window's foot and moved in from the window's right edge, and a long one is no taller than
// the window and scrolls. A press outside it dismisses it and goes on — but not one on its
// anchor, whose button closes it itself — as do Escape (the innermost of nested popups) and
// the window losing the focus; a press inside a popup opened from inside another dismisses
// neither, and one in the outer away from the inner dismisses the inner. Opened from the
// keyboard, the focus goes to its first control, and back to the anchor when it closes.
func TestRun_UiPopup(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find, Found }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, Key, Modifiers, Enter, Escape, Tab, WindowUnfocused }
import ui.draw.{ Size, Point, Rect }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, button, popup, align, Alignment, themed, tight, PopsUp, Pressable, FloatBeside }

data Ev = Toggle | Dismiss | Pick(i64) | ToggleInner | DismissInner

/// The framework's state, the program's — whether each popup is open, where the anchor sits
/// in the window, how many items more the popup holds — and the events since the last line.
struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  open: bool = false,
  inner: bool = false,
  corner: Alignment = Alignment {},
  more: i64 = 0,
  events: []string = [],
}

const W: f32 = 400.0
const H: f32 = 300.0

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: W, h: H }) }

  run.open = true
  placed(run, fonts, "top left")
  run.corner = Alignment { x: 1.0, y: 1.0 }
  placed(run, fonts, "bottom right")
  run.corner = Alignment {}
  run.more = 40
  placed(run, fonts, "forty more")
  run.more = 0

  play(run, fonts, click(Point { x: 390.0, y: 290.0 }))
  say(run, fonts, "press outside")
  run.open = true
  play(run, fonts, click(anchor_middle(run, fonts)))
  say(run, fonts, "press the anchor")
  run.open = true
  play(run, fonts, click(control_middle(run, fonts, Pick(1))))
  say(run, fonts, "press First")
  run.open = true
  play(run, fonts, key(Escape))
  say(run, fonts, "Escape")
  run.open = true
  play(run, fonts, [WindowUnfocused])
  say(run, fonts, "the window loses the focus")

  run.open = true
  play(run, fonts, click(control_middle(run, fonts, ToggleInner)))
  say(run, fonts, "press More")
  beside(run, fonts)
  play(run, fonts, click(control_middle(run, fonts, Pick(3))))
  say(run, fonts, "press Deep, in the inner popup")
  run.open = true
  run.inner = true
  play(run, fonts, click(control_middle(run, fonts, Pick(1))))
  say(run, fonts, "press First, outside the inner")
  run.open = true
  run.inner = true
  play(run, fonts, key(Escape))
  say(run, fonts, "Escape, both open")

  run.open = false
  run.inner = false
  play(run, fonts, key(Tab))
  say(run, fonts, "Tab")
  play(run, fonts, key(Enter))
  say(run, fonts, "Enter")
  play(run, fonts, key(Escape))
  say(run, fonts, "Escape")
  0
}

/// Where the popup's content went against its anchor, laid out as it is now.
let placed = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let laid = laid_out(run, fonts)
  let Some(p) = popup_in(laid, Point {}) else {
    println("${name}: no popup")
    return
  }
  let a = Rect { x: p.origin.x, y: p.origin.y, w: p.laid.size.w, h: p.laid.size.h }
  let c = p.laid.children[1]
  let b = Rect { x: p.origin.x + c.at.x, y: p.origin.y + c.at.y, w: c.laid.size.w, h: c.laid.size.h }
  let side = if b.y >= a.y + a.h { "below" } else if b.y + b.h <= a.y { "above" } else { "over" }
  let inside = b.x >= 0.0 && b.y >= 0.0 && b.x + b.w <= W + 0.01 && b.y + b.h <= H + 0.01
  let left = if b.x == a.x { "at the anchor's left" } else { "moved in" }
  let scrolls = match c.laid.scroller {
    Some(sc) => sc.content.h > sc.port.h + 0.5,
    None => false,
  }
  println("${name}: ${side} the anchor, ${left}, inside the window ${inside}, scrolls ${scrolls}")
}

/// Where the inner popup, opened from More, went against More.
let beside = (run: mut Run, fonts: mut Fonts) -> void => {
  let Some(outer) = popup_in(laid_out(run, fonts), Point {}) else { return }
  let c = outer.laid.children[1]
  let Some(inner) = popup_in(c.laid, Point { x: outer.origin.x + c.at.x, y: outer.origin.y + c.at.y }) else {
    println("  no inner popup")
    return
  }
  let content = inner.laid.children[1]
  let right = inner.origin.x + content.at.x >= inner.origin.x + inner.laid.size.w
  let level = (content.at.y).abs() < 0.01
  println("  the inner popup: to More's right ${right}, level with it ${level}")
}

/// Each input played against a fresh layout, its events applied as a program would.
let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) {
      run.events.push(named(e))
      match e {
        Toggle => { run.open = !run.open },
        Dismiss => {
          run.open = false
          run.inner = false
        },
        Pick(_) => {
          run.open = false
          run.inner = false
        },
        ToggleInner => { run.inner = !run.inner },
        DismissInner => { run.inner = false },
      }
    }
  }
}

/// A line for the step just played: its events, what is open, and what has the focus.
let say = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let laid = laid_out(run, fonts)
  let focus = match find(laid, run.ui.state.focus, Point {}) {
    Some(f) => match f.laid.kind {
      Pressable(e) => named(e),
      _ => "?",
    },
    None => "nothing",
  }
  println("${name}: [${run.events.join(", ")}] open ${run.open}, inner ${run.inner}, focus ${focus}")
  run.events = []
}

let named = pure (e: Ev) -> string => match e {
  Toggle => "Toggle",
  Dismiss => "Dismiss",
  Pick(i) => "Pick(${i})",
  ToggleInner => "ToggleInner",
  DismissInner => "DismissInner",
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run), tight(Size { w: W, h: H }), fonts, run.cache)

let view = pure (run: Run) -> Node<Ev> => {
  let deep = column([button("Deep", Pick(3))])
  let items = [
    button("First", Pick(1)),
    popup(button("More", ToggleInner), run.inner, deep, DismissInner, FloatBeside),
    button("Last", Pick(2)),
    button("An item wider than the anchor", Pick(4)),
    ...[i in 0..<run.more | button("Item ${i}", Pick(10 + i))],
  ]
  themed::<Ev>(light_theme(), Sheet {}, [
    align(run.corner, popup(button("Menu", Toggle), run.open, column(items), Dismiss)),
  ])
}

/// The outermost popup in laid, and where it is.
let popup_in = pure (laid: Laid<Ev>, origin: Point) -> Maybe<Found<Ev>> => {
  if let PopsUp(_, _, _) = laid.kind { return Some(Found { laid: laid, origin: origin }) }
  for child in laid.children {
    if let Some(f) = popup_in(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }) {
      return Some(f)
    }
  }
  None
}

let anchor_middle = (run: mut Run, fonts: mut Fonts) -> Point => {
  let Some(p) = popup_in(laid_out(run, fonts), Point {}) else { return Point {} }
  Point { x: p.origin.x + p.laid.size.w / 2.0, y: p.origin.y + p.laid.size.h / 2.0 }
}

/// The middle of the button raising e.
let control_middle = (run: mut Run, fonts: mut Fonts, e: Ev) -> Point =>
  button_at(laid_out(run, fonts), Point {}, named(e)) ?? Point { x: -1.0, y: -1.0 }

let button_at = pure (laid: Laid<Ev>, origin: Point, name: string) -> Maybe<Point> => {
  if let Pressable(e) = laid.kind {
    if named(e) == name { return Some(Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 }) }
  }
  for child in laid.children {
    if let Some(p) = button_at(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, name) {
      return Some(p)
    }
  }
  None
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]
`)
	want := `top left: below the anchor, at the anchor's left, inside the window true, scrolls false
bottom right: above the anchor, moved in, inside the window true, scrolls false
forty more: over the anchor, at the anchor's left, inside the window true, scrolls true
press outside: [Dismiss] open false, inner false, focus nothing
press the anchor: [Toggle] open false, inner false, focus Toggle
press First: [Pick(1)] open false, inner false, focus Toggle
Escape: [Dismiss] open false, inner false, focus Toggle
the window loses the focus: [Dismiss] open false, inner false, focus Toggle
press More: [ToggleInner] open true, inner true, focus ToggleInner
  the inner popup: to More's right true, level with it true
press Deep, in the inner popup: [Pick(3)] open false, inner false, focus Toggle
press First, outside the inner: [DismissInner, Pick(1)] open false, inner false, focus Toggle
Escape, both open: [DismissInner] open true, inner false, focus Toggle
Tab: [] open false, inner false, focus Toggle
Enter: [Toggle] open true, inner false, focus Pick(1)
Escape: [Dismiss] open false, inner false, focus Toggle
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A select's list (WINDOW.md §1, on the popups' placement): longer than the window, it is no
// taller than the window and scrolls, opening with the chosen option in view and keeping the
// option under the keys in view as they move it; at the window's foot it opens above the
// select.
func TestRun_UiSelectListScrollsAndFlips(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, Key, Modifiers, Escape, ArrowDown, Home, End }
import ui.draw.{ Size, Point, Rect }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, select, SelectSpec, sized, expanded, themed, tight, Choosing, option_id }

data Ev = Chose(i64) | Other(i64)

struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  chosen: i64 = 45,
}

const W: f32 = 400.0
const H: f32 = 300.0

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: W, h: H }) }
  let (top, bottom) = selects(laid_out(run, fonts))

  play(run, fonts, click(middle(laid_out(run, fonts), top)))
  seen(run, fonts, top, "opened, option 45 chosen", 45)
  play(run, fonts, [i in 0..<10 | KeyPressed(ArrowDown, Modifiers {})])
  seen(run, fonts, top, "Down ten times", 55)
  play(run, fonts, [KeyPressed(End, Modifiers {})])
  seen(run, fonts, top, "End", 59)
  play(run, fonts, [KeyPressed(Home, Modifiers {})])
  seen(run, fonts, top, "Home", 0)
  play(run, fonts, [KeyPressed(Escape, Modifiers {})])

  play(run, fonts, click(middle(laid_out(run, fonts), bottom)))
  let laid = laid_out(run, fonts)
  let face = box_of(laid, bottom)
  let list = box_of(laid, "${bottom}/:list")
  println("the select at the foot: its list ${if list.y + list.h <= face.y { "above" } else { "not above" }} it")
  0
}

/// How the open list of the select identified as id sits: within the window, scrolled, and
/// whether option shown is in view in it.
let seen = (run: mut Run, fonts: mut Fonts, id: string, name: string, shown: i64) -> void => {
  let laid = laid_out(run, fonts)
  let list = box_of(laid, "${id}/:list")
  let option = box_of(laid, option_id(id, shown))
  let inside = list.y >= 0.0 && list.y + list.h <= H + 0.01
  let in_view = option.y >= list.y - 0.01 && option.y + option.h <= list.y + list.h + 0.01
  println("${name}: list inside the window ${inside}, option ${shown} in view ${in_view}")
}

let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) {
      if let Chose(i) = e { run.chosen = i }
    }
  }
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run.chosen), tight(Size { w: W, h: H }), fonts, run.cache)

let view = pure (chosen: i64) -> Node<Ev> => themed::<Ev>(light_theme(), Sheet {}, [
  column([
    select(SelectSpec { options: [i in 0..<60 | "Option ${i}"], selected: chosen, on_change: Chose }),
    expanded(sized(None, None)),
    select(SelectSpec { options: ["one", "two", "three"], selected: 0, on_change: Other }),
  ]),
])

/// The two selects' identities, top first.
let selects = pure (laid: Laid<Ev>) -> (string, string) => {
  var ids: []string = []
  collect(laid, ids)
  (ids[0], ids[1])
}

let collect = (laid: Laid<Ev>, ids: mut []string) -> void => {
  if let Choosing(_) = laid.kind { ids.push(laid.id) }
  for child in laid.children { collect(child.laid, ids) }
}

let box_of = pure (laid: Laid<Ev>, id: string) -> Rect => {
  let Some(f) = find(laid, id, Point {}) else { return Rect { x: -1.0, y: -1.0 } }
  Rect { x: f.origin.x, y: f.origin.y, w: f.laid.size.w, h: f.laid.size.h }
}

let middle = pure (laid: Laid<Ev>, id: string) -> Point => {
  let b = box_of(laid, id)
  Point { x: b.x + b.w / 2.0, y: b.y + b.h / 2.0 }
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]
`)
	want := `opened, option 45 chosen: list inside the window true, option 45 in view true
Down ten times: list inside the window true, option 55 in view true
End: list inside the window true, option 59 in view true
Home: list inside the window true, option 0 in view true
the select at the foot: its list above it
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/popups.lyra played from the keyboard: Tab to File and Enter opens its popup
// with the focus on its first item; Tab to Recent and Enter opens the submenu beside it, the
// focus going in again, and Enter chooses notes.txt, closing both; Tab past the select to More
// and Enter opens its popup, Escape dismisses it; and the select, opened with Space, chooses
// two options down. The last frame is written.
func TestRun_UiPopupsExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "popups")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "popups.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "end.png")
	cmd := command(t, bin, "--script",
		"Tab Enter Tab Tab Enter Enter Tab Tab Enter Escape click:20,300 Tab Tab Space Down Down Enter",
		"--png", shot)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	want := `toggled file
open notes.txt
toggled more
dismissed
font Typeface 33
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Errorf("no last frame: %v", err)
	}
}

// Menus drawn (WINDOW.md §2): a press on a title opens its menu and another closes it; while
// one is open the pointer over another title opens that one, over a submenu's item opens the
// submenu beside it, and over another item closes the submenu; a press on an item chooses it,
// closing every menu with the focus on the first's title; a press outside closes them. From
// the keyboard, Enter on a title opens its menu on its first item; Down and Up walk the items
// that can be chosen, wrapping, past separators and greyed items; Right opens a submenu or the
// next title's menu, Left closes the submenu; Escape closes the deepest; Tab closes them all and
// moves on. A layout from the cache raises this frame's events, a menu's and a popup's content's
// alike. Shortcuts are spelled for Apple's systems and the others, and gathered from every menu
// drawn, greyed items left out.
func TestRun_UiMenus(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, Key, Modifiers, Enter, Escape, Tab, ArrowDown, ArrowUp, ArrowLeft, ArrowRight, Character, FunctionKey, Chord }
import ui.draw.{ Size, Point }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, row, button, text, popup, themed, tight, Pressable, Opens, Menu, MenuEntry, MenuItem, MenuItemEntry, Submenu, MenuSeparator, menu_bar, menu_button, menu_shortcuts, chord_text }

data Ev = Chose(string) | Count(i64)

struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  count: i64 = 0,
  popup: bool = false,
  events: []string = [],
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: 500.0, h: 400.0 }) }

  play(run, fonts, click(title(run, fonts, "File")))
  say(run, fonts, "press File")
  play(run, fonts, [PointerMoved(title(run, fonts, "Edit"))])
  say(run, fonts, "over Edit")
  play(run, fonts, click(item(run, fonts, "cut")))
  say(run, fonts, "press Cut")
  play(run, fonts, click(title(run, fonts, "View")))
  play(run, fonts, [PointerMoved(title(run, fonts, "Zoom"))])
  say(run, fonts, "press View, over Zoom")
  play(run, fonts, [PointerMoved(item(run, fonts, "full"))])
  say(run, fonts, "over Full Screen")
  play(run, fonts, click(Point { x: 480.0, y: 380.0 }))
  say(run, fonts, "press outside")
  play(run, fonts, click(title(run, fonts, "File")))
  play(run, fonts, click(title(run, fonts, "File")))
  say(run, fonts, "press File twice")

  play(run, fonts, [...click(Point { x: 480.0, y: 380.0 }), ...key(Tab), ...key(Enter)])
  say(run, fonts, "press outside, Tab, Enter")
  play(run, fonts, [...key(ArrowDown), ...key(ArrowDown)])
  say(run, fonts, "Down, Down")
  play(run, fonts, key(ArrowDown))
  say(run, fonts, "Down")
  play(run, fonts, key(ArrowUp))
  say(run, fonts, "Up")
  play(run, fonts, key(Escape))
  say(run, fonts, "Escape")
  play(run, fonts, [...key(Enter), ...key(ArrowRight)])
  say(run, fonts, "Enter, Right")
  play(run, fonts, [...key(ArrowRight), ...key(ArrowDown), ...key(ArrowRight)])
  say(run, fonts, "Right, Down, Right")
  play(run, fonts, key(ArrowLeft))
  say(run, fonts, "Left")
  play(run, fonts, [...key(ArrowRight), ...key(Enter)])
  say(run, fonts, "Right, Enter")
  play(run, fonts, [...key(Enter), ...key(Tab)])
  say(run, fonts, "Enter, Tab")

  run.count = 5
  play(run, fonts, click(title(run, fonts, "File")))
  run.count = 7
  play(run, fonts, click(item(run, fonts, "Count")))
  say(run, fonts, "count 5 then 7, press Count")
  run.popup = true
  run.count = 1
  let _ = laid_out(run, fonts)
  run.count = 2
  play(run, fonts, click(item(run, fonts, "Count")))
  say(run, fonts, "a popup open, count 1 then 2, press Inner")
  run.popup = false

  for (chord, e) in menu_shortcuts(laid_out(run, fonts)) {
    println("shortcut ${chord_text(chord, true)} / ${chord_text(chord, false)}: ${named(e)}")
  }
  let alt_f4 = Chord { key: FunctionKey(4), mods: Modifiers { alt: true } }
  let ctrl_c = Chord { key: Character('c'), mods: Modifiers { control: true } }
  let enter = Chord { key: Enter, mods: Modifiers { primary: true, shift: true } }
  println("${chord_text(alt_f4, true)} ${chord_text(alt_f4, false)} ${chord_text(ctrl_c, true)} ${chord_text(enter, true)} ${chord_text(enter, false)}")
  0
}

let menus = pure (count: i64) -> []Menu<Ev> => [
  Menu {
    label: "File",
    entries: [
      entry("New", Chose("new"), Some('n')),
      entry("Open", Chose("open"), None),
      MenuSeparator,
      MenuItemEntry(MenuItem { label: "Count", on_select: Count(count) }),
    ],
  },
  Menu {
    label: "Edit",
    entries: [
      MenuItemEntry(MenuItem { label: "Undo", on_select: Chose("undo"), shortcut: Some(Chord { key: Character('z'), mods: Modifiers { primary: true } }), enabled: false }),
      entry("Cut", Chose("cut"), Some('x')),
    ],
  },
  Menu {
    label: "View",
    entries: [
      entry("Full Screen", Chose("full"), None),
      Submenu("Zoom", [entry("Zoom In", Chose("zoom in"), Some('=')), entry("Zoom Out", Chose("zoom out"), None)]),
    ],
  },
]

let entry = pure (label: string, e: Ev, key: Maybe<rune>) -> MenuEntry<Ev> => {
  let shortcut: Maybe<Chord> = match key {
    Some(k) => Some(Chord { key: Character(k), mods: Modifiers { primary: true } }),
    None => None,
  }
  MenuItemEntry(MenuItem { label: label, on_select: e, shortcut: shortcut })
}

let view = pure (run: Run) -> Node<Ev> => {
  let bar = row([menu_bar(menus(run.count)), menu_button(text::<Ev>("More"), [entry("About", Chose("about"), None)])])
  let below: []Node<Ev> = if run.popup { [popup(button("Pop", Chose("pop")), true, button("Inner", Count(run.count)), Chose("dismiss"))] } else { [] }
  themed::<Ev>(light_theme(), Sheet {}, [column([bar, ...below])])
}

let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) { run.events.push(named(e)) }
  }
}

/// A line for the step just played: its events, the menus open by their titles, and the focus.
let say = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let laid = laid_out(run, fonts)
  let open = [id in run.ui.state.menus | named_id(laid, id)]
  println("${name}: [${run.events.join(", ")}] open [${open.join(", ")}], focus ${named_id(laid, run.ui.state.focus)}")
  run.events = []
}

let named = pure (e: Ev) -> string => match e {
  Chose(what) => what,
  Count(n) => "Count(${n})",
}

let named_id = pure (laid: Laid<Ev>, id: string) -> string => match find(laid, id, Point {}) {
  Some(f) => match f.laid.kind {
    Opens(_) => f.laid.label,
    Pressable(e) => named(e),
    _ => "?",
  },
  None => "nothing",
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run), tight(Size { w: 500.0, h: 400.0 }), fonts, run.cache)

/// The middle of the opener labelled label.
let title = (run: mut Run, fonts: mut Fonts, label: string) -> Point =>
  middle_where(laid_out(run, fonts), Point {}, (l: Laid<Ev>) -> bool => match l.kind {
    Opens(_) => l.label == label,
    _ => false,
  }) ?? Point { x: -1.0, y: -1.0 }

/// The middle of the item raising the event named name.
let item = (run: mut Run, fonts: mut Fonts, name: string) -> Point =>
  middle_where(laid_out(run, fonts), Point {}, (l: Laid<Ev>) -> bool => match l.kind {
    Pressable(e) => named(e) == name || (name == "Count" && named(e).starts_with("Count")),
    _ => false,
  }) ?? Point { x: -1.0, y: -1.0 }

let middle_where = pure (laid: Laid<Ev>, origin: Point, wanted: (Laid<Ev>) -> bool) -> Maybe<Point> => {
  if wanted(laid) { return Some(Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 }) }
  for child in laid.children {
    if let Some(p) = middle_where(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, wanted) {
      return Some(p)
    }
  }
  None
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]
`)
	want := `press File: [] open [File], focus File
over Edit: [] open [Edit], focus Edit
press Cut: [cut] open [], focus Edit
press View, over Zoom: [] open [View, Zoom], focus View
over Full Screen: [] open [View], focus View
press outside: [] open [], focus nothing
press File twice: [] open [], focus File
press outside, Tab, Enter: [] open [File], focus new
Down, Down: [] open [File], focus Count(0)
Down: [] open [File], focus new
Up: [] open [File], focus Count(0)
Escape: [] open [], focus File
Enter, Right: [] open [Edit], focus cut
Right, Down, Right: [] open [View, Zoom], focus zoom in
Left: [] open [View], focus Zoom
Right, Enter: [zoom in] open [], focus View
Enter, Tab: [] open [], focus More
count 5 then 7, press Count: [Count(7)] open [], focus File
a popup open, count 1 then 2, press Inner: [Count(2)] open [], focus Count(2)
shortcut ⌘N / Ctrl+N: new
shortcut ⌘X / Ctrl+X: cut
shortcut ⌘= / Ctrl+=: zoom in
⌥F4 Alt+F4 ⌃C ⇧⌘↩ Ctrl+Shift+Enter
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/menus.lyra played: from the keyboard File's Save (Down past a separator), then
// View's Zoom In (Right across the bar, into the submenu); the shortcuts the menus show, taken
// by the app with no table of the program's (Primary+N, Primary+Shift+S); a press outside an
// open menu choosing nothing; and ☰'s first item by pointer. Then File ▸ Open…, its dialog
// answered from the script (choose:), and by its shortcut cancelled. The last frame is written.
func TestRun_UiMenusExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "menus")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "menus.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	for _, c := range []struct{ script, want string }{
		{"Tab Enter Down Down Enter Enter Right Right Down Right Enter Escape Primary+n Primary+Shift+s click:22,14 click:400,300 click:540,14 click:520,52",
			"save\nzoom 1\nnew\nsave as\npreferences\n"},
		// File ▸ Open…, and its dialog answered; then its shortcut, and the dialog cancelled.
		{"Tab Enter Down Enter choose:/notes/today.txt Primary+o choose:", "opened /notes/today.txt\nopen cancelled\n"},
	} {
		shot := filepath.Join(dir, "end.png")
		cmd := command(t, bin, "--script", c.script, "--png", shot)
		cmd.Dir = dir
		cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
		out, err := cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
			strings.Contains(string(out), "SDL could not") {
			t.Skipf("SDL could not open a dummy window here: %s", out)
		} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
			t.Skipf("no font here: %s", out)
		} else if err != nil {
			t.Fatalf("running: %v\n%s", err, out)
		}
		if string(out) != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.script, out, c.want)
		}
		if _, err := os.Stat(shot); err != nil {
			t.Errorf("no last frame: %v", err)
		}
	}
}

// App.menus (WINDOW.md step 3) through the host: each pass the menus the state says are handed
// to the platform, every item a tag in order — greyed and checked as the state has them — and a
// chosen tag raises that item's event. Where the platform has a bar of its own (a Mac), a key the
// bar holds is left to it, AppKit choosing the item; where it has none, the same key is taken as
// a shortcut, as App.shortcuts' are. A program with no menus hands the platform none.
func TestRun_UiAppMenus(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, Menus, Command, Subscription, host_of }
import ui.script.{ timed_platform, ScriptPlatform, Played, Step }
import ui.backend.{ Renderer, TextureId, KeyPressed, MenuChosen, Modifiers, Character, Chord, NativeMenu, NativeEntry, NativeItem, NativeSubmenu, NativeSeparator }
import ui.draw.{ Size, Color }
import ui.text.{ fonts_new }
import ui.layout.{ Node, Menu, MenuEntry, MenuItem, MenuItemEntry, Submenu, MenuSeparator, column, text }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Added(i64) | Undone

let main = () -> u8 => {
  let steps = [
    Played(MenuChosen(0)),
    Played(MenuChosen(1)),
    Played(KeyPressed(Character('n'), Modifiers { primary: true })),
  ]
  for native in [true, false] {
    var platform = timed_platform(steps, Size { w: 200.0, h: 100.0 })
    platform.native_menus = native
    var renderer = Blank {}
    var fonts = fonts_new()
    var host = host_of(app(true), 0, platform)
    while host.step(platform, renderer, fonts) {}
    println("native ${native}: state ${host.state}")
    for m in platform.menus { println("  ${m.label}: ${described(m.entries)}") }
  }
  var platform = timed_platform([], Size { w: 200.0, h: 100.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  var host = host_of(app(false), 0, platform)
  while host.step(platform, renderer, fonts) {}
  println("no menus: ${platform.menus.len()} handed over")
  0
}

let app = pure (with_menus: bool) -> App<i64, Ev> => App {
  view: (n: i64) -> Node<Ev> => column([text("${n}")]),
  update: (n: i64, e: Ev) -> (i64, []Command<Ev>) => match e {
    Added(k) => (n + k, []),
    Undone => (n - 100, []),
  },
  subscriptions: (_: i64) -> []Subscription<Ev> => [],
  background: Color { r: 0, g: 0, b: 0 },
  menus: if with_menus { [Menus { of: menus }] } else { [] },
}

/// Counter's New adds 1 (Primary+N) and More adds 10; Edit's Undo is greyed; the ticked item
/// is checked while the count is odd.
let menus = pure (n: i64) -> []Menu<Ev> => [
  Menu {
    label: "Counter",
    entries: [
      MenuItemEntry(MenuItem { label: "New", on_select: Added(1), shortcut: Some(Chord { key: Character('n'), mods: Modifiers { primary: true } }) }),
      MenuSeparator,
      Submenu("More", [MenuItemEntry(MenuItem { label: "Ten", on_select: Added(10) })]),
    ],
  },
  Menu {
    label: "Edit",
    entries: [
      MenuItemEntry(MenuItem { label: "Undo", on_select: Undone, enabled: false }),
      MenuItemEntry(MenuItem { label: "Odd", on_select: Added(0), checked: n % 2 == 1 }),
    ],
  },
]

let described = pure (entries: []NativeEntry) -> string => [
  entry in entries |
    match entry {
      NativeItem(it) => "${it.label}#${it.tag}${if it.shortcut.is_some() { "@" } else { "" }}${if it.enabled { "" } else { " (off)" }}${if it.checked { " [x]" } else { "" }}",
      NativeSubmenu(label, inner) => "${label} > (${described(inner)})",
      NativeSeparator => "---",
    }
].join(", ")
`)
	want := `native true: state 11
  Counter: New#0@, ---, More > (Ten#1)
  Edit: Undo#2 (off), Odd#3 [x]
native false: state 12
  Counter: New#0@, ---, More > (Ten#1)
  Edit: Undo#2 (off), Odd#3
no menus: 0 handed over
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A tab bar (WINDOW.md §3): tabs as wide as their labels where they fit; shrunk together, none
// below TAB_LEAST, where they do not; past that as many as fit — the selected among them — and
// the more-tabs button, whose menu lists the rest, one chosen as a press on it would (open
// question 2). A press selects a tab, on its × closes it — the focus left where it was — a middle click closes one (not
// Home, which is not closable); a drag past a neighbour's middle moves it, never past Home, which
// is not movable, and the press stays on the tab through the move; the arrows, Home and End
// select along the strip with the focus. Heard as a tab list of tabs.
func TestRun_UiTabs(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find, Found }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, Middle, KeyPressed, Key, Modifiers, ArrowRight, Home, End, AxTabList, AxTab }
import ui.access.{ access_tree }
import ui.draw.{ Size, Point, Rect }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, button, themed, tight, TabsSpec, TabSpec, tab_bar, Tabbed, TabStrip, TAB_LEAST, TAB_MOST }

data Ev = Selected(i64) | Closed(i64) | Moved(i64, i64) | Added

struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  names: []string = [],
  selected: i64 = 0,
  events: []string = [],
}

const W: f32 = 600.0

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: W, h: 200.0 }) }
  var own: f32 = 0.0
  for n in [4, 8, 16] {
    run.names = ["Home", ...[i in 1..<n | "Document ${i}"]]
    run.selected = 0
    own = widths(run, fonts, "${n} tabs", own)
  }
  run.selected = 15
  let _ = widths(run, fonts, "16 tabs, the last selected", own)
  play(run, fonts, click(tagged_middle(laid_out(run, fonts), Point {}, "TabsMore")))
  let items = count_tagged(laid_out(run, fonts), "MenuItem")
  play(run, fonts, click(tagged_middle(laid_out(run, fonts), Point {}, "MenuItem")))
  println("the more-tabs menu: ${items} items, the first chosen: [${run.events.join(", ")}], selected ${run.names[run.selected]}")
  run.events = []

  run.names = ["Home", "One", "Two", "Three", "Four"]
  run.selected = 0
  play(run, fonts, click(tab_middle(run, fonts, 2)))
  say(run, fonts, "press Two")
  play(run, fonts, click(close_middle(run, fonts, 3)))
  say(run, fonts, "press Three's ×")
  play(run, fonts, [PointerDown(Middle, tab_middle(run, fonts, 1), 1), PointerUp(Middle, tab_middle(run, fonts, 1))])
  say(run, fonts, "middle click One")
  play(run, fonts, [PointerDown(Middle, tab_middle(run, fonts, 0), 1), PointerUp(Middle, tab_middle(run, fonts, 0))])
  say(run, fonts, "middle click Home")

  run.names = ["Home", "One", "Two", "Three"]
  run.selected = 0
  let one = tab_middle(run, fonts, 1)
  let past_two = Point { x: box_of(run, fonts, 2).x + box_of(run, fonts, 2).w * 0.75, y: one.y }
  let past_three = Point { x: box_of(run, fonts, 3).x + box_of(run, fonts, 3).w * 0.75, y: one.y }
  play(run, fonts, [PointerMoved(one), PointerDown(Primary, one, 1), PointerMoved(past_two), PointerMoved(past_three), PointerUp(Primary, past_three)])
  say(run, fonts, "drag One past Two and Three")
  let three = tab_middle(run, fonts, 2)
  let on_home = tab_middle(run, fonts, 0)
  play(run, fonts, [PointerMoved(three), PointerDown(Primary, three, 1), PointerMoved(on_home), PointerUp(Primary, on_home)])
  // Past Two's middle first, which it takes the place of; Home it never passes.
  say(run, fonts, "drag Three onto Home")

  play(run, fonts, click(tab_middle(run, fonts, 2)))
  play(run, fonts, key(ArrowRight))
  say(run, fonts, "press the third tab, Right")
  play(run, fonts, key(Home))
  say(run, fonts, "Home")
  play(run, fonts, key(End))
  say(run, fonts, "End")

  let heard = access_tree(laid_out(run, fonts), run.ui.state.focus)
  var tabs = 0
  for n in heard.nodes {
    match n.role {
      AxTabList => println("heard: a tab list of ${n.children.len()} (the tabs and +)"),
      AxTab => { tabs += 1 },
      _ => {},
    }
  }
  println("heard: ${tabs} tabs")
  0
}

/// How the strip laid the tabs out — whether the documents' tabs (Home's label is shorter than
/// TAB_LEAST, and never grows to it) are narrower than own, the widest they were with room,
/// and none below TAB_LEAST; how many it shows, and whether the more-tabs button; whether the
/// selected tab is in view — and the widest document's tab.
let widths = (run: mut Run, fonts: mut Fonts, name: string, own: f32) -> f32 => {
  let laid = laid_out(run, fonts)
  let Some(strip) = strip_of(laid, Point {}) else { return 0.0 }
  var least = TAB_MOST
  var widest: f32 = 0.0
  var seen = false
  for child in strip.laid.children {
    if let Tabbed(spot) = child.laid.kind {
      if spot.index > 0 {
        least = least.min(child.laid.size.w)
        widest = widest.max(child.laid.size.w)
      }
      if spot.selected {
        let x = child.at.x
        seen = x >= -0.5 && x + child.laid.size.w <= strip.laid.size.w + 0.5
      }
    }
  }
  var shown = 0
  for c in strip.laid.children { if let Tabbed(_) = c.laid.kind { shown += 1 } }
  let more = strip.laid.children.any((c) => c.laid.tag == "TabsMore")
  let shrunk = own > 0.0 && widest < own - 0.5
  println("${name}: narrowest ${if least >= TAB_LEAST - 0.5 { "at least TAB_LEAST" } else { "below TAB_LEAST" }}, shrunk ${shrunk}, ${shown} shown, the more-tabs button ${more}, the selected in view ${seen}")
  if own > 0.0 { own } else { widest }
}

let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) {
      match e {
        Selected(i) => {
          run.events.push("Selected(${i})")
          run.selected = i
        },
        Closed(i) => {
          run.events.push("Closed(${i})")
          run.names = [k in 0..<run.names.len() | k != i | run.names[k]]
          if run.selected >= run.names.len() { run.selected = run.names.len() - 1 }
        },
        // The shown tab stays shown, by name.
        Moved(from, to) => {
          run.events.push("Moved(${from}, ${to})")
          let shown = run.names[run.selected]
          let moving = run.names[from]
          let rest = [k in 0..<run.names.len() | k != from | run.names[k]]
          run.names = [...rest.slice(0, to), moving, ...rest.slice(to, rest.len())]
          run.selected = run.names.index_of(shown) ?? 0
        },
        Added => run.events.push("Added"),
      }
    }
  }
}

let say = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let laid = laid_out(run, fonts)
  let focus = match find(laid, run.ui.state.focus, Point {}) {
    Some(f) => f.laid.label,
    None => "nothing",
  }
  println("${name}: [${run.events.join(", ")}] ${run.names.join(" ")}, selected ${run.names[run.selected]}, focus ${focus}")
  run.events = []
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run), tight(Size { w: W, h: 200.0 }), fonts, run.cache)

let view = pure (run: Run) -> Node<Ev> => themed::<Ev>(light_theme(), Sheet {}, [
  column([
    tab_bar(TabsSpec {
      tabs: [i in 0..<run.names.len() | TabSpec { label: run.names[i], key: run.names[i], closable: i > 0 || run.names[i] != "Home", movable: run.names[i] != "Home" }],
      selected: run.selected,
      on_select: Selected,
      on_close: Closed,
      on_move: Moved,
      after: [button("+", Added)],
    }),
  ]),
])

let strip_of = pure (laid: Laid<Ev>, origin: Point) -> Maybe<Found<Ev>> => {
  if let TabStrip = laid.kind { return Some(Found { laid: laid, origin: origin }) }
  for child in laid.children {
    if let Some(f) = strip_of(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }) { return Some(f) }
  }
  None
}

/// The middle of the first element tagged tag, in the window.
let tagged_middle = pure (laid: Laid<Ev>, origin: Point, tag: string) -> Point => {
  if laid.tag == tag { return Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 } }
  for child in laid.children {
    let p = tagged_middle(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, tag)
    if p.x >= 0.0 { return p }
  }
  Point { x: -1.0, y: -1.0 }
}

/// How many elements are tagged tag.
let count_tagged = pure (laid: Laid<Ev>, tag: string) -> i64 => {
  var n = if laid.tag == tag { 1 } else { 0 }
  for child in laid.children { n += count_tagged(child.laid, tag) }
  n
}

/// Tab i's box in the window.
let box_of = (run: mut Run, fonts: mut Fonts, i: i64) -> Rect => {
  let Some(strip) = strip_of(laid_out(run, fonts), Point {}) else { return Rect {} }
  var k = 0
  for child in strip.laid.children {
    if let Tabbed(_) = child.laid.kind {
      if k == i { return Rect { x: strip.origin.x + child.at.x, y: strip.origin.y + child.at.y, w: child.laid.size.w, h: child.laid.size.h } }
      k += 1
    }
  }
  Rect {}
}

let tab_middle = (run: mut Run, fonts: mut Fonts, i: i64) -> Point => {
  let b = box_of(run, fonts, i)
  Point { x: b.x + b.w * 0.3, y: b.y + b.h / 2.0 }
}

/// The middle of tab i's ×: its last child's.
let close_middle = (run: mut Run, fonts: mut Fonts, i: i64) -> Point => {
  let b = box_of(run, fonts, i)
  Point { x: b.x + b.w - 14.0, y: b.y + b.h / 2.0 }
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]
`)
	want := `4 tabs: narrowest at least TAB_LEAST, shrunk false, 4 shown, the more-tabs button false, the selected in view true
8 tabs: narrowest at least TAB_LEAST, shrunk true, 8 shown, the more-tabs button false, the selected in view true
16 tabs: narrowest at least TAB_LEAST, shrunk true, 8 shown, the more-tabs button true, the selected in view true
16 tabs, the last selected: narrowest at least TAB_LEAST, shrunk true, 8 shown, the more-tabs button true, the selected in view true
the more-tabs menu: 8 items, the first chosen: [Selected(0)], selected Home
press Two: [Selected(2)] Home One Two Three Four, selected Two, focus Two
press Three's ×: [Closed(3)] Home One Two Four, selected Two, focus Two
middle click One: [Closed(1)] Home Two Four, selected Four, focus Two
middle click Home: [] Home Two Four, selected Four, focus Two
drag One past Two and Three: [Selected(1), Moved(1, 2), Moved(2, 3)] Home Two Three One, selected One, focus One
drag Three onto Home: [Selected(2), Moved(2, 1)] Home Three Two One, selected Three, focus Three
press the third tab, Right: [Selected(2), Selected(3)] Home Three Two One, selected One, focus One
Home: [Selected(0)] Home Three Two One, selected Home, focus Home
End: [Selected(3)] Home Three Two One, selected One, focus One
heard: a tab list of 5 (the tabs and +)
heard: 4 tabs
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The tab strip's gap gives way before its tabs: the gap its style asks for (8 here) while the
// tabs fit at the widths they want, narrowed toward TAB_GAP_LEAST where they do not — the tabs
// keeping their widths — and only past that the tabs shrunk.
func TestRun_UiTabsGapGivesWay(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ ui_new }
import ui.draw.{ Size }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet, Rule, Style }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, themed, tight, TabsSpec, TabSpec, tab_bar, Tabbed, TabStrip, TAB_GAP_LEAST }

data Ev = Selected(i64) | Closed(i64) | Moved(i64, i64)

/// The gap between the first two tabs, and every tab's width together.
struct Measure {
  gap: f32 = 0.0,
  tabs: f32 = 0.0,
}

/// The light theme's tab bar is padded 2 a side.
const PADDING: f32 = 4.0

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  let room = measure(2000.0, fonts)
  println("room: gap ${room.gap.round()}")
  // Room for the tabs as wide as they want and gaps of 4.
  let tighter = measure(PADDING + room.tabs + 3.0 * 4.0, fonts)
  println("tighter: gap ${tighter.gap.round()}, shrunk ${tighter.tabs < room.tabs - 0.5}")
  // Room for gaps of 1 only: the gap stops at its least, and the tabs give up the rest.
  let tightest = measure(PADDING + room.tabs + 3.0 * 1.0, fonts)
  println("tightest: gap at its least ${(tightest.gap - TAB_GAP_LEAST).abs() < 0.01}, shrunk ${tightest.tabs < room.tabs - 0.5}")
  0
}

let measure = (w: f32, fonts: mut Fonts) -> Measure => {
  var ui = ui_new()
  var cache: LayoutCache<Ev> = layout_cache()
  let laid = ui.frame_within(view(), tight(Size { w: w, h: 100.0 }), fonts, cache)
  let Some(strip) = strip_of(laid) else { return Measure {} }
  var tabs: f32 = 0.0
  var ends: []f32 = []
  var starts: []f32 = []
  for child in strip.children {
    if let Tabbed(_) = child.laid.kind {
      tabs += child.laid.size.w
      starts.push(child.at.x)
      ends.push(child.at.x + child.laid.size.w)
    }
  }
  Measure { gap: starts[1] - ends[0], tabs: tabs }
}

let strip_of = pure (laid: Laid<Ev>) -> Maybe<Laid<Ev>> => {
  if let TabStrip = laid.kind { return Some(laid) }
  for child in laid.children {
    if let Some(s) = strip_of(child.laid) { return Some(s) }
  }
  None
}

let view = pure () -> Node<Ev> => themed::<Ev>(light_theme(), Sheet { rules: [Rule { selector: "TabBar", style: Style { gap: Some(8.0) } }] }, [
  column([
    tab_bar(TabsSpec {
      tabs: [i in 0..<4 | TabSpec { label: "Document ${i}", key: "d${i}" }],
      selected: 0,
      on_select: Selected,
      on_close: Closed,
      on_move: Moved,
    }),
  ]),
])
`)
	want := "room: gap 8\ntighter: gap 4, shrunk false\ntightest: gap at its least true, shrunk true\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/tabs.lyra played: a press selects Document 2, a drag moves Document 1 past it,
// and Document 3's × closes it; then, from the keyboard, + pressed nine times, the strip shrinking
// and then holding the rest in its more-tabs menu. The last frame is written.
func TestRun_UiTabsExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "tabs")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "tabs.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	for _, c := range []struct{ script, want string }{
		{"click:230,16 down:110,16 move:180,16 move:250,16 up:250,16 click:400,16", "Home Document 2 [Document 1]\n"},
		{"Tab Tab Tab Tab Tab Space Space Space Space Space Space Space Space Space", "Home Document 1 Document 2 Document 3 Document 4 Document 5 Document 6 Document 7 Document 8 Document 9 Document 10 Document 11 [Document 12]\n"},
	} {
		shot := filepath.Join(dir, "end.png")
		cmd := command(t, bin, "--script", c.script, "--png", shot)
		cmd.Dir = dir
		cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
		out, err := cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
			strings.Contains(string(out), "SDL could not") {
			t.Skipf("SDL could not open a dummy window here: %s", out)
		} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
			t.Skipf("no font here: %s", out)
		} else if err != nil {
			t.Fatalf("running: %v\n%s", err, out)
		}
		if string(out) != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.script, out, c.want)
		}
		if _, err := os.Stat(shot); err != nil {
			t.Errorf("no last frame: %v", err)
		}
	}
}

// A title bar the program draws (WINDOW.md §4) through the host: the window's frame taken away
// once; each pass the platform told the drag area the view marks and the controls inside it
// that keep a press; the window's buttons pressed — maximize, then restore (its glyph two boxes
// while maximized), minimize — carried out by the framework and heard through OnWindowState, as
// the system's own changes are; the program's own button asking the same by command; nothing of
// the buttons on Apple's systems; and close ending the run as the system's close does.
func TestRun_UiDrawnTitleBar(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, Host, DrawnTitleBar, Command, Subscription, OnWindowState, WindowMaximize, host_of }
import ui.script.{ timed_platform, ScriptPlatform, Played, Waited }
import ui.backend.{ Renderer, TextureId, PointerMoved, PointerDown, PointerUp, Primary, WindowState, WindowStateChanged }
import ui.draw.{ Size, Point, Color }
import ui.text.{ Fonts, fonts_new }
import ui.layout.{ Node, Laid, row, column, text, button, expanded, window_buttons, WindowControl, WindowButton, MinimizeButton, MaximizeButton, CloseButton, Pressable }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Changed(WindowState) | Max

let main = () -> u8 => {
  for apple in [false, true] {
    var platform = timed_platform([Waited(1)], Size { w: 400.0, h: 200.0 })
    platform.apple_keys = apple
    var renderer = Blank {}
    var fonts = fonts_new()
    var host = host_of(app(), "", platform)
    let _ = host.step(platform, renderer, fonts)
    let laid = host.shown[0]
    if apple {
      println("on Apple's systems: the buttons ${width_of(laid, Point {}, 0.0)} wide")
      continue
    }
    println("frame ${platform.decorated}, drag areas ${platform.drag_areas.len()}, controls in them ${platform.keep_areas.len()}, margin ${platform.resize_margin}")
    let steps: [](string, Point) = [
      ("maximize", middle_of(laid, MaximizeButton)),
      ("restore", middle_of(laid, MaximizeButton)),
      ("the program's Max", program_button(laid, Point {}) ?? Point {}),
    ]
    for (name, at) in steps {
      press(platform, host, renderer, fonts, at)
      println("${name}: heard '${host.state}', maximized ${platform.window.maximized}, the maximize glyph's boxes ${frames(host.shown[0])}")
    }
    platform.steps = [...platform.steps, Played(WindowStateChanged(WindowState {})), Waited(1)]
    while platform.next < platform.steps.len() { let _ = host.step(platform, renderer, fonts) }
    println("the system restores it: heard '${host.state}'")
    press(platform, host, renderer, fonts, middle_of(host.shown[0], MinimizeButton))
    println("minimize: heard '${host.state}', minimized ${platform.window.minimized}")
    let at = middle_of(host.shown[0], CloseButton)
    platform.steps = [...platform.steps, Played(PointerMoved(at)), Played(PointerDown(Primary, at, 1)), Played(PointerUp(Primary, at)), Waited(1)]
    var passes = 0
    while host.step(platform, renderer, fonts) { passes += 1 }
    println("close: the run ended with ${platform.steps.len() - platform.next} steps unplayed")
  }
  0
}

let app = pure () -> App<string, Ev> => App {
  view: (heard: string) -> Node<Ev> => column([
    row([text("Title"), expanded(row([])), button("Max", Max), window_buttons()]).window_drag(),
    text(heard),
  ]),
  update: (heard: string, e: Ev) -> (string, []Command<Ev>) => match e {
    Changed(w) => (if w.minimized { "minimized" } else if w.maximized { "maximized" } else { "restored" }, []),
    Max => (heard, [WindowMaximize]),
  },
  subscriptions: (_: string) -> []Subscription<Ev> => [OnWindowState(Changed)],
  background: Color { r: 0, g: 0, b: 0 },
  title_bar: DrawnTitleBar,
}

/// A press and release at at, played and passed.
let press = (platform: mut ScriptPlatform, host: mut Host<string, Ev>, renderer: mut Blank, fonts: mut Fonts, at: Point) -> void => {
  platform.steps = [...platform.steps, Played(PointerMoved(at)), Played(PointerDown(Primary, at, 1)), Played(PointerUp(Primary, at)), Waited(1)]
  // Every step played — the system's answer comes before the last — and no pass more: a script
  // platform out of steps closes the window.
  while platform.next < platform.steps.len() { let _ = host.step(platform, renderer, fonts) }
}

let middle_of = pure (laid: Laid<Ev>, b: WindowButton) -> Point => control(laid, Point {}, b) ?? Point { x: -1.0, y: -1.0 }

let control = pure (laid: Laid<Ev>, origin: Point, b: WindowButton) -> Maybe<Point> => {
  if let WindowControl(w) = laid.kind {
    if w == b { return Some(Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 }) }
  }
  for child in laid.children {
    if let Some(p) = control(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, b) { return Some(p) }
  }
  None
}

let program_button = pure (laid: Laid<Ev>, origin: Point) -> Maybe<Point> => {
  if let Pressable(_) = laid.kind { return Some(Point { x: origin.x + laid.size.w / 2.0, y: origin.y + laid.size.h / 2.0 }) }
  for child in laid.children {
    if let Some(p) = program_button(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }) { return Some(p) }
  }
  None
}

/// How many boxes the maximize button's glyph draws: one to maximize, two to restore.
let frames = pure (laid: Laid<Ev>) -> i64 => {
  if let WindowControl(MaximizeButton) = laid.kind { return boxes(laid) }
  var n = 0
  for child in laid.children { n += frames(child.laid) }
  n
}

let boxes = pure (laid: Laid<Ev>) -> i64 => {
  var n = if laid.style.border_width > 0.0 { 1 } else { 0 }
  for child in laid.children { n += boxes(child.laid) }
  n
}

/// The window buttons' row's width.
let width_of = pure (laid: Laid<Ev>, origin: Point, none: f32) -> f32 => {
  if laid.tag == "WindowButtons" { return laid.size.w }
  for child in laid.children {
    let w = width_of(child.laid, origin, -1.0)
    if w >= 0.0 { return w }
  }
  none
}
`)
	want := `frame false, drag areas 1, controls in them 4, margin 5
maximize: heard 'maximized', maximized true, the maximize glyph's boxes 2
restore: heard 'restored', maximized false, the maximize glyph's boxes 1
the program's Max: heard 'maximized', maximized true, the maximize glyph's boxes 2
the system restores it: heard 'restored'
minimize: heard 'minimized', minimized true
close: the run ended with 1 steps unplayed
on Apple's systems: the buttons 0 wide
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// bindings.hittest's answers, asked with no window: a borderless, resizable window is resized
// from a margin round its edges and corners — not while maximized — moved from its drag areas,
// and the controls inside them keep their presses; elsewhere it is the window's own.
func TestRun_HitTestAnswers(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import bindings.hittest.{ set_areas_only, answer }

let main = () -> void => {
  // A bar along the top, a button at its right end inside it; a margin of 4.
  set_areas_only([0.0, 0.0, 400.0, 30.0], [350.0, 0.0, 50.0, 30.0], 4.0)
  let frameless: u64 = 0x10 | 0x20
  let maximized: u64 = frameless | 0x80
  for (name, x, y, flags) in [
    ("the bar", 100.0, 15.0, frameless),
    ("its button", 370.0, 15.0, frameless),
    ("the page", 100.0, 200.0, frameless),
    ("the top-left corner", 1.0, 1.0, frameless),
    ("the top edge", 100.0, 2.0, frameless),
    ("the right edge", 398.0, 150.0, frameless),
    ("the bottom-right corner", 399.0, 299.0, frameless),
    ("the left edge", 2.0, 150.0, frameless),
    ("the top-left corner, maximized", 1.0, 1.0, maximized),
    ("the left edge, framed", 2.0, 150.0, 0x20),
  ] {
    println("${name}: ${answer(f32(x), f32(y), 400.0, 300.0, flags)}")
  }
}
`)
	want := `the bar: 1
its button: 0
the page: 0
the top-left corner: 2
the top edge: 3
the right edge: 5
the bottom-right corner: 6
the left edge: 9
the top-left corner, maximized: 1
the left edge, framed: 0
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/titlebar.lyra played: its window's maximize button pressed, the system restoring
// it, its minimize button pressed — each heard — and the drag area and controls it told the
// platform, the system's frame taken away. The buttons' places are the window's right end, a
// glyph's width each, whatever the font.
func TestRun_UiTitleBarExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "titlebar")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "titlebar.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "end.png")
	cmd := command(t, bin, "--script", "move:503,12 click:503,12 restore move:465,12 click:465,12", "--png", shot)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	want := `maximized
restored
minimized
drag areas 1, controls in them 4, the system's frame false
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Errorf("no last frame: %v", err)
	}
}

// Command.ChooseFile (WINDOW.md §5) through the host: the platform asked for the dialog — its
// filters, where it starts, open or save — under a tag of the host's; its answer, a path or a
// cancel, raising the event the command made with it, the program going on meanwhile; and an
// answer with no dialog asked raising nothing.
func TestRun_UiChooseFile(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, Command, Subscription, ChooseFile, host_of }
import ui.script.{ timed_platform, ScriptPlatform, Played, Waited, Answered }
import ui.backend.{ Renderer, TextureId, KeyPressed, Modifiers, Tab, Enter, FileDialog }
import ui.draw.{ Size, Color }
import ui.text.{ fonts_new }
import ui.layout.{ Node, column, button, text }

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Open | SaveAs | Chosen(Maybe<string>)

let main = () -> u8 => {
  var platform = timed_platform([Waited(1)], Size { w: 300.0, h: 200.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  var host = host_of(app(), "nothing", platform)
  let _ = host.step(platform, renderer, fonts)
  for (name, steps) in [
    ("Tab, Enter on Open", [Played(KeyPressed(Tab, Modifiers {})), Played(KeyPressed(Enter, Modifiers {}))]),
    ("a font chosen", [Answered(Some("/fonts/a.ttf"))]),
    ("Tab, Enter on Save As", [Played(KeyPressed(Tab, Modifiers {})), Played(KeyPressed(Enter, Modifiers {}))]),
    ("cancelled", [Answered(None)]),
    ("an answer with none asked", [Answered(Some("/nowhere"))]),
  ] {
    platform.steps = [...platform.steps, ...steps, Waited(1)]
    while platform.next < platform.steps.len() { let _ = host.step(platform, renderer, fonts) }
    let asked = [d in platform.dialogs | "#${d.0} ${if d.1.save { "save" } else { "open" }} ${[f in d.1.filters | "${f.0}:${f.1}"].join(",")} from ${d.1.start ?? "anywhere"}"]
    println("${name}: '${host.state}'; waiting ${asked.join("; ")}")
  }
  0
}

let app = pure () -> App<string, Ev> => App {
  view: (heard: string) -> Node<Ev> => column([button("Open", Open), button("Save As", SaveAs), text(heard)]),
  update: (heard: string, e: Ev) -> (string, []Command<Ev>) => match e {
    Open => ("opening", [ChooseFile(FileDialog { filters: [("Fonts", "ttf;otf"), ("All", "*")], start: Some("/fonts") }, Chosen)]),
    SaveAs => ("saving", [ChooseFile(FileDialog { save: true }, Chosen)]),
    Chosen(path) => (path ?? "cancelled", []),
  },
  subscriptions: (_: string) -> []Subscription<Ev> => [],
  background: Color { r: 0, g: 0, b: 0 },
}
`)
	want := `Tab, Enter on Open: 'opening'; waiting #0 open Fonts:ttf;otf,All:* from /fonts
a font chosen: '/fonts/a.ttf'; waiting 
Tab, Enter on Save As: 'saving'; waiting #1 save  from anywhere
cancelled: 'cancelled'; waiting 
an answer with none asked: 'cancelled'; waiting 
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// bindings.dialog's queue, with no dialog shown: answers queued as SDL's callback would queue
// them — a path, a cancel, a failure with its reason — handed back oldest first under their
// tags, then none.
func TestRun_DialogQueue(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import bindings.dialog.{ answer, poll, PathChosen, ChoiceCancelled, ChoiceFailed }

let main = () -> void => {
  answer(PathChosen(3, "/fonts/a.ttf"))
  answer(ChoiceCancelled(4))
  answer(ChoiceFailed(5, "no portal"))
  for _ in 0..<4 {
    println(match poll() {
      Some(PathChosen(tag, path)) => "#${tag} chose ${path}",
      Some(ChoiceCancelled(tag)) => "#${tag} cancelled",
      Some(ChoiceFailed(tag, why)) => "#${tag} failed: ${why}",
      None => "none",
    })
  }
}
`)
	want := "#3 chose /fonts/a.ttf\n#4 cancelled\n#5 failed: no portal\nnone\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// ui.color (WINDOW.md §6): CSS Color 4's text read and written — hex at every length, rgb() with
// commas or spaces and a slash's alpha, oklab() and oklch() with percentages and degrees — CSS's
// red the same each way it is written; an OKLCH green too vivid for sRGB mapped in by its chroma,
// its hue kept within a few degrees (clipping alone turns it yellower); HSV both ways; and
// refusals.
func TestRun_UiColor(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.color.{ parse_color, color_text, oklch, to_oklch, in_gamut, hsv, to_hsv }

let main = () -> void => {
  for text in [
    "#fff",
    "#1234",
    "#11223380",
    "rgb(255 0 0)",
    "rgba(0, 128, 255, 50%)",
    "rgb(100% 50% 0% / 0.25)",
    "oklch(62.8% 0.2577 29.23)",
    "oklab(0.628 0.2249 0.1258)",
    "OKLCH(0.5 0.1 250deg / 50%)",
    "oklch(100% 0 0)",
    "oklch(0% 0.2 40)",
    "oklch(0.7 0.4 150)",
    "red",
    "#12",
    "oklch(0.5 0.1)",
    "rgb(1 2 3 4 5)",
  ] {
    let shown = match parse_color(text) {
      Some(c) => color_text(c),
      None => "not a colour",
    }
    println("${text}: ${shown}")
  }
  let green = to_oklch(oklch(0.7, 0.4, 150.0))
  println("the vivid green mapped: in gamut ${in_gamut(green)}, hue within 4 degrees ${(green.h - 150.0).abs() < 4.0}, chroma lowered ${green.c < 0.4}")
  let back = to_hsv(hsv(210.0, 0.5, 0.8))
  println("hsv(210, 0.5, 0.8): ${color_text(hsv(210.0, 0.5, 0.8))}, back ${back.h} ${back.s} ${back.v}")
}
`)
	want := `#fff: #ffffff
#1234: #11223344
#11223380: #11223380
rgb(255 0 0): #ff0000
rgba(0, 128, 255, 50%): #0080ff80
rgb(100% 50% 0% / 0.25): #ff800040
oklch(62.8% 0.2577 29.23): #ff0000
oklab(0.628 0.2249 0.1258): #ff0000
OKLCH(0.5 0.1 250deg / 50%): #32669a80
oklch(100% 0 0): #ffffff
oklch(0% 0.2 40): #000000
oklch(0.7 0.4 150): #00c248
red: not a colour
#12: not a colour
oklch(0.5 0.1): not a colour
rgb(1 2 3 4 5): not a colour
the vivid green mapped: in gamut true, hue within 4 degrees true, chroma lowered true
hsv(210, 0.5, 0.8): #6699cc, back 210 0.5 0.8
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/notebook.lyra (WINDOW.md step 8) headless — the window's elements written in
// markup: the menus' shortcuts taken by the app, a submenu's item reached by the pointer (a
// press on its opener leaves it open), a menu item closing the shown note, a tab's ×, and a
// swatch's picker changing a note's hue.
func TestRun_UiNotebookExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "notebook")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "notebook.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	for _, c := range []struct{ script, want string }{
		{"", "* Groceries #f2c14e\n  Ideas #5d8cd6\nwrap false\n"},
		{"Primary+n Alt+z", "  Groceries #f2c14e\n  Ideas #5d8cd6\n* Note 1 #f2c14e\nwrap true\n"},
		// File ▸ Go To ▸ Ideas.
		{"click:20,16 click:60,108 click:220,140", "  Groceries #f2c14e\n* Ideas #5d8cd6\nwrap false\n"},
		// File ▸ Close Note, then Groceries' tab's ×.
		{"click:20,16 click:60,73", "* Ideas #5d8cd6\nwrap false\n"},
		{"click:87,50", "* Ideas #5d8cd6\nwrap false\n"},
		// Ideas' swatch, its hue strip's start, and a press away.
		{"click:561,183 click:380,364 click:300,450", "* Groceries #f2c14e\n  Ideas #d67f5d\nwrap false\n"},
	} {
		shot := filepath.Join(dir, "end.png")
		cmd := command(t, bin, "--script", c.script, "--png", shot)
		cmd.Dir = dir
		cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
		out, err := cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
			strings.Contains(string(out), "SDL could not") {
			t.Skipf("SDL could not open a dummy window here: %s", out)
		} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
			t.Skipf("no font here: %s", out)
		} else if err != nil {
			t.Fatalf("running: %v\n%s", err, out)
		}
		if string(out) != c.want {
			t.Errorf("%q: got\n%s\nwant\n%s", c.script, out, c.want)
		}
		if _, err := os.Stat(shot); err != nil {
			t.Errorf("no last frame: %v", err)
		}
	}
}

// examples/ui/colors.lyra (WINDOW.md step 7) headless: its three colours as CSS writes them; the
// Accent's picker opened and its square pressed; and the colour typed into its field as oklch( and
// taken as the focus leaves.
func TestRun_UiColorsExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "colors")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "colors.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	for _, c := range []struct{ script, want string }{
		{"", "Accent #1d84f5\nBackground #1e1e24\nSelection #3c78dc59\n"},
		{"click:157,33 click:200,120", "Accent #738192\nBackground #1e1e24\nSelection #3c78dc59\n"},
		{"click:157,33 click:260,266 Primary+a 'oklch(70% 0.15 30)' click:400,380", "Accent #ed7665\nBackground #1e1e24\nSelection #3c78dc59\n"},
	} {
		shot := filepath.Join(dir, "end.png")
		cmd := command(t, bin, "--script", c.script, "--png", shot)
		cmd.Dir = dir
		cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
		out, err := cmd.CombinedOutput()
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
			strings.Contains(string(out), "SDL could not") {
			t.Skipf("SDL could not open a dummy window here: %s", out)
		} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
			t.Skipf("no font here: %s", out)
		} else if err != nil {
			t.Fatalf("running: %v\n%s", err, out)
		}
		if string(out) != c.want {
			t.Errorf("%q: got\n%s\nwant\n%s", c.script, out, c.want)
		}
		if _, err := os.Stat(shot); err != nil {
			t.Errorf("no last frame: %v", err)
		}
	}
}

// A swatch and its colour picker (WINDOW.md §6): a press on the swatch opens it and another
// closes it; the square makes saturation and value at the hue, the hue strip a hue, the opacity
// strip an opacity, the arrows step the hue; the hue is held through white, which has none; the
// field keeps a half-typed oklch( while the colour stays, takes it whole, and drops the draft as
// the focus leaves.
func TestRun_UiColorPicker(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new, find, Found }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ InputEvent, PointerMoved, PointerDown, PointerUp, Primary, KeyPressed, TextTyped, Key, Modifiers, ArrowRight, Enter, Character }
import ui.draw.{ Size, Point, Color }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ light_theme, Sheet }
import ui.color.{ color_text, hsv, to_hsv }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, button, swatch, themed, tight, ColorAxis, ColorChannel, SatValue, HueChannel, AlphaChannel, Pressable, Editable }

data Ev = Open | Close | Changed(Color) | Away

struct Run {
  ui: Ui,
  cache: LayoutCache<Ev>,
  platform: ScriptPlatform,
  color: Color,
  open: bool = false,
  events: []string = [],
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var run = Run { ui: ui_new(), cache: layout_cache(), platform: script_platform([], Size { w: 400.0, h: 500.0 }), color: hsv(240.0, 0.8, 0.8) }
  play(run, fonts, click(swatch_face(run, fonts)))
  say(run, fonts, "press the swatch")
  play(run, fonts, click(swatch_face(run, fonts)))
  say(run, fonts, "press it again")
  run.open = true
  play(run, fonts, click(on_axis(run, fonts, SatValue, 1.0, 0.0)))
  say(run, fonts, "the square's top right")
  play(run, fonts, click(on_axis(run, fonts, SatValue, 0.0, 0.0)))
  say(run, fonts, "its top left, white")
  play(run, fonts, click(on_axis(run, fonts, SatValue, 0.5, 0.5)))
  say(run, fonts, "its middle: the hue held through white")
  play(run, fonts, click(on_axis(run, fonts, HueChannel, 0.5, 0.5)))
  say(run, fonts, "the hue strip's middle")
  play(run, fonts, key(ArrowRight))
  say(run, fonts, "Right on it")
  play(run, fonts, click(on_axis(run, fonts, AlphaChannel, 0.0, 0.5)))
  say(run, fonts, "the opacity strip's start")

  play(run, fonts, [...click(field_middle(run, fonts)), KeyPressed(Character('a'), Modifiers { primary: true }), TextTyped("oklch(62.8% 0.2577 ")])
  say(run, fonts, "typing oklch( half-way")
  play(run, fonts, [TextTyped("29.23)")])
  say(run, fonts, "and to its end")
  play(run, fonts, click(on_axis(run, fonts, HueChannel, 0.0, 0.5)))
  say(run, fonts, "the focus away, on the hue strip")
  0
}

let play = (run: mut Run, fonts: mut Fonts, inputs: []InputEvent) -> void => {
  for input in inputs {
    let laid = laid_out(run, fonts)
    for e in run.ui.handle(laid, input, run.platform) {
      match e {
        Open => {
          run.open = true
          run.events.push("Open")
        },
        Close => {
          run.open = false
          run.events.push("Close")
        },
        Changed(c) => {
          run.color = c
          run.events.push("Changed")
        },
        Away => run.events.push("Away"),
      }
    }
  }
}

/// A line: the events, the colour (its hue too), whether open, the field's text, and which strips
/// are shown.
let say = (run: mut Run, fonts: mut Fonts, name: string) -> void => {
  let laid = laid_out(run, fonts)
  let changes = run.events.len()
  let h = to_hsv(run.color)
  println("${name}: ${if changes > 0 { run.events[changes - 1] } else { "-" }} ${color_text(run.color)} hue ${h.h.round()}, open ${run.open}, field '${field_text(laid)}', strips ${strips(laid)}")
  run.events = []
}

let laid_out = (run: mut Run, fonts: mut Fonts) -> Laid<Ev> =>
  run.ui.frame_within(view(run), tight(Size { w: 400.0, h: 500.0 }), fonts, run.cache)

let view = pure (run: Run) -> Node<Ev> => themed::<Ev>(light_theme(), Sheet {}, [
  column([swatch(run.color, run.open, Open, Changed, Close), button("Away", Away)]),
])

let where_is = pure (laid: Laid<Ev>, origin: Point, wanted: (Laid<Ev>) -> bool) -> Maybe<Found<Ev>> => {
  if wanted(laid) { return Some(Found { laid: laid, origin: origin }) }
  for child in laid.children {
    if let Some(f) = where_is(child.laid, Point { x: origin.x + child.at.x, y: origin.y + child.at.y }, wanted) { return Some(f) }
  }
  None
}

let middle = pure (f: Maybe<Found<Ev>>) -> Point => match f {
  Some(found) => Point { x: found.origin.x + found.laid.size.w / 2.0, y: found.origin.y + found.laid.size.h / 2.0 },
  None => Point { x: -1.0, y: -1.0 },
}

let swatch_face = (run: mut Run, fonts: mut Fonts) -> Point =>
  middle(where_is(laid_out(run, fonts), Point {}, (l: Laid<Ev>) -> bool => l.tag == "Swatch"))

/// The point x across and y down the axis moving channel.
let on_axis = (run: mut Run, fonts: mut Fonts, channel: ColorChannel, x: f32, y: f32) -> Point => {
  let found = where_is(laid_out(run, fonts), Point {}, (l: Laid<Ev>) -> bool => match l.kind {
    ColorAxis(spec) => spec.channel == channel,
    _ => false,
  })
  match found {
    Some(f) => Point { x: f.origin.x + x.clamp(0.0, 0.99) * f.laid.size.w, y: f.origin.y + y.clamp(0.0, 0.99) * f.laid.size.h },
    None => Point { x: -1.0, y: -1.0 },
  }
}

let field_middle = (run: mut Run, fonts: mut Fonts) -> Point =>
  middle(where_is(laid_out(run, fonts), Point {}, (l: Laid<Ev>) -> bool => match l.kind {
    Editable(_) => true,
    _ => false,
  }))

let field_text = pure (laid: Laid<Ev>) -> string => match where_is(laid, Point {}, (l: Laid<Ev>) -> bool => match l.kind {
  Editable(_) => true,
  _ => false,
}) {
  Some(f) => match f.laid.kind {
    Editable(spec) => spec.value,
    _ => "",
  },
  None => "",
}

/// The strips shown, by name.
let strips = pure (laid: Laid<Ev>) -> string => {
  var out: []string = []
  gather(laid, out)
  out.join(",")
}

let gather = (laid: Laid<Ev>, out: mut []string) -> void => {
  if let ColorAxis(spec) = laid.kind { out.push(laid.label) }
  for child in laid.children { gather(child.laid, out) }
}

let click = pure (at: Point) -> []InputEvent =>
  [PointerMoved(at), PointerDown(Primary, at, 1), PointerUp(Primary, at)]

let key = pure (k: Key) -> []InputEvent => [KeyPressed(k, Modifiers {})]
`)
	want := `press the swatch: Open #2929cc hue 240, open true, field '#2929cc', strips Saturation and value,Hue,Opacity
press it again: Close #2929cc hue 240, open false, field '', strips 
the square's top right: Changed #0303ff hue 240, open true, field '#0303ff', strips Saturation and value,Hue,Opacity
its top left, white: Changed #ffffff hue 0, open true, field '#ffffff', strips Saturation and value,Hue,Opacity
its middle: the hue held through white: Changed #404080 hue 240, open true, field '#404080', strips Saturation and value,Hue,Opacity
the hue strip's middle: Changed #408080 hue 180, open true, field '#408080', strips Saturation and value,Hue,Opacity
Right on it: Changed #407880 hue 188, open true, field '#407880', strips Saturation and value,Hue,Opacity
the opacity strip's start: Changed #40788000 hue 188, open true, field '#40788000', strips Saturation and value,Hue,Opacity
typing oklch( half-way: Changed #40788000 hue 188, open true, field 'oklch(62.8% 0.2577 ', strips Saturation and value,Hue,Opacity
and to its end: Changed #ff0000 hue 0, open true, field 'oklch(62.8% 0.2577 29.23)', strips Saturation and value,Hue,Opacity
the focus away, on the hue strip: - #ff0000 hue 0, open true, field '#ff0000', strips Saturation and value,Hue,Opacity
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A control disabled or enabled again with nothing else changed is laid out anew: its
// `:disabled` look and its refusal of input follow at once. The layout cache gave the last
// frame's layout back for a node the same in every field but `disabled` until 10/22, so 7GUIs'
// flight booker showed its Book button's state only once a hover or a focus change asked for a
// fresh layout.
func TestRun_UiDisabledIsLaidOutAtOnce(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.draw.{ Size, Color }
import ui.text.{ fonts_new }
import ui.style.{ Style, Rule, Sheet, light_theme }
import ui.layout.{ Node, NoEvent, LayoutCache, layout_cache, column, button, themed, tight }

let main = () -> u8 => {
  var ui = ui_new()
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var cache: LayoutCache<NoEvent> = layout_cache()
  let states: []bool = [false, true, false]
  for off in states {
    let laid = ui.frame_within(view(off), tight(Size { w: 200.0, h: 100.0 }), fonts, cache)
    let go = laid.children[0].laid.children[0].laid
    println("disabled ${off}: laid out disabled ${go.disabled}, looks it ${go.style.background == Some(Color { r: 1, g: 2, b: 3 })}")
  }
  0
}

let view = pure (off: bool) -> Node<NoEvent> =>
  themed::<NoEvent>(light_theme(), sheet, [column([button("Go", NoEvent).disabled_if(off)])])

let sheet = Sheet { rules: [
  Rule { selector: "Button:disabled", style: Style { background: Some(Color { r: 1, g: 2, b: 3 }) } },
] }
`)
	want := `disabled false: laid out disabled false, looks it false
disabled true: laid out disabled true, looks it true
disabled false: laid out disabled false, looks it false
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRun_UiReadOnlyField(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.interact.{ Ui, ui_new }
import ui.script.{ script_platform, parse_script }
import std.collections
import ui.draw.{ Size, Color }
import ui.text.{ Fonts, fonts_new }
import ui.style.{ Style, Rule, Sheet, light_theme }
import ui.layout.{ Node, Laid, LayoutCache, layout_cache, column, read_only_input, themed, tight }

data Ev = Edited(string)

/// A read-only field takes the focus, moves its caret, selects and copies, and raises no edit
/// for typing, deleting, pasting, cutting or an assistive technology's new value; the cascade
/// sees it :read-only.
let main = () -> u8 => {
  var ui = ui_new()
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var cache: LayoutCache<Ev> = layout_cache()
  var platform = script_platform([], Size { w: 300.0, h: 100.0 })
  for script in ["Tab", "'x' Backspace Delete", "Primary+a Primary+c", "Primary+v Primary+x Primary+z", "Home Shift+End"] {
    let Ok(inputs) = parse_script(script) else { return 1 }
    var raised = 0
    for input in inputs {
      let laid = frame(ui, cache, fonts)
      raised += ui.handle(laid, input, platform).len()
    }
    let laid = frame(ui, cache, fonts)
    let field = laid.children[0].laid.children[0].laid
    let state = ui.state.widgets.get(field.id)
    let caret = state.map((w) => w.caret) ?? -1
    let anchor = state.map((w) => w.anchor) ?? -1
    println("${script}: ${raised} events, caret ${caret} anchor ${anchor}, clipboard '${platform.clip}', focused ${ui.state.focus == field.id}, read-only look ${field.style.background == Some(Color { r: 1, g: 2, b: 3 })}")
  }
  0
}

let frame = (ui: mut Ui, cache: mut LayoutCache<Ev>, fonts: mut Fonts) -> Laid<Ev> =>
  ui.frame_within(view(), tight(Size { w: 300.0, h: 100.0 }), fonts, cache)

let view = pure () -> Node<Ev> => themed::<Ev>(light_theme(), sheet, [column([read_only_input::<Ev>("42")])])

let sheet = Sheet { rules: [
  Rule { selector: "TextInput:read-only", style: Style { background: Some(Color { r: 1, g: 2, b: 3 }) } },
] }
`)
	want := `Tab: 0 events, caret 2 anchor 0, clipboard '', focused true, read-only look true
'x' Backspace Delete: 0 events, caret 2 anchor 0, clipboard '', focused true, read-only look true
Primary+a Primary+c: 0 events, caret 2 anchor 0, clipboard '42', focused true, read-only look true
Primary+v Primary+x Primary+z: 0 events, caret 2 anchor 2, clipboard '42', focused true, read-only look true
Home Shift+End: 0 events, caret 2 anchor 0, clipboard '42', focused true, read-only look true
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A painted element (`ui.layout`'s `painted`): asked for its whole size and told what its
// scroller shows of it, at the display's scale; its fills moved to its place and its text set
// in the fonts, inside a clip of its box (and of the scroller around it); given back by the
// layout cache with the newest function, so a cached layout paints the newest state; and its
// size heard through `on_size` before the frame that size is painted in — every frame's red
// fill (the size the program heard) is its blue one (the size painted), resizes and a resize
// to the same size alike, which raises nothing.
func TestRun_UiPaintedElement(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, sandbox, host_of }
import ui.script.{ timed_platform, Played, Step }
import ui.backend.{ Renderer, Resized }
import ui.draw.{ Point, Size, Rect, Color, TextureId, DrawOp, Fill, PushClip, PopClip, Glyphs, radius }
import ui.text.{ FontSpec, fonts_new, glyph_atlas }
import ui.style.{ Style, OverflowVisible }
import ui.layout.{
  Node,
  PaintArea,
  PaintOp,
  PaintDraw,
  PaintText,
  Vertical,
  column,
  sized,
  scroll,
  expanded,
  painted,
  layout,
  layout_cached,
  layout_cache,
  interaction_new,
  tight,
  paint,
}

/// A renderer that draws nothing, and says what a frame's fills were.
struct Recorder {
  textures: i64 = 0,
}

impl Renderer for Recorder {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {
    println("  frame: ${[op in ops | is_fill(op) | described(op)].join(", ")}")
  }
}

let is_fill = pure (op: DrawOp) -> bool => match op { Fill(_, _, _) => true, _ => false }

let described = pure (op: DrawOp) -> string => match op {
  Fill(r, _, c) => "fill ${c.r},${c.g},${c.b} at ${r.x},${r.y} ${r.w}x${r.h}",
  PushClip(r, _) => "clip ${r.x},${r.y} ${r.w}x${r.h}",
  PopClip => "unclip",
  Glyphs(_, quads, c) => "${quads.len()} glyphs in ${c.r},${c.g},${c.b} from ${i64(quads[0].at.x.round())},${i64(quads[0].at.y.round())}",
  _ => "other",
}

data Ev = Sized(Size)

const RED: Color = Color { r: 255, g: 0, b: 0 }
const BLUE: Color = Color { r: 0, g: 0, b: 255 }

/// The sizes the board was heard at, in order.
struct State {
  heard: []Size = [],
}

let update = pure (s: State, e: Ev) -> State => match e {
  Sized(size) => State { heard: [...s.heard, size] },
}

/// Under a 40-high strip, a board filling the rest: a red fill as large as the program last
/// heard it was, under a blue one as large as it is painted.
let view = pure (s: State) -> Node<Ev> => {
  let heard = if s.heard.len() > 0 { s.heard[s.heard.len() - 1] } else { Size {} }
  column([
    sized::<Ev>(None, Some(40.0)),
    expanded(painted((area: PaintArea) -> []PaintOp => [
      PaintDraw(Fill(Rect { x: 0.0, y: 0.0, w: heard.w, h: heard.h }, radius(0.0), RED)),
      PaintDraw(Fill(Rect { x: 0.0, y: 0.0, w: area.size.w, h: area.size.h }, radius(0.0), BLUE)),
    ], Some(Sized))),
  ])
}

/// A painter that says, by its tag, when it is painted.
let tagged = pure (tag: string) -> Node<Ev> => sized::<Ev>(Some(50.0), Some(50.0), [
  painted::<Ev>((area: PaintArea) -> []PaintOp => {
    println("  painted ${tag} at ${area.size.w}x${area.size.h}")
    []
  }),
])

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var renderer = Recorder {}
  var atlas = glyph_atlas()

  println("a painter in a scroller, scrolled 300 down a page of 1000, at 10,20, twice the pixels:")
  let page = painted::<Ev>((area: PaintArea) -> []PaintOp => {
    println("  asked for ${area.size.w}x${area.size.h}, showing ${area.visible.x},${area.visible.y} ${area.visible.w}x${area.visible.h}, ${area.scale} pixels a unit")
    [
      PaintDraw(Fill(Rect { x: 0.0, y: 300.0, w: 200.0, h: 20.0 }, radius(0.0), RED)),
      PaintText(Point { x: 4.0, y: 316.0 }, "row", FontSpec { size: 14.0 }, BLUE),
    ]
  }).with_style(Style { height: Some(1000.0) })
  let window = sized::<Ev>(Some(200.0), Some(100.0), [scroll(Vertical, 300.0, page)])
  let laid = layout(window, tight(Size { w: 200.0, h: 100.0 }), fonts)
  for op in paint(laid, Point { x: 10.0, y: 20.0 }, 2.0, fonts, atlas, renderer) {
    if !is_fill(op) || described(op).starts_with("fill 255") { println("  ${described(op)}") }
  }

  println("a painter given back by the layout cache paints with its new function:")
  var cache = layout_cache::<Ev>()
  let _ = layout_cached(tagged("first"), tight(Size { w: 50.0, h: 50.0 }), fonts, interaction_new(), cache)
  let again = layout_cached(tagged("second"), tight(Size { w: 50.0, h: 50.0 }), fonts, interaction_new(), cache)
  println("  ${cache.reused} reused")
  let _ = paint(again, Point {}, 1.0, fonts, atlas, renderer)

  println("a board's size heard before it is painted, at 400x300, 500x200, the same again, 300x300:")
  var steps: []Step = []
  for size in [Size { w: 500.0, h: 200.0 }, Size { w: 500.0, h: 200.0 }, Size { w: 300.0, h: 300.0 }] {
    steps.push(Played(Resized(size)))
  }
  var platform = timed_platform(steps, Size { w: 400.0, h: 300.0 })
  let app = App { sandbox(view, update, Color { r: 0, g: 0, b: 0 }) | window_overflow: OverflowVisible }
  var host = host_of(app, State {}, platform)
  while host.step(platform, renderer, fonts) {}
  println("  heard: ${[s in host.state.heard | "${s.w}x${s.h}"].join(" ")}")
  fonts.close()
  0
}
`)
	// The glyphs' first quad is the "r"'s bitmap, placed from its pen at 14 across and its
	// baseline at 36 down; its exact corner is the font's, so only its row is pinned loosely.
	glyphs := regexp.MustCompile(`3 glyphs in 0,0,255 from (\d+),(\d+)`)
	m := glyphs.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no glyphs painted:\n%s", got)
	}
	if x, _ := strconv.Atoi(m[1]); x < 14 || x > 17 {
		t.Errorf("the text's first glyph starts at x %d, not just right of the pen at 14", x)
	}
	if y, _ := strconv.Atoi(m[2]); y < 24 || y > 33 {
		t.Errorf("the text's first glyph's top is at y %d, not a little above its baseline at 36", y)
	}
	got = glyphs.ReplaceAllString(got, "3 glyphs in 0,0,255")
	want := `a painter in a scroller, scrolled 300 down a page of 1000, at 10,20, twice the pixels:
  asked for 200x1000, showing 0,300 200x100, 2 pixels a unit
  clip 10,20 200x100
  clip 10,-280 200x1000
  fill 255,0,0 at 10,20 200x20
  3 glyphs in 0,0,255
  unclip
  unclip
a painter given back by the layout cache paints with its new function:
  1 reused
  painted second at 50x50
a board's size heard before it is painted, at 400x300, 500x200, the same again, 300x300:
  frame: fill 255,0,0 at 0,40 400x260, fill 0,0,255 at 0,40 400x260
  frame: fill 255,0,0 at 0,40 500x160, fill 0,0,255 at 0,40 500x160
  frame: fill 255,0,0 at 0,40 500x160, fill 0,0,255 at 0,40 500x160
  frame: fill 255,0,0 at 0,40 300x260, fill 0,0,255 at 0,40 300x260
  heard: 400x260 500x160 300x260
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/painted.lyra played headless: resized to 520 × 400, the board beside the
// 180-wide list is 340 wide and has 7 squares a side, which the heading — made from what
// on_size told the program — says in the same frame; the squares alternate from the board's
// top left, light then dark, and stop at its edge, where the list begins.
func TestRun_UiPaintedExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	bin := filepath.Join(dir, "painted")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(root, "examples", "ui", "painted.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.png")
	cmd := command(t, bin, "--script", "resize:520,400 move:450,200 scroll:5", "--png", shot)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("playing: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "board: 340x") || !strings.Contains(string(out), ", 7 by 7 squares") {
		t.Errorf("the board as the program heard it: %s", out)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the frame is not a PNG: %v", err)
	}
	light, dark := [3]int{0xEE, 0xD8, 0xB4}, [3]int{0x8C, 0x5E, 0x3C}
	// The board's top: below the heading, whose height is its font's.
	top := -1
	for y := 0; y < 120; y++ {
		if near(rgbAt(frame, 24, y), light, 2) {
			top = y
			break
		}
	}
	if top < 0 {
		t.Fatal("no light square at the board's top left")
	}
	y := top + 12
	for _, c := range []struct {
		x    int
		want [3]int
		what string
	}{
		{24, light, "a7, the top left square"},
		{72, dark, "b7, the next"},
		{312, light, "g7, the last of the row"},
	} {
		if got := rgbAt(frame, c.x, y); !near(got, c.want, 2) {
			t.Errorf("%s at %d,%d is %v, not %v", c.what, c.x, y, got, c.want)
		}
	}
	if got := rgbAt(frame, 24, top+48+12); !near(got, dark, 2) {
		t.Errorf("a6, below a7, is %v, not dark", got)
	}
	if got := rgbAt(frame, 350, y); near(got, light, 8) || near(got, dark, 8) {
		t.Errorf("the list beside the board, at 350,%d, is a square's colour %v", y, got)
	}
}

// examples/ui/pixels.lyra played headless: zoomed in a step by Primary and the wheel, a stroke
// across the canvas in the starting colour, another colour chosen from the palette and a stroke
// down in it; a press on a pixel already that colour changes nothing, so the picture's revision
// moves only with the strokes' changes. In the frame the strokes' pixels are their colours, at
// the zoom, and a pixel between them is the canvas's grey: the picture was written again as it
// changed.
func TestRun_UiPixelsExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	bin := filepath.Join(dir, "pixels")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(root, "examples", "ui", "pixels.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.png")
	script := "move:400,250 Primary+scroll:-1 down:300,150 move:420,150 up:420,150 click:30,60 " +
		"down:400,300 move:400,360 up:400,360 down:400,300 up:400,300"
	cmd := command(t, bin, "--script", script, "--png", shot)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("playing: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "32x32 at 13x, colour 2: ") || !strings.HasSuffix(strings.TrimSpace(string(out)), ", revision 5") {
		t.Errorf("the picture as the program left it: %s", out)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the frame is not a PNG: %v", err)
	}
	// The canvas: where its grey starts, inside the grid's first lines.
	field := [3]int{0xCF, 0xD2, 0xD8}
	left, top := -1, -1
	b := frame.Bounds()
	for y := b.Min.Y; y < b.Max.Y && left < 0; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if near(rgbAt(frame, x, y), field, 1) {
				left, top = x-1, y-1
				break
			}
		}
	}
	if left < 0 {
		t.Fatal("no canvas in the frame")
	}
	const zoom = 13
	// The middle of the picture's pixel under the window's point x, y.
	middle := func(x, y int) (int, int) {
		return left + (x-left)/zoom*zoom + zoom/2, top + (y-top)/zoom*zoom + zoom/2
	}
	red, plum := [3]int{0xFF, 0x00, 0x4D}, [3]int{0x7E, 0x25, 0x53}
	for _, c := range []struct {
		x, y int
		want [3]int
		what string
	}{
		{300, 150, red, "the first stroke's start"},
		{360, 150, red, "its middle"},
		{420, 150, red, "its end"},
		{400, 330, plum, "the second stroke, in the colour chosen"},
		{360, 250, field, "a pixel no stroke reached"},
	} {
		x, y := middle(c.x, c.y)
		if got := rgbAt(frame, x, y); !near(got, c.want, 2) {
			t.Errorf("%s, at %d,%d, is %v, not %v", c.what, x, y, got, c.want)
		}
	}
}

// The glyph atlas's pages (`ui.text`'s `GlyphAtlas`), with pages of 48 texels and two of them,
// so a capital at 40 pixels takes a page whatever the font: a glyph that fits nowhere opens a
// page; past two, the page drawn from least recently is cleared and filled again (its glyphs
// rendered anew when next drawn), never one the frame being painted has drawn from, whose ops
// still show it — when every page has been, the atlas grows a third; a glyph on a page not
// emptied is never rendered again; and a block across pages is an op a page, in the order the
// pages were first needed.
func TestRun_UiAtlasPages(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.backend.{ Renderer }
import ui.draw.{ Point, Color, TextureId, DrawOp, Glyphs }
import ui.text.{ Fonts, FontSpec, GlyphAtlas, fonts_new, glyph_atlas_sized }

/// A renderer that draws nothing, and counts what it is given: textures uploaded, pages
/// cleared (an update the size of a page) and glyphs written.
struct Counter {
  uploads: i64 = 0,
  clears: i64 = 0,
  glyphs: i64 = 0,
}

impl Renderer for Counter {
  upload = (self, image) => {
    self.uploads += 1
    Some(TextureId(self.uploads - 1))
  }
  update = (self, id, at, image) => {
    if image.width == PAGE { self.clears += 1 } else { self.glyphs += 1 }
    true
  }
  draw = (self, background, ops) => {}
}

const PAGE: i64 = 48

/// One glyph a page: at 40 pixels a capital is more than half a 48-texel page each way.
let draw = (atlas: mut GlyphAtlas, fonts: mut Fonts, renderer: mut Counter, text: string) -> string => {
  let block = fonts.lay_out(text, FontSpec { size: 40.0 }, 1000.0)
  let ops = atlas.glyphs(fonts, renderer, block, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  let pages = [op in ops | describe(op)].join(" ")
  "${text}: on ${pages}; ${atlas.page_count()} pages, ${atlas.evictions()} emptied, ${renderer.uploads} uploaded, ${renderer.clears} cleared, ${renderer.glyphs} rendered"
}

let describe = pure (op: DrawOp) -> string => match op {
  Glyphs(texture, quads, _) => "${i64(texture)}×${quads.len()}",
  _ => "?",
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var atlas = glyph_atlas_sized(PAGE, 2)
  var renderer = Counter {}
  atlas.next_frame()
  println("frame 1, ${draw(atlas, fonts, renderer, "MW")}")
  atlas.next_frame()
  println("frame 2, ${draw(atlas, fonts, renderer, "O")}")
  atlas.next_frame()
  println("frame 3, ${draw(atlas, fonts, renderer, "W")}")
  println("frame 3, ${draw(atlas, fonts, renderer, "MH")}")
  println("frame 3, ${draw(atlas, fonts, renderer, "WMH")}")
  atlas.next_frame()
  println("frame 4, ${draw(atlas, fonts, renderer, "MM")}")
  fonts.close()
  0
}
`)
	want := `frame 1, MW: on 0×1 1×1; 2 pages, 0 emptied, 2 uploaded, 0 cleared, 2 rendered
frame 2, O: on 0×1; 2 pages, 1 emptied, 2 uploaded, 1 cleared, 3 rendered
frame 3, W: on 1×1; 2 pages, 1 emptied, 2 uploaded, 1 cleared, 3 rendered
frame 3, MH: on 0×1 2×1; 3 pages, 2 emptied, 3 uploaded, 2 cleared, 5 rendered
frame 3, WMH: on 1×1 0×1 2×1; 3 pages, 2 emptied, 3 uploaded, 2 cleared, 5 rendered
frame 4, MM: on 0×2; 3 pages, 2 emptied, 3 uploaded, 2 cleared, 5 rendered
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A family's faces (`ui.text`'s `Fonts`), with one font file added as a family's 300, 400, 700
// and italic 400 — so any font will do: a weight drawn in the face CSS Fonts 4's matching
// picks (500 the 400, 600 and 900 the 700, 100 and 350 the 300), the italic face for italic,
// a family's only face whatever is asked, the first family's for an unknown one; what the
// family lacks made once — a 700 italic the italic emboldened, a regular-only family's bold
// and italic drawn wider than its regular; a glyph hinted another way rendered again; faces
// loaded while the app runs (`LoadFonts`), true for a file that is a font and false for one
// that is not there; and the loaded family's metrics through `MeasureFont`, the advance whole
// while glyphs are hinted. Where the
// machine has a variable font, its 700 is its own instance — wider, not emboldened — and a
// weight past its axis its heaviest.
func TestRun_UiFontFaces(t *testing.T) {
	t.Parallel()
	program := `import ui.app.{ App, sandbox, host_of, Command, MeasureFont, LoadFonts }
import ui.script.{ timed_platform, parse_steps }
import ui.backend.{ Renderer }
import ui.draw.{ Point, Size, Color, TextureId }
import ui.text.{ Fonts, FontSpec, FontMetrics, FontFile, GlyphAtlas, fonts_new, glyph_atlas, SYSTEM_FONTS }
import ui.layout.{ Node, button, sized }
import bindings.freetype.{ HintNone, HintLight }

/// A renderer that draws nothing, and keeps the width of each glyph it is given.
struct Widths {
  textures: i64 = 0,
  widths: []i64 = [],
}

impl Renderer for Widths {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = (self, id, at, image) => {
    if image.width < 1024 { self.widths.push(image.width) }
    true
  }
  draw = (self, background, ops) => {}
}

/// The face each spec is drawn in, by number: 0–3 as added, 4 and on made.
let face_of = (fonts: mut Fonts, spec: FontSpec) -> i64 => fonts.lay_out("H", spec, 1000.0).face

/// How wide an "H" in spec is, in pixels at 48 to the em.
let ink_width = (fonts: mut Fonts, spec: FontSpec) -> i64 => {
  var atlas = glyph_atlas()
  var renderer = Widths {}
  let block = fonts.lay_out("H", FontSpec { spec | size: 48.0 }, 1000.0)
  let _ = atlas.glyphs(fonts, renderer, block, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  if renderer.widths.len() == 0 { -1 } else { renderer.widths[0] }
}

data Ev = Measure | MissingLoaded(bool) | Loaded(bool) | Measured(FontMetrics)

/// What the app heard: whether the missing file loaded, whether the real one did, and the
/// loaded family's metrics.
struct Probe {
  missing: bool = true,
  loaded: bool = false,
  metrics: Maybe<FontMetrics> = None,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  var path = ""
  for p in SYSTEM_FONTS {
    if fonts.add("t", p, 0, 300) {
      path = p
      break
    }
  }
  if path == "" { return 1 }
  // The same file as four faces of one family — 300, 400, 700 and an italic 400 — and as the
  // only, italic, face of another.
  let _ = fonts.add("t", path, 0, 400)
  let _ = fonts.add("t", path, 0, 700)
  let _ = fonts.add("t", path, 0, 400, true)
  let _ = fonts.add("slanted", path, 0, 400, true)
  println("CSS's choice of face:")
  for (weight, italic) in [(400, false), (500, false), (600, false), (900, false), (100, false), (350, false), (400, true)] {
    println("  ${weight}${if italic { " italic" } else { "" }}: face ${face_of(fonts, FontSpec { family: "t", weight: weight, italic: italic })}")
  }
  println("  slanted, upright asked: face ${face_of(fonts, FontSpec { family: "slanted" })}")
  println("  an unknown family is the first's: face ${face_of(fonts, FontSpec { family: "nope" })}")
  println("what the family lacks, made once:")
  let bold_italic = face_of(fonts, FontSpec { family: "t", weight: 700, italic: true })
  println("  700 italic: face ${bold_italic}, again ${face_of(fonts, FontSpec { family: "t", weight: 700, italic: true })}, emboldened ${fonts.faces[bold_italic].embolden}, slanted ${fonts.faces[bold_italic].oblique}")
  var plain = fonts_new()
  let _ = plain.add("p", path)
  let regular = ink_width(plain, FontSpec { family: "p" })
  let bold = ink_width(plain, FontSpec { family: "p", weight: 700 })
  let slanted = ink_width(plain, FontSpec { family: "p", italic: true })
  println("  a made bold is wider: ${bold > regular}; a made italic is wider: ${slanted > regular}; a 500 is the regular: ${face_of(plain, FontSpec { family: "p", weight: 500 }) == 0}")
  println("a glyph hinted another way is another glyph:")
  var atlas = glyph_atlas()
  var renderer = Widths {}
  let block = plain.lay_out("H", FontSpec { family: "p" }, 1000.0)
  let _ = atlas.glyphs(plain, renderer, block, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  let _ = atlas.glyphs(plain, renderer, block, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  plain.hinting = HintNone
  let _ = atlas.glyphs(plain, renderer, block, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  println("  rendered ${renderer.widths.len()} times")
  println("faces loaded while the app runs, and measured:")
  plain.hinting = HintLight
  let app = App {
    sandbox(view, update, Color { r: 0, g: 0, b: 0 }) |
    update: (s: Probe, e: Ev) -> (Probe, []Command<Ev>) => match e {
      Measure => (s, [LoadFonts([FontFile { family: "q", path: "/no/such/font.ttf" }], MissingLoaded)]),
      MissingLoaded(ok) => (Probe { s | missing: ok }, [LoadFonts([FontFile { family: "q", path: path, weight: 700 }], Loaded)]),
      Loaded(ok) => (Probe { s | loaded: ok }, [MeasureFont(FontSpec { family: "q", size: 13.0, weight: 700 }, Measured)]),
      Measured(m) => (Probe { s | metrics: Some(m) }, []),
    },
  }
  // A pass for each command of the chain: the missing load, the real one, the measuring.
  let Ok(steps) = parse_steps("click:20,10 wait:10 wait:10 wait:10") else { return 2 }
  var platform = timed_platform(steps, Size { w: 200.0, h: 100.0 })
  var host = host_of(app, Probe {}, platform)
  var paint = Widths {}
  while host.step(platform, paint, plain) {}
  println("  a missing file loaded ${host.state.missing}, a font file ${host.state.loaded}, its family ${plain.faces.any((f) => f.family == "q")}")
  let Some(m) = host.state.metrics else {
    println("  never measured")
    return 0
  }
  println("  ascent ${m.ascent > 6.0 && m.ascent < 15.0}, descent ${m.descent > 1.0 && m.descent < 6.0}, advance ${m.advance > 6.0 && m.advance < 15.0} and whole ${m.advance == f32(m.advance.round())}, underline below ${m.underline_y > 0.0} and ${m.underline_thickness > 0.0} thick, strikeout above ${m.strike_y > 0.0} and ${m.strike_thickness > 0.0} thick")
  VARIABLE
  0
}

let view = pure (_: Probe) -> Node<Ev> => sized(Some(100.0), Some(30.0), [button("measure", Measure)])

let update = pure (s: Probe, _: Ev) -> Probe => s
`
	variable := `  var varied = fonts_new()
  if varied.add("v", "VARIABLE_PATH") {
    let regular = varied.lay_out("HHHH", FontSpec { family: "v", size: 40.0 }, 1000.0)
    let bold = varied.lay_out("HHHH", FontSpec { family: "v", size: 40.0, weight: 700 }, 1000.0)
    let heaviest = varied.lay_out("HHHH", FontSpec { family: "v", size: 40.0, weight: 2000 }, 1000.0)
    let again = varied.lay_out("HHHH", FontSpec { family: "v", size: 40.0, weight: 2000 }, 1000.0)
    println("a variable font's weights: 700 is another face ${bold.face != regular.face}, wider ${bold.width > regular.width}, not emboldened ${!varied.faces[bold.face].embolden}; past its axis is its heaviest ${heaviest.face == again.face && heaviest.width >= bold.width}")
  }
`
	want := `CSS's choice of face:
  400: face 1
  500: face 1
  600: face 2
  900: face 2
  100: face 0
  350: face 0
  400 italic: face 3
  slanted, upright asked: face 4
  an unknown family is the first's: face 1
what the family lacks, made once:
  700 italic: face 5, again 5, emboldened true, slanted false
  a made bold is wider: true; a made italic is wider: true; a 500 is the regular: true
a glyph hinted another way is another glyph:
  rendered 2 times
faces loaded while the app runs, and measured:
  a missing file loaded false, a font file true, its family true
  ascent true, descent true, advance true and whole true, underline below true and true thick, strikeout above true and true thick
`
	// A variable font with a weight axis, where there is one.
	found := ""
	for _, path := range []string{"/System/Library/Fonts/SFNS.ttf", "/usr/share/fonts/truetype/noto/NotoSans[wdth,wght].ttf", "/usr/share/fonts/opentype/inter/InterVariable.ttf"} {
		if _, err := os.Stat(path); err == nil {
			found = path
			break
		}
	}
	if found != "" {
		program = strings.Replace(program, "  VARIABLE\n", strings.Replace(variable, "VARIABLE_PATH", found, 1), 1)
		want += "a variable font's weights: 700 is another face true, wider true, not emboldened true; past its axis is its heaviest true\n"
	} else {
		program = strings.Replace(program, "  VARIABLE\n", "", 1)
	}
	if got := runUiProgram(t, program); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Fallback fonts (`ui.text`): a paragraph cut into runs by the face each character is drawn
// from — the first of a spec's families, then the fallbacks, whose font has it — each shaped
// in its face, every glyph checked against HarfBuzz's own answer, so the test holds whatever
// fonts the machine has; the layout made again once fallbacks are added; a combining mark in
// its base's face; an unknown family in a list passed over; and every face's glyphs drawn
// into one atlas page. Where the machine has a CJK font, 漢 is drawn from it, where without
// it the system font drew its missing glyph.
func TestRun_UiFontFallback(t *testing.T) {
	t.Parallel()
	program := `import ui.backend.{ Renderer }
import ui.draw.{ Point, Color, TextureId }
import ui.text.{ Fonts, FontSpec, TextBlock, fonts_new, glyph_atlas }
import bindings.harfbuzz.{ has_glyph }

/// A renderer that draws nothing, and counts the glyphs it is given.
struct Counter {
  textures: i64 = 0,
  glyphs: i64 = 0,
}

impl Renderer for Counter {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = (self, id, at, image) => {
    if image.width < 1024 { self.glyphs += 1 }
    true
  }
  draw = (self, background, ops) => {}
}

/// The characters of text, each with the byte it starts at.
let starts = (text: string) -> [](i64, rune) => {
  var out: [](i64, rune) = []
  var at = 0
  for c in text {
    out.push((at, c))
    at += "${c}".encode_utf8().len()
  }
  out
}

/// Whether every glyph of block is drawn from the first of order whose font has the
/// character its cluster starts with — or the first of all where none has.
let agrees = (fonts: Fonts, block: TextBlock, text: string, order: []i64) -> bool => {
  let chars = starts(text)
  for line in block.lines {
    for g in line.glyphs {
      var c: rune = ' '
      for (at, ch) in chars { if at == g.cluster { c = ch } }
      var want = order[0]
      for f in order {
        if has_glyph(fonts.faces[f].hb, c) {
          want = f
          break
        }
      }
      if c != ' ' && g.face != want { return false }
    }
  }
  true
}

/// The faces of block's glyphs, in order.
let faces_of = pure (block: TextBlock) -> string => {
  var out: []string = []
  for line in block.lines { for g in line.glyphs { out.push("${g.face}") } }
  out.join(" ")
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  let text = "A漢B☃ é𝐀"
  let spec = FontSpec { family: "system" }
  let before = fonts.lay_out(text, spec, 1000.0)
  println("one face, no fallbacks: every glyph from it ${faces_of(before).split(" ").all((f) => f == "0")}")
  let found = fonts.add_system_fallbacks()
  let order = [0, ...[i in 1..<(found + 1) | i]]
  let after = fonts.lay_out(text, spec, 1000.0)
  println("with the system's fallbacks: each glyph from the first face that has it ${agrees(fonts, after, text, order)}, the layout made again ${faces_of(after) != faces_of(before) || found == 0 || !after.lines[0].glyphs.any((g) => g.face > 0)}")
  let marked = fonts.lay_out("漢\u0301", spec, 1000.0)
  let mark_faces = faces_of(marked).split(" ")
  println("a mark keeps its base's face: ${mark_faces.all((f) => f == mark_faces[0])}")
  let listed = fonts.lay_out("A", FontSpec { family: "nope, system" }, 1000.0)
  println("an unknown family in a list is passed over: ${faces_of(listed) == "0"}")
  var atlas = glyph_atlas()
  var renderer = Counter {}
  let ops = atlas.glyphs(fonts, renderer, after, Point {}, 1.0, Color { r: 0, g: 0, b: 0 })
  println("drawn from every face: glyphs rendered ${renderer.glyphs >= 5}, in one op ${ops.len() == 1}")
  CJK
  fonts.close()
  0
}
`
	cjk := `  let han = fonts.lay_out("漢", spec, 1000.0).lines[0].glyphs[0]
  println("漢 from the CJK fallback: ${han.face == 1}, a glyph of its own ${han.glyph != 0}; without it, the missing glyph ${before.lines[0].glyphs[1].glyph == 0}")
`
	want := `one face, no fallbacks: every glyph from it true
with the system's fallbacks: each glyph from the first face that has it true, the layout made again true
a mark keeps its base's face: true
an unknown family in a list is passed over: true
drawn from every face: glyphs rendered true, in one op true
`
	found := false
	for _, path := range []string{"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", "/System/Library/Fonts/Hiragino Sans GB.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc", "/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc", "/usr/share/fonts/google-noto-sans-cjk-vf-fonts/NotoSansCJK-VF.ttc", "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"} {
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}
	if found {
		program = strings.Replace(program, "  CJK\n", cjk, 1)
		want += "漢 from the CJK fallback: true, a glyph of its own true; without it, the missing glyph true\n"
	} else {
		program = strings.Replace(program, "  CJK\n", "", 1)
	}
	if got := runUiProgram(t, program); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A row of cells (`ui.text`'s `Fonts.cells`, `ui.layout`'s `PaintCells`): unshaped, each glyph
// centred on its cell's columns — a narrow one on its own, a wide one across two — a space
// drawing nothing, a cell with a combining mark shaped as one cluster in one face, a bold
// cell in a face made bold, a character no face has the missing glyph; painted at the
// element's place on its baseline, inside its cell. And a wave (`PaintWave`): columns a device
// pixel wide covering its width with no gap, within its amplitude and thickness.
func TestRun_UiCells(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.backend.{ Renderer }
import ui.draw.{ Point, Size, Rect, Color, TextureId, DrawOp, Fill, Glyphs }
import ui.text.{ Fonts, FontSpec, Cell, fonts_new, glyph_atlas }
import ui.layout.{ Node, PaintArea, PaintOp, PaintCells, PaintWave, painted, layout, tight, paint }
import bindings.harfbuzz.{ advance_of }

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Nothing

let near = pure (a: f32, b: f32) -> bool => (a - b).abs() < 0.01

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  let font = FontSpec { size: 20.0 }
  let cell: f32 = 30.0
  let row = fonts.cells([
    Cell { col: 0, text: "M" },
    Cell { col: 1, text: " " },
    Cell { col: 2, text: "i" },
    Cell { col: 3, text: "W", width: 2 },
    Cell { col: 5, text: "é" },
    Cell { col: 6, text: "B", bold: true },
  ], font, cell)
  let glyphs = row.lines[0].glyphs
  let i_width = advance_of(fonts.faces[0].hb, 20.0, 'i') ?? 0.0
  let w_width = advance_of(fonts.faces[0].hb, 20.0, 'W') ?? 0.0
  println("a row of cells, 30 wide:")
  println("  the space draws nothing: ${[g in glyphs | g.pen >= cell && g.pen < 2.0 * cell | g].len() == 0}")
  println("  M's pen centred on column 0: ${near(glyphs[0].pen, (cell - (advance_of(fonts.faces[0].hb, 20.0, 'M') ?? 0.0)) / 2.0)}")
  println("  i's centred on column 2: ${near(glyphs[1].pen, 2.0 * cell + (cell - i_width) / 2.0)}")
  println("  a wide W centred across columns 3 and 4: ${near(glyphs[2].pen, 3.0 * cell + (2.0 * cell - w_width) / 2.0)}")
  let marked = [g in glyphs | g.pen >= 5.0 * cell && g.pen < 6.0 * cell | g]
  println("  e and its accent on column 5, one face: ${marked.len() >= 1 && marked.all((g) => g.face == marked[0].face)}")
  let bold = glyphs[glyphs.len() - 1]
  println("  the bold B in a face of its own: ${bold.face != glyphs[0].face && fonts.faces[bold.face].embolden}")
  println("  the row is as wide as its columns: ${near(row.width, 7.0 * cell)}")
  let missing = fonts.cells([Cell { col: 0, text: "\uE000" }], font, cell)
  println("  a character no face has is the missing glyph: ${missing.lines[0].glyphs[0].glyph == 0}")

  println("painted at 100,50:")
  let grid = painted::<Ev>((_: PaintArea) -> []PaintOp => [
    PaintCells(Point { x: 10.0, y: 40.0 }, cell, [Cell { col: 1, text: "M" }], font, Color { r: 1, g: 2, b: 3 }),
    PaintWave(Point { x: 0.0, y: 60.0 }, 40.0, 3.0, 2.0, Color { r: 4, g: 5, b: 6 }),
  ])
  let laid = layout(grid, tight(Size { w: 200.0, h: 100.0 }), fonts)
  var atlas = glyph_atlas()
  var renderer = Blank {}
  let ops = paint(laid, Point { x: 100.0, y: 50.0 }, 2.0, fonts, atlas, renderer)
  var wave: []Rect = []
  for op in ops {
    match op {
      Glyphs(_, quads, c) => {
        let q = quads[0].at
        println("  M's glyph in 1,2,3 inside its cell: ${q.x >= 140.0 && q.x + q.w <= 170.0}, above its baseline at 90: ${q.y < 90.0 && q.y + q.h <= 90.5}")
      },
      Fill(r, _, c) => { if c.r == 4 { wave.push(r) } },
      _ => {},
    }
  }
  var covered = true
  for i, r in wave {
    if i > 0 && !near(wave[i - 1].x + wave[i - 1].w, r.x) { covered = false }
  }
  var top: f32 = 1000.0
  var bottom: f32 = 0.0
  for r in wave {
    top = top.min(r.y)
    bottom = bottom.max(r.y + r.h)
  }
  println("  the wave: ${wave.len()} columns a device pixel wide, from 100 to 140 with no gap ${covered && near(wave[0].x, 100.0) && near(wave[wave.len() - 1].x + wave[wave.len() - 1].w, 140.0)}, within its amplitude and thickness ${top >= 106.0 && bottom <= 114.0 && top < 108.0 && bottom > 112.0}")
  fonts.close()
  0
}
`)
	want := `a row of cells, 30 wide:
  the space draws nothing: true
  M's pen centred on column 0: true
  i's centred on column 2: true
  a wide W centred across columns 3 and 4: true
  e and its accent on column 5, one face: true
  the bold B in a face of its own: true
  the row is as wide as its columns: true
  a character no face has is the missing glyph: true
painted at 100,50:
  M's glyph in 1,2,3 inside its cell: true, above its baseline at 90: true
  the wave: 80 columns a device pixel wide, from 100 to 140 with no gap true, within its amplitude and thickness true
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// examples/ui/cells.lyra drawn headless, read a pixel at a time at the cells its printed cell
// size places: a row of full blocks fills its cells in blue and stops at its last; a selected
// run's background and a warning's are their colours under their spaces; the screen's
// background between rows.
func TestRun_UiCellsExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	bin := filepath.Join(dir, "cells")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(root, "examples", "ui", "cells.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.png")
	cmd := command(t, bin, "--script", "", "--png", shot)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	} else if err != nil {
		t.Fatalf("drawing: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`cell: ([0-9.]+)x([0-9.]+), from ([0-9.]+),([0-9.]+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("no cell size printed: %s", out)
	}
	w, _ := strconv.ParseFloat(m[1], 64)
	h, _ := strconv.ParseFloat(m[2], 64)
	x0, _ := strconv.ParseFloat(m[3], 64)
	y0, _ := strconv.ParseFloat(m[4], 64)
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	frame, err := png.Decode(f)
	if err != nil {
		t.Fatalf("the frame is not a PNG: %v", err)
	}
	// The middle of the cell at a column and row.
	at := func(col, row float64) (int, int) {
		return int(x0 + (col+0.5)*w), int(y0 + (row+0.5)*h)
	}
	blue, background := [3]int{0x61, 0xAF, 0xEF}, [3]int{0x1E, 0x1F, 0x26}
	selected, yellow := [3]int{0x3E, 0x44, 0x51}, [3]int{0xE5, 0xC0, 0x7B}
	for _, c := range []struct {
		col, row float64
		want     [3]int
		what     string
	}{
		{0, 8, blue, "the first full block"},
		{7, 8, blue, "the last full block"},
		{8.5, 8, background, "past the blocks"},
		{0, 3, selected, "the selected run's leading space"},
		{11, 3, yellow, "the warning's leading space"},
		{40, 2, background, "the screen beyond the text"},
	} {
		x, y := at(c.col, c.row)
		if got := rgbAt(frame, x, y); !near(got, c.want, 6) {
			t.Errorf("%s, at %d,%d, is %v, not %v", c.what, x, y, got, c.want)
		}
	}
}

// An element taking input raw (`ui.layout`'s `RawInput`, a painter's) through a host and a
// script: a click on it a move, a press and a release in its own coordinates, the press giving
// it the focus; every key it is given before the framework — Tab too, and F5 and Insert — and
// a key it hands back (Down) the framework's; typed text and an input method's composition; a
// Primary shortcut the program's before the element hears the key (`App.shortcuts`); Shift held
// on a press; a drag past its edge still its own, the press holding the pointer; a wheel it
// hands back raising nothing; the window's focus heard (`OnFocus`); a button's `FocusKeyed`
// giving it the focus again; and the input method's window placed at its caret.
func TestRun_UiRawInput(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, host_of, Command, Subscription, OnFocus, FocusKeyed, Shortcut, Shortcuts }
import ui.script.{ timed_platform, parse_steps }
import ui.backend.{ Renderer, Key, Modifiers, Character, Tab, ArrowDown, FunctionKey, Insert, PointerButton, Primary, Secondary }
import ui.draw.{ Point, Size, Rect, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{
  Node,
  PaintArea,
  PaintOp,
  RawInput,
  KeyInput,
  TextInput,
  TypedText,
  ComposedText,
  PointerInput,
  PointerPress,
  PointerRelease,
  PointerMove,
  PointerWheel,
  row,
  MainStart,
  CrossStart,
  sized,
  button,
  painted,
}

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

data Ev = Heard(string) | Pressed | Focus(bool) | NewTab

/// What the program heard, in order.
struct State {
  heard: []string = [],
}

let update = pure (s: State, e: Ev) -> (State, []Command<Ev>) => match e {
  Heard(what) => (State { heard: [...s.heard, what] }, []),
  Pressed => (State { heard: [...s.heard, "the button"] }, [FocusKeyed("term")]),
  Focus(on) => (State { heard: [...s.heard, if on { "window focused" } else { "window unfocused" }] }, []),
  NewTab => (State { heard: [...s.heard, "shortcut: new tab"] }, []),
}

let name_of = pure (key: Key) -> string => match key {
  Character(' ') => "Space",
  Character(c) => "${c}",
  Tab => "Tab",
  FunctionKey(n) => "F${n}",
  Insert => "Insert",
  _ => "another key",
}

let mods_of = pure (m: Modifiers) -> string =>
  "${if m.shift { "Shift+" } else { "" }}${if m.primary { "Primary+" } else { "" }}"

let button_of = pure (b: PointerButton) -> string => match b {
  Primary => "primary",
  Secondary => "secondary",
  _ => "another",
}

/// A 200 × 100 painter taking input raw — every key but Down, which it hands back, all text,
/// and the pointer but the wheel — beside a button.
let view = pure (_: State) -> Node<Ev> => row([
  sized(Some(200.0), Some(100.0), [
    painted::<Ev>((_: PaintArea) -> []PaintOp => []).takes_input(RawInput {
      on_key: (k: KeyInput) -> Maybe<Ev> => if k.key == ArrowDown { None } else {
        Some(Heard("key ${mods_of(k.mods)}${name_of(k.key)}${if k.pressed { "" } else { " up" }}"))
      },
      on_text: (t: TextInput) -> Maybe<Ev> => match t {
        TypedText(text) => Some(Heard("typed '${text}'")),
        ComposedText(text, caret, _) => Some(Heard("composing '${text}' caret ${caret}")),
      },
      on_pointer: (p: PointerInput) -> Maybe<Ev> => match p.kind {
        PointerPress(b, clicks) => Some(Heard("${mods_of(p.mods)}${button_of(b)} press ${clicks} at ${p.at.x},${p.at.y}")),
        PointerRelease(b) => Some(Heard("${button_of(b)} release at ${p.at.x},${p.at.y}")),
        PointerMove => Some(Heard("move to ${p.at.x},${p.at.y}")),
        PointerWheel(_, _) => None,
      },
      caret: Some(Rect { x: 10.0, y: 20.0, w: 1.0, h: 16.0 }),
    }).keyed("term"),
  ]),
  button("other", Pressed),
]).aligned(MainStart, CrossStart)

let main = () -> u8 => {
  let script = "click:30,40 a Tab Down F5 Insert 'hi' compose:'ka' Primary+t Shift+click:30,40 down:30,40 move:300,300 up:300,300 move:50,50 scroll:2 blur focus click:210,10 a"
  let Ok(steps) = parse_steps(script) else { return 2 }
  var platform = timed_platform(steps, Size { w: 400.0, h: 200.0 })
  let app = App {
    view: view,
    update: update,
    subscriptions: (_: State) -> []Subscription<Ev> => [OnFocus(Focus)],
    background: Color { r: 0, g: 0, b: 0 },
    shortcuts: [Shortcuts {
      of: (_: State) -> []Shortcut<Ev> => [Shortcut { key: Character('t'), mods: Modifiers { primary: true }, event: NewTab }],
    }],
  }
  var host = host_of(app, State {}, platform)
  var renderer = Blank {}
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  while host.step(platform, renderer, fonts) {}
  for line in host.state.heard { println(line) }
  let at = platform.typing ?? Rect {}
  println("the input method's window at ${at.x},${at.y} ${at.w}x${at.h}")
  fonts.close()
  0
}
`)
	want := `move to 30,40
primary press 1 at 30,40
primary release at 30,40
key a
key Tab
key F5
key Insert
typed 'hi'
composing 'ka' caret 2
shortcut: new tab
move to 30,40
Shift+primary press 1 at 30,40
primary release at 30,40
primary press 1 at 30,40
move to 300,300
primary release at 300,300
move to 50,50
window unfocused
window focused
the button
key a
the input method's window at 10,20 1x16
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A descriptor the state reads (`ui.app`'s `Readable`) through a host: readable while it holds
// data — what was written before the loop, then what the first update wrote — and not once
// it is drained. And a scroller that follows its end (`scroll_follow`): kept there as its
// content grows, left where the user scrolled it off its end however it grows, and following
// again once `ScrollTo` puts it at its end; `ScrollTo` to an offset is that offset.
func TestRun_UiReadableAndScrollFollow(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ App, host_of, Command, Subscription, Readable, ScrollTo }
import ui.script.{ timed_platform, script_platform, Waited, Step }
import ui.backend.{ Renderer, PointerMoved, Scrolled }
import ui.draw.{ Point, Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.style.{ Style, OverflowAuto }
import ui.layout.{ Node, Laid, column, sized, layout_cache, tight, UNBOUNDED }
import ui.interact.{ ui_new }
import std.ffi

/// A renderer that draws nothing.
struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = (self, background, ops) => {}
}

@symbol("pipe")
unsafe extern posix_pipe: (fds: ^mut i32) -> i32
@symbol("read")
unsafe extern posix_read: (fd: i32, buf: ^mut u8, count: u64) -> i64
@symbol("write")
unsafe extern posix_write: (fd: i32, buf: ^u8, count: u64) -> i64

/// The pipe's ends, what was read from it, and how many times it was readable.
struct Piped {
  read_end: i32,
  write_end: i32,
  got: string = "",
  heard: i64 = 0,
}

data Ev = Ready

/// Read what is waiting; the first time, write more for the next pass to find.
let update = (s: Piped, _: Ev) -> (Piped, []Command<Ev>) => {
  var buf: []u8 = [0; 64]
  let n = unsafe { posix_read(s.read_end, buf.data_mut(), 64) }
  let got = "${s.got}${[b in buf.slice(0, n.max(0)) | rune(i32(b))].join()}"
  if s.heard == 0 {
    let more = "world".encode_utf8()
    let _ = unsafe { posix_write(s.write_end, more.data(), u64(more.len())) }
  }
  (Piped { s | got: got, heard: s.heard + 1 }, [])
}

/// A scroller 100 high keyed "log", following its end, holding rows rows 30 high — in a
/// column, as a key names an element among its siblings.
let log = pure (rows: i64) -> Node<Ev> => column([
  column([i in 0..<rows | sized::<Ev>(Some(200.0), Some(30.0))])
    .with_style(Style { height: Some(100.0), overflow_y: Some(OverflowAuto), scroll_follow: Some(true) })
    .keyed("log"),
])

let offset_of = pure (laid: Laid<Ev>) -> f32 => match laid.children[0].laid.scroller {
  Some(sc) => sc.offset.y,
  None => -1.0,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  if !fonts.add_system_font() { return 1 }
  var renderer = Blank {}

  println("Readable, on a pipe written before the loop and by the first update:")
  var ends: []i32 = [0, 0]
  if unsafe { posix_pipe(ends.data_mut()) } != 0 { return 2 }
  let hello = "hello".encode_utf8()
  let _ = unsafe { posix_write(ends[1], hello.data(), u64(hello.len())) }
  let piped_view = pure (_: Piped) -> Node<Ev> => sized::<Ev>(Some(10.0), Some(10.0))
  let app = App {
    view: piped_view,
    update: update,
    subscriptions: (s: Piped) -> []Subscription<Ev> => [Readable(s.read_end, Ready)],
    background: Color { r: 0, g: 0, b: 0 },
  }
  var steps: []Step = []
  for _ in 0..<6 { steps.push(Waited(1)) }
  var platform = timed_platform(steps, Size { w: 100.0, h: 100.0 })
  var host = host_of(app, Piped { read_end: ends[0], write_end: ends[1] }, platform)
  while host.step(platform, renderer, fonts) {}
  println("  read \"${host.state.got}\", readable ${host.state.heard} times")

  println("a scroller following its end:")
  var ui = ui_new()
  var cache = layout_cache::<Ev>()
  var fake = script_platform([], Size { w: 200.0, h: 100.0 })
  let window = tight(Size { w: 200.0, h: 100.0 })
  var laid = ui.frame_within(log(5), window, fonts, cache)
  println("  5 rows (150 in 100): at ${offset_of(laid)}")
  laid = ui.frame_within(log(8), window, fonts, cache)
  println("  8 rows: at ${offset_of(laid)}, still its end")
  let _ = ui.handle(laid, PointerMoved(Point { x: 50.0, y: 50.0 }), fake)
  let _ = ui.handle(laid, Scrolled(0.0, -2.0), fake)
  laid = ui.frame_within(log(8), window, fonts, cache)
  let scrolled = offset_of(laid)
  println("  scrolled up 2 lines: at ${scrolled}")
  laid = ui.frame_within(log(12), window, fonts, cache)
  println("  12 rows: still at ${offset_of(laid)}, where it was ${offset_of(laid) == scrolled}")
  ui.scroll_keyed("log", UNBOUNDED)
  laid = ui.frame_within(log(12), window, fonts, cache)
  println("  ScrollTo past its end: at ${offset_of(laid)}")
  laid = ui.frame_within(log(15), window, fonts, cache)
  println("  15 rows: at ${offset_of(laid)}, following again")
  ui.scroll_keyed("log", 30.0)
  laid = ui.frame_within(log(15), window, fonts, cache)
  println("  ScrollTo 30: at ${offset_of(laid)}")
  fonts.close()
  0
}
`)
	want := `Readable, on a pipe written before the loop and by the first update:
  read "helloworld", readable 2 times
a scroller following its end:
  5 rows (150 in 100): at 50
  8 rows: at 140, still its end
  scrolled up 2 lines: at 60
  12 rows: still at 60, where it was true
  ScrollTo past its end: at 260
  15 rows: at 350, following again
  ScrollTo 30: at 30
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// bindings.wake's watcher thread waking an SDL wait: armed, a write to the pipe it watches
// wakes `wait_event` with its event; having woken it is disarmed, and a second write does
// not; armed again with that data unread, it wakes at once.
func TestRun_UiWakeWatch(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	setenv(t, "SDL_VIDEODRIVER", "dummy")
	src := filepath.Join(t.TempDir(), "main.lyra")
	if err := os.WriteFile(src, []byte(`import bindings.sdl3.{ init, quit, INIT_VIDEO, Event, wait_event, Other as SdlOther }
import bindings.wake
import std.ffi

@symbol("pipe")
unsafe extern posix_pipe: (fds: ^mut i32) -> i32
@symbol("write")
unsafe extern posix_write: (fd: i32, buf: ^u8, count: u64) -> i64

/// Whether event is the watcher's wake, of event type kind.
let woke = pure (event: Maybe<Event>, kind: u32) -> bool => match event {
  Some(SdlOther(k)) => k == kind,
  _ => false,
}

let send = (fd: i32, text: string) -> void => {
  let bytes = text.encode_utf8()
  let _ = unsafe { posix_write(fd, bytes.data(), u64(bytes.len())) }
}

let main = () -> u8 => {
  if !init(INIT_VIDEO) {
    println("SDL could not start")
    return 2
  }
  // Whatever SDL queued as it started, out of the way.
  while let Some(_) = wait_event(50) {}
  var ends: []i32 = [0, 0]
  if unsafe { posix_pipe(ends.data_mut()) } != 0 { return 3 }
  let kind = wake.watch([ends[0]])
  println("watching: ${kind != 0}")
  send(ends[1], "x")
  println("armed, a write wakes the wait: ${woke(wait_event(2000), kind)}")
  send(ends[1], "y")
  println("disarmed, another write does not: ${!woke(wait_event(300), kind)}")
  let _ = wake.watch([ends[0]])
  println("armed again with data unread, it wakes at once: ${woke(wait_event(2000), kind)}")
  let _ = wake.watch([])
  quit()
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRunUnoptimized(t, "run", src)
	if code == 2 && strings.Contains(stdout, "SDL could not start") {
		t.Skipf("SDL could not start here: %s", stderr)
	}
	want := `watching: true
armed, a write wakes the wait: true
disarmed, another write does not: true
armed again with data unread, it wakes at once: true
`
	if code != 0 || stdout != want {
		t.Errorf("exited %d\ngot\n%s\nwant\n%s\nstderr: %s", code, stdout, want, stderr)
	}
}

// examples/ui/follow.lyra run headless, the wake and all: a command's forty lines read as they
// come, through `Readable` and bindings.wake's watcher waking SDL's wait, until the command
// ends and the program with it.
func TestRun_UiFollowExample(t *testing.T) {
	t.Parallel()
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "follow")
	if _, stderr, code := captureRunUnoptimized(t, "build", "-o", bin, filepath.Join(root, "examples", "ui", "follow.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := commandContext(t, ctx, bin, "--headless").CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("the command's output never ended the program (a wake missed?):\n%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if ok && exitErr.ExitCode() == 1 && strings.HasPrefix(string(out), "no font") {
		t.Skipf("no font here: %s", out)
	}
	if err != nil || !strings.Contains(string(out), `read 40 lines, the last "line 40 of 40"`) {
		t.Errorf("following the command: %v\n%s", err, out)
	}
}
