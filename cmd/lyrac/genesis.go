package main

// `lyrac build` for the Genesis (a `lyra.toml` naming `target = "genesis"`): the program's
// IR compiled by an M68k LLVM, the runtime (runtime/genesis) and LLVM's compiler-rt
// helpers compiled beside it, and the objects linked into a cartridge by pkg/rom — a
// `.bin` any Genesis emulator or flash cartridge runs.
//
// The toolchain is LLVM with its experimental M68k target, which no package ships: built
// by `tools/llvm-m68k.sh` into ~/Dev/llvm-m68k (or $LYRA_M68K_LLVM) — `build/bin` for the
// tools and `src/compiler-rt` for the helpers. The runtime and helpers are compiled once
// per toolchain and source, into the user cache directory.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/abi"
	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/rom"
)

// genesisFlags are llc's for Genesis code. The medium code model and static relocation
// are required, not tuning: LLVM's default small model stores to a global through a
// PC-relative destination, which no 68000 accepts, and RAM at FF0000 is beyond a
// PC-relative reach from ROM anyway.
var genesisFlags = []string{
	"-mtriple=m68k-unknown-elf", "-mcpu=M68000",
	"-code-model=medium", "-relocation-model=static", "-filetype=obj",
}

// genesisHelpers are the compiler-rt builtins a Genesis program may call: 32/64-bit
// division, 64-bit multiply and shifts, and soft float. Linked only when used.
var genesisHelpers = []string{
	"udivsi3", "divsi3", "umodsi3", "modsi3", "udivmodsi4", "divmodsi4",
	"muldi3", "udivdi3", "divdi3", "umoddi3", "moddi3", "udivmoddi4",
	"ashldi3", "ashrdi3", "lshrdi3", "negdi2", "cmpdi2", "ucmpdi2",
	"addsf3", "subsf3", "mulsf3", "divsf3", "comparesf2", "negsf2",
	"floatsisf", "floatunsisf", "fixsfsi", "fixunssfsi", "floatdisf", "fixsfdi",
	"adddf3", "subdf3", "muldf3", "divdf3", "comparedf2", "negdf2",
	"floatsidf", "floatunsidf", "fixdfsi", "fixunsdfsi", "floatdidf", "fixdfdi",
	"extendsfdf2", "truncdfsf2", "extendhfsf2", "powidf2",
	"clzsi2", "ctzsi2", "popcountsi2",
}

// m68kToolchain is where the M68k LLVM is.
type m68kToolchain struct {
	root string // the directory tools/llvm-m68k.sh built into
}

func (t m68kToolchain) tool(name string) string { return filepath.Join(t.root, "build", "bin", name) }
func (t m68kToolchain) builtins() string {
	return filepath.Join(t.root, "src", "compiler-rt", "lib", "builtins")
}

// findM68kToolchain finds the M68k LLVM, or says how to get one.
func findM68kToolchain() (m68kToolchain, error) {
	root := os.Getenv("LYRA_M68K_LLVM")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return m68kToolchain{}, err
		}
		root = filepath.Join(home, "Dev", "llvm-m68k")
	}
	t := m68kToolchain{root: root}
	for _, tool := range []string{"opt", "llc", "clang"} {
		if _, err := os.Stat(t.tool(tool)); err != nil {
			return t, fmt.Errorf("no M68k LLVM at %s (%s is missing): build one with tools/llvm-m68k.sh, or set LYRA_M68K_LLVM to where it is", root, tool)
		}
	}
	return t, nil
}

// buildGenesis is `lyrac build` for the Genesis: the ROM at o.out (or the source's stem
// with `.bin`), and its path.
func buildGenesis(o buildOptions, res *driver.Result, entry *driver.EntryPoint) (string, int) {
	tc, err := findM68kToolchain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	ir, err := llvm.NewForTarget(abi.Unknown).Emit(res, entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: llvm backend: %v\n", err)
		return "", 1
	}
	if o.emitOnly {
		llFile := llPath(o)
		if err := os.WriteFile(llFile, ir, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
			return "", 1
		}
		fmt.Printf("%s: wrote %s (llvm backend, for the Genesis)\n", o.path, llFile)
		return llFile, 0
	}
	work, err := os.MkdirTemp("", "lyrac-genesis-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	defer os.RemoveAll(work)

	program, err := compileProgram(tc, ir, work, o.opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	runtime, helpers, err := genesisRuntime(tc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	required := []rom.Object{{Name: filepath.Base(o.path), Bytes: program}, runtime}
	if missing, err := rom.Undefined(required, helpers); err == nil && len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "lyrac: %s cannot run on the Genesis: %s\n", o.path, rom.ExplainUndefined(missing))
		return "", 1
	}
	img, err := rom.Link(required, helpers, rom.GenesisLayout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: linking %s: %v\n", o.path, err)
		return "", 1
	}
	out := o.out
	if out == "" {
		out = replaceExt(o.path, ".bin")
	}
	title := strings.TrimSuffix(filepath.Base(o.path), filepath.Ext(o.path))
	if err := os.WriteFile(out, rom.GenesisCartridge(img, title), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	if !o.ephemeral {
		fmt.Printf("%s: wrote %s (Genesis ROM: %d bytes of code and constants, %d of RAM)\n",
			o.path, out, len(img.ROM)-0x200, img.DataSize+img.BSSSize)
	}
	return out, 0
}

// runGenesis is `lyrac run` for the Genesis: the ROM built to a temp file and played in
// Sheliak — `$SHELIAK`, else `sheliak` on the PATH — with the arguments after `--` handed
// to the emulator (`-- --frames 60 --screenshot shot.bmp` runs it headless).
func runGenesis(o buildOptions, res *driver.Result, entry *driver.EntryPoint) int {
	o.out += ".bin"
	romPath, code := buildGenesis(o, res, entry)
	if code != 0 {
		return code
	}
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		path, err := exec.LookPath("sheliak")
		if err != nil {
			fmt.Fprintf(os.Stderr, "lyrac: a Genesis program runs in an emulator, and there is no `sheliak` on the PATH (or set SHELIAK); `lyrac build` writes the ROM for any emulator\n")
			return 1
		}
		emulator = path
	}
	cmd := exec.Command(emulator, append([]string{romPath}, o.programArgs...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "lyrac: %s: %v\n", filepath.Base(emulator), err)
		return 1
	}
	return 0
}

// compileProgram optimizes the program's IR for the 68000 and compiles it to an object.
// Everything but `main` is internalized first, so what the program does not reach — most
// of the prelude, with its host calls — is dropped rather than linked.
func compileProgram(tc m68kToolchain, ir []byte, work, opt string) ([]byte, error) {
	ll := filepath.Join(work, "program.ll")
	bc := filepath.Join(work, "program.bc")
	obj := filepath.Join(work, "program.o")
	if err := os.WriteFile(ll, ir, 0o644); err != nil {
		return nil, err
	}
	level := strings.TrimPrefix(opt, "-")
	if level == "" {
		level = "O2"
	}
	if err := runTool(tc.tool("opt"), "-mtriple=m68k-unknown-elf",
		"-passes=internalize,default<"+level+">", "-internalize-public-api-list=main",
		ll, "-o", bc); err != nil {
		return nil, err
	}
	if err := runTool(tc.tool("llc"), append(append([]string{}, genesisFlags...), "-"+level, bc, "-o", obj)...); err != nil {
		return nil, err
	}
	return os.ReadFile(obj)
}

// genesisRuntime is the runtime object and the helper library, compiled on first use and
// cached by toolchain and source.
func genesisRuntime(tc m68kToolchain) (rom.Object, []rom.Object, error) {
	source := filepath.Join(modules.StdRoot(), "runtime", "genesis", "runtime.c")
	if _, err := os.Stat(source); err != nil {
		return rom.Object{}, nil, fmt.Errorf("the Genesis runtime is missing (%s): rebuild lyrac with ./build.sh", source)
	}
	runtime, err := cachedObject(tc, source, nil)
	if err != nil {
		return rom.Object{}, nil, err
	}
	var helpers []rom.Object
	for _, name := range genesisHelpers {
		src := filepath.Join(tc.builtins(), name+".c")
		obj, err := cachedObject(tc, src, []string{"-I" + tc.builtins()})
		if err != nil {
			return rom.Object{}, nil, err
		}
		helpers = append(helpers, obj)
	}
	return runtime, helpers, nil
}

// cachedObject compiles a C source for the 68000 — through IR, so llc applies the same
// code model the program gets — or reads the object a previous build made of it.
func cachedObject(tc m68kToolchain, source string, extra []string) (rom.Object, error) {
	text, err := os.ReadFile(source)
	if err != nil {
		return rom.Object{}, err
	}
	sum := sha256.New()
	sum.Write(text)
	sum.Write([]byte(tc.root))
	// The toolchain itself, by its compiler's size and time: rebuilding LLVM at another
	// revision (tools/llvm-m68k.sh's pin moves) must not reuse objects the old one made.
	if info, err := os.Stat(tc.tool("llc")); err == nil {
		fmt.Fprintf(sum, "%d %d", info.Size(), info.ModTime().UnixNano())
	}
	sum.Write([]byte(strings.Join(append(append([]string{}, genesisFlags...), extra...), " ")))
	key := hex.EncodeToString(sum.Sum(nil))[:16]
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "lyra-genesis")
	obj := filepath.Join(dir, strings.TrimSuffix(filepath.Base(source), ".c")+"-"+key+".o")
	if data, err := os.ReadFile(obj); err == nil {
		return rom.Object{Name: filepath.Base(source), Bytes: data}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rom.Object{}, err
	}
	ll := obj + ".ll"
	args := append([]string{"--target=m68k-unknown-elf", "-mcpu=68000", "-O2",
		"-ffreestanding", "-fno-builtin", "-fno-common", "-S", "-emit-llvm"}, extra...)
	if err := runTool(tc.tool("clang"), append(args, source, "-o", ll)...); err != nil {
		return rom.Object{}, err
	}
	defer os.Remove(ll)
	tmp := obj + ".tmp"
	if err := runTool(tc.tool("llc"), append(append([]string{}, genesisFlags...), "-O2", ll, "-o", tmp)...); err != nil {
		return rom.Object{}, err
	}
	if err := os.Rename(tmp, obj); err != nil {
		return rom.Object{}, err
	}
	data, err := os.ReadFile(obj)
	return rom.Object{Name: filepath.Base(source), Bytes: data}, err
}

// runTool runs one toolchain step, its output becoming the error when it fails.
func runTool(tool string, args ...string) error {
	out, err := exec.Command(tool, args...).CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("%s failed:\n%s", filepath.Base(tool), strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("%s: %v", filepath.Base(tool), err)
	}
	return nil
}
