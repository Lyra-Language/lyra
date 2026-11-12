package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `lyrac m68k-bundle`: the toolchain a release carries, made from the full one, builds the
// same ROM byte for byte — its opt and llc the same programs, its runtime and helpers the
// objects a build compiles — with no clang beside it; and a bundle made from another
// runtime.c is refused rather than linked.
func TestGenesis_BundleBuildsTheSameROM(t *testing.T) {
	genesisToolchainOrSkip(t)
	if _, err := fullM68kToolchain(); err != nil {
		t.Skip(err)
	}
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	bundle := filepath.Join(dir, "m68k")
	if stdout, stderr, code := captureRun(t, "m68k-bundle", bundle); code != 0 {
		t.Fatalf("m68k-bundle exited %d: %s%s", code, stdout, stderr)
	}
	for _, f := range []string{"bin/opt", "bin/llc", "objects/runtime.o", "objects/udivsi3.o", "objects/runtime.sha256", "VERSION"} {
		if _, err := os.Stat(filepath.Join(bundle, f)); err != nil {
			t.Fatalf("the bundle has no %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(bundle, "bin", "clang")); err == nil {
		t.Fatal("the bundle carries clang")
	}
	game := filepath.Join(root, "examples", "genesis", "backdrop.lyra")
	full := filepath.Join(dir, "full.bin")
	if _, stderr, code := captureRun(t, "build", "-o", full, game); code != 0 {
		t.Fatalf("build with the full toolchain: %s", stderr)
	}
	setenv(t, "LYRA_M68K_LLVM", bundle)
	bundled := filepath.Join(dir, "bundled.bin")
	if _, stderr, code := captureRun(t, "build", "-o", bundled, game); code != 0 {
		t.Fatalf("build with the bundle: %s", stderr)
	}
	a, _ := os.ReadFile(full)
	b, _ := os.ReadFile(bundled)
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Fatalf("the bundle's ROM (%d bytes) is not the full toolchain's (%d)", len(b), len(a))
	}
	// Made from another runtime.c: refused.
	if err := os.WriteFile(filepath.Join(bundle, "objects", "runtime.sha256"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := captureRun(t, "build", "-o", bundled, game); code == 0 || !strings.Contains(stderr, "made from another Genesis runtime") {
		t.Fatalf("a bundle of another runtime built (exit %d): %s", code, stderr)
	}
}
