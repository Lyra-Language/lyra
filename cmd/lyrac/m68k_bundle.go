package main

// `lyrac m68k-bundle OUT`: the Genesis toolchain a release carries, made from a full M68k
// LLVM (tools/llvm-m68k.sh's): `bin/opt` and `bin/llc`, and the runtime (runtime/genesis)
// and compiler-rt's helpers compiled into `objects/` as a build would compile them — so a
// machine with the bundle builds a Genesis game with no clang and no LLVM source. A full
// build is about 420 MB; this is two programs (stripped by the release, about 43 MB each)
// and fifty small objects.
//
// **The objects are this lyrac's runtime's**: `objects/runtime.sha256` names the
// runtime.c they were made from, and a build with a bundle made from another refuses,
// rather than linking a runtime that no longer matches the compiler's code.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/rom"
)

// m68kBundle makes the bundle in out from a full toolchain: $LYRA_M68K_LLVM's or
// ~/Dev/llvm-m68k's, never another bundle.
func m68kBundle(out string) int {
	full, err := fullM68kToolchain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	if err := makeM68kBundle(full, out); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	fmt.Printf("lyrac: wrote the M68k toolchain to %s (from %s)\n", out, full.root)
	return 0
}

// fullM68kToolchain is a full build of the M68k LLVM — what a bundle is made from.
func fullM68kToolchain() (m68kToolchain, error) {
	root := os.Getenv("LYRA_M68K_LLVM")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return m68kToolchain{}, err
		}
		root = filepath.Join(home, "Dev", "llvm-m68k")
	}
	t, ok := m68kAt(root)
	if !ok || t.bundle {
		return m68kToolchain{}, fmt.Errorf("no full M68k LLVM at %s (a bundle is made from one): build it with tools/llvm-m68k.sh, or set LYRA_M68K_LLVM to where it is", root)
	}
	return t, nil
}

func makeM68kBundle(full m68kToolchain, out string) error {
	bundle := m68kToolchain{root: out, bundle: true}
	for _, dir := range []string{filepath.Join(out, "bin"), filepath.Join(out, "objects")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, tool := range []string{"opt", "llc"} {
		if err := copyExecutable(full.tool(tool), bundle.tool(tool)); err != nil {
			return err
		}
	}
	source := filepath.Join(modules.StdRoot(), "runtime", "genesis", "runtime.c")
	runtime, helpers, err := genesisRuntime(full, buildOptions{})
	if err != nil {
		return err
	}
	for _, obj := range append([]rom.Object{runtime}, helpers...) {
		name := strings.TrimSuffix(obj.Name, ".c") + ".o"
		if err := os.WriteFile(filepath.Join(out, "objects", name), obj.Bytes, 0o644); err != nil {
			return err
		}
	}
	sum, err := fileSum(source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "objects", "runtime.sha256"), []byte(sum+"\n"), 0o644); err != nil {
		return err
	}
	// What made it, for a person reading the bundle.
	version, _ := exec.Command(full.tool("llc"), "--version").Output()
	return os.WriteFile(filepath.Join(out, "VERSION"), version, 0o644)
}

// bundledRuntime is a bundle's runtime and helpers, read from its objects — refused when
// they were made from another runtime.c than this lyrac's.
func bundledRuntime(tc m68kToolchain, source string) (rom.Object, []rom.Object, error) {
	objects := filepath.Join(tc.root, "objects")
	want, err := fileSum(source)
	if err != nil {
		return rom.Object{}, nil, err
	}
	made, err := os.ReadFile(filepath.Join(objects, "runtime.sha256"))
	if err != nil || strings.TrimSpace(string(made)) != want {
		return rom.Object{}, nil, fmt.Errorf("the M68k toolchain at %s was made from another Genesis runtime than this lyrac's: make it again with `lyrac m68k-bundle`", tc.root)
	}
	read := func(name string) (rom.Object, error) {
		data, err := os.ReadFile(filepath.Join(objects, name+".o"))
		if err != nil {
			return rom.Object{}, fmt.Errorf("the M68k toolchain at %s has no %s.o: make it again with `lyrac m68k-bundle`", tc.root, name)
		}
		return rom.Object{Name: name + ".c", Bytes: data}, nil
	}
	runtime, err := read("runtime")
	if err != nil {
		return rom.Object{}, nil, err
	}
	var helpers []rom.Object
	for _, name := range genesisHelpers {
		obj, err := read(name)
		if err != nil {
			return rom.Object{}, nil, err
		}
		helpers = append(helpers, obj)
	}
	return runtime, helpers, nil
}

// fileSum is the SHA-256 of the file at path, in hex.
func fileSum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// copyExecutable copies the program at from (following a link: clang's are) to to.
func copyExecutable(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, to); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
