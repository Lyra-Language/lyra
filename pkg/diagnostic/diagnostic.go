package diagnostic

import "github.com/Lyra-Language/lyra/pkg/ast"

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
)

// RelatedInformation points to a secondary source location that helps explain a diagnostic.
type RelatedInformation struct {
	Location ast.Location
	Message  string
}

type Diagnostic struct {
	Location           ast.Location
	Severity           Severity
	Message            string
	RelatedInformation []RelatedInformation
}

func (d Diagnostic) Error() string { return d.Message }
