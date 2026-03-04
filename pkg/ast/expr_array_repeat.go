package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type ArrayRepeatExpr struct {
	ExprBase
	Value Expression // The value to repeat
	Count Expression // The count (compile-time constant)
}

func (a *ArrayRepeatExpr) exprNode() {}

func (a *ArrayRepeatExpr) GetName() string {
	return fmt.Sprintf("ArrayRepeatExpr(Value: %s, Count: %s)", a.Value.GetName(), a.Count.GetName())
}

func (a *ArrayRepeatExpr) Print(indent string) {
	fmt.Printf("%sArrayRepeatExpr {\n", indent)
	fmt.Printf("%s\tValue: {\n", indent)
	a.Value.Print(indent + "\t")
	a.Count.Print(indent + "\t")
	fmt.Printf("%s}\n", indent)
}

func (a *ArrayRepeatExpr) GetType() types.Type {
	var size int
	intLiteral, ok := a.Count.(*IntegerLiteralExpr)
	if ok {
		size = int(intLiteral.Value)
	} else {
		size = -1 // TODO: handle other types of count
	}

	return types.StaticArrayType{
		ElementType: a.Value.GetType(),
		Size:        size,
	}
}
