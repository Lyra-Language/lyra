package types

import "fmt"

type RangeType struct {
	Start       Type
	End         Type
	EndOperator string
	Step        Type
}

func (r RangeType) typeNode()           {}
func (r RangeType) IsNumericType() bool { return false }
func (r RangeType) GetName() string {
	return fmt.Sprintf("range(%s, %s, %s)", r.Start.GetName(), r.End.GetName(), r.Step.GetName())
}
func (r RangeType) Print(indent string) {
	fmt.Printf("%sRangeType {\n", indent)
	fmt.Printf("%s  Start: {\n", indent)
	r.Start.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  EndOperator: %s\n", indent, r.EndOperator)
	fmt.Printf("%s  End: {\n", indent)
	r.End.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	if r.Step != nil {
		fmt.Printf("%s  Step: {\n", indent)
		r.Step.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
