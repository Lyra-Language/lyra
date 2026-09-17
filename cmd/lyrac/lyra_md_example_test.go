package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/lyra-md/lyra-md.lyra` is the standard library's forcing function, the way
// `lyrafmt` is the compiler's: it is an ordinary program made of text handling, file
// handling and collections, so whatever it has to write by hand is something `std` is
// missing. `lines`, `strip_prefix` and `strip_suffix` were added on 09/17 because this
// program wrote all three itself.
//
// This slice is the **block level** — headings, fenced code, paragraphs, escaping — and
// the test pins the rules a renderer gets wrong quietly rather than loudly: a paragraph's
// wrapping, a blank line inside a fence, and what is *not* a heading.
func TestExample_LyraMdRendersBlocks(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "lyra-md")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "lyra-md", "lyra-md.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	render := func(t *testing.T, source string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "in.md")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, path).Output()
		if err != nil {
			t.Fatalf("rendering failed: %v", err)
		}
		return string(out)
	}

	for _, c := range []struct{ name, in, want string }{
		{"a heading", "# Title\n", "<h1>Title</h1>\n"},
		{"every level", "###### Six\n", "<h6>Six</h6>\n"},

		// **Seven is not a heading and neither is `#tag`**, which is CommonMark's rule and
		// the one a human writing a post wants: a hash that is part of a word is a word.
		{"seven hashes", "####### Seven\n", "<p>####### Seven</p>\n"},
		{"a hash with no space", "#tag\n", "<p>#tag</p>\n"},

		// A paragraph the author wrapped is one paragraph: where it was wrapped is not
		// content, so the lines join with a space rather than a newline.
		{"a wrapped paragraph", "one\ntwo\nthree\n", "<p>one two three</p>\n"},
		{"two paragraphs", "one\n\ntwo\n", "<p>one</p>\n<p>two</p>\n"},

		// Escaping is every character of text, everywhere, and `&` is the one a multi-pass
		// escaper gets wrong by running it after the others (`<` → `&lt;` → `&amp;lt;`).
		{"escaping", "a < b & c > d\n", "<p>a &lt; b &amp; c &gt; d</p>\n"},
		{"an entity is not special", "&amp;\n", "<p>&amp;amp;</p>\n"},

		// **A blank line inside a fence is content**, which is why the fence is tested
		// before the blank-line rule that ends every other block.
		{
			"a fence with a blank line",
			"```lyra\na\n\nb\n```\n",
			"<pre><code class=\"language-lyra\">a\n\nb</code></pre>\n",
		},
		{"a bare fence", "```\nx\n```\n", "<pre><code>x</code></pre>\n"},
		{"code is escaped too", "```\na < b\n```\n", "<pre><code>a &lt; b</code></pre>\n"},

		// A document that stops mid-block still ends it: no trailing newline, and an
		// unclosed fence renders as the code it looks like rather than vanishing.
		{"no trailing newline", "text", "<p>text</p>\n"},
		{"an unclosed fence", "```\na\n", "<pre><code>a</code></pre>\n"},

		// CRLF input must not leave an invisible byte at the end of every line — the
		// reason `lines` is in the prelude rather than at each call site.
		{"CRLF", "# Title\r\n\r\ntext\r\n", "<h1>Title</h1>\n<p>text</p>\n"},

		// Spans are the next slice, and until then their markers are literal text. Pinned
		// so that the day they render, this test is what says so.
		{"spans are not yet rendered", "**bold**\n", "<p>**bold**</p>\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := render(t, c.in); got != c.want {
				t.Errorf("rendered %q as\n%q\nwant\n%q", c.in, got, c.want)
			}
		})
	}

	// A missing file is reported against the name the user typed, and exits 1.
	out, err := exec.Command(bin, filepath.Join(t.TempDir(), "nope.md")).CombinedOutput()
	if err == nil {
		t.Errorf("a missing file exited 0")
	}
	if !strings.Contains(string(out), "cannot read") {
		t.Errorf("a missing file reported %q", out)
	}
}
