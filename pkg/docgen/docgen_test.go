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
/// Foreign.
@link("m")
unsafe extern pure sqrt: (f64) -> f64
unsafe extern det noalloc fill: (^mut u8, u64) -> void
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
		case strings.Contains(d.Signature, "extern "):
			// An extern *is* a signature — there is no body to append, which is the
			// whole of what it is. Ahead of the `->` arm, which would otherwise give
			// it one and make the round-trip fail on a page that was correct.
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

// An operator-named trait method is rendered in the spelling an author writes —
// `(_/_)`, not `/`. Members go through `MethodName.Key()`, the same source-syntax rule
// the round-trip above enforces for declarations, one level down.
//
// It rendered as the bare `/` until 08/14, which is a line that does not compile on a
// page read as the code to write. Nothing caught it because every trait method in the
// standard library was an ordinary identifier until the prelude gained `Add`/`Sub`/
// `Mul`/`Div`, and for an identifier `GetName()` and `Key()` agree — so the bug needed a
// *new kind of declaration* to become visible, not a new code path.
func TestSignature_OperatorMethodsRenderInSourceSpelling(t *testing.T) {
	src := `
pub trait Div { (_/_): (Self, Self) -> Self }
pub trait Neg { (-_): (Self) -> Self }
pub struct Cents { c: i64 }
impl Div for Cents { (_/_) = (self, o) => Cents { c: self.c / o.c } }
`
	m := collectOne(t, src, docgen.Options{})

	want := map[string]string{"Div": "(_/_)", "Neg": "(-_)", "Div for Cents": "(_/_)"}
	seen := map[string]bool{}
	for _, d := range m.Decls {
		spelling, expected := want[d.Name]
		if !expected {
			continue
		}
		seen[d.Name] = true
		if len(d.Members) != 1 {
			t.Fatalf("%s: got %d members, want 1", d.Name, len(d.Members))
		}
		if got := d.Members[0].Name; got != spelling {
			t.Errorf("%s: member name %q, want %q", d.Name, got, spelling)
		}
		if !strings.HasPrefix(d.Members[0].Signature, spelling) {
			t.Errorf("%s: member signature %q should start with %q",
				d.Name, d.Members[0].Signature, spelling)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("declaration %q never appeared on the page", name)
		}
	}
}

// A trait method's rendered signature must parse, which the declaration-level round-trip
// does not cover: it checks `Decl.Signature` only, and a trait's methods are Members. That
// gap is what let the bare `/` spelling survive.
func TestSignature_TraitMembersRoundTripThroughTheParser(t *testing.T) {
	src := `
pub trait Div { (_/_): (Self, Self) -> Self }
pub trait Neg { (-_): (Self) -> Self }
pub trait Show { pure show: (Self) -> string }
`
	m := collectOne(t, src, docgen.Options{})

	for _, d := range m.Decls {
		if d.Kind != docgen.KindTrait {
			continue
		}
		for _, member := range d.Members {
			wrapped := "module m\ntrait T { " + member.Signature + " }\n"
			reparse := driver.Analyze([]byte(wrapped))
			for _, diagnostic := range reparse.Errors() {
				if strings.Contains(diagnostic.Message, "syntax error") {
					t.Errorf("member signature does not parse: %s\n  %s",
						member.Signature, diagnostic.Message)
				}
			}
		}
	}
}

// With UFCS there is no separate method declaration — `self` is the only thing that says
// `trim` belongs to `string` — so a flat alphabetical list hides which type each function
// is for. These pin the regrouping.
func TestPartition_GroupsMethodsByReceiver(t *testing.T) {
	src := `
pub struct Rng { state: u64 }
pub let next_u64 = det noalloc (self: mut Rng) -> u64 => self.state
pub let below = det noalloc (self: mut Rng, bound: i64) -> i64 => bound
pub let trim = pure (self: string) -> string => self
pub let rng_seeded = pure noalloc (seed: u64) -> Rng => Rng { state: seed }
`
	m := collectOne(t, src, docgen.Options{})
	rest, free, groups := m.Partition()

	if names := names(rest); len(names) != 1 || names[0] != "Rng" {
		t.Errorf("rest = %v, want just the struct", names)
	}
	if n := names(free); len(n) != 1 || n[0] != "rng_seeded" {
		t.Errorf("free = %v, want [rng_seeded] — a constructor takes no self", n)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (Rng, string)", len(groups))
	}
	// Case-insensitive display order: Rng before string.
	if groups[0].Receiver != "Rng" || groups[1].Receiver != "string" {
		t.Errorf("group order = %q, %q", groups[0].Receiver, groups[1].Receiver)
	}
	if n := names(groups[0].Decls); len(n) != 2 {
		t.Errorf("Rng methods = %v, want both", n)
	}
}

// `self: mut Rng` is a method on `Rng`. The borrow modifier is a fact about the method,
// not about the group, and a heading reading "Methods on `mut Rng`" would split a type's
// methods in two by whether each one mutates.
func TestPartition_BorrowModifierIsNotPartOfTheReceiver(t *testing.T) {
	src := `
pub struct Rng { state: u64 }
pub let draw = det noalloc (self: mut Rng) -> u64 => self.state
pub let peek = pure noalloc (self: Rng) -> u64 => self.state
`
	_, _, groups := collectOne(t, src, docgen.Options{}).Partition()
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 — `mut Rng` and `Rng` are one receiver: %+v", len(groups), groups)
	}
	if groups[0].Receiver != "Rng" {
		t.Errorf("Receiver = %q, want %q", groups[0].Receiver, "Rng")
	}
}

// The key is HeadName, which is an identity and not a rendering: a dynamic array keys as
// `[]` and must be *shown* as `[]t`.
func TestPartition_ReceiverKeyIsNotTheDisplayName(t *testing.T) {
	src := "pub let first = pure (self: []i64) -> i64 => self[0]"
	_, _, groups := collectOne(t, src, docgen.Options{}).Partition()
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if got, want := groups[0].Key, "[]"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := groups[0].Receiver, "[]i64"; got != want {
		t.Errorf("Receiver = %q, want %q — the key must not leak into the heading", got, want)
	}
}

// A generic receiver heads as nothing, exactly as it does for overloading: `self: t`
// accepts everything, so it names no group and stays a free function.
func TestPartition_GenericReceiverIsNotAGroup(t *testing.T) {
	src := "pub let id<t> = pure (self: t) -> t => self"
	_, free, groups := collectOne(t, src, docgen.Options{}).Partition()
	if len(groups) != 0 {
		t.Errorf("got %d groups, want none: %+v", len(groups), groups)
	}
	if len(free) != 1 {
		t.Errorf("free = %v, want the declaration to stay ungrouped", names(free))
	}
}

func TestRenderMarkdown_HeadsEachReceiverGroup(t *testing.T) {
	page := renderOne(t, `
pub struct Rng { state: u64 }
/// Draws.
pub let next_u64 = det noalloc (self: mut Rng) -> u64 => self.state
/// Trims.
pub let trim = pure (self: string) -> string => self
/// Builds one.
pub let rng_seeded = pure noalloc (seed: u64) -> Rng => Rng { state: seed }
`)
	for _, want := range []string{"## Functions", "## Methods on `Rng`", "## Methods on `string`"} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q:\n%s", want, page)
		}
	}
	// A method appears under its receiver, not in the free-function list.
	fns := strings.Index(page, "## Functions")
	rng := strings.Index(page, "## Methods on `Rng`")
	if !(fns < rng) {
		t.Errorf("free functions should precede the method groups")
	}
	if i := strings.Index(page, "### `next_u64`"); !(i > rng) {
		t.Errorf("next_u64 is not under its receiver's heading:\n%s", page)
	}
}

func names(decls []docgen.Decl) []string {
	out := make([]string, len(decls))
	for i, d := range decls {
		out[i] = d.Name
	}
	return out
}

// An `extern` reaches a page, under `--private` — which is the only way it can, there
// being no `pub extern` to write. Missing until 08/20: `declFor` switched over the
// top-level declaration kinds and had no case for one, so `lyrac doc --private` silently
// omitted every foreign declaration a module had.
//
// It gets a **kind of its own** rather than joining the ordinary functions, because the
// difference is what a reader of the page needs: calling one requires `unsafe`, its
// effect bound is asserted rather than checked, and it drags a `@link` requirement into
// every program that reaches it.
func TestCollect_ExternIsDocumented(t *testing.T) {
	src := `
/// Square root, from libm.
@link("m")
unsafe extern pure sqrt: (f64) -> f64
pub let ordinary = pure () -> i64 => 1
`
	m := collectOne(t, src, docgen.Options{IncludePrivate: true})

	var found *docgen.Decl
	for i := range m.Decls {
		if m.Decls[i].Name == "sqrt" {
			found = &m.Decls[i]
		}
	}
	if found == nil {
		t.Fatal("no `sqrt` declaration on the page")
	}
	if found.Kind != docgen.KindExtern {
		t.Errorf("kind = %v; want KindExtern", found.Kind)
	}
	if found.IsPublic {
		t.Error("an extern is always private — there is no `pub extern` to write")
	}
	// **The `@link` line is part of the signature.** Elsewhere an attribute is metadata
	// a reader can skip; here it is a build requirement riding the declaration, so a
	// page omitting it documents a function the reader cannot successfully call.
	want := "@link(\"m\")\nunsafe extern pure sqrt: (f64) -> f64"
	if found.Signature != want {
		t.Errorf("signature =\n%q\nwant\n%q", found.Signature, want)
	}
	if found.Doc == nil || found.Doc.Summary != "Square root, from libm." {
		t.Errorf("doc = %v; want the summary above the declaration", found.Doc)
	}
}
