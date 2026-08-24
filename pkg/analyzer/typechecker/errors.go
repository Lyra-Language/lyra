package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

type TypeError struct {
	Location           ast.Location
	Severity           Severity
	Code               string
	Message            string
	Tags               []diag.Tag
	RelatedInformation []diag.RelatedInformation
}

func (e TypeError) Error() string {
	return e.Message
}

// Diagnostic is this error as the shared diagnostic type, which is what every consumer
// actually wants — the driver, the LSP and lyrac all publish diag.Diagnostic.
//
// The only thing that needs doing is the severity, because this package keeps its own
// two-valued Severity rather than importing diag's. That one mapping is the reason the
// driver used to copy all six fields across by hand; it lives here now, beside the two
// types it bridges, so a field added to TypeError does not silently fail to reach the
// output.
func (e TypeError) Diagnostic() diag.Diagnostic {
	sev := diag.SeverityError
	if e.Severity == SeverityWarning {
		sev = diag.SeverityWarning
	}
	return diag.Diagnostic{
		Severity:           sev,
		Code:               e.Code,
		Location:           e.Location,
		Message:            e.Message,
		Tags:               e.Tags,
		RelatedInformation: e.RelatedInformation,
	}
}
