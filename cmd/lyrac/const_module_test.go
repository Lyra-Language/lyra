package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// **Two modules may each have a private `const` of one name, and each must get its own**
// (09/21). A `const` has no storage — a reference inlines its value expression — and the
// backend kept them in a map keyed by the **bare** name, so the second module's
// declaration replaced the first's and every reference in either module inlined whichever
// had won.
//
// The crash that found it was the lucky shape: `bindings/raylib` has a private
// `HEX_DIGITS: string`, the prelude a private `HEX_DIGITS: []rune`, and a program importing
// raylib made the prelude's `to_hex` index a string, which the backend refused to emit. The
// shape this test pins is the one nothing would have caught — **two constants of the same
// type**, where the wrong value inlines silently and the program merely computes something
// else. Before the fix this printed `222 222`.
//
// It is the same per-module keying that `l.globals` and `l.funcs` already use, and the
// comments on both say why: a bare name is one slot for the whole program, and a private
// declaration is not the whole program's.
func TestExec_PrivateConstsDoNotCollideAcrossModules(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	files := map[string]string{
		"lib.lyra": `module lib

/// Answers this module's own TAG, which the program below also declares.
pub let lib_tag = pure () -> i64 => TAG

/// A tag no other module can see.
const TAG: i64 = 111
`,
		"prog.lyra": `module main
import lib.{ lib_tag }

const TAG: i64 = 222

let main = () -> void => println("${lib_tag()} ${TAG}")
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := captureRun(t, "run", filepath.Join(dir, "prog.lyra"))
	if code != 0 {
		t.Fatalf("running exited %d\nstderr: %s", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != "111 222" {
		t.Errorf("each module's own constant should reach it: got %q, want %q", got, "111 222")
	}
}
