package main

import (
	"errors"
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

// **A package's directories come first, once each** (09/26): `@link(…, pkg: "sdl3")` has
// lyrac ask pkg-config where the library is, and pass `-L` ahead of the `-l`s. A package
// pkg-config cannot answer for costs only the directory — the build still links against
// the defaults and `LIBRARY_PATH`. pkg-config is stubbed so the test does not depend on
// what this machine has installed.
func TestLinkFlags_AddsThePackagesLibraryDirectories(t *testing.T) {
	saved := pkgConfigLibDirs
	defer func() { pkgConfigLibDirs = saved }()
	pkgConfigLibDirs = func(pkg string) ([]string, error) {
		switch pkg {
		case "sdl3":
			return []string{"/opt/brew/lib"}, nil
		case "sdl3-image":
			return []string{"/opt/brew/Cellar/sdl3_image/lib", "/opt/brew/lib"}, nil
		}
		return nil, errors.New("unknown package")
	}
	res := &driver.Result{
		Links:    []string{"SDL3", "SDL3_image"},
		Packages: []string{"missing", "sdl3", "sdl3-image"},
	}
	want := "-L/opt/brew/lib -L/opt/brew/Cellar/sdl3_image/lib -lm -lSDL3 -lSDL3_image"
	if got := strings.Join(linkFlags(res), " "); got != want {
		t.Errorf("linkFlags = %q; want %q", got, want)
	}
}

// **The install's `lib/` is searched first**, so an archive `build.sh` made for a binding's
// C half (`liblyra-menubar.a`) links with no `LIBRARY_PATH`, and a package pkg-config
// reports at the same directory does not repeat it.
func TestLinkFlags_SearchesTheInstallsLibFirst(t *testing.T) {
	savedLib, savedPkg := installLibDir, pkgConfigLibDirs
	defer func() { installLibDir, pkgConfigLibDirs = savedLib, savedPkg }()
	installLibDir = func() string { return "/opt/lyra/lib" }
	pkgConfigLibDirs = func(pkg string) ([]string, error) {
		return []string{"/opt/brew/lib", "/opt/lyra/lib"}, nil
	}
	res := &driver.Result{Links: []string{"lyra-menubar", "SDL3"}, Packages: []string{"sdl3"}}
	want := "-L/opt/lyra/lib -L/opt/brew/lib -lm -llyra-menubar -lSDL3"
	if got := strings.Join(linkFlags(res), " "); got != want {
		t.Errorf("linkFlags = %q; want %q", got, want)
	}
}
