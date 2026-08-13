package docgen_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/docgen"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// analyze runs the real front end over a snippet with no prelude, which keeps a test's
// expected output to what the snippet itself declares.
func analyze(t *testing.T, src string) *driver.Result {
	t.Helper()
	t.Setenv("LYRA_NO_PRELUDE", "1")
	res := driver.Analyze([]byte(src))
	for _, d := range res.Errors() {
		t.Fatalf("snippet did not analyze cleanly: %s", d.Message)
	}
	return res
}

func collectOne(t *testing.T, src string, opts docgen.Options) docgen.Module {
	t.Helper()
	mods := docgen.Collect(analyze(t, src), opts)
	if len(mods) != 1 {
		t.Fatalf("got %d modules, want 1", len(mods))
	}
	return mods[0]
}

func findDecl(t *testing.T, m docgen.Module, name string) docgen.Decl {
	t.Helper()
	for _, d := range m.Decls {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no declaration named %q in %v", name, declNames(m))
	return docgen.Decl{}
}

func declNames(m docgen.Module) []string {
	out := make([]string, len(m.Decls))
	for i, d := range m.Decls {
		out[i] = d.Name
	}
	return out
}

// A signature is what a reader copies, so every one of these asserts the *exact* source
// spelling. The failure mode being guarded is a type printed with its diagnostic name —
// `DynamicArray<string>` for `[]string` — which is not a spelling the parser accepts.
func TestSignature_RendersSourceSyntax(t *testing.T) {
	cases := []struct {
		name string
		src  string
		decl string
		want string
	}{
		{
			name: "function with modifiers and generics",
			src:  "pub let unwrap_or<t> = pure noalloc (self: t, fallback: t) -> t => self",
			decl: "unwrap_or",
			want: "pub let unwrap_or<t> = pure noalloc (self: t, fallback: t) -> t",
		},
		{
			name: "dynamic array parameter and return",
			src:  "pub let ids = pure (xs: []string) -> []i64 => [1]",
			decl: "ids",
			want: "pub let ids = pure (xs: []string) -> []i64",
		},
		{
			name: "fixed-size array",
			src:  "pub let first = pure (xs: [3]i64) -> i64 => xs[0]",
			decl: "first",
			want: "pub let first = pure (xs: [3]i64) -> i64",
		},
		{
			name: "anonymous tuple",
			src:  "pub let pair = pure (p: (i64, string)) -> i64 => p.0",
			decl: "pair",
			want: "pub let pair = pure (p: (i64, string)) -> i64",
		},
		{
			name: "default argument",
			src:  "pub let at = pure (s: string, offset: i64 = 0) -> i64 => offset",
			decl: "at",
			want: "pub let at = pure (s: string, offset: i64 = 0) -> i64",
		},
		{
			name: "struct declaration",
			src:  "pub struct Point { x: f64, y: f64 }",
			decl: "Point",
			want: "pub struct Point",
		},
		{
			name: "data declaration keeps its constructors",
			src:  "pub data Shape = Circle(f64) | Empty",
			decl: "Shape",
			want: "pub data Shape = Circle(f64) | Empty",
		},
		{
			name: "type alias",
			src:  "pub type Index = i64",
			decl: "Index",
			want: "pub type Index = i64",
		},
		{
			name: "bool is spelled bool, not boolean",
			src:  "pub let yes = pure (a: bool) -> bool => a",
			decl: "yes",
			want: "pub let yes = pure (a: bool) -> bool",
		},
		{
			name: "trait",
			src:  "pub trait Show { show: (Self) -> string }",
			decl: "Show",
			want: "pub trait Show",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := collectOne(t, tc.src, docgen.Options{})
			if got := findDecl(t, m, tc.decl).Signature; got != tc.want {
				t.Errorf("signature =\n  %s\nwant\n  %s", got, tc.want)
			}
		})
	}
}

// A generic parameter's bounds may be written inline or in a trailing `where`, and the
// signature has to show them either way — a page that drops half the bounds in the
// language depending on the author's spelling is worse than one that shows none.
func TestSignature_BoundsFromEitherSpelling(t *testing.T) {
	for _, src := range []string{
		"pub trait Show { show: (Self) -> string }\npub let dump<t> where t: Show = pure (v: t) -> string => v.show()",
		"pub trait Show { show: (Self) -> string }\npub let dump<t: Show> = pure (v: t) -> string => v.show()",
	} {
		m := collectOne(t, src, docgen.Options{})
		got := findDecl(t, m, "dump").Signature
		if !strings.Contains(got, "where t: Show") {
			t.Errorf("signature lost its bound: %s", got)
		}
	}
}

func TestCollect_PrivateExcludedByDefault(t *testing.T) {
	src := "pub let shown = pure () -> i64 => 1\nlet hidden = pure () -> i64 => 2"

	m := collectOne(t, src, docgen.Options{})
	if names := declNames(m); len(names) != 1 || names[0] != "shown" {
		t.Errorf("default run documented %v, want only [shown]", names)
	}

	m = collectOne(t, src, docgen.Options{IncludePrivate: true})
	if names := declNames(m); len(names) != 2 {
		t.Errorf("--private run documented %v, want both", names)
	}
}

// An undocumented public declaration is listed with its signature rather than dropped:
// the signature is real information, and omitting it makes a page silently
// misrepresent the module's surface.
func TestCollect_UndocumentedIsListedAndCounted(t *testing.T) {
	m := collectOne(t, "/// Documented.\npub let a = pure () -> i64 => 1\npub let b = pure () -> i64 => 2", docgen.Options{})
	if len(m.Decls) != 2 {
		t.Fatalf("got %v, want both declarations listed", declNames(m))
	}
	cov := docgen.Measure([]docgen.Module{m})
	if cov.Documented != 1 || cov.Total != 2 {
		t.Errorf("coverage = %d/%d, want 1/2", cov.Documented, cov.Total)
	}
	if len(cov.Undocumented) != 1 || !strings.HasSuffix(cov.Undocumented[0], ".b") {
		t.Errorf("Undocumented = %v, want [b]", cov.Undocumented)
	}
}

// An impl's methods are deliberately not counted: the contract lives on the trait, so
// requiring a doc here produces a paragraph restating it.
func TestMeasure_ImplMethodsAreNotGaps(t *testing.T) {
	src := "/// A trait.\npub trait Show {\n  /// Renders self.\n  show: (Self) -> string\n}\n" +
		"/// Strings show as themselves.\nimpl Show for string { show = (self) => self }"
	m := collectOne(t, src, docgen.Options{})
	cov := docgen.Measure([]docgen.Module{m})
	if len(cov.Undocumented) != 0 {
		t.Errorf("Undocumented = %v, want none", cov.Undocumented)
	}
}

func TestCollect_MembersCarryTheirDocs(t *testing.T) {
	m := collectOne(t, "/// A point.\npub struct Point {\n  /// The x axis.\n  x: f64,\n  y: f64,\n}", docgen.Options{})
	d := findDecl(t, m, "Point")
	if len(d.Members) != 2 {
		t.Fatalf("got %d members, want 2", len(d.Members))
	}
	if d.Members[0].Doc == nil || d.Members[0].Doc.Summary != "The x axis." {
		t.Errorf("member x doc = %+v", d.Members[0].Doc)
	}
	if d.Members[1].Doc != nil {
		t.Errorf("member y should be undocumented, got %+v", d.Members[1].Doc)
	}
	if got, want := d.Members[0].Signature, "x: f64"; got != want {
		t.Errorf("member signature = %q, want %q", got, want)
	}
}

// Every generated signature must be a spelling the parser accepts.
//
// This is the promise a reference page makes — a reader copies a signature to write a
// call — and it is not one that reading the output verifies: `(mut self: Rng)` and
// `DynamicArray<string>` both look entirely plausible on a page and neither compiles.
// Round-tripping through the real parser is the only check that catches them.
func TestSignature_RoundTripsThroughTheParser(t *testing.T) {
	src := `
pub struct Rng { state: u64 }
/// Draws.
pub let next_u64 = det noalloc (self: mut Rng) -> u64 => self.state
pub let ids = pure (xs: []string, n: [3]i64, p: (i64, string), flag: bool = true) -> []i64 => [1]
pub trait Show { show: (Self) -> string }
impl Show for string { show = (self) => self }
pub data Shape = Circle(f64) | Empty
`
	m := collectOne(t, src, docgen.Options{})

	for _, d := range m.Decls {
		body := ""
		switch {
		case strings.HasPrefix(d.Signature, "impl "):
			body = " { }"
		case strings.Contains(d.Signature, "trait "):
			body = " { m: (Self) -> i64 }"
		case strings.Contains(d.Signature, "struct "):
			// The page shows a struct's head and lists its fields as members.
			body = " { f: i64 }"
		case strings.Contains(d.Signature, "->"):
			body = ` => panic("x")`
		}
		reparse := driver.Analyze([]byte("module m\n" + d.Signature + body + "\n"))
		for _, diagnostic := range reparse.Errors() {
			if strings.Contains(diagnostic.Message, "syntax error") {
				t.Errorf("signature does not parse: %s\n  %s", d.Signature, diagnostic.Message)
			}
		}
	}
}
