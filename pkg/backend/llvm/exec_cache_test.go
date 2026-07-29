package llvm

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file is the one compile path for every behavioral test helper
// (buildAndRun, buildAndRunCapture, buildAndRunASan, runModule): compile IR
// with clang, but reuse a previously compiled binary when the IR and flags are
// unchanged.
//
// Why a cache: macOS security-assesses every newly created executable on its
// first exec (syspolicyd/XProtect), serialized system-wide at ~200ms per
// binary. For a suite that compiles hundreds of tiny test programs, that
// serial scan — not compilation, which parallelizes fine, and not re-exec of
// an already-assessed file, which is ~1ms — is the suite's entire wall time.
// Content-addressing the binary by its IR means only tests whose emitted IR
// actually changed pay the assessment cost; a repeat run reuses the assessed
// files and skips clang entirely.

var clangTool struct {
	once sync.Once
	path string // "" when clang is not on PATH
	ver  string // `clang --version` first line, part of the cache key
}

// lookClang returns the clang path, skipping the test when the toolchain is
// absent (mirroring the old per-helper LookPath behavior, resolved once).
func lookClang(t *testing.T) string {
	t.Helper()
	clangTool.once.Do(func() {
		path, err := exec.LookPath("clang")
		if err != nil {
			return
		}
		clangTool.path = path
		if out, verr := exec.Command(path, "--version").Output(); verr == nil {
			if i := strings.IndexByte(string(out), '\n'); i > 0 {
				clangTool.ver = string(out[:i])
			}
		}
	})
	if clangTool.path == "" {
		t.Skip("clang not found on PATH; skipping behavioral test")
	}
	return clangTool.path
}

var binCache struct {
	once sync.Once
	dir  string // "" when no persistent cache dir is available (compile uncached)
}

func binCacheDir() string {
	binCache.once.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir := filepath.Join(base, "lyra-llvm-tests")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		binCache.dir = dir
		// Best-effort prune: entries untouched for 30 days are stale IR from
		// old compiler states (and any tmp-* orphans from a killed run).
		cutoff := time.Now().AddDate(0, 0, -30)
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
					os.Remove(filepath.Join(dir, e.Name()))
				}
			}
		}
	})
	return binCache.dir
}

// compileCached compiles ir with clang (plus extraArgs) and returns the path of
// the executable, reusing the previously compiled (and OS-assessed) binary when
// nothing in the key — IR bytes, flags, clang version, platform — changed.
// -lm links libm for the float intrinsics (floor/ceil/round, fmod); it is
// passed unconditionally since it's harmless for programs that don't need it
// and keeps the key uniform.
func compileCached(t *testing.T, clang, ir string, extraArgs ...string) string {
	t.Helper()

	compile := func(binPath string) {
		llPath := filepath.Join(t.TempDir(), "prog.ll")
		if err := os.WriteFile(llPath, []byte(ir), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append(append([]string{}, extraArgs...), llPath, "-lm", "-o", binPath)
		if out, err := exec.Command(clang, args...).CombinedOutput(); err != nil {
			os.Remove(binPath)
			t.Fatalf("clang rejected the IR: %v\n%s\n--- IR ---\n%s", err, out, ir)
		}
	}

	dir := binCacheDir()
	if dir == "" {
		bin := filepath.Join(t.TempDir(), "prog")
		compile(bin)
		return bin
	}

	h := sha256.New()
	h.Write([]byte(clangTool.ver + "|" + runtime.GOOS + "/" + runtime.GOARCH + "|" + strings.Join(extraArgs, " ") + "\x00"))
	h.Write([]byte(ir))
	bin := filepath.Join(dir, hex.EncodeToString(h.Sum(nil)))
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	// Compile to a temp name in the cache dir, then atomically rename into
	// place: a concurrent test compiling the same key just wins/loses the
	// rename race harmlessly, and a partial binary can never sit at the final
	// path for another run to exec.
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	compile(tmpPath)
	if err := os.Rename(tmpPath, bin); err != nil {
		os.Remove(tmpPath)
		t.Fatal(err)
	}
	return bin
}
