package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/rom"
)

// z80Flags are llc's for a Z80 program. A section each, so the linker keeps only what is
// reached: a cartridge has no room for the rest.
var z80Flags = []string{
	"-mtriple=z80-unknown-elf", "-relocation-model=static", "-filetype=obj",
	"-function-sections", "-data-sections",
}

// z80Toolchain is where tools/llvm-z80.sh built.
type z80Toolchain struct{ root string }

func (t z80Toolchain) tool(name string) string { return filepath.Join(t.root, "build", "bin", name) }

// findZ80Toolchain finds the Z80 LLVM, or says how to get one.
func findZ80Toolchain() (z80Toolchain, error) {
	root := os.Getenv("LYRA_Z80_LLVM")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return z80Toolchain{}, err
		}
		root = filepath.Join(home, "Dev", "llvm-z80")
	}
	t := z80Toolchain{root: root}
	for _, tool := range []string{"opt", "llc"} {
		if _, err := os.Stat(t.tool(tool)); err != nil {
			return t, fmt.Errorf("no Z80 LLVM at %s (%s is missing): build one with tools/llvm-z80.sh, or set LYRA_Z80_LLVM to where it is", root, tool)
		}
	}
	return t, nil
}

// buildZ80 is `lyrac build` for the Master System or Game Gear: the ROM at o.out (or the
// source's stem with `.sms` or `.gg`), and its path.
func buildZ80(o buildOptions, res *driver.Result, entry *driver.EntryPoint) (string, int) {
	fail := func(err error) (string, int) {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	tc, err := findZ80Toolchain()
	if err != nil {
		return fail(err)
	}
	o.report(20, "generating code")
	ir, err := llvm.NewForZ80().Emit(res, entry)
	if err != nil {
		return fail(fmt.Errorf("llvm backend: %w", err))
	}
	if o.emitOnly {
		llFile := llPath(o)
		if err := os.WriteFile(llFile, ir, 0o644); err != nil {
			return fail(err)
		}
		fmt.Printf("%s: wrote %s (llvm backend, for %s)\n", o.path, llFile, res.Target.Display)
		return llFile, 0
	}
	work, err := os.MkdirTemp("", "lyrac-z80-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(work)

	program, err := compileZ80Program(tc, ir, work, o)
	if err != nil {
		return fail(err)
	}
	o.report(80, "compiling the runtime")
	runtime, err := z80Runtime(tc)
	if err != nil {
		return fail(err)
	}
	o.report(90, "linking")
	img, err := rom.LinkZ80([]rom.Object{{Name: filepath.Base(o.path), Bytes: program}, runtime})
	if err != nil {
		var undefined *rom.UndefinedZ80
		if errors.As(err, &undefined) {
			fmt.Fprintf(os.Stderr, "lyrac: %s cannot run on %s: %s\n", o.path, res.Target.Display,
				rom.ExplainUndefinedOn(undefined.Names, res.Target.Display))
			return "", 1
		}
		return fail(fmt.Errorf("linking %s: %w", o.path, err))
	}
	gameGear := res.Target.Name == "gg"
	out := o.out
	if out == "" {
		out = replaceExt(o.path, "."+res.Target.Name)
	}
	if err := os.WriteFile(out, rom.Z80Cartridge(img, gameGear), 0o644); err != nil {
		return fail(err)
	}
	if !o.ephemeral {
		fmt.Printf("%s: wrote %s (%s ROM: %d bytes of code and constants, %d of RAM)\n",
			o.path, out, strings.TrimPrefix(res.Target.Display, "the "), img.Used, img.DataSize+img.BSSSize)
	}
	return out, 0
}

// compileZ80Program optimizes the IR for the Z80 and compiles it to an object, everything
// but `main` and the interrupt handlers internalized so the unreached is dropped.
func compileZ80Program(tc z80Toolchain, ir []byte, work string, o buildOptions) ([]byte, error) {
	ll := filepath.Join(work, "program.ll")
	bc := filepath.Join(work, "program.bc")
	obj := filepath.Join(work, "program.o")
	if err := os.WriteFile(ll, ir, 0o644); err != nil {
		return nil, err
	}
	level := strings.TrimPrefix(o.opt, "-")
	if level == "" {
		level = "O2"
	}
	o.report(30, "optimizing")
	if err := runTool(tc.tool("opt"), "-mtriple=z80-unknown-elf",
		"-passes=internalize,default<"+level+">", "-internalize-public-api-list=main,__lyra_interrupt_vblank,__lyra_interrupt_hblank",
		ll, "-o", bc); err != nil {
		return nil, err
	}
	o.report(40, "compiling for the Z80")
	if err := runTool(tc.tool("llc"), append(append([]string{}, z80Flags...), "-O2", bc, "-o", obj)...); err != nil {
		return nil, err
	}
	return os.ReadFile(obj)
}

// z80Runtime is runtime/sms/runtime.ll compiled, cached by its text and the compiler. It goes
// through llc alone: opt would turn its memory loops back into calls to themselves.
func z80Runtime(tc z80Toolchain) (rom.Object, error) {
	source := filepath.Join(modules.StdRoot(), "runtime", "sms", "runtime.ll")
	text, err := os.ReadFile(source)
	if err != nil {
		return rom.Object{}, fmt.Errorf("the Z80 runtime is missing (%s): rebuild lyrac with ./build.sh", source)
	}
	sum := sha256.New()
	sum.Write(text)
	if info, err := os.Stat(tc.tool("llc")); err == nil {
		fmt.Fprintf(sum, "%s %d %d", tc.root, info.Size(), info.ModTime().UnixNano())
	}
	sum.Write([]byte(strings.Join(z80Flags, " ")))
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "lyra-z80")
	obj := filepath.Join(dir, "runtime-"+hex.EncodeToString(sum.Sum(nil))[:16]+".o")
	if data, err := os.ReadFile(obj); err == nil {
		return rom.Object{Name: "runtime.ll", Bytes: data}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rom.Object{}, err
	}
	tmp, err := scratchFile(dir, filepath.Base(obj)+"-*.tmp")
	if err != nil {
		return rom.Object{}, err
	}
	defer os.Remove(tmp)
	if err := runTool(tc.tool("llc"), append(append([]string{}, z80Flags...), "-O2", source, "-o", tmp)...); err != nil {
		return rom.Object{}, err
	}
	if err := os.Rename(tmp, obj); err != nil {
		return rom.Object{}, err
	}
	data, err := os.ReadFile(obj)
	return rom.Object{Name: "runtime.ll", Bytes: data}, err
}
