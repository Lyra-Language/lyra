package llvm

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// `std.rar` reads RAR 5, held to archives it did not make: libarchive's own RAR 5 test
// archives — ordinary, solid, filtered, versioned, encrypted, linked and deliberately
// malformed — and hand-built ones exercising each filter (testdata/rar5, whose README says
// where each came from and how the results below were checked: against libarchive's
// bsdtar, and libarchive's own test expectations). Every extracted file must match the
// CRC-32 RAR itself recorded, and every refusal must say why, never trap or hang.

// rarProgram lists and extracts every entry of each archive named on stdin.
const rarProgram = `module main
import std.rar.{ open_rar, extract_rar, is_rar }
import std.compress.{ crc32 }
import std.io.{ read_bytes }
let main = () -> void => {
  loop {
    let Some(name) = read_line() else { break }
    println("== ${name}")
    let Some(data) = read_bytes("testdata/rar5/${name}") else {
      println("unreadable")
      continue
    }
    if !is_rar(data) { println("not recognised as RAR") }
    match open_rar(data) {
      Ok(archive) => for entry in archive.entries {
        match extract_rar(archive, entry) {
          Ok(bytes) => println("${entry.name}|${bytes.len()}|${entry.directory}|${entry.link}|${crc32(bytes).to_hex()}"),
          Err(why) => println("error|${why}"),
        }
      },
      Err(why) => println("error|${why}"),
    }
  }
}
`

// rarExpected is what rarProgram prints over every archive in testdata/rar5, by name: a
// line per entry, `name|size|directory|link|crc32`, or `error|why`.
const rarExpected = `== arm.rar
elf-Linux-ARMv7-ls|90808|false|false|886f91eb
== bad_tables.rar
error|bad_tables.txt: a code table with more codes than its lengths allow
== blake2.rar
cebula.txt|814|false|false|7e5ec49e
== block_size_is_too_small.rar
error|a RAR archive cut off inside a header (a partial download?)
== compressed.rar
test.bin|1200|false|false|7cca70cd
== decode_number_out_of_bounds_read.rar
error|a RAR archive cut off inside a file's data (a partial download?)
== distance_overflow.rar
error|a RAR archive cut off inside a header (a partial download?)
== encrypted.rar
a.txt|18|false|false|ee5a6e55
error|b.txt is encrypted (it needs a password), which is not read
c.txt|18|false|false|949a3d35
error|d.txt is encrypted (it needs a password), which is not read
== encrypted_filenames.rar
error|encrypted (it needs a password), which is not read
== extra_field_version.rar
bin/2to3;1|95|false|false|f24181b7
bin/2to3|95|false|false|f24181b7
== filter-arm.rar
arm.bin|2000|false|false|96fe234c
== filter-delta.rar
delta.bin|1500|false|false|bcd3a8d9
== filter-e8-offset.rar
e8-offset.bin|3000|false|false|50b0c5db
== filter-e8.rar
e8.bin|3000|false|false|323bb9c3
== filter-e8e9.rar
e8e9.bin|3000|false|false|ec9cfc6c
== hardlink.rar
file.txt|5|false|false|7d931721
error|hardlink.txt is a link, which is not extracted
== invalid_dict_reference.rar
error|a RAR archive cut off inside a header (a partial download?)
== leftshift1.rar
error|a RAR archive cut off inside a header (a partial download?)
== loop_bug.rar
error|a: 66299 bytes decoded, not the 196608 its header says
== main_block_extra_bytes.rar
helloworld.txt|29|false|false|95a043b4
== multiarchive.part01.rar
error|one volume of a multi-volume archive, which is not read; join the volumes into one archive
== multiple_files.rar
test1.bin|4096|false|false|7e13b2c6
test2.bin|4096|false|false|f166afcb
test3.bin|4096|false|false|9fb123d9
test4.bin|4096|false|false|10c43ed4
== multiple_files_solid.rar
test1.bin|4096|false|false|7e13b2c6
test2.bin|4096|false|false|f166afcb
test3.bin|4096|false|false|9fb123d9
test4.bin|4096|false|false|10c43ed4
== only_crypt_exfld.rar
error|file.txt is encrypted (it needs a password), which is not read
== rar4.rar
error|an archive in RAR's older format (RAR 1.5 to 4.x), which is not read; only RAR 5 is
== readtables_overflow.rar
error|a RAR archive cut off inside a header (a partial download?)
== solid.rar
test.bin|1200|false|false|7cca70cd
test1.bin|4096|false|false|7e13b2c6
test2.bin|4096|false|false|f166afcb
test3.bin|4096|false|false|9fb123d9
test4.bin|4096|false|false|10c43ed4
test5.bin|4096|false|false|b9d155f2
test6.bin|4096|false|false|36a448ff
== stored.rar
helloworld.txt|29|false|false|95a043b4
== stored_manyfiles.rar
make_uue.tcl|405|false|false|49478adc
cebula.txt|814|false|false|7e5ec49e
test.bin|1200|false|false|7cca70cd
== symlink.rar
file.txt|5|false|false|7d931721
error|symlink.txt is a link, which is not extracted
error|dirlink is a link, which is not extracted
dir|0|true|false|0
== truncated_huff.rar
error|a RAR archive cut off inside a header (a partial download?)
== unicode.rar
👋🌎.txt|13|false|false|ebe6c6e6
error|Ⓗⓐⓡⓓ Ⓛⓘⓝⓚ.txt is a link, which is not extracted
error|𝒮𝓎𝓂𝒷𝑜𝓁𝒾𝒸 𝐿𝒾𝓃𝓀.txt is a link, which is not extracted
== unpacked_size_exceeds_declared.rar
error|a RAR archive cut off inside a file's data (a partial download?)
== win32.rar
testdir|0|true|false|0
test.bin|1200|false|false|7cca70cd
test1.bin|4096|false|false|7e13b2c6
test2.bin|4096|false|false|f166afcb
test3.bin|4096|false|false|9fb123d9
test4.bin|4096|false|false|10c43ed4
test5.bin|4096|false|false|b9d155f2
test6.bin|4096|false|false|36a448ff
== window_buf_and_size_desync.rar
error|one volume of a multi-volume archive, which is not read; join the volumes into one archive
`

func TestExec_RarReadsWhatRarWrites(t *testing.T) {
	t.Parallel()
	names, err := filepath.Glob("testdata/rar5/*.rar")
	if err != nil || len(names) == 0 {
		t.Fatalf("no test archives: %v", err)
	}
	for i := range names {
		names[i] = filepath.Base(names[i])
	}
	sort.Strings(names)
	out := buildAndRunWithPrelude(t, rarProgram, strings.Join(names, "\n")+"\n")
	if got, want := strings.TrimSpace(out), strings.TrimSpace(rarExpected); got != want {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
			g, w := "", ""
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Errorf("first difference at line %d:\n got  %q\n want %q", i+1, g, w)
				break
			}
		}
	}
}

// A self-extracting archive is an executable with the archive after it: the reader finds
// the signature past the stub. Made here — a stub of bytes that are not the signature,
// before an ordinary archive — rather than committing a real SFX module, which is RARLAB's.
func TestExec_RarFindsTheArchiveAfterAnSFXStub(t *testing.T) {
	t.Parallel()
	archive, err := os.ReadFile("testdata/rar5/stored.rar")
	if err != nil {
		t.Fatal(err)
	}
	stub := append([]byte("MZ"), make([]byte, 70000)...)
	for i := range stub[2:] {
		stub[2+i] = byte(i*7 + 3)
	}
	path := filepath.Join(t.TempDir(), "sfx.exe")
	if err := os.WriteFile(path, append(stub, archive...), 0o644); err != nil {
		t.Fatal(err)
	}
	out := buildAndRunWithPrelude(t, `module main
import std.rar.{ open_rar, extract_rar }
import std.io.{ read_bytes }
let main = () -> void => {
  let Some(path) = read_line() else { return }
  let Some(data) = read_bytes(path) else { return }
  match open_rar(data) {
    Ok(archive) => for entry in archive.entries {
      match extract_rar(archive, entry) {
        Ok(bytes) => println("${entry.name} ${bytes.len()}"),
        Err(why) => println(why),
      }
    },
    Err(why) => println(why),
  }
}
`, path+"\n")
	if got := strings.TrimSpace(out); got != "helloworld.txt 29" {
		t.Errorf("got %q, want %q", got, "helloworld.txt 29")
	}
}

// A solid archive, the ARM binary and the filter archives read whole under AddressSanitizer
// (and LeakSanitizer on Linux): the decoder grows its window while copying matches out of
// it and filters write into a copy of it — an index a byte past either, or a missing retain
// on the tables a block leaves for the next, shows here where the output alone might not.
func TestExec_RarASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `module main
import std.rar.{ open_rar, extract_rar }
import std.io.{ read_bytes }
let main = () -> u8 => {
  var total: i64 = 0
  for name in ["solid.rar", "arm.rar", "filter-e8e9.rar", "filter-delta.rar", "encrypted.rar"] {
    let Some(data) = read_bytes("testdata/rar5/${name}") else { return 11 }
    let Ok(archive) = open_rar(data) else { return 12 }
    for entry in archive.entries {
      if let Ok(bytes) = extract_rar(archive, entry) { total += bytes.len() }
    }
  }
  if total == 1200 + 6 * 4096 + 90808 + 3000 + 1500 + 36 { 0 } else { 13 }
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0 (1 is a leak)", got)
	}
}
