package typechecker

import "github.com/Lyra-Language/lyra/pkg/ast"

type Severity int

const (
	SeverityError   Severity = iota
	SeverityWarning
)

type TypeError struct {
	Location ast.Location
	Severity Severity
	Message  string
}

func (e TypeError) Error() string {
	return e.Message
}
