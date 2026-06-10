package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

type Severity int

const (
	SeverityError   Severity = iota
	SeverityWarning
)

type TypeError struct {
	Location           ast.Location
	Severity           Severity
	Message            string
	Tags               []diag.Tag
	RelatedInformation []diag.RelatedInformation
}

func (e TypeError) Error() string {
	return e.Message
}
