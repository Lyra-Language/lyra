package diagnostic

import "github.com/Lyra-Language/lyra/pkg/ast"

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
)

// Tag carries additional rendering metadata about a diagnostic, mirroring the
// LSP DiagnosticTag values so VS Code can grey-out or strike-through symbols.
type Tag int

const (
	// TagUnnecessary marks unused or unreachable code (greyed-out in VS Code).
	TagUnnecessary Tag = 1
	// TagDeprecated marks deprecated symbols (struck-through in VS Code).
	TagDeprecated Tag = 2
)

// RelatedInformation points to a secondary source location that helps explain a diagnostic.
type RelatedInformation struct {
	Location ast.Location
	Message  string
}

type Diagnostic struct {
	Location           ast.Location
	Severity           Severity
	Code               string
	Message            string
	Tags               []Tag
	RelatedInformation []RelatedInformation
}

func (d Diagnostic) Error() string { return d.Message }
