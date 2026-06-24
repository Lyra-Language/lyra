package checker

import (
	"fmt"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestScratchHang2(t *testing.T) {
	src := `
struct Box {
	show: () -> string
}`
	tree, err := parser.Parse(src)
	fmt.Printf("parse err=%v\n", err)
	c := collector.NewCollector([]byte(src))
	_, _, _, errs := c.Collect(tree.RootNode())
	fmt.Printf("collector errs=%v\n", errs)
}
