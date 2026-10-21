package main

import (
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command(bin, "--frames", "1", "--png", shot)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 &&
		strings.Contains(string(out), "SDL could not") {
		t.Skipf("SDL could not open a dummy window here: %s", out)
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
	return frame
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
