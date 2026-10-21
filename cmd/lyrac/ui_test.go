package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
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
	t.Setenv("LYRA_STD", repoRoot(t))
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
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "ui")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "frame.png")
	cmd := exec.Command(bin, append([]string{"--frames", "1", "--png", shot}, extra...)...)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
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
	if err := exec.Command("pkg-config", "--exists", "harfbuzz", "freetype2").Run(); err != nil {
		t.Skip("no HarfBuzz or FreeType")
	}
}

// runUiProgram runs a `ui` program written here and answers what it printed.
func runUiProgram(t *testing.T, program string) string {
	t.Helper()
	skipWithoutText(t)
	t.Setenv("LYRA_STD", repoRoot(t))
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
	got := runUiProgram(t, `import ui.draw.{ Size }
import ui.text.{ fonts_new }
import ui.style.{ Style, insets }
import ui.layout.{
  Node, Laid, Edges, Constraints, CENTER, Vertical,
  MainStart, MainCenter, MainEnd, SpaceBetween, SpaceAround, SpaceEvenly,
  CrossStart, CrossCenter, CrossEnd, CrossStretch,
  row, column, expanded, stack, positioned, padding, align, sized, scroll,
  layout, tight, loose,
}

let main = () -> u8 => {
  var fonts = fonts_new()
  let box = (w: f32, h: f32) -> Node => sized(Some(w), Some(h))
  let screen = loose(Size { w: 300.0, h: 200.0 })
  // A row: two boxes 10 apart, the middle one growing into the rest.
  show("row", layout(row([box(40.0, 20.0), expanded(box(0.0, 10.0)), box(30.0, 30.0)]).with_style(Style { gap: Some(10.0) }), screen, fonts))
  // Flex factors share the free space 1:3.
  show("flex", layout(row([expanded(box(0.0, 10.0)), expanded(box(0.0, 10.0), 3.0)]), tight(Size { w: 200.0, h: 50.0 }), fonts))
  for along in [MainStart, MainCenter, MainEnd, SpaceBetween, SpaceAround, SpaceEvenly] {
    show("main", layout(column([box(10.0, 20.0), box(10.0, 20.0)]).aligned(along, CrossStart), screen, fonts))
  }
  for cross in [CrossStart, CrossCenter, CrossEnd, CrossStretch] {
    show("cross", layout(column([box(10.0, 20.0), sized(None, Some(20.0))]).aligned(MainStart, cross).with_style(Style { padding: Some(insets(5.0)) }), tight(Size { w: 100.0, h: 100.0 }), fonts))
  }
  // A row in a column: unbounded nowhere, so it fills the width.
  show("nest", layout(column([row([box(10.0, 10.0)])]), screen, fonts))
  // Stack: the largest unpositioned child sizes it; positioned ones by their edges.
  show("stack", layout(stack([
    box(100.0, 60.0),
    positioned(Edges { left: Some(10.0), right: Some(20.0), top: Some(5.0), height: Some(8.0) }, sized(None, None)),
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
let show = (name: string, laid: Laid) -> void => println("${name} ${shape(laid)}")

let shape = pure (laid: Laid) -> string => {
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
	got := runUiProgram(t, `import ui.draw.{ Size, Color }
import ui.text.{ fonts_new }
import ui.style.{ Style, Sheet, Rule, Theme, light_theme, dark_theme, specificity }
import ui.layout.{ Laid, column, text, themed, component, layout, loose }

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
  let tree = themed(theme, globals, [
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
        column([]).with_class("child"),
        column([column([]).with_class("grandchild").with_style(Style { opacity: Some(0.5) })])
          .with_style(Style { font_size: Some(30.0) }),
      ]).with_class("parent").with_style(Style {
        color: Some(gray(20)),
        opacity: Some(0.5),
        background: Some(gray(21)),
      }),
      // A theme's subtree takes its text colour again.
      themed(dark_theme(), Sheet {}, [column([]).with_class("dark")]),
    ]),
  ])
  report(layout(tree, loose(Size { w: 100.0, h: 100.0 }), fonts))
  0
}

/// A rule setting the colour gray n, so which rule won can be read back.
let rule = pure (selector: string, n: u8) -> Rule => Rule { selector: selector, style: Style { color: Some(gray(n)) } }

let gray = pure (n: u8) -> Color => Color { r: n, g: n, b: n }

/// Each element with a class: its colour's gray, font size, opacity and whether it has a
/// background.
let report = (laid: Laid) -> void => {
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
	skipWithoutText(t)
	cc := os.Getenv("LYRA_CC")
	if cc == "" {
		cc = "clang"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no %s", cc)
	}
	flags, err := exec.Command("pkg-config", "--cflags", "freetype2").Output()
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
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compiling: %v\n%s", err, out)
	}
	measured, err := exec.Command(bin).Output()
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
