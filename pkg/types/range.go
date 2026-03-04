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
	fmt.Printf("%s\tStart: {\n", indent)
	r.Start.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tEndOperator: %s\n", indent, r.EndOperator)
	fmt.Printf("%s\tEnd: {\n", indent)
	r.End.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
	if r.Step != nil {
		fmt.Printf("%s\tStep: {\n", indent)
		r.Step.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
