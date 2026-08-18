package docgen_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/docgen"
)

func renderOne(t *testing.T, src string) string {
	t.Helper()
	return string(docgen.RenderMarkdown(collectOne(t, src, docgen.Options{})))
}

// A doc comment is written as a standalone document, so its `# Panics` is an h1. Nested
// under a declaration on a page, an h1 in the middle of the body breaks the outline and
// every table of contents built from it.
func TestRenderMarkdown_ShiftsDocHeadingsIntoThePage(t *testing.T) {
	page := renderOne(t, "/// Divides.\n///\n/// # Panics\n///\n/// Traps on zero.\npub let div = pure (a: i64, b: i64) -> i64 => a / b")

	if strings.Contains(page, "\n# Panics") {
		t.Errorf("a doc heading stayed at h1 inside the page:\n%s", page)
	}
	if !strings.Contains(page, "#### Panics") {
		t.Errorf("expected the Panics heading shifted under its declaration's h3:\n%s", page)
	}
}

// An untagged fence in a Lyra doc comment is Lyra — the most common code block in the
// standard library's documentation is the one an author has no reason to tag.
func TestRenderMarkdown_TagsBareFencesAsLyra(t *testing.T) {
	page := renderOne(t, "/// Adds.\n///\n/// # Examples\n///\n/// ```\n/// add(1, 2)\n/// ```\npub let add = pure (a: i64, b: i64) -> i64 => a + b")

	if !strings.Contains(page, "```lyra\nadd(1, 2)") {
		t.Errorf("a bare fence was not tagged as lyra:\n%s", page)
	}
}

// A fence that says what it holds is left alone.
func TestRenderMarkdown_LeavesTaggedFencesAlone(t *testing.T) {
	page := renderOne(t, "/// Runs.\n///\n/// ```bash\n/// lyrac run x.lyra\n/// ```\npub let go = pure () -> i64 => 1")

	if !strings.Contains(page, "```bash") {
		t.Errorf("a tagged fence lost its language:\n%s", page)
	}
	if strings.Contains(page, "```bashlyra") {
		t.Errorf("a tagged fence was tagged twice:\n%s", page)
	}
}

// A `#` line inside a fence is a comment in whatever language the fence holds. Shifting
// it would corrupt the example; treating it as a heading would restructure the page.
func TestRenderMarkdown_LeavesHeadingsInsideFencesAlone(t *testing.T) {
	page := renderOne(t, "/// Reads.\n///\n/// ```bash\n/// # a shell comment\n/// echo hi\n/// ```\npub let read = pure () -> i64 => 1")

	if !strings.Contains(page, "# a shell comment") {
		t.Errorf("a comment inside a fence was rewritten:\n%s", page)
	}
	if strings.Contains(page, "#### a shell comment") {
		t.Errorf("a comment inside a fence was shifted as a heading:\n%s", page)
	}
}

// The frontmatter is parsed by another repository's build. A summary containing a colon
// is ordinary prose and unquoted YAML would read it as a nested mapping — a failure that
// surfaces as a broken site build rather than as an error here.
func TestRenderMarkdown_FrontmatterIsQuoted(t *testing.T) {
	page := renderOne(t, "pub let a = pure () -> i64 => 1")
	if !strings.HasPrefix(page, "---\ntitle: \"") {
		t.Errorf("title is not a quoted scalar:\n%s", page)
	}
}

func TestRenderMarkdown_GroupsByKind(t *testing.T) {
	page := renderOne(t, "pub struct Point { x: f64 }\npub let go = pure () -> i64 => 1\npub trait Show { show: (Self) -> string }")

	types := strings.Index(page, "## Types")
	traits := strings.Index(page, "## Traits")
	funcs := strings.Index(page, "## Functions")
	if types < 0 || traits < 0 || funcs < 0 {
		t.Fatalf("missing a kind heading:\n%s", page)
	}
	if !(types < traits && traits < funcs) {
		t.Errorf("kind sections out of order: types=%d traits=%d funcs=%d", types, traits, funcs)
	}
}

// A heading butted against a list is parsed as part of the list by some renderers, so a
// declaration ending in a member list must still be followed by a blank line.
func TestRenderMarkdown_BlankLineBeforeTheNextHeading(t *testing.T) {
	page := renderOne(t, "/// A shape.\npub data Shape =\n  /// A circle.\n  Circle(f64)\n  /// Nothing.\n  | Empty\n\n/// A point.\npub struct Point { x: f64 }")

	if strings.Contains(page, "Nothing.\n### ") {
		t.Errorf("a heading follows a list with no blank line between:\n%s", page)
	}
}

func TestFileName(t *testing.T) {
	// Dashes, not dots: a site generator strips dots from the slug, so
	// `std.prelude.md` would publish at `/reference/stdprelude/`.
	if got, want := docgen.FileName("std.prelude"), "std-prelude.md"; got != want {
		t.Errorf("FileName = %q, want %q", got, want)
	}
	// The entry file of a single-file program declares no module and still gets a page.
	if got, want := docgen.FileName(""), "main.md"; got != want {
		t.Errorf("FileName(\"\") = %q, want %q", got, want)
	}
}

// Implementations go last. std/prelude/math.lyra alone contributes 78 of them, so
// anywhere earlier they are a wall between the traits and the part of the page anyone
// reads — and the sidebar's table of contents becomes a list of arithmetic
// instantiations with the module's real surface below the fold.
func TestRenderMarkdown_ImplementationsGoLast(t *testing.T) {
	page := renderOne(t, `
pub trait Show { show: (Self) -> string }
pub struct Pt { x: f64 }
/// Shows.
impl Show for string { show = (self) => self }
/// Trims.
pub let trim = pure (self: string) -> string => self
/// Builds.
pub let mk = pure () -> f64 => 1.0
`)
	impls := strings.Index(page, "## Implementations")
	if impls < 0 {
		t.Fatalf("no Implementations section:\n%s", page)
	}
	for _, earlier := range []string{"## Types", "## Traits", "## Functions", "## Methods on `string`"} {
		i := strings.Index(page, earlier)
		if i < 0 {
			t.Errorf("missing %q", earlier)
			continue
		}
		if i > impls {
			t.Errorf("%q comes after Implementations; impls must be last", earlier)
		}
	}
}
