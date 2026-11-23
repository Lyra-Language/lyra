package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// std.unicode breaks text into grapheme clusters as UAX #29 says: every case of the Unicode
// version's GraphemeBreakTest.txt (testdata/unicode, Unicode's own file, unchanged) — CR LF,
// controls, Hangul jamo, combining and spacing marks, prepended marks, Indic conjuncts,
// emoji modifier and ZWJ sequences and flags — gives exactly its boundaries. And the
// functions a caret moves by answer from them: before and after an index, inside a cluster
// and at its edges, and the nearest boundary to one inside.
func TestRun_UnicodeBreaksGraphemesAsUAX29Says(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	cases := filepath.Join(root, "cmd", "lyrac", "testdata", "unicode", "GraphemeBreakTest.txt")
	src := filepath.Join(t.TempDir(), "main.lyra")
	program := fmt.Sprintf(`import std.io.{ read_file }
import std.unicode.{ UNICODE_VERSION }

let main = () -> u8 => {
  let text = read_file(%q) ?? ""
  var checked = 0
  var failed = 0
  var line_no = 0
  for whole in text.lines() {
    line_no += 1
    let line = (whole.split("#")[0]).trim()
    if line.len() == 0 { continue }
    var runes: []rune = []
    var want: []i64 = []
    for token in line.split(" ") {
      if token == "÷" { want.push(runes.len()) } else if token == "×" { } else if token.len() > 0 {
        runes.push(rune(hex(token)))
      }
    }
    let got = runes.grapheme_boundaries()
    checked += 1
    if got != want {
      failed += 1
      if failed <= 10 { println("line ${line_no}: ${line}: got ${shown(got)}") }
    }
  }
  println("unicode ${UNICODE_VERSION}: checked ${checked}, failed ${failed}")
  let word = "ae\u0301🇫🇷👍🏽b".to_runes()
  println("clusters ${shown(word.grapheme_boundaries())}; parts ${"ae\u0301🇫🇷👍🏽b".graphemes().len()}")
  println("after ${shown([i in 0..<(word.len() + 1) | word.grapheme_after(i)])}")
  println("before ${shown([i in 0..<(word.len() + 1) | word.grapheme_before(i)])}")
  println("nearest ${shown([i in 0..<(word.len() + 1) | word.nearest_grapheme_boundary(i)])}")
  println("empty ${shown("".to_runes().grapheme_boundaries())} ${"".graphemes().len()}")
  0
}

let shown = pure (xs: []i64) -> string => "[${[x in xs | "${x}"].join(", ")}]"

let hex = pure (s: string) -> i64 => {
  var out = 0
  for c in s {
    let d = if c >= '0' && c <= '9' { i64(c) - i64('0') } else { i64(c) - i64('A') + 10 }
    out = out * 16 + d
  }
  out
}
`, cases)
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	// a, e + acute, the French flag, a thumbs up with its skin tone, b: runes 0, 1-2, 3-4, 5-6, 7.
	want := `unicode 17.0.0: checked 766, failed 0
clusters [0, 1, 3, 5, 7, 8]; parts 5
after [1, 3, 3, 5, 5, 7, 7, 8, 8]
before [0, 0, 1, 1, 3, 3, 5, 5, 7]
nearest [0, 1, 1, 3, 3, 5, 5, 7, 8]
empty [0] 0
`
	if stdout != want {
		t.Errorf("got\n%s\nwant\n%s", stdout, want)
	}
	if !strings.Contains(stdout, "checked 766") {
		t.Errorf("the file's cases were not all read")
	}
}

// std/unicode/grapheme_table.lyra is what tools/unicode/gen.lyra writes from the character
// database tools/unicode/unicode.sh downloads, byte for byte — run where the files are
// (build/deps/unicode-17.0.0), skipped where they are not, as on CI, which has no download.
func TestRun_UnicodeTableIsWhatTheGeneratorWrites(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	ucd := filepath.Join(root, "build", "deps", "unicode-17.0.0")
	for _, name := range []string{"GraphemeBreakProperty.txt", "emoji-data.txt", "DerivedCoreProperties.txt"} {
		if _, err := os.Stat(filepath.Join(ucd, name)); err != nil {
			t.Skipf("no %s: run tools/unicode/unicode.sh", name)
		}
	}
	stdout, stderr, code := captureRun(t, "run", filepath.Join(root, "tools", "unicode", "gen.lyra"), "--", ucd)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join(root, "std", "unicode", "grapheme_table.lyra"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want) {
		t.Errorf("std/unicode/grapheme_table.lyra is not what gen.lyra writes: run tools/unicode/unicode.sh")
	}
}
