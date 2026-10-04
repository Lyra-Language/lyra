package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// std.xml reads documents as Go's encoding/xml does: each element's name, its attributes
// in order, the text directly inside it (every piece of character data between its
// children, CDATA included, joined) and its children. The documents meet what a program
// writes — a declaration, a DOCTYPE with an internal subset, comments, processing
// instructions, both quotes, the five entities, decimal and hex character references,
// CDATA, self-closing tags, whitespace between children and non-ASCII text — and each must
// print exactly as Go's own reading of it. Malformed documents are refused, each saying
// why and on which line; and a prefix stays part of a name, since namespaces are not read.
func TestRun_XmlReadsWhatGoReads(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()

	var files []string
	var want []string
	add := func(name, doc string) {
		path := filepath.Join(dir, name+".xml")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
		want = append(want, goTree(t, name, doc))
	}
	refuse := func(name, doc, why string) {
		path := filepath.Join(dir, name+".xml")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
		want = append(want, "error: "+why)
	}

	add("tiled", `<?xml version="1.0" encoding="UTF-8"?>
<!-- written by Tiled -->
<map version="1.10" orientation="orthogonal" width="3" height="2" tilewidth="16">
 <tileset firstgid="1" source="meadow.tsx"/>
 <layer id="1" name='ground &amp; "grass"' width="3" height="2">
  <properties>
   <property name="priority" type="bool" value="true"/>
  </properties>
  <data encoding="csv">
1,2,3,
4,5,2147483654
</data>
 </layer>
 <objectgroup id="2" name="things">
  <object id="1" name="hero" type="player" x="12.5" y="40"/>
  <object id="2" gid="7" x="0" y="16" width="16" height="16"><ellipse/></object>
 </objectgroup>
</map>
`)
	add("entities", `<a t="&lt;&gt;&amp;&quot;&apos;" n="&#65;&#x263A;&#x42;">x &lt; y &amp;&amp; z&#10;é</a>`)
	add("cdata", `<s>before <![CDATA[<raw> & ]] stays]]> after<?pi inside?><!-- gone -->end</s>`)
	add("doctype", `<?xml version="1.0"?>
<!DOCTYPE note [
  <!ELEMENT note (#PCDATA)>
]>
<note
   to = "a"   from='b'
>text<b/>more<c></c></note>
<!-- trailing -->
`)
	add("unicode", "<café naïve=\"日本\">ü<ß/>\t</café>")
	add("nested", `<r><a><b><c d="1"/></b></a><a/><a>  </a></r>`)

	refuse("unclosed", "<a>\n<b>\n</a>", "line 3: </a> where </b> was expected")
	refuse("unknown-entity", "<a>\n&nbsp;</a>", "line 2: an unknown entity &nbsp;")
	refuse("unquoted", "<a x=1/>", "line 1: attribute x of <a> is not quoted")
	refuse("after-root", "<a/>\n<b/>", "line 2: something after the root element")
	refuse("empty", "  \n", "line 2: no element")
	refuse("never-closed", "<a><b/>", "line 1: <a> is never closed")
	refuse("comment", "<a><!-- no end</a>", "a comment is never closed")
	refuse("upper-x", "<a>&#X42;</a>", "line 1: an unknown entity &#X42;")

	var calls []string
	for _, f := range files {
		calls = append(calls, fmt.Sprintf("  show(%q)", f))
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.io.{ read_file }
import std.xml.{ Element, parse_xml }

let main = () -> void => {
`+strings.Join(calls, "\n")+`
  prefixed()
}

let show = (path: string) -> void => match parse_xml(read_file(path).unwrap_or("")) {
  Err(why) => println("error: ${why}"),
  Ok(e) => println(tree(e)),
}

/// What Go's test prints too: (E name A key value … T text children…).
let tree = pure (e: Element) -> string => {
  var out = "(E ${quoted(e.name)}"
  for a in e.attributes { out = "${out} A ${quoted(a.name)} ${quoted(a.value)}" }
  if e.text.len() > 0 { out = "${out} T ${quoted(e.text)}" }
  for c in e.children { out = "${out} ${tree(c)}" }
  "${out})"
}

let quoted = pure (s: string) -> string =>
  "\"" ++ s.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", "\\n").replace("\t", "\\t") ++ "\""

/// A prefix is part of the name; the accessors find by it.
let prefixed = () -> void => match parse_xml("<svg:g xlink:href=\"#a\"><svg:rect/></svg:g>") {
  Err(why) => println("prefixed: ${why}"),
  Ok(e) => println(
    "prefixed: ${e.name} ${e.attribute("xlink:href").unwrap_or("?")} ${e.children_named("svg:rect").len()} ${e.child("rect").is_some()}"
  ),
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	want = append(want, "prefixed: svg:g #a 1 false")
	got := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(want), stdout)
	}
	for i := range want {
		name := "prefixed"
		if i < len(files) {
			name = filepath.Base(files[i])
		}
		if why, isError := strings.CutPrefix(want[i], "error: "); isError {
			if !strings.HasPrefix(got[i], "error: ") || !strings.Contains(got[i], why) {
				t.Errorf("%s: got %q, want an error saying %q", name, got[i], why)
			}
			continue
		}
		if got[i] != want[i] {
			t.Errorf("%s:\n got %s\nwant %s", name, got[i], want[i])
		}
	}
}

// goTree is a document as the Lyra program prints it, read by encoding/xml: each element
// its name, attributes in order, its own character data joined, and its children.
func goTree(t *testing.T, name, doc string) string {
	t.Helper()
	type node struct {
		head     string
		text     strings.Builder
		children []string
	}
	quoted := func(s string) string {
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
		return `"` + r.Replace(s) + `"`
	}
	d := xml.NewDecoder(strings.NewReader(doc))
	var stack []*node
	var result string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: Go cannot read it: %v", name, err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			head := "(E " + quoted(tok.Name.Local)
			for _, a := range tok.Attr {
				head += " A " + quoted(a.Name.Local) + " " + quoted(a.Value)
			}
			stack = append(stack, &node{head: head})
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(tok)
			}
		case xml.EndElement:
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			out := n.head
			if n.text.Len() > 0 {
				out += " T " + quoted(n.text.String())
			}
			for _, c := range n.children {
				out += " " + c
			}
			out += ")"
			if len(stack) > 0 {
				stack[len(stack)-1].children = append(stack[len(stack)-1].children, out)
			} else {
				result = out
			}
		}
	}
	return result
}
