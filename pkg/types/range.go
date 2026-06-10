package types

import "fmt"

type RangeType struct {
	Start       Type
	End         Type
	EndOperator string
	Step        Type
}

func (r RangeType) typeNode() {}
func (r RangeType) GetName() string {
	start := "<unknown>"
	if r.Start != nil {
		start = r.Start.String()
	}
	end := "<unknown>"
	if r.End != nil {
		end = r.End.String()
	}
	if r.Step == nil {
		return fmt.Sprintf("range(%s, %s)", start, end)
	}
	return fmt.Sprintf("range(%s, %s, step: %s)", start, end, r.Step.String())
}

func (r RangeType) String() string {
	return r.GetName()
}
