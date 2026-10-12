package llvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// **The backend is told its platform, never asks the machine** (Backend.os): a program
// built here for the iOS Simulator or Linux takes that platform's OS facts, and one asked
// of `runtime` would be the Mac's in silence.
func TestBackend_AsksTheMachineNothing(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "backend", "*", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob(filepath.Join("..", "*.go"))
	for _, f := range append(files, more...) {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // a comment may name what the code no longer reads
			}
			for _, banned := range []string{"runtime.GOOS", "runtime.GOARCH"} {
				if strings.Contains(line, banned) {
					t.Errorf("%s reads %s; the platform is the backend's to be told (NewForPlatform)", f, banned)
				}
			}
		}
	}
}

// Each platform's facts: iOS is Darwin's, Linux its own, x86_64 macOS the one with the
// renamed `readdir`.
func TestBackend_OSFactsArePerPlatform(t *testing.T) {
	for _, c := range []struct {
		os, arch string
		offset   int64
		readdir  string
		winsize  int64
	}{
		{"darwin", "arm64", 21, "readdir", 0x40087468},
		{"darwin", "x86_64", 21, "readdir$INODE64", 0x40087468},
		{"ios", "arm64", 21, "readdir", 0x40087468},
		{"linux", "arm64", 19, "readdir", 0x5413},
		{"linux", "x86_64", 19, "readdir", 0x5413},
	} {
		offset, err := direntNameOffset(c.os)
		if err != nil || offset != c.offset {
			t.Errorf("%s: d_name at %d (%v), want %d", c.os, offset, err, c.offset)
		}
		if got := readdirSymbol(c.os, c.arch); got != c.readdir {
			t.Errorf("%s/%s: readdir is %s, want %s", c.os, c.arch, got, c.readdir)
		}
		if got := tiocgwinsz(c.os); got != c.winsize {
			t.Errorf("%s: TIOCGWINSZ %#x, want %#x", c.os, got, c.winsize)
		}
	}
	if _, err := direntNameOffset(""); err == nil {
		t.Error("a platform nobody measured should be refused")
	}
}
