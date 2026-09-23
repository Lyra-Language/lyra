package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// **Every `.lyra` file in the repo type-checks, under both module roots.**
//
// Nothing checked this before 09/22, and two different bugs were living in the gap. CI
// builds the handful of examples that have tests and formats everything; it type-checked
// nothing, so a file could be committed with a missing import and only a reader who opened
// it would find out.
//
// The two roots are the point, not a detail. `lyrac` finds the standard library beside its
// own executable, so an editor's language server resolves modules against **`build/`**,
// where `std` and `bindings` are symlinks — while a command line usually passes the repo
// root. A module reachable from one and not the other compiles for whoever runs the build
// and shows errors to whoever opens the file, which is the worse half of that trade: the
// editor is where the errors are read.
//
// That is exactly what happened to `examples/collector`. Its modules were named by dotted
// path (`examples.collector.ast`) so a test could import them from a temp directory; the
// path resolved only when the root was the repo root, and the Zed extension showed 11
// errors on a file the command line called clean. They are siblings now, as every other
// example's modules are, and this test is the thing that would have said so.
// editorRoot builds the module root a language server sees: a directory holding **only**
// `std` and `bindings`, as `build/` does after `./build.sh` links them there.
//
// Built here rather than pointed at `build/`, which is what the first version did and why
// it failed in CI: CI never runs `build.sh`, so `build/std` does not exist and every
// standard-library file "failed" for want of a symlink. A test that depends on an artifact
// of someone's local build tests the artifact. Constructing the shape makes the second root
// exist everywhere, which matters more here than anywhere — it is the half that catches the
// bug the other root cannot see.
func editorRoot(t *testing.T, repo string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"std", "bindings"} {
		if err := os.Symlink(filepath.Join(repo, name), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRepo_EveryLyraFileChecksUnderBothRoots(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, dir := range []string{"std", "examples", "bindings"} {
		if err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// The compiler's own fixtures are meant to be rejected — one of them does not
			// even parse — which is why `.lyrafmtignore` names them too.
			if !info.IsDir() && strings.HasSuffix(path, ".lyra") && !strings.Contains(path, "testdata") {
				files = append(files, path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("found no .lyra files to check; the walk is wrong")
	}

	for _, roots := range []struct{ name, std string }{
		{"repo root", root},
		{"beside the executable", editorRoot(t, root)},
	} {
		t.Run(roots.name, func(t *testing.T) {
			t.Setenv("LYRA_STD", roots.std)
			for _, file := range files {
				rel, _ := filepath.Rel(root, file)
				_, stderr, code := captureRun(t, "check", file)
				if code != 0 {
					t.Errorf("%s does not check with the root %s:\n%s", rel, roots.name, stderr)
				}
			}
		})
	}
}
