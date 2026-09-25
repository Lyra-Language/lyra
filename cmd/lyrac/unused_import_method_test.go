package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// **A name used only method-style needs no import of the name** (`lyra-W004`, 09/25).
//
// A method call does not require the method's name in the import list — the decision of
// 09/22, taken so a type's accessors need not be dragged into the bare scope — so
// `import lib.{ twice }` beside nothing but `1.twice()` lists a name doing no work:
// `import lib` alone compiles. The module still has to be imported, which is why the
// advice is "drop the name" and not "drop the import", and why it can only be said where
// the module-level use is known.
//
// Written here rather than beside the checker's unit tests because it cannot be faked. The
// warning turns on a *UFCS resolution*, which needs a real second module, a real
// typechecker pass and the desugaring that synthesizes the callee — and the whole point is
// telling that callee from a written reference to the same name. A hand-built ImportUse
// would be testing the shape of a map instead.
func TestUnusedImport_NameUsedOnlyMethodStyle(t *testing.T) {
	for _, c := range []struct {
		name, lib, other, prog string
		want                   string
	}{
		{
			name: "a name only ever called method-style",
			lib:  "module lib\npub let twice = pure (self: i64) -> i64 => self * 2\n",
			prog: "module main\nimport lib.{ twice }\nlet main = () -> void => println(\"${1.twice()}\")\n",
			want: "only ever called method-style",
		},
		{
			// Written as an ordinary call, so the import list is what found it.
			name: "the same name written bare",
			lib:  "module lib\npub let twice = pure (self: i64) -> i64 => self * 2\n",
			prog: "module main\nimport lib.{ twice }\nlet main = () -> void => println(\"${twice(1)}\")\n",
			want: "",
		},
		{
			// Both spellings: one written reference is enough to keep the name.
			name: "both spellings in one file",
			lib:  "module lib\npub let twice = pure (self: i64) -> i64 => self * 2\n",
			prog: "module main\nimport lib.{ twice }\n" +
				"let main = () -> void => println(\"${twice(1)} ${2.twice()}\")\n",
			want: "",
		},
		{
			// **The ambiguity guard.** The list is what breaks the tie between two
			// modules exporting the same method name; dropping the name turns a working
			// call into `receiver.twice is ambiguous`. So a second exporter silences it.
			name:  "another module exports the same name",
			lib:   "module lib\npub let twice = pure (self: i64) -> i64 => self * 2\n",
			other: "module other\npub let twice = pure (self: i64) -> i64 => self * 20\n",
			prog: "module main\nimport lib.{ twice }\nimport other.{ twice as t2 }\n" +
				"let main = () -> void => println(\"${1.twice()} ${t2(1)}\")\n",
			want: "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := repoRoot(t)
			t.Setenv("LYRA_STD", root)
			dir := t.TempDir()
			files := map[string]string{"lib.lyra": c.lib, "prog.lyra": c.prog}
			if c.other != "" {
				files["other.lyra"] = c.other
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, stderr, _ := captureRun(t, "check", filepath.Join(dir, "prog.lyra"))
			if c.want == "" {
				if strings.Contains(stderr, "only ever called method-style") {
					t.Errorf("the name is needed; should be silent:\n%s", stderr)
				}
				return
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("want a warning containing %q, got:\n%s", c.want, stderr)
			}
		})
	}
}
