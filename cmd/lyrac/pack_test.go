package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packInputs are what the LZ is tried on: nothing, one byte, a long run (a count past
// 15 + 255 on the match side), a long stretch of bytes that never repeat (past 15 + 255 on
// the literal side), patterned bytes as tiles and maps are, and two copies of a block far
// apart.
func packInputs() [][]byte {
	run := bytes.Repeat([]byte{7}, 1000)
	unique := make([]byte, 600)
	seed := uint32(2463534242)
	for i := range unique {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		unique[i] = byte(seed)
	}
	pattern := make([]byte, 5000)
	for i := range pattern {
		pattern[i] = byte((i*7 + i/13) & 0xFF)
	}
	far := append(append(append([]byte{}, unique...), pattern[:3000]...), unique...)
	return [][]byte{{}, {42}, run, unique, pattern, far}
}

// lzPackInLyra is std.compress.lz_pack of each input, run on this machine.
func lzPackInLyra(t *testing.T, inputs [][]byte) [][]byte {
	t.Helper()
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	var calls []string
	for i, in := range inputs {
		path := filepath.Join(dir, fmt.Sprintf("in%d", i))
		if err := os.WriteFile(path, in, 0o644); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("  show(%q)", path))
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.io.{ read_bytes }
import std.compress.{ lz_pack, lz_unpack }

let main = () -> void => {
`+strings.Join(calls, "\n")+`
}

let show = (path: string) -> void => {
  let data = read_bytes(path).unwrap_or([])
  let packed = lz_pack(data)
  match lz_unpack(packed, data.len()) {
    Ok(back) => if back != data { println("lz_unpack differs") },
    Err(why) => println("lz_unpack: ${why}"),
  }
  var line = ""
  for b in packed { line = "${line}${hex(b)}" }
  println(line)
}

let hex = pure (b: u8) -> string => {
  let digits = "0123456789abcdef"
  "${digits.slice(i64(b >> 4), i64(b >> 4) + 1)}${digits.slice(i64(b & 15), i64(b & 15) + 1)}"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(inputs), stdout)
	}
	var out [][]byte
	for _, l := range lines {
		packed, err := hex.DecodeString(l)
		if err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		out = append(out, packed)
	}
	return out
}

// lzUnpack is the format as std.compress's documentation states it, written again here
// so the compressor is held to the description rather than to its own reader: a token, its
// literals (15 in the high nibble and more bytes, each 255 one more), then — until `size`
// bytes are out — a big-endian offset and a match of the low nibble plus 4 (likewise
// extended), which may overlap what it copies.
func lzUnpack(packed []byte, size int) ([]byte, error) {
	var out []byte
	at := 0
	more := func(n int) (int, error) {
		for {
			if at >= len(packed) {
				return 0, fmt.Errorf("a count runs past the stream")
			}
			b := int(packed[at])
			at++
			n += b
			if b != 255 {
				return n, nil
			}
		}
	}
	for len(out) < size {
		if at >= len(packed) {
			return nil, fmt.Errorf("ends before %d bytes", size)
		}
		token := int(packed[at])
		at++
		literals := token >> 4
		if literals == 15 {
			var err error
			if literals, err = more(literals); err != nil {
				return nil, err
			}
		}
		if at+literals > len(packed) {
			return nil, fmt.Errorf("literals run past the stream")
		}
		out = append(out, packed[at:at+literals]...)
		at += literals
		if len(out) >= size {
			break
		}
		if at+2 > len(packed) {
			return nil, fmt.Errorf("an offset is cut off")
		}
		distance := int(packed[at])<<8 | int(packed[at+1])
		at += 2
		length := token&15 + 4
		if token&15 == 15 {
			var err error
			if length, err = more(length); err != nil {
				return nil, err
			}
		}
		if distance == 0 || distance > len(out) {
			return nil, fmt.Errorf("a match reaches before the start")
		}
		from := len(out) - distance
		for k := range length {
			out = append(out, out[from+k])
		}
	}
	if len(out) != size || at != len(packed) {
		return nil, fmt.Errorf("%d bytes from %d of %d, want %d from all", len(out), at, len(packed), size)
	}
	return out, nil
}

// TestRun_LzPackMakesTheFormatItDescribes: std.compress.lz_pack's output, read by the
// format's description (lzUnpack above) and by lz_unpack, is the input again, every byte of
// the stream used; and the patterned inputs shrink.
func TestRun_LzPackMakesTheFormatItDescribes(t *testing.T) {
	t.Parallel()
	inputs := packInputs()
	packed := lzPackInLyra(t, inputs)
	for i, in := range inputs {
		back, err := lzUnpack(packed[i], len(in))
		if err != nil {
			t.Errorf("input %d (%d bytes): %v", i, len(in), err)
			continue
		}
		if !bytes.Equal(back, in) {
			t.Errorf("input %d (%d bytes) came back different", i, len(in))
		}
	}
	if len(packed[2]) > 10 || len(packed[4]) > len(inputs[4])/5 {
		t.Errorf("a run packed to %d bytes, the pattern %d of %d", len(packed[2]), len(packed[4]), len(inputs[4]))
	}
}

// byteTable is a Lyra `const` table of bytes.
func byteTable(name string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "const %s: [%d]u8 = #[", name, max(len(data), 1))
	if len(data) == 0 {
		b.WriteString("0")
	}
	for i, v := range data {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", v)
	}
	b.WriteString("]\n")
	return b.String()
}

// TestGenesis_PackedDataUnpacksOnThe68000: each input lz_pack made, unpacked by
// std.genesis.pack into RAM, is the input byte for byte — and what follows the area it was
// told to fill is untouched — and a tile packed and loaded by `pack.load_tiles` is what the
// screen shows.
func TestGenesis_PackedDataUnpacksOnThe68000(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	inputs := packInputs()[1:]
	packed := lzPackInLyra(t, inputs)
	// A tile all colour 2, and the backdrop's colour 2 green.
	tile := bytes.Repeat([]byte{0x22}, 32)
	tilePacked := lzPackInLyra(t, [][]byte{tile})[0]
	var tables, checks strings.Builder
	for i, in := range inputs {
		tables.WriteString(byteTable(fmt.Sprintf("RAW_%d", i), in))
		tables.WriteString(byteTable(fmt.Sprintf("PACKED_%d", i), packed[i]))
		fmt.Fprintf(&checks, "  if !same(PACKED_%d, RAW_%d, %d) { right = false }\n", i, i, len(in))
	}
	tables.WriteString(byteTable("TILE_PACKED", tilePacked))
	out := genesisProgram(t, "unpack", `import std.genesis.vdp
import std.genesis.pack

var area: [5200]u8 = #[0; 5200]

let main = () -> void => {
  vdp.init()
  var right = true
`+checks.String()+`
  unsafe { pack.load_tiles(1, TILE_PACKED, 32, &mut area[0]) }
  vdp.set_color(0, 2, vdp.rgb(0, 7, 0))
  for y: u16 in 0..<28 {
    for x: u16 in 0..<40 { vdp.write_cell(vdp.PLANE_A, x, y, 1) }
  }
  // Green tiles over a green backdrop when right, a blue backdrop when wrong; the
  // runtime's panic screen is red.
  vdp.set_color(0, 0, if right { vdp.rgb(0, 7, 0) } else { vdp.rgb(0, 0, 7) })
  vdp.display_on()
  loop {}
}

/// The bytes unpacked from packed are raw's, and the byte after them is still the marker.
let same<const N: i64, const M: i64> = (packed: ref [N]u8, raw: ref [M]u8, size: i64) -> bool => {
  area[size] = 0xA5
  unsafe { pack.unpack(packed, &mut area[0], u32(size)) }
  for k in 0..<size {
    if area[k] != raw[k] { return false }
  }
  area[size] == 0xA5
}

`+tables.String())
	if got := runROM(t, out, "30", "").dominant(); got != "00ff00" {
		t.Errorf("the screen is %s, want 00ff00 (blue: unpacked bytes differ; black: the tile did not load)", got)
	}
}
