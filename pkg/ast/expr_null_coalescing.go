package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type NullCoalescingExpr struct {
	ExprBase
	Optional Expression
	Default  Expression
}

func (e *NullCoalescingExpr) exprNode() {}

func (e *NullCoalescingExpr) GetName() string {
	return "null_coalescing"
}

func (e *NullCoalescingExpr) GetType() types.Type {
	return nil
}

func (e *NullCoalescingExpr) Print(indent string) {
	fmt.Printf("%sNullCoalescingExpr {\n", indent)
	fmt.Printf("%s\tOptional: {\n", indent)
	e.Optional.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tDefault: {\n", indent)
	e.Default.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}