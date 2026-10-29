package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cachedObjects lists the objects kept under dir (objects.go), sorted.
func cachedObjects(t *testing.T, dir string) []string {
	t.Helper()
	var objs []string
	_ = filepath.WalkDir(filepath.Join(dir, "objects"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".o") && !strings.Contains(path, string(filepath.Separator)+"lto"+string(filepath.Separator)) {
			objs = append(objs, path)
		}
		return nil
	})
	return objs
}

// A program is built in parts, each kept under the hash of its text (objects.go): a second
// build of the same program compiles nothing, and its program is the same program. -O0
// links ordinary objects; the default -O2 goes through ThinLTO where the linker does it, and
// is built whole where it does not — either way the program runs.
func TestBuild_PartsAreKeptAndReused(t *testing.T) {
	t.Parallel()
	requireCC(t)
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			cache := t.TempDir()
			setenv(t, "LYRA_CACHE_DIR", cache)
			path := copyFixtureToTemp(t, "hello.lyra")
			exe := filepath.Join(t.TempDir(), "prog")
			if _, stderr, code := captureRun(t, "build", opt, "-o", exe, path); code != 0 {
				t.Fatalf("first build exited %d: %s", code, stderr)
			}
			first := cachedObjects(t, cache)
			if opt == "-O0" && len(first) == 0 {
				t.Fatalf("an -O0 build kept no objects under %s", cache)
			}
			want := runBinary(t, exe)
			if want != "hello\n" {
				t.Fatalf("the program printed %q", want)
			}
			if _, stderr, code := captureRun(t, "build", opt, "-o", exe, path); code != 0 {
				t.Fatalf("second build exited %d: %s", code, stderr)
			}
			if again := cachedObjects(t, cache); len(again) != len(first) {
				t.Errorf("the second build compiled %d parts again; want none", len(again)-len(first))
			}
			if got := runBinary(t, exe); got != want {
				t.Errorf("the rebuilt program printed %q; the first printed %q", got, want)
			}
		})
	}
}

// LYRA_NO_OBJECT_CACHE builds the program whole, keeping nothing.
func TestBuild_NoObjectCacheKeepsNothing(t *testing.T) {
	t.Parallel()
	requireCC(t)
	cache := t.TempDir()
	setenv(t, "LYRA_CACHE_DIR", cache)
	setenv(t, "LYRA_NO_OBJECT_CACHE", "1")
	path := copyFixtureToTemp(t, "ok.lyra")
	exe := filepath.Join(t.TempDir(), "prog")
	if _, stderr, code := captureRun(t, "build", "-O0", "-o", exe, path); code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	if objs := cachedObjects(t, cache); len(objs) != 0 {
		t.Errorf("kept %d objects with LYRA_NO_OBJECT_CACHE set", len(objs))
	}
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("no program: %v", err)
	}
}

// runBinary is what a built program prints.
func runBinary(t *testing.T, exe string) string {
	t.Helper()
	out, err := command(t, exe).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s: %v\n%s", exe, err, out)
	}
	return string(out)
}
