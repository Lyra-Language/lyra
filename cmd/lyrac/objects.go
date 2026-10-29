package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/target"
)

// A program is built in parts, each compiled once and kept (10/29). One module of IR went
// to one clang process: Vega's was 60 MB, 204 seconds at -O2 on one core, and every build
// paid all of it whatever had changed. Now the backend splits the program (llvm.EmitParts:
// a part for each module's code, parts for what the program makes for itself), each part
// is compiled on a core of its own, and the object is kept in the user's cache under the
// hash of its text and of how it was compiled — so the next build compiles only the parts
// whose text changed, and `std` and `ui` parts are shared by every program using them.
//
// **Optimized builds link with ThinLTO**, so code is still inlined across parts: each part
// is compiled to bitcode with its summary (`-flto=thin`), and the link imports and inlines
// across them and generates code in parallel, keeping what it generated in its own cache.
// Measured on Vega: 34 seconds cold where the one module took 204, under a second with
// nothing changed. An -O0 build has nothing to inline, so its parts are ordinary objects.
//
// It needs a linker that does ThinLTO: Apple's own, beside Apple's clang, or lld (`ld.lld`
// on the PATH) elsewhere. Without one an optimized build is built whole, as it was, and so
// is any build the split does not serve — `--emit-llvm` and `--keep-ll` (which promise one
// .ll), a live build, another platform's or an app — or one with LYRA_NO_OBJECT_CACHE set.
// A part that fails to compile is a split the backend got wrong: the build says so and is
// made whole instead.

// cacheFormat changes when what an object is keyed by or how it is built changes, so an
// old cache is never read as a new one.
const cacheFormat = "lyra-objects-2" // 2: parts state the target's data layout (datalayout.go)

// buildFromParts builds exe from the program's parts, if this build is one the split
// serves (see above). handled false: build it whole.
func buildFromParts(o buildOptions, res *driver.Result, entry *driver.EntryPoint, be *llvm.Backend, p target.Platform, libs []string, exe string) (handled bool, code int) {
	if os.Getenv("LYRA_NO_OBJECT_CACHE") != "" || o.emitOnly || o.keepLL || o.liveEmit != nil || !p.IsMachine() {
		return false, 0
	}
	cc, err := findCC(o.cc)
	if err != nil {
		return false, 0 // the whole build reports it
	}
	lto := o.opt != "-O0"
	var ltoFlags []string
	if lto {
		var ok bool
		if ltoFlags, ok = thinLTOLink(cc, p); !ok {
			return false, 0
		}
	}
	cache, err := objectCacheDir()
	if err != nil {
		return false, 0
	}
	parts, err := be.EmitParts(res, entry)
	if errors.Is(err, llvm.ErrNotSplittable) {
		return false, 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s backend: %v\n", be.Name(), err)
		return true, 1
	}
	for i := range parts {
		parts[i].IR = withDataLayout(cc, p, parts[i].IR)
	}
	// LYRA_DUMP_PARTS=<dir> writes each part's IR there: the way to see why a part was
	// compiled again (diff it against the last build's).
	if dump := os.Getenv("LYRA_DUMP_PARTS"); dump != "" {
		for _, part := range parts {
			_ = os.WriteFile(filepath.Join(dump, part.Name+".ll"), part.IR, 0o644)
		}
	}
	for _, part := range parts {
		if err := llvm.CheckCoroutineSupport(part.IR, cc); err != nil {
			return false, 0 // the whole build refuses it, keeping the IR
		}
	}

	compile := []string{o.opt}
	if lto {
		compile = append(compile, "-flto=thin")
	}
	identity := compilerIdentity(cc)
	objects, err := compileParts(cc, compile, identity, parts, cache)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: note: building the program whole, since %v\n", err)
		return false, 0
	}

	args := append([]string{}, compile...)
	args = append(args, objects...)
	args = append(args, libs...)
	args = append(args, ltoFlags...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s failed to link the program: %v\n%s", cc, err, out)
		return true, 1
	}
	pruneObjectCache(cache)
	return true, 0
}

// thinLTOLink is the link flags that keep ThinLTO's generated code between builds, if this
// compiler's linker does ThinLTO: Apple's ld64 reads Apple clang's bitcode, and lld any
// clang's. ok false: build an optimized program whole.
func thinLTOLink(cc string, p target.Platform) ([]string, bool) {
	dir, err := objectCacheDir()
	if err != nil {
		return nil, false
	}
	dir = filepath.Join(dir, "lto")
	switch {
	case p.OS == "darwin" && strings.Contains(compilerIdentity(cc), "Apple clang"):
		return []string{"-Wl,-cache_path_lto," + dir}, true
	case p.OS == "linux":
		// **Opt-in on Linux for now** (LYRA_THINLTO=1): on CI's x86_64 runner a std.temporal
		// program built this way segfaulted — an `i128` slot allocated 8-aligned, read before
		// the target's data layout was known (datalayout.go, the fix). Until CI has run it
		// on real x86_64 hardware since, an optimized Linux build is made whole.
		if os.Getenv("LYRA_THINLTO") != "1" {
			return nil, false
		}
		if _, err := exec.LookPath("ld.lld"); err != nil {
			return nil, false
		}
		return []string{"-fuse-ld=lld", "-Wl,--thinlto-cache-dir=" + dir}, true
	}
	return nil, false
}

// objectCacheDir is where objects are kept: $LYRA_CACHE_DIR, else `lyra` in the user's
// cache directory (~/Library/Caches on macOS, ~/.cache on Linux).
func objectCacheDir() (string, error) {
	dir := os.Getenv("LYRA_CACHE_DIR")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "lyra")
	}
	dir = filepath.Join(dir, "objects")
	return dir, os.MkdirAll(dir, 0o755)
}

var identities sync.Map // compiler path → its --version, once a process

// compilerIdentity is the compiler's path and first --version line: an object compiled by
// another clang is another object.
func compilerIdentity(cc string) string {
	if v, ok := identities.Load(cc); ok {
		return v.(string)
	}
	out, _ := exec.Command(cc, "--version").Output()
	line, _, _ := strings.Cut(string(out), "\n")
	id := cc + "\n" + line
	identities.Store(cc, id)
	return id
}

// compileParts compiles each part not already kept, a core each, and returns the objects
// in the parts' order. A kept object is touched, so pruning keeps what is used.
func compileParts(cc string, flags []string, identity string, parts []llvm.Part, cache string) ([]string, error) {
	objects := make([]string, len(parts))
	errs := make([]error, len(parts))
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				objects[i], errs[i] = compilePart(cc, flags, identity, parts[i], cache)
			}
		}()
	}
	for i := range parts {
		work <- i
	}
	close(work)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("part %s did not compile: %w", parts[i].Name, err)
		}
	}
	return objects, nil
}

func compilePart(cc string, flags []string, identity string, part llvm.Part, cache string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", cacheFormat, identity, strings.Join(flags, " "))
	h.Write(part.IR)
	key := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(cache, key[:2])
	obj := filepath.Join(dir, key+".o")
	if _, err := os.Stat(obj); err == nil {
		now := time.Now()
		_ = os.Chtimes(obj, now, now)
		return obj, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "lyrac-part-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	ll := filepath.Join(tmp, "part.ll")
	if err := os.WriteFile(ll, part.IR, 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(tmp, "part.o")
	args := append(append([]string{}, flags...), "-c", ll, "-o", out)
	if msg, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%v\n%s", err, msg)
	}
	// Into the cache by rename, which is atomic: a build beside this one (the tests run
	// many) sees the whole object or none.
	staged := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", key, os.Getpid()))
	if err := copyFile(out, staged); err != nil {
		return "", err
	}
	if err := os.Rename(staged, obj); err != nil {
		os.Remove(staged)
		return "", err
	}
	return obj, nil
}

const (
	// maxCacheBytes is how large the kept objects grow before the least recently used go.
	maxCacheBytes = 2 << 30
	// pruneEvery is how often a build looks: the walk is cheap, but not free.
	pruneEvery = time.Hour
)

// pruneObjectCache removes the least recently used objects once they pass maxCacheBytes,
// looking at most once every pruneEvery. ThinLTO's own cache (`lto/`) is its linker's to
// prune, by its default policy.
func pruneObjectCache(cache string) {
	stamp := filepath.Join(cache, ".pruned")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < pruneEvery {
		return
	}
	_ = os.WriteFile(stamp, nil, 0o644)
	type entry struct {
		path string
		size int64
		used time.Time
	}
	var entries []entry
	var total int64
	_ = filepath.WalkDir(cache, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "lto" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".o") {
			return nil
		}
		if info, err := d.Info(); err == nil {
			entries = append(entries, entry{path, info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	if total <= maxCacheBytes {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].used.Before(entries[j].used) })
	for _, e := range entries {
		if total <= maxCacheBytes*3/4 {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}
