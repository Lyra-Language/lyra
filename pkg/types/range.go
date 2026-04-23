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
	return fmt.Sprintf("range(%s, %s, %s)", r.Start.String(), r.End.String(), r.Step.String())
}

func (r RangeType) String() string {
	return r.GetName()
}
