package llvm

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `std.compress` and `std.zip` are held to Go's own compress/flate and archive/zip — an
// independent implementation of both formats. Go writes the streams and archives, the
// Lyra program reads them back, and the bytes must be Go's exactly. The directory the
// files are in comes on stdin, so the program's IR — and its cached binary — is the same
// on every run.

// inflateCorpus is data of the shapes a compressor treats differently: nothing, one byte,
// text, incompressible bytes, long runs (distances of 1, overlapping copies), matches
// reaching back the full 32 KB window, and enough of it for many blocks.
func inflateCorpus() map[string][]byte {
	r := rand.New(rand.NewSource(1))
	random := make([]byte, 70000)
	r.Read(random)
	var text bytes.Buffer
	for i := 0; text.Len() < 200000; i++ {
		fmt.Fprintf(&text, "line %d: the quick brown fox %d jumps over the lazy dog\n", i, i*i%977)
	}
	far := make([]byte, 0, 100000)
	chunk := random[:32000]
	far = append(far, chunk...)
	far = append(far, random[40000:40300]...)
	far = append(far, chunk...)
	return map[string][]byte{
		"empty":  {},
		"one":    {'x'},
		"text":   text.Bytes(),
		"random": random,
		"runs":   bytes.Repeat([]byte("a"), 100000),
		"pairs":  bytes.Repeat([]byte("ab"), 40000),
		"far":    far,
	}
}

func TestExec_InflateMatchesGoFlate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	levels := map[string]int{
		"stored": flate.NoCompression, "speed": flate.BestSpeed, "default": flate.DefaultCompression,
		"best": flate.BestCompression, "huffman": flate.HuffmanOnly,
	}
	var input strings.Builder
	input.WriteString(dir + "\n")
	blockTypes := map[int]bool{}
	for name, data := range inflateCorpus() {
		for level, n := range levels {
			var z bytes.Buffer
			w, err := flate.NewWriter(&z, n)
			if err != nil {
				t.Fatal(err)
			}
			w.Write(data)
			w.Close()
			stream := z.Bytes()
			blockTypes[int(stream[0]>>1)&3] = true
			file := name + "-" + level
			os.WriteFile(filepath.Join(dir, file+".z"), stream, 0o644)
			os.WriteFile(filepath.Join(dir, file+".raw"), data, 0o644)
			fmt.Fprintf(&input, "%s %d\n", file, crc32.ChecksumIEEE(data))
		}
	}
	// The corpus must reach all three block types, or a pass here says nothing about one.
	for _, kind := range []int{0, 1, 2} {
		if !blockTypes[kind] {
			t.Fatalf("no stream in the corpus begins with a block of type %d", kind)
		}
	}
	out := buildAndRunWithPrelude(t, `module main
import std.compress.{ inflate, crc32 }
import std.io.{ read_bytes }
let main = () -> void => {
  let Some(dir) = read_line() else { return }
  for {
    let Some(line) = read_line() else { break }
    let parts = line.split(" ")
    let name = parts[0]
    let Some(stream) = read_bytes("${dir}/${name}.z") else {
      println("${name}: unreadable")
      continue
    }
    let Some(want) = read_bytes("${dir}/${name}.raw") else {
      println("${name}: unreadable")
      continue
    }
    match inflate(stream) {
      Ok(got) => {
        if got != want {
          println("${name}: ${got.len()} bytes, differing from the ${want.len()} wanted")
        } else if "${crc32(got)}" != parts[1] {
          println("${name}: crc32 ${crc32(got)}, want ${parts[1]}")
        } else {
          println("${name}: ok")
        }
      },
      Err(why) => println("${name}: ${why}"),
    }
  }
}
`, input.String())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := len(inflateCorpus()) * len(levels)
	if len(lines) != want {
		t.Fatalf("%d results for %d streams:\n%s", len(lines), want, out)
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, ": ok") {
			t.Errorf("%s", line)
		}
	}
}

// bitWriter packs a DEFLATE stream by hand, for the malformed ones no compressor writes:
// values lowest bit first, Huffman codes highest bit first.
type bitWriter struct {
	out  []byte
	bits int
}

func (w *bitWriter) value(v, n int) {
	for i := range n {
		if w.bits%8 == 0 {
			w.out = append(w.out, 0)
		}
		w.out[len(w.out)-1] |= byte((v>>i)&1) << (w.bits % 8)
		w.bits++
	}
}

func (w *bitWriter) code(c, n int) {
	for i := n - 1; i >= 0; i-- {
		w.value((c>>i)&1, 1)
	}
}

func lyraBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// Each malformed stream is refused, with the reason that applies — never a trap, and never
// bytes.
func TestExec_InflateRefusesMalformed(t *testing.T) {
	t.Parallel()
	// A fixed block copying before the start: length 3 (code 257, 0000001), distance 1.
	var early bitWriter
	early.value(1, 1)
	early.value(1, 2)
	early.code(1, 7)
	early.code(0, 5)
	early.code(0, 7)
	// A fixed block with literal/length code 286, which the fixed code has but DEFLATE
	// never uses (11000110).
	var high bitWriter
	high.value(1, 1)
	high.value(1, 2)
	high.code(0xC6, 8)
	// A dynamic block whose code-length code over-fills its space: four codes of length 1.
	var over bitWriter
	over.value(1, 1)
	over.value(2, 2)
	over.value(0, 5)
	over.value(0, 5)
	over.value(0, 4)
	for range 4 {
		over.value(1, 3)
	}
	var good bytes.Buffer
	w, _ := flate.NewWriter(&good, flate.BestCompression)
	w.Write(bytes.Repeat([]byte("truncate me "), 500))
	w.Close()
	cases := []struct{ name, stream, want string }{
		{"empty", "[]", "the input ends before its final block"},
		{"type 3", "[0x07]", "a block of type 3"},
		{"stored, bad complement", "[0x01, 0x05, 0x00, 0x00, 0x00]", "does not match its complement"},
		{"stored, cut short", "[0x01, 0x05, 0x00, 0xfa, 0xff, 0x61]", "runs past the input"},
		{"distance before the start", lyraBytes(early.out), "reaching back before the start"},
		{"length code 286", lyraBytes(high.out), "a length code out of range"},
		{"over-full code", lyraBytes(over.out), "more codes than its lengths allow"},
		{"cut in half", lyraBytes(good.Bytes()[:good.Len()/2]), "the input ends inside a block"},
	}
	var src strings.Builder
	src.WriteString("module main\nimport std.compress.{ inflate }\nlet main = () -> void => {\n")
	for _, c := range cases {
		fmt.Fprintf(&src, "  match inflate(%s) {\n    Ok(b) => println(\"ok ${b.len()}\"),\n    Err(why) => println(why),\n  }\n", c.stream)
	}
	src.WriteString("}\n")
	lines := strings.Split(strings.TrimSpace(buildAndRunWithPrelude(t, src.String(), "")), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d results for %d cases:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}
	for i, c := range cases {
		if !strings.Contains(lines[i], c.want) {
			t.Errorf("%s: %q, want it to say %q", c.name, lines[i], c.want)
		}
	}
}

// An archive of every kind of entry Go's writer makes — stored, deflated (with data
// descriptors, as Go always writes them), empty, a directory, a UTF-8 name, a method
// std.zip does not read, a comment after it all — listed and read back; and a damaged copy.
func TestExec_ZipMatchesGoArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	payload := []byte(strings.Repeat("sheliak reads roms from zips; ", 3000))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, method uint16, data []byte) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	add("dir/", zip.Store, nil)
	add("dir/stored.bin", zip.Store, payload[:5000])
	add("deflated.txt", zip.Deflate, payload)
	add("empty.txt", zip.Deflate, nil)
	add("héllo.txt", zip.Deflate, []byte("héllo"))
	raw, err := zw.CreateRaw(&zip.FileHeader{Name: "other.bz2", Method: 12, CompressedSize64: 3, UncompressedSize64: 3})
	if err != nil {
		t.Fatal(err)
	}
	raw.Write([]byte("abc"))
	zw.SetComment("a comment at the end")
	zw.Close()
	archive := buf.Bytes()
	os.WriteFile(filepath.Join(dir, "good.zip"), archive, 0o644)
	// Damage the stored entry's data: its CRC-32 no longer matches.
	reader, _ := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	offset, _ := reader.File[1].DataOffset()
	damaged := append([]byte(nil), archive...)
	damaged[offset+100] ^= 0xFF
	os.WriteFile(filepath.Join(dir, "damaged.zip"), damaged, 0o644)
	os.WriteFile(filepath.Join(dir, "plain.bin"), payload[:1000], 0o644)

	out := buildAndRunWithPrelude(t, `module main
import std.zip.{ zip_entries, unzip, is_zip }
import std.compress.{ crc32 }
import std.io.{ read_bytes }
let main = () -> void => {
  let Some(dir) = read_line() else { return }
  for file in ["good.zip", "damaged.zip", "plain.bin"] {
    let Some(archive) = read_bytes("${dir}/${file}") else { continue }
    println("${file} zip=${is_zip(archive)}")
    match zip_entries(archive) {
      Ok(entries) => for e in entries {
        match unzip(archive, e) {
          Ok(b) => println("  ${e.name} method=${e.method} ${b.len()} crc=${crc32(b)} dir=${e.is_directory()}"),
          Err(why) => println("  ${why}"),
        }
      },
      Err(why) => println("  ${why}"),
    }
  }
}
`, dir+"\n")
	entry := func(name string, method int, data []byte, isDir bool) string {
		return fmt.Sprintf("  %s method=%d %d crc=%d dir=%t", name, method, len(data), crc32.ChecksumIEEE(data), isDir)
	}
	good := []string{
		entry("dir/", 0, nil, true),
		entry("dir/stored.bin", 0, payload[:5000], false),
		entry("deflated.txt", 8, payload, false),
		entry("empty.txt", 8, nil, false),
		entry("héllo.txt", 8, []byte("héllo"), false),
		"  other.bz2 is compressed by method 12; only stored and DEFLATE are read",
	}
	damagedWant := append([]string{}, good...)
	damagedWant[1] = "  dir/stored.bin is damaged: its CRC-32 does not match"
	want := strings.Join(append(append(append([]string{"good.zip zip=true"}, good...),
		"damaged.zip zip=true"), append(damagedWant,
		"plain.bin zip=false", "  not a zip archive: no end-of-central-directory record")...), "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("std.zip results:\n%s\nwant:\n%s", got, want)
	}
}

// A zip of a deflated entry, read under AddressSanitizer: inflate builds its tables and
// output in arrays it grows, and unzip slices the archive — an index past an array's end
// or a missing retain on a returned table would show here, where the output alone might
// not.
func TestExec_ZipInflateASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	var text bytes.Buffer
	for i := range 400 {
		fmt.Fprintf(&text, "entry %d of a text long enough for dynamic blocks and far matches\n", i%37)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("t.txt")
	w.Write(text.Bytes())
	zw.Close()
	src := fmt.Sprintf(`module main
import std.zip.{ zip_entries, unzip }
import std.compress.{ crc32 }
let main = () -> u8 => {
  let archive: []u8 = %s
  let Ok(entries) = zip_entries(archive) else { return 1 }
  let Ok(bytes) = unzip(archive, entries[0]) else { return 2 }
  if bytes.len() != %d || crc32(bytes) != %d { return 3 }
  0
}
`, lyraBytes(buf.Bytes()), text.Len(), crc32.ChecksumIEEE(text.Bytes()))
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}
