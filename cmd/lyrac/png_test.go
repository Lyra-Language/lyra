package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// std.png reads what Go's own encoder writes — paletted at 1, 2, 4 and 8 bits (with
// transparency), RGB, RGBA, 16-bit RGBA, greyscale and 16-bit greyscale, Go choosing each
// row's filter, so all five are met — and what a hand-built file has that Go never writes:
// 2-bit greyscale with a transparent grey, greyscale with alpha, and 16-bit RGB with a
// transparent colour. Each image's RGBA (and an indexed one's indices) must be Go's own
// decoding of the same bytes. An interlaced image, a chunk with a bad CRC and a file that
// is not a PNG are refused, each saying why.
func TestRun_PngReadsEveryColourType(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()

	const w, h = 7, 5
	var files []string
	var want []string
	add := func(name string, data []byte, indexed bool) {
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: Go cannot read it: %v", name, err)
		}
		want = append(want, describe(img, indexed))
	}
	refuse := func(name string, data []byte, why string) {
		path := filepath.Join(dir, name+".png")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
		want = append(want, "error: "+why)
	}
	encode := func(img image.Image) []byte {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	// A pattern busy enough that Go picks different filters for different rows.
	at := func(x, y int) (uint8, uint8, uint8, uint8) {
		return uint8(x*37 + y*11), uint8(255 - x*23 - y*7), uint8(x * y * 19), uint8(80 + x*20 + y*5)
	}
	for _, colors := range []int{2, 4, 16, 200} {
		pal := color.Palette{}
		for i := range colors {
			a := uint8(255)
			if i == 1 {
				a = 0 // a transparent entry: tRNS
			}
			pal = append(pal, color.NRGBA{uint8(i * 13), uint8(255 - i), uint8(i * 7 % 256), a})
		}
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := range h {
			for x := range w {
				img.SetColorIndex(x, y, uint8((x*3+y*5)%colors))
			}
		}
		add(fmt.Sprintf("paletted%d", colors), encode(img), true)
	}
	rgb := image.NewRGBA(image.Rect(0, 0, w, h))
	rgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	rgba64 := image.NewNRGBA64(image.Rect(0, 0, w, h))
	gray := image.NewGray(image.Rect(0, 0, w, h))
	gray16 := image.NewGray16(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			r, g, b, a := at(x, y)
			rgb.Set(x, y, color.RGBA{r, g, b, 255})
			rgba.Set(x, y, color.NRGBA{r, g, b, a})
			rgba64.Set(x, y, color.NRGBA64{uint16(r)<<8 | uint16(g), uint16(g)<<8 | 3, uint16(b) << 8, uint16(a)<<8 | 0xFF})
			gray.Set(x, y, color.Gray{r})
			gray16.Set(x, y, color.Gray16{uint16(g)<<8 | uint16(r)})
		}
	}
	add("rgb", encode(rgb), false)
	add("rgba", encode(rgba), false)
	add("rgba64", encode(rgba64), false)
	add("gray", encode(gray), false)
	add("gray16", encode(gray16), false)

	// What Go never writes, built by hand: rows of samples, filter 0.
	gray2 := handPNG(w, h, 2, 0, 0, func(x, y int) []int { return []int{(x + y) % 4} }, []byte{0, 1})
	add("gray2-trns", gray2, false)
	grayAlpha := handPNG(w, h, 8, 4, 0, func(x, y int) []int { return []int{x * 30, y * 50} }, nil)
	add("gray-alpha", grayAlpha, false)
	rgb16 := handPNG(w, h, 16, 2, 0, func(x, y int) []int {
		return []int{x * 9000, y * 12000, (x + y) * 3000}
	}, []byte{0, 0, 0, 0, 0, 0})
	add("rgb16-trns", rgb16, false)
	// Every filter, in turn, on a busy RGBA image ten rows tall: two of each.
	filtered := handPNGFiltered(w, 10, 8, 6, 0, func(x, y int) []int {
		r, g, b, a := at(x, y)
		return []int{int(r), int(g), int(b), int(a)}
	}, nil, true)
	add("rgba-every-filter", filtered, false)
	refuse("interlaced", handPNG(w, h, 8, 0, 1, func(x, y int) []int { return []int{x} }, nil),
		"an interlaced image: save it without interlacing")
	bad := encode(gray)
	bad[33+8] ^= 0xFF // a byte of the first chunk after IHDR, its CRC left as it was
	refuse("bad-crc", bad, "fails its CRC")
	refuse("not-png", []byte("GIF89a, not a PNG at all"), "not a PNG: its signature is wrong")

	var calls []string
	for _, f := range files {
		calls = append(calls, fmt.Sprintf("  show(%q)", f))
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.io.{ read_bytes }
import std.png.{ read_png }

let main = () -> void => {
`+strings.Join(calls, "\n")+`
}

let show = (path: string) -> void => {
  match read_png(read_bytes(path).unwrap_or([])) {
    Err(why) => println("error: ${why}"),
    Ok(png) => {
      var line = "${png.width}x${png.height}"
      for p in 0..<png.width * png.height {
        let o = p * 4
        line = if png.rgba[o + 3] == 0 {
          "${line} * * * 0"
        } else {
          "${line} ${png.rgba[o]} ${png.rgba[o + 1]} ${png.rgba[o + 2]} ${png.rgba[o + 3]}"
        }
      }
      if png.indices.len() > 0 {
        line = "${line} |"
        for i in png.indices { line = "${line} ${i}" }
      }
      println(line)
    },
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	got := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(want), stdout)
	}
	for i := range want {
		name := filepath.Base(files[i])
		if why, isError := strings.CutPrefix(want[i], "error: "); isError {
			if !strings.HasPrefix(got[i], "error: ") || !strings.Contains(got[i], why) {
				t.Errorf("%s: got %q, want an error saying %q", name, got[i], why)
			}
			continue
		}
		if got[i] != want[i] {
			t.Errorf("%s:\n got %s\nwant %s", name, got[i], want[i])
		}
	}
}

// describe is an image as the Lyra program prints it: its size, each pixel's
// non-premultiplied RGBA to 8 bits (a 16-bit sample's high byte), and a paletted image's
// indices.
func describe(img image.Image, indexed bool) string {
	b := img.Bounds()
	parts := []string{fmt.Sprintf("%dx%d", b.Dx(), b.Dy())}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBA64Model.Convert(img.At(x, y)).(color.NRGBA64)
			if c.A == 0 {
				// Go reports a fully transparent pixel as transparent black; the colour a
				// file stores there is its own, so it is not compared.
				parts = append(parts, "*", "*", "*", "0")
				continue
			}
			parts = append(parts, fmt.Sprint(c.R>>8), fmt.Sprint(c.G>>8), fmt.Sprint(c.B>>8), fmt.Sprint(c.A>>8))
		}
	}
	if p, ok := img.(*image.Paletted); ok && indexed {
		parts = append(parts, "|")
		for _, i := range p.Pix {
			parts = append(parts, fmt.Sprint(i))
		}
	}
	return strings.Join(parts, " ")
}

// handPNG builds a PNG whose rows are samples from `pixel`, every row filtered with 0:
// colour type `ctype` at `depth` bits, `interlace` its IHDR's interlace byte, and `trns`
// a tRNS chunk's body when not nil.
func handPNG(w, h, depth, ctype, interlace int, pixel func(x, y int) []int, trns []byte) []byte {
	return handPNGFiltered(w, h, depth, ctype, interlace, pixel, trns, false)
}

// handPNGFiltered is handPNG with row y filtered with filter y % 5 when `cycle`, so every
// filter — None, Sub, Up, Average, Paeth — is met, whichever an encoder would choose.
func handPNGFiltered(w, h, depth, ctype, interlace int, pixel func(x, y int) []int, trns []byte, cycle bool) []byte {
	var rows [][]byte
	for y := range h {
		var row bytes.Buffer
		var acc, nbits int
		for x := range w {
			for _, s := range pixel(x, y) {
				switch depth {
				case 16:
					row.WriteByte(byte(s >> 8))
					row.WriteByte(byte(s))
				case 8:
					row.WriteByte(byte(s))
				default:
					acc = acc<<depth | s
					nbits += depth
					if nbits == 8 {
						row.WriteByte(byte(acc))
						acc, nbits = 0, 0
					}
				}
			}
		}
		if nbits > 0 {
			row.WriteByte(byte(acc << (8 - nbits)))
		}
		rows = append(rows, row.Bytes())
	}
	channels := map[int]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[ctype]
	back := max(channels*depth/8, 1)
	var raw bytes.Buffer
	for y, row := range rows {
		filter := 0
		if cycle {
			filter = y % 5
		}
		raw.WriteByte(byte(filter))
		for x := range row {
			var a, b, c int
			if x >= back {
				a = int(row[x-back])
			}
			if y > 0 {
				b = int(rows[y-1][x])
				if x >= back {
					c = int(rows[y-1][x-back])
				}
			}
			predicted := 0
			switch filter {
			case 1:
				predicted = a
			case 2:
				predicted = b
			case 3:
				predicted = (a + b) / 2
			case 4:
				p := a + b - c
				pa, pb, pc := abs(p-a), abs(p-b), abs(p-c)
				switch {
				case pa <= pb && pa <= pc:
					predicted = a
				case pb <= pc:
					predicted = b
				default:
					predicted = c
				}
			}
			raw.WriteByte(byte(int(row[x]) - predicted))
		}
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(raw.Bytes())
	zw.Close()
	var out bytes.Buffer
	out.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	chunk := func(kind string, body []byte) {
		binary.Write(&out, binary.BigEndian, uint32(len(body)))
		out.WriteString(kind)
		out.Write(body)
		binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), body...)))
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8], ihdr[9], ihdr[12] = byte(depth), byte(ctype), byte(interlace)
	chunk("IHDR", ihdr)
	if trns != nil {
		chunk("tRNS", trns)
	}
	chunk("IDAT", z.Bytes())
	chunk("IEND", nil)
	return out.Bytes()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
