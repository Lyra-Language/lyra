package llvm

import (
	"fmt"
	"hash/crc32"
	"sort"
	"strings"
	"testing"
)

// `std.sevenzip` (and the LZMA and LZMA2 decoders in `std.compress` under it) are held to
// archives 7-Zip itself wrote: testdata/sevenzip, made by its make.sh, one archive per
// setting a user is likely to meet — the default (solid LZMA2 behind a compressed
// header), LZMA with odd literal settings, DEFLATE, non-solid, an uncompressed header,
// multithreaded LZMA2 (dictionary resets mid-block) and stored — and one per thing the
// reader refuses. The files in them are made by formulas this test repeats, so each entry
// is checked against what it must extract to, not against another reader's opinion.

// sevenzipNoise is make.sh's `noise`: a linear congruential generator's high bytes.
func sevenzipNoise(n int, seed uint64) []byte {
	x := seed
	out := make([]byte, n)
	for i := range out {
		x = (x*1103515245 + 12345) % (1 << 31)
		out[i] = byte(x >> 16)
	}
	return out
}

// sevenzipFiles is make.sh's `all` set: each file's bytes, by path.
func sevenzipFiles() map[string][]byte {
	var text strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&text, "line %d: the quick brown fox %d jumps\n", i, i*i%977)
	}
	t := []byte(text.String())
	mixed := append(append(append([]byte{}, t[:20000]...), sevenzipNoise(3000, 2)...), t[:20000]...)
	return map[string][]byte{
		"dir/text.txt":      t,
		"dir/noise.bin":     sevenzipNoise(6000, 1),
		"dir/sub/zeros.bin": make([]byte, 100000),
		"mixed.bin":         mixed,
		"empty.txt":         {},
		"héllo wörld.txt":   []byte("héllo"),
		"emoji 🎮.txt":       []byte("game"),
	}
}

// sevenzipProgram lists and extracts every entry of each archive named on stdin: a line
// per entry, its name, size, whether it is a directory and its CRC-32 — or why it could
// not be read.
const sevenzipProgram = `module main
import std.sevenzip.{ open_7z, extract_7z }
import std.compress.{ crc32 }
import std.io.{ read_bytes }
let main = () -> void => {
  for {
    let Some(name) = read_line() else { break }
    println("== ${name}")
    let Some(data) = read_bytes("testdata/sevenzip/${name}.7z") else {
      println("unreadable")
      continue
    }
    match open_7z(data) {
      Ok(archive) => for entry in archive.entries {
        match extract_7z(archive, entry) {
          Ok(bytes) => println("${entry.name}|${bytes.len()}|${entry.directory}|${crc32(bytes)}"),
          Err(why) => println("error|${why}"),
        }
      },
      Err(why) => println("error|${why}"),
    }
  }
}
`

// sevenzipResults runs the program over the archives and returns each one's lines.
func sevenzipResults(t *testing.T, names []string) map[string][]string {
	t.Helper()
	out := buildAndRunWithPrelude(t, sevenzipProgram, strings.Join(names, "\n")+"\n")
	results := map[string][]string{}
	current := ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if name, ok := strings.CutPrefix(line, "== "); ok {
			current = name
			results[current] = []string{}
			continue
		}
		results[current] = append(results[current], line)
	}
	return results
}

func TestExec_SevenZipReadsWhat7ZipWrites(t *testing.T) {
	t.Parallel()
	var want []string
	for _, dir := range []string{"dir", "dir/sub", "emptydir"} {
		want = append(want, fmt.Sprintf("%s|0|true|0", dir))
	}
	for name, data := range sevenzipFiles() {
		want = append(want, fmt.Sprintf("%s|%d|false|%d", name, len(data), crc32.ChecksumIEEE(data)))
	}
	sort.Strings(want)
	archives := []string{"default", "ultra", "lzma", "lzma-lclp", "deflate", "nonsolid", "plain-header", "multithread"}
	results := sevenzipResults(t, append(archives, "copy"))
	for _, name := range archives {
		got := append([]string{}, results[name]...)
		sort.Strings(got)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s.7z:\n%s\nwant:\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	short := []byte(strings.Repeat("a short file\n", 20))
	if got, want := strings.Join(results["copy"], "\n"),
		fmt.Sprintf("short.txt|%d|false|%d", len(short), crc32.ChecksumIEEE(short)); got != want {
		t.Errorf("copy.7z: %q, want %q", got, want)
	}
}

// Each thing the reader does not read is refused with a message naming it — the method,
// the chain, or the password — never extracted wrongly or trapped on.
func TestExec_SevenZipRefusesByName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bzip2":            "short.txt: a 7z block compressed with BZip2, which is not read",
		"ppmd":             "short.txt: a 7z block compressed with PPMd, which is not read",
		"bcj":              "short.txt: a 7z block through a chain of coders (LZMA2, BCJ), which is not read",
		"encrypted":        "short.txt: encrypted (it needs a password), which is not read",
		"encrypted-header": "encrypted (it needs a password), which is not read",
	}
	var names []string
	for name := range cases {
		names = append(names, name)
	}
	results := sevenzipResults(t, names)
	for name, want := range cases {
		if got := strings.Join(results[name], "\n"); got != "error|"+want {
			t.Errorf("%s.7z: %q, want %q", name, got, "error|"+want)
		}
	}
}

// The default archive read whole under AddressSanitizer: the LZMA2 decoder grows its
// output while copying matches out of it, and the header reader slices and re-slices the
// archive — an index a byte past an array's end, or a missing retain on a block handed
// between them, shows here where the output alone might not.
func TestExec_SevenZipASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `module main
import std.sevenzip.{ open_7z, extract_7z }
import std.io.{ read_bytes }
let main = () -> u8 => {
  let Some(data) = read_bytes("testdata/sevenzip/default.7z") else { return 11 }
  let Ok(archive) = open_7z(data) else { return 12 }
  var total: i64 = 0
  for entry in archive.entries {
    let Ok(bytes) = extract_7z(archive, entry) else { return 13 }
    total += bytes.len()
  }
  if total == ` + fmt.Sprint(sevenzipTotal()) + ` { 0 } else { 14 }
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0 (1 is a leak)", got)
	}
}

func sevenzipTotal() int {
	total := 0
	for _, data := range sevenzipFiles() {
		total += len(data)
	}
	return total
}
