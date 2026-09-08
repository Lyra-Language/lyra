package llvm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// UsesCoroutines reports whether emitted IR holds a coroutine — a sequence held as a
// value (seq_coro.go) — and so needs a compiler that can split one.
func UsesCoroutines(ir []byte) bool {
	return strings.Contains(string(ir), " presplitcoroutine {")
}

// CheckCoroutineSupport answers an error when ir holds a coroutine and the given C
// compiler cannot split it. LLVM 15 and later split a function carrying
// `presplitcoroutine`; LLVM 14 and earlier reject the word, and — from IR input — run
// no coroutine passes at all, so the intrinsics reach instruction selection and the
// compiler crashes. Refused up front, by name, rather than left to that crash.
//
// Decided by trying the attribute once per compiler path, not by parsing a version:
// Apple numbers its clang on a different scale from LLVM's, and the question is only
// ever "does this parse".
func CheckCoroutineSupport(ir []byte, compiler string) error {
	if !UsesCoroutines(ir) || splitsCoroutines(compiler) {
		return nil
	}
	return fmt.Errorf("%s cannot split coroutines: a sequence held as a value (a `Seq<t>` binding, `next()`, `zip`) needs clang 15 or later", compiler)
}

var coroutineProbe sync.Map // compiler path → bool

func splitsCoroutines(compiler string) bool {
	if v, ok := coroutineProbe.Load(compiler); ok {
		return v.(bool)
	}
	supported := probeCoroutineAttribute(compiler)
	coroutineProbe.Store(compiler, supported)
	return supported
}

func probeCoroutineAttribute(compiler string) bool {
	dir, err := os.MkdirTemp("", "lyra-coro-probe-*")
	if err != nil {
		return true
	}
	defer os.RemoveAll(dir)
	ll := filepath.Join(dir, "probe.ll")
	if err := os.WriteFile(ll, []byte("define void @f() presplitcoroutine {\n  ret void\n}\n"), 0o644); err != nil {
		return true
	}
	return exec.Command(compiler, "-c", "-o", filepath.Join(dir, "probe.o"), ll).Run() == nil
}
