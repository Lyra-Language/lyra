package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// The link line: `-lm` unconditionally, plus one `-l` per library an `extern`'s `@link`
// named. A requirement rides the declaration that has it, so a module needing zlib says so
// once and every program reaching it links zlib — which a CLI flag could not do, since a
// module's requirement would not compose into its callers' build.
func TestLinkFlags_AddsOneFlagPerLinkedLibrary(t *testing.T) {
	res := &driver.Result{Links: []string{"curl", "z"}}
	if got := strings.Join(linkFlags(res), " "); got != "-lm -lcurl -lz" {
		t.Errorf("linkFlags = %q; want \"-lm -lcurl -lz\"", got)
	}
}

// `@link("m")` does not print `-lm` twice: libm is already passed for the float
// intrinsics, and a program that also names it explicitly is asking for what it has.
func TestLinkFlags_DoesNotRepeatLibm(t *testing.T) {
	res := &driver.Result{Links: []string{"m"}}
	if got := strings.Join(linkFlags(res), " "); got != "-lm" {
		t.Errorf("linkFlags = %q; want \"-lm\"", got)
	}
}

func TestLinkFlags_NothingAskedIsJustLibm(t *testing.T) {
	if got := strings.Join(linkFlags(&driver.Result{}), " "); got != "-lm" {
		t.Errorf("linkFlags = %q; want \"-lm\"", got)
	}
}

// **The "compile with" hint prints the same flags the build would use.** A hint naming
// fewer libraries than the build it stands in for is worse than no hint: it is a command
// that fails at link time on a program that compiles, and the user has no way to know
// which library the message left out.
func TestBuild_EmitLLVMHintCarriesTheLinkedLibraries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "linked.lyra")
	src := `module main
@link("z")
unsafe extern pure crc32: (crc: u64, buf: ^u8, len: u32) -> u64
let main = () -> u8 => {
  var bytes: []u8 = [104, 105]
  unsafe { u8(crc32(0, &bytes[0], 2) %% 251) }
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "build", "--emit-llvm", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "-lm -lz -o") {
		t.Errorf("the compile hint should name every linked library:\n%s", stdout)
	}
}
