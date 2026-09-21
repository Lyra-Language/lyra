package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/lyra-md/site.lyra` renders a directory of Markdown into a directory of HTML,
// and it is the standard library's second probe — deliberately a different shape from
// `lyra-md`, which reads one file and writes to standard output and so needs almost
// nothing of `std`. Walking a tree, deciding where each output file goes and making the
// directories to put it in is **path handling**, and `std` had none: `std.path` and
// `std.io.create_dir_all` were written for this program, in that order, as it needed them.
//
// What the test pins is the tree, not the HTML — the HTML is `lyra-md`'s test, and the two
// programs share one renderer so that they cannot disagree about it. Here the questions
// are: does the output mirror the input's shape, are the directories made for it, is a
// page's title taken from its first heading, and is the walk the same walk `lyrafmt` does
// (sorted, skipping dotfiles, not following links into itself).
func TestExample_SiteRendersATree(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	bin := filepath.Join(t.TempDir(), "site")
	if _, stderr, code := captureRun(t, "build", "-o", bin,
		filepath.Join(root, "examples", "lyra-md", "site.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}

	source := t.TempDir()
	for _, dir := range []string{"guide", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"intro.md":  "# Getting Started\n\nWelcome to *Lyra*.\n",
		"plain.md":  "no heading here\n",
		"notes.txt": "not markdown\n",
		filepath.Join("guide", "deep.md"): "# The Guide\n\nSee the [intro](../intro.md), " +
			"the [missing](../nope.md), an [anchor](../intro.md#start), " +
			"[elsewhere](https://example.com/x.md) and [here](#here).\n",
		filepath.Join(".hidden", "skip.md"): "# Hidden\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The output directory does not exist yet: making it, and the `guide/` under it, is
	// half of what this program is for.
	out := filepath.Join(t.TempDir(), "does", "not", "exist")
	// **A broken link fails the build**, which is what makes this a checker as well as a
	// builder: a site whose links do not resolve is the thing a reader notices first.
	stdout, err := exec.Command(bin, source, out, "--title", "Lyra Docs").CombinedOutput()
	if err == nil {
		t.Errorf("a site with a broken link exited 0:\n%s", stdout)
	}
	if !strings.Contains(string(stdout), "guide/deep.md: ../nope.md goes nowhere") {
		t.Errorf("the broken link was not reported:\n%s", stdout)
	}

	// **Every `.md` and nothing else**, at the path it was written at, with `.html` for
	// `.md` — a dotted directory skipped and a `.txt` passed over.
	var got []string
	if err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(out, path)
			got = append(got, rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join("guide", "deep.html"), "index.html", "intro.html", "plain.html"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the output tree is\n  %v\nwant\n  %v", got, want)
	}

	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// A page's title is its first heading, and the site's name follows it.
	if page := read("intro.html"); !strings.Contains(page, "<title>Getting Started — Lyra Docs</title>") {
		t.Errorf("intro.html has no title from its heading:\n%s", page)
	}
	// A page with no heading is called by its file name, since the index needs a name for
	// it either way.
	if page := read("plain.html"); !strings.Contains(page, "<title>plain — Lyra Docs</title>") {
		t.Errorf("plain.html is not titled by its file name:\n%s", page)
	}
	// The index links every page, at the path it was written to, in the walk's order.
	index := read("index.html")
	for _, href := range []string{`href="guide/deep.html"`, `href="intro.html"`, `href="plain.html"`} {
		if !strings.Contains(index, href) {
			t.Errorf("the index does not link %s:\n%s", href, index)
		}
	}
	// The index is the one page whose title *is* the site's, and `X — X` is not a title.
	if !strings.Contains(index, "<title>Lyra Docs</title>") {
		t.Errorf("the index's title doubles the site's name:\n%s", index)
	}
	// **A link names the file the author wrote, and the reader needs the file that was
	// built.** Rewriting is what lets both be true, and it is the whole reason the program
	// keeps a set of its pages: a link is rewritten when it names one and reported when it
	// does not. The four that must survive untouched are as much the rule as the one that
	// changes — an anchor keeps its fragment, another site's `.md` is not ours to rewrite,
	// and a bare `#here` never left the page.
	deep := read(filepath.Join("guide", "deep.html"))
	for _, want := range []string{
		`href="../intro.html"`,            // rewritten to the built page
		`href="../nope.md"`,               // broken: left as written, so the author sees what they meant
		`href="../intro.html#start"`,      // the fragment survives the rewrite
		`href="https://example.com/x.md"`, // another site's page is not ours
		`href="#here"`,                    // an anchor on this page
	} {
		if !strings.Contains(deep, want) {
			t.Errorf("guide/deep.html has no %s:\n%s", want, deep)
		}
	}

	// The shared renderer did the body: this is `lyra-md`'s output, not a second one.
	if page := read("intro.html"); !strings.Contains(page, "<p>Welcome to <em>Lyra</em>.</p>") {
		t.Errorf("intro.html was not rendered by the shared renderer:\n%s", page)
	}
}
