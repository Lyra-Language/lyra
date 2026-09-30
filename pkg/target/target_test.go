package target

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A lyra.toml in a parent directory governs the files below it; the nearest one wins.
func TestForFile_FindsTheNearestConfigAbove(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ConfigName), "target = \"genesis\"\n")
	write(t, filepath.Join(root, "src", "game", "main.lyra"), "")
	got, problem := ForFile(filepath.Join(root, "src", "game", "main.lyra"))
	if problem != nil || got.Name != "genesis" {
		t.Fatalf("expected the parent's genesis target; got %q, %v", got.Name, problem)
	}

	write(t, filepath.Join(root, "src", ConfigName), "# the tools\ntarget = \"host\"\n")
	got, problem = ForFile(filepath.Join(root, "src", "game", "main.lyra"))
	if problem != nil || !got.Host {
		t.Fatalf("expected the nearer config's host target; got %q, %v", got.Name, problem)
	}
}

// No config anywhere, or no file at all, is the host.
func TestForFile_DefaultsToTheHost(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main.lyra"), "")
	if got, problem := ForFile(filepath.Join(root, "main.lyra")); problem != nil || !got.Host {
		t.Fatalf("expected the host; got %q, %v", got.Name, problem)
	}
	if got, problem := ForFile(""); problem != nil || !got.Host {
		t.Fatalf("an unnamed buffer is the host; got %q, %v", got.Name, problem)
	}
}

// What cannot be understood is said, with its line, rather than ignored.
func TestParse_RefusesWhatItDoesNotUnderstand(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"target = \"gameboy\"\n", "unknown target \"gameboy\"; the targets are genesis, host"},
		{"\n\ntarget = genesis\n", "expected a quoted string"},
		{"[package]\n", "expected `key = \"value\"`"},
		{"optimize = \"size\"\n", "unknown setting \"optimize\""},
	} {
		_, problem := parse("lyra.toml", []byte(tc.text))
		if problem == nil || !strings.Contains(problem.Message, tc.want) {
			t.Errorf("%q: expected a problem containing %q; got %+v", tc.text, tc.want, problem)
		}
	}
	_, problem := parse("lyra.toml", []byte("\n\ntarget = genesis\n"))
	if problem == nil || problem.Line != 3 {
		t.Errorf("the problem carries its line; got %+v", problem)
	}
}

// Comments, blank lines and a `#` inside the string are all fine.
func TestParse_ReadsCommentsAndStrings(t *testing.T) {
	got, problem := parse("lyra.toml", []byte("# a game\n\ntarget = \"genesis\"  # the console\n"))
	if problem != nil || got.Name != "genesis" || got.Heap || got.Host {
		t.Fatalf("expected genesis; got %+v, %v", got, problem)
	}
	if !got.Emulates("i64") || got.Emulates("i32") {
		t.Errorf("the 68000 emulates i64 and not i32")
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
