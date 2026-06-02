package diagnostic

import "github.com/Lyra-Language/lyra/pkg/ast"

type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
)

type Diagnostic struct {
	Location ast.Location
	Severity Severity
	Message  string
}

func (d Diagnostic) Error() string { return d.Message }
