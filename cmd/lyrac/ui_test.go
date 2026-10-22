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
	stdout, stderr, code := captureRun(t, "run", src)
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
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
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
	stdout, stderr, code := captureRun(t, "run", src)
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
// component's sheet does not reach a component inside it, whose parts its own part rules
// style. Colour, font and opacity are inherited (opacity multiplied), the background is not,
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
  let badge_sheet = Sheet { rules: [rule(".dot", 12)] }
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
          // The card's sheet does not reach inside the badge (the global Text rule does), and
          // the card's part rule for the badge's dot beats the badge's own rule.
          text("").with_class("nested"),
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
nested: 6 14 1 false
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
// the arrow anywhere else. **A checkbox**: its box bordered ticked or not, and when ticked
// holding the accent a gap inside the border, its corners less round by the inset. **Roles and labels**: each control's role, and its
// label its visible text unless it was given one.
func TestRun_UiInteraction(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, run }
import ui.script.{ ScriptPlatform, script_platform, parse_script }
import ui.interact.{ ui_new, find }
import ui.backend.{ Renderer, Arrow, IBeam }
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
/// the arrow over anything else.
let cursors = (fonts: mut Fonts) -> void => {
  let view = (s: State) -> Node<Ev> => column([
    sized(Some(100.0), Some(30.0), [text_input(FieldSpec { value: s.value, on_edit: Typed })]),
    sized(Some(100.0), Some(30.0), [button("B", Pick("b"))]),
  ])
  var shapes: []string = []
  for script in ["move:10,10", "move:10,10 move:10,40", "move:10,40 move:10,10 move:10,100", "move:10,10 down:10,10 move:10,40"] {
    let (_, platform) = play(view, script, fonts)
    shapes.push(if platform.shape == IBeam { "ibeam" } else if platform.shape == Arrow { "arrow" } else { "other" })
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
cursors ibeam,arrow,arrow,ibeam
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
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "form.lyra")); code != 0 {
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
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "components.lyra")); code != 0 {
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
	stdout, stderr, code := captureRun(t, "run", src)
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
// stepped twice and set outright, Send pressed — the events the keyboard would raise — and
// an action for an element no longer there ignored.
func TestRun_UiAccessibilityTree(t *testing.T) {
	t.Parallel()
	got := runUiProgram(t, `import ui.app.{ sandbox, host_of }
import ui.script.{ script_platform, ScriptPlatform }
import ui.backend.{ AxWindow, AxGroup, AxLabel, AxImage, AxButton, AxCheckbox, AxSlider, AxTextInput, AxScroll, AxContainer, Renderer, InputEvent, AccessAction, AccessTree, AccessNode, AxSetValue, AxClick, AxIncrement, AxFocus, AxSetNumber }
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
	stdout, stderr, code := captureRun(t, "run", src)
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
		if _, stderr, code := captureRun(t, "build", path, "-o", bin); code != 0 {
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
	stdout, stderr, code := captureRun(t, "run", src)
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
// selector (.card.hover, 20) beats a single class (.card, 10) though written before it,
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
  Rule { selector: ".card.hover", style: Style { box_shadow: Some([BoxShadow { y: 9.0, blur: 20.0, color: rgba(0x00000040) }]) } },
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
	stdout, stderr, code := captureRun(t, "run", src)
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
