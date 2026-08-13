package ast

import "strings"

// Doc is a documentation comment attached to a declaration.
//
// **Documentation attaches to declarations, not to types.** A struct field's doc
// belongs to the `struct` declaration that names the field (TypeDeclStmt.MemberDocs),
// not to the `types.NamedStructType` it builds — which is why an *anonymous* struct's
// field cannot carry one, and why nothing in pkg/types knows this type exists. Two
// structurally identical types must stay structurally equal (types.TypesEqual), and a
// field of prose is exactly the kind of thing that would quietly stop being true.
//
// The body is Markdown. There are no `@param`/`@returns` tags: the signature is already
// in the AST, so a tag restating it is a second copy that goes stale, and the one thing
// it could add — prose about a parameter — is a sentence in the body.
type Doc struct {
	// Text is the whole comment body: the `///` (or `//!`) markers and one following
	// space stripped from each line, lines joined with "\n", trailing blank lines
	// removed. This is what a renderer displays and what a doc generator emits.
	Text string
	// Summary is the first paragraph of Text — everything up to the first blank line,
	// with its own line breaks flattened to spaces. It is the one-line description a
	// module index or a completion item shows, and it is derived here rather than at
	// each of those call sites so they cannot disagree about where a summary ends.
	Summary string
	// Sections are the `#`-heading sections of Text, in source order, with the
	// recognized ones classified. Prose before the first heading is not a section —
	// it is Text's lead, of which Summary is the first paragraph.
	Sections []DocSection
	// Location spans the comment lines themselves, not the declaration they document.
	// A diagnostic about a doc comment (lyra-W017) has to point at the comment.
	Location Location `print:"-"`
	// IsInner records that this came from `//!` rather than `///`. Only a module doc
	// is inner, and the flag exists so the collector can report the two mistakes apart
	// — a `///` documenting nothing, and a `//!` somewhere that is not a file header.
	IsInner bool `print:"-"`
}

// DocSectionKind classifies a doc section's heading. Recognizing a fixed set is what
// lets a renderer give them a house style (and, later, lets a doc generator index every
// `# Panics` in the standard library) without turning the body into a sub-grammar: an
// unrecognized heading is still a heading, it is simply DocSectionOther.
type DocSectionKind int

const (
	DocSectionOther DocSectionKind = iota
	// DocSectionExamples holds runnable Lyra. Nothing compiles it yet; when
	// something does, this is the section it will look in.
	DocSectionExamples
	// DocSectionPanics documents the inputs on which the function traps —
	// `panic`, an overflowing `+`, a violated newtype constraint, a bad index.
	// In a language that traps by default this is the section most worth having.
	DocSectionPanics
	// DocSectionErrors documents what a `Result`-returning function returns `Err`
	// for. Distinct from Panics on purpose: the split is the language's own, since
	// a trap ends the program and an `Err` is a value the caller handles.
	DocSectionErrors
)

func (k DocSectionKind) String() string {
	switch k {
	case DocSectionExamples:
		return "Examples"
	case DocSectionPanics:
		return "Panics"
	case DocSectionErrors:
		return "Errors"
	default:
		return "Other"
	}
}

// DocSection is one `#`-heading section of a doc comment.
type DocSection struct {
	Kind DocSectionKind
	// Title is the heading text as written, so an unrecognized heading survives
	// rendering intact and a recognized one keeps the author's capitalization.
	Title string
	// Body is everything under the heading up to the next heading, trimmed.
	Body string
}

// recognizedSections maps a heading to its kind. Matching is case-insensitive and
// accepts the singular, because "# Panic" and "# Example" are what a hand writes and
// silently demoting either to DocSectionOther is the kind of near-miss that is never
// noticed — the section still renders, it just stops being indexed.
var recognizedSections = map[string]DocSectionKind{
	"examples": DocSectionExamples,
	"example":  DocSectionExamples,
	"panics":   DocSectionPanics,
	"panic":    DocSectionPanics,
	"errors":   DocSectionErrors,
	"error":    DocSectionErrors,
}

// NewDoc builds a Doc from the raw text of the comment lines, each still carrying its
// `///` or `//!` marker. Returns nil for a comment with no content at all, so an
// author's stray `///` does not attach an empty doc that a renderer then has to guard
// against — "documented" and "documented with nothing" are not worth distinguishing.
func NewDoc(lines []string, loc Location, isInner bool) *Doc {
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped = append(stripped, stripMarker(line))
	}
	text := strings.Trim(strings.Join(stripped, "\n"), "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &Doc{
		Text:     text,
		Summary:  docSummary(text),
		Sections: docSections(text),
		Location: loc,
		IsInner:  isInner,
	}
}

// JoinDocs concatenates two docs into one, separated by a blank line, and re-derives
// Summary and Sections from the joined text rather than merging the parts' own — a
// summary is "the first paragraph", which is a property of the result, and a heading in
// the second part is a section of the whole.
//
// It exists for multi-file modules: several files of one module may each carry a `//!`
// header, and the module's documentation is all of them.
func JoinDocs(a, b *Doc) *Doc {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	text := a.Text + "\n\n" + b.Text
	// The span runs from the first contribution to the last. They are in different
	// files, so it names a region no editor can show — but the File is the first
	// one's, which is where a reader looking for the module's docs should start.
	loc := a.Location
	return &Doc{
		Text:     text,
		Summary:  docSummary(text),
		Sections: docSections(text),
		Location: loc,
		IsInner:  a.IsInner,
	}
}

// stripMarker removes the leading `///` or `//!` and **one** following space.
//
// One space, not all leading whitespace: indentation inside a doc comment is
// meaningful — a fenced code block in `# Examples` is Lyra source, and Lyra is
// brace-delimited but still read by humans who indent it. Trimming every leading space
// would flatten an example's nesting, and trimming none would make every ordinary line
// start with the space that separates the marker from the prose.
func stripMarker(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(trimmed, "///"):
		trimmed = trimmed[3:]
	case strings.HasPrefix(trimmed, "//!"):
		trimmed = trimmed[3:]
	}
	return strings.TrimPrefix(trimmed, " ")
}

// docSummary returns the first paragraph with its line breaks flattened to spaces.
func docSummary(text string) string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		// A heading immediately after the lead means there is no lead prose at
		// all, so the summary is empty rather than the heading's own text.
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		out = append(out, strings.TrimSpace(line))
	}
	return strings.Join(out, " ")
}

// docSections splits text on its `#` headings. A heading inside a fenced code block is
// not a heading — `# comment` is not Lyra, but a fence may hold any language, and a
// shell or Python example's comment starting a phantom "Errors" section would be a
// silent misclassification rather than a visible one.
func docSections(text string) []DocSection {
	var sections []DocSection
	var current *DocSection
	var body []string
	inFence := false

	flush := func() {
		if current != nil {
			current.Body = strings.Trim(strings.Join(body, "\n"), "\n")
			sections = append(sections, *current)
		}
		body = nil
	}

	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(trimmed, "#") {
			flush()
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			current = &DocSection{Kind: recognizedSections[strings.ToLower(title)], Title: title}
			continue
		}
		if current != nil {
			body = append(body, line)
		}
	}
	flush()
	return sections
}

// Section returns the first section of the given kind, and whether there was one.
// DocSectionOther is never matched — several sections may share it, so "the Other
// section" names nothing.
func (d *Doc) Section(kind DocSectionKind) (DocSection, bool) {
	if d == nil || kind == DocSectionOther {
		return DocSection{}, false
	}
	for _, s := range d.Sections {
		if s.Kind == kind {
			return s, true
		}
	}
	return DocSection{}, false
}
