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
	skipWithoutText(t)
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "form")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "form.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	shot := filepath.Join(dir, "form.png")
	cmd := exec.Command(bin, "--script",
		"Tab 'Ada' Tab 'ada@example.com' Tab Space Tab Right Right Tab Enter", "--png", shot)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
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
	t.Setenv("UI_TMP", t.TempDir())
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
	skipWithoutText(t)
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	bin := filepath.Join(dir, "components")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(repoRoot(t), "examples", "ui", "components.lyra")); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	cmd := exec.Command(bin, "--script",
		"Tab Tab Tab Enter Tab Tab Tab 'Ada' Tab Enter Tab Right Right Tab Tab Enter Tab Tab Space Tab Tab Enter Shift+Tab Shift+Tab Enter wait:2500",
		"--png", filepath.Join(dir, "end.png"), "--save-to", filepath.Join(dir, "name.txt"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
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
	skipWithoutText(t)
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("SDL_VIDEODRIVER", "dummy")
	t.Setenv("SDL_RENDER_DRIVER", "software")
	t.Setenv("LYRA_STD", repoRoot(t))
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
	if err := exec.Command("pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
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
