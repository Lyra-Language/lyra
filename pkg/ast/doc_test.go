package ast

import "testing"

func lines(ss ...string) []string { return ss }

func TestNewDoc_StripsMarkerAndOneSpace(t *testing.T) {
	d := NewDoc(lines("/// Adds two numbers.", "///", "///     indented example"), Location{}, false)
	if d == nil {
		t.Fatal("NewDoc returned nil for a doc with content")
	}
	want := "Adds two numbers.\n\n    indented example"
	if d.Text != want {
		t.Errorf("Text = %q, want %q", d.Text, want)
	}
}

func TestNewDoc_EmptyIsNil(t *testing.T) {
	// A `///` run with no content is not "documented with nothing" — it is
	// undocumented, so every consumer's nil check is the only check needed.
	if d := NewDoc(lines("///", "///   "), Location{}, false); d != nil {
		t.Errorf("NewDoc(empty) = %+v, want nil", d)
	}
}

func TestNewDoc_InnerMarker(t *testing.T) {
	d := NewDoc(lines("//! The module.", "//! Second line."), Location{}, true)
	if d == nil || d.Text != "The module.\nSecond line." {
		t.Fatalf("Text = %+v", d)
	}
	if !d.IsInner {
		t.Error("IsInner = false, want true")
	}
}

func TestDoc_SummaryIsFirstParagraph(t *testing.T) {
	d := NewDoc(lines(
		"/// Adds two numbers",
		"/// together.",
		"///",
		"/// This second paragraph is not the summary.",
	), Location{}, false)
	if got, want := d.Summary, "Adds two numbers together."; got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
}

func TestDoc_SummaryEmptyWhenDocOpensWithHeading(t *testing.T) {
	d := NewDoc(lines("/// # Panics", "/// Traps on overflow."), Location{}, false)
	if d.Summary != "" {
		t.Errorf("Summary = %q, want empty — a heading is not lead prose", d.Summary)
	}
}

func TestDoc_RecognizedSections(t *testing.T) {
	d := NewDoc(lines(
		"/// Divides two numbers.",
		"///",
		"/// # Panics",
		"///",
		"/// Traps when b is zero.",
		"///",
		"/// # Errors",
		"///",
		"/// Never — it traps instead.",
		"///",
		"/// # See also",
		"///",
		"/// checked_div",
	), Location{}, false)

	if len(d.Sections) != 3 {
		t.Fatalf("got %d sections, want 3: %+v", len(d.Sections), d.Sections)
	}
	if s, ok := d.Section(DocSectionPanics); !ok || s.Body != "Traps when b is zero." {
		t.Errorf("Panics section = %+v, ok=%v", s, ok)
	}
	if s, ok := d.Section(DocSectionErrors); !ok || s.Body != "Never — it traps instead." {
		t.Errorf("Errors section = %+v, ok=%v", s, ok)
	}
	if got := d.Sections[2]; got.Kind != DocSectionOther || got.Title != "See also" {
		t.Errorf("third section = %+v, want an Other titled %q", got, "See also")
	}
	// DocSectionOther matches nothing: several sections can share it, so "the Other
	// section" names no one section.
	if _, ok := d.Section(DocSectionOther); ok {
		t.Error("Section(DocSectionOther) returned a match; it must never match")
	}
}

func TestDoc_SingularHeadingsAreRecognized(t *testing.T) {
	d := NewDoc(lines("/// Sums.", "///", "/// # panic", "///", "/// On overflow."), Location{}, false)
	if _, ok := d.Section(DocSectionPanics); !ok {
		t.Errorf("`# panic` was not recognized as the Panics section: %+v", d.Sections)
	}
}

func TestDoc_HeadingInsideFenceIsNotASection(t *testing.T) {
	// A fence may hold any language, and `#` starts a comment in most of them. A
	// phantom section here would be a silent misclassification.
	d := NewDoc(lines(
		"/// Reads a line.",
		"///",
		"/// # Examples",
		"///",
		"/// ```",
		"/// # Errors are not a section here",
		"/// let line = read_line()",
		"/// ```",
	), Location{}, false)

	if len(d.Sections) != 1 {
		t.Fatalf("got %d sections, want 1: %+v", len(d.Sections), d.Sections)
	}
	if _, ok := d.Section(DocSectionErrors); ok {
		t.Error("a `#` line inside a fence was treated as an Errors section")
	}
	ex, ok := d.Section(DocSectionExamples)
	if !ok {
		t.Fatal("Examples section missing")
	}
	if want := "```\n# Errors are not a section here\nlet line = read_line()\n```"; ex.Body != want {
		t.Errorf("Examples body = %q, want %q", ex.Body, want)
	}
}

func TestJoinDocs_RederivesSummaryAndSections(t *testing.T) {
	a := NewDoc(lines("//! Maybe and its combinators."), Location{File: "maybe.lyra"}, true)
	b := NewDoc(lines("//! # Panics", "//!", "//! `unwrap` traps on None."), Location{File: "result.lyra"}, true)

	j := JoinDocs(a, b)
	if got, want := j.Summary, "Maybe and its combinators."; got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
	if _, ok := j.Section(DocSectionPanics); !ok {
		t.Error("the second doc's section did not survive the join")
	}
	if j.Location.File != "maybe.lyra" {
		t.Errorf("Location.File = %q, want the first contribution's file", j.Location.File)
	}
}

func TestJoinDocs_NilOperands(t *testing.T) {
	a := NewDoc(lines("/// A."), Location{}, false)
	if got := JoinDocs(nil, a); got != a {
		t.Error("JoinDocs(nil, a) should return a")
	}
	if got := JoinDocs(a, nil); got != a {
		t.Error("JoinDocs(a, nil) should return a")
	}
}

func TestDoc_SectionOnNilReceiver(t *testing.T) {
	var d *Doc
	if _, ok := d.Section(DocSectionPanics); ok {
		t.Error("Section on a nil Doc reported a match")
	}
}
