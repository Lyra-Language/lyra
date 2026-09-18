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
// The test pins the rules a renderer gets wrong quietly rather than loudly: at the block
// level a paragraph's wrapping, a blank line inside a fence, and what is *not* a heading;
// at the span level what is *not* a delimiter, which is most of the difficulty of
// emphasis and all of the reason `snake_case_names` survives a paragraph intact.
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

		// --- inline spans (09/17) ------------------------------------------------
		{"emphasis", "*em*\n", "<p><em>em</em></p>\n"},
		{"strong", "**strong**\n", "<p><strong>strong</strong></p>\n"},
		{"underscores", "_em_ and __strong__\n", "<p><em>em</em> and <strong>strong</strong></p>\n"},
		{"a code span", "`x`\n", "<p><code>x</code></p>\n"},
		{"a link", "[t](/u)\n", "<p><a href=\"/u\">t</a></p>\n"},
		{"an image", "![a](/i.png)\n", "<p><img src=\"/i.png\" alt=\"a\" /></p>\n"},
		{"spans in a heading", "# *a* `b`\n", "<h1><em>a</em> <code>b</code></h1>\n"},

		// **A code span binds tightest**, so its content is text however it reads, and it
		// is still escaped — the two rules that make a post about markup safe to write.
		{"code beats emphasis", "`*x*`\n", "<p><code>*x*</code></p>\n"},
		{"code is escaped", "`a < b`\n", "<p><code>a &lt; b</code></p>\n"},

		// Nesting, in both directions. The link's text is rendered rather than escaped,
		// and emphasis recurses on a strictly shorter slice.
		{"emphasis inside a link", "[**b** t](/u)\n", "<p><a href=\"/u\"><strong>b</strong> t</a></p>\n"},
		{"code inside emphasis", "*a `b`*\n", "<p><em>a <code>b</code></em></p>\n"},

		// **A same-width closer wins.** Scanning for the first run that merely can close
		// makes the `**` after `b` close the opening `*`, which ends the emphasis mid
		// sentence and leaves the strong unpaired: `<em>a **b</em>* c*`.
		{"strong nested in emphasis", "*a **b** c*\n", "<p><em>a <strong>b</strong> c</em></p>\n"},

		// **A run with a space beside it cannot close** (CommonMark's flanking rule), and
		// a run is stepped over whole — entering one let the refused `**` close from its
		// second star, emitting `*</em>this`.
		{"an opening run does not close", "a *b and so does **this\n", "<p>a *b and so does **this</p>\n"},
		{"loose asterisks", "a * b * c\n", "<p>a * b * c</p>\n"},
		{"an unclosed delimiter is literal", "*a and `b\n", "<p>*a and `b</p>\n"},

		// **`_` is refused inside a word**, which this repo needs more than most: a
		// paragraph mentioning `snake_case_names` must not lose its underscores.
		{"intraword underscores", "snake_case_names\n", "<p>snake_case_names</p>\n"},

		// A backslash escapes ASCII punctuation, and a code span drops the one padding
		// space that let it hold a backtick at the edge.
		{"backslash escapes", "\\*not em\\*\n", "<p>*not em*</p>\n"},
		{"code span padding", "`` `x` ``\n", "<p><code>`x`</code></p>\n"},

		// An attribute is escaped where text is not: `\"` would otherwise end it.
		{"a url is attribute-escaped", "[t](/a?b=1&c=\"2\")\n",
			"<p><a href=\"/a?b=1&amp;c=&quot;2&quot;\">t</a></p>\n"},

		// A span that is not one stays literal rather than failing: a title is not
		// supported, so a URL with a space is not a link at all — and the quotes come
		// through as quotes, because **`"` is escaped in an attribute and not in text**.
		// That asymmetry is the whole reason `escape_attr` is a second function rather
		// than a flag: a quote in a sentence is a quote, and only an attribute value can
		// be ended by one.
		{"a link with a title is not a link", "[t](/u \"title\")\n", "<p>[t](/u \"title\")</p>\n"},
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
