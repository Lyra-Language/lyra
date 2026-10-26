package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// **`bindings.sdl3_image` decodes a file to pixels** (`load_pixels`, `load_svg_pixels`): a PNG
// Go wrote, each pixel `0xAARRGGBB` with straight alpha, its rows in order; and an SVG drawn
// at its own size and at the size asked. Skipped where SDL3_image is not installed.
func TestRun_SdlImagePixels(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3-image").Run(); err != nil {
		t.Skip("no SDL3_image")
	}
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	// 3×2: red, half-clear green, blue; then white, black, clear.
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(1, 0, color.NRGBA{0, 255, 0, 128})
	img.Set(2, 0, color.NRGBA{0, 0, 255, 255})
	img.Set(0, 1, color.NRGBA{255, 255, 255, 255})
	img.Set(1, 1, color.NRGBA{0, 0, 0, 255})
	img.Set(2, 1, color.NRGBA{0, 0, 0, 0})
	pngPath := filepath.Join(dir, "small.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	svgPath := filepath.Join(dir, "shape.svg")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10" viewBox="0 0 20 10"><rect width="20" height="10" fill="#ff0000"/></svg>`
	if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.lyra")
	program := fmt.Sprintf(`import bindings.sdl3_image.{ load_pixels, load_svg_pixels }

let main = () -> u8 => {
  let Some(p) = load_pixels(%q) else { return 2 }
  println("png ${p.width}x${p.height}: ${[c in p.argb | c.to_hex(8)].join(" ")}")
  let Some(s) = load_pixels(%q) else { return 3 }
  println("svg at its own size ${s.width}x${s.height}, its first pixel ${s.argb[0].to_hex(8)}")
  let Some(big) = load_svg_pixels(%q, 80, 40) else { return 4 }
  println("svg asked 80x40: ${big.width}x${big.height}, its last pixel ${big.argb[big.argb.len() - 1].to_hex(8)}")
  0
}
`, pngPath, svgPath, svgPath)
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exited %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	want := "png 3x2: ffff0000 8000ff00 ff0000ff ffffffff ff000000 00000000\n" +
		"svg at its own size 20x10, its first pixel ffff0000\n" +
		"svg asked 80x40: 80x40, its last pixel ffff0000\n"
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
}
