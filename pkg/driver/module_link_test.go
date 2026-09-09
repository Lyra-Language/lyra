package driver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// `@link` on a module header covers every `extern` below it.
//
// **The fact is the module's, not each declaration's.** A binding module links one library
// and declares a dozen externs against it, so the per-extern form was the same claim
// written a dozen times — `bindings/sdl3` carried fourteen `@link("SDL3")` lines for one
// library. The per-extern spelling stays legal, because a lone extern in a module-less
// program has no header to put it on, and the driver unions the two.

func linksOf(t *testing.T, src string) []string {
	t.Helper()
	res := driver.Analyze([]byte(src))
	for _, d := range res.Errors() {
		t.Fatalf("unexpected diagnostic: %s", d.Message)
	}
	return res.Links
}

func TestLinks_ModuleHeaderCoversEveryExtern(t *testing.T) {
	links := linksOf(t, `
@link("SDL3")
module bindings.sdl3
unsafe extern sdl_init: (flags: u32) -> u8
unsafe extern sdl_quit: () -> void
let main = () -> void => println("x")
`)
	if !slices.Contains(links, "SDL3") {
		t.Errorf("module-level @link did not reach the link set: %v", links)
	}
}

// The per-extern form still works — it is what a lone extern with no module header uses.
func TestLinks_PerExternStillWorks(t *testing.T) {
	links := linksOf(t, `
@link("z")
unsafe extern crc32: (crc: u64, buf: ^u8, len: u32) -> u64
let main = () -> void => println("x")
`)
	if !slices.Contains(links, "z") {
		t.Errorf("per-extern @link did not reach the link set: %v", links)
	}
}

// **Both, unioned and deduplicated.** Neither form supersedes the other, so a module that
// links one library and has a single extern needing a second gets both — and naming the
// same library twice yields it once.
func TestLinks_BothFormsUnionAndDeduplicate(t *testing.T) {
	links := linksOf(t, `
@link("SDL3")
module app
@link("z")
unsafe extern crc32: (crc: u64, buf: ^u8, len: u32) -> u64
@link("SDL3")
unsafe extern sdl_quit: () -> void
let main = () -> void => println("x")
`)
	for _, want := range []string{"SDL3", "z"} {
		if !slices.Contains(links, want) {
			t.Errorf("missing %q from %v", want, links)
		}
	}
	var sdl int
	for _, l := range links {
		if l == "SDL3" {
			sdl++
		}
	}
	if sdl != 1 {
		t.Errorf("SDL3 appears %d times; the set is deduplicated: %v", sdl, links)
	}
}

// `@symbol` is a declaration's C name and a module has no single one, so it is refused by
// name here rather than ignored — the standing rule that an attribute which parses and is
// read by nobody costs more than an absent one.
func TestLinks_ModuleRefusesSymbolAttribute(t *testing.T) {
	res := driver.Analyze([]byte(`
@symbol("nope")
module app
let main = () -> void => println("x")
`))
	found := false
	for _, d := range res.Errors() {
		if strings.Contains(d.Message, "unknown attribute `@symbol` on a module") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a diagnostic naming `@symbol` on a module, got: %v", res.Errors())
	}
}
