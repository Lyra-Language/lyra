package llvm

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// `bindings/jpeg.lyra` decodes a JPEG with libjpeg-turbo, which raylib's own build usually
// cannot: without `SUPPORT_FILEFORMAT_JPG` every JPEG loads as an invalid image, and a glTF
// whose textures are JPEG draws untextured (41 of the Khronos sample models).
//
// The fixture is a 16x8 image, red on the left half and blue on the right, embedded here so
// the test depends on no asset. JPEG is lossy and its chroma subsampled, so the pixels are
// checked with room: red where red belongs, blue where blue does, and alpha opaque —
// TurboJPEG fills it, JPEG having no alpha of its own.
func TestExec_JpegDecode(t *testing.T) {
	t.Parallel()
	raylibDir := pkgConfigLibDir(t, "raylib")
	jpegDir := pkgConfigLibDir(t, "libturbojpeg")
	fixture, err := base64.StdEncoding.DecodeString(fixtureJPEG)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "half.jpg")
	if err := os.WriteFile(path, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
module main
import bindings.raylib.{ read_file_bytes }
import bindings.jpeg.{ decode_jpeg }
let main = () -> void => {
  match read_file_bytes("JPEG_PATH") {
    Some(raw) => match decode_jpeg(raw) {
      Some(img) => {
        // Row 3, a pixel inside each half; four bytes to a pixel.
        let left = 4 * (3 * i64(img.width) + 2)
        let right = 4 * (3 * i64(img.width) + 13)
        println("${img.width} ${img.height} ${img.pixels.len()}")
        println("${img.pixels[left]} ${img.pixels[left + 1]} ${img.pixels[left + 2]} ${img.pixels[left + 3]}")
        println("${img.pixels[right]} ${img.pixels[right + 1]} ${img.pixels[right + 2]} ${img.pixels[right + 3]}")
      },
      None => println("not decoded"),
    },
    None => println("not read"),
  }
  match decode_jpeg("this is not a jpeg".encode_utf8()) {
    Some(_) => println("decoded rubbish"),
    None => println("rubbish refused"),
  }
}
`
	src = strings.Replace(src, "JPEG_PATH", path, 1)
	bin := compileCached(t, lookClang(t), emitWithPrelude(t, src), "-L"+raylibDir, "-lraylib", "-L"+jpegDir, "-lturbojpeg")
	raw, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the jpeg binary failed: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.HasPrefix(l, "INFO:") && !strings.HasPrefix(l, "WARNING:") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 4 {
		t.Fatalf("unexpected output:\n%s", strings.Join(lines, "\n"))
	}
	if lines[0] != "16 8 512" {
		t.Errorf("header = %q; want \"16 8 512\" (16x8, RGBA)", lines[0])
	}
	red, blue := channels(t, lines[1]), channels(t, lines[2])
	if !(red[0] > 150 && red[1] < 110 && red[2] < 110 && red[3] == 255) {
		t.Errorf("left pixel = %v; want red with opaque alpha", red)
	}
	if !(blue[2] > 150 && blue[0] < 110 && blue[1] < 110 && blue[3] == 255) {
		t.Errorf("right pixel = %v; want blue with opaque alpha", blue)
	}
	if lines[3] != "rubbish refused" {
		t.Errorf("decoding rubbish: %q; want \"rubbish refused\"", lines[3])
	}
}

func channels(t *testing.T, line string) []int {
	t.Helper()
	fields := strings.Fields(line)
	if len(fields) != 4 {
		t.Fatalf("expected four channels, got %q", line)
	}
	out := make([]int, 4)
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			t.Fatalf("channel %q: %v", f, err)
		}
		out[i] = n
	}
	return out
}

// A 16x8 JPEG, red on the left half and blue on the right.
const fixtureJPEG = "" +
	"/9j/4AAQSkZJRgABAQAASABIAAD/4QBMRXhpZgAATU0AKgAAAAgAAYdpAAQAAAABAAAAGgAAAAAAA6ABAAMAAAABAAEAAKACAAQA" +
	"AAABAAAAEKADAAQAAAABAAAACAAAAAD/7QA4UGhvdG9zaG9wIDMuMAA4QklNBAQAAAAAAAA4QklNBCUAAAAAABDUHYzZjwCyBOmA" +
	"CZjs+EJ+/8AAEQgACAAQAwERAAIRAQMRAf/EAB8AAAEFAQEBAQEBAAAAAAAAAAABAgMEBQYHCAkKC//EALUQAAIBAwMCBAMFBQQE" +
	"AAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldY" +
	"WVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk" +
	"5ebn6Onq8fLz9PX29/j5+v/EAB8BAAMBAQEBAQEBAQEAAAAAAAABAgMEBQYHCAkKC//EALURAAIBAgQEAwQHBQQEAAECdwABAgMR" +
	"BAUhMQYSQVEHYXETIjKBCBRCkaGxwQkjM1LwFWJy0QoWJDThJfEXGBkaJicoKSo1Njc4OTpDREVGR0hJSlNUVVZXWFlaY2RlZmdo" +
	"aWpzdHV2d3h5eoKDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uLj5OXm5+jp6vLz" +
	"9PX29/j5+v/bAEMAAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEB" +
	"Af/bAEMBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAf/dAAQA" +
	"Av/aAAwDAQACEQMRAD8A/Lev8/z/AK+D5vr/ALqD/gnP/9k="
