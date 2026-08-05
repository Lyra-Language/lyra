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

// A range's end operator, decoded — the **only** place its four spellings are read.
//
// `<` `<=` ascend and `>` `>=` descend; `<` `>` exclude the end bound and `<=` `>=` include
// it. Two axes, four spellings, and every consumer wants one axis rather than the string.
//
// They are predicates rather than comparisons at each site because the sites are scattered
// and a missed one is silent: before descending ranges existed there were two spellings,
// every reader tested `== "<"`, and adding `>` would have made each of those tests quietly
// mean "ascending *and* exclusive". The `lyra-E032` note in `diagnostic/codes.go` records
// the last time this exact shape went wrong — an omitted operator read as inclusive at
// every one of those sites at once.

// RangeExcludesEnd reports whether the end bound is outside the range.
func RangeExcludesEnd(op string) bool { return op == "<" || op == ">" }

// RangeDescends reports whether the range counts downwards.
//
// Direction is a property of the *operator*, never of the bounds: `5..<1` is an ascending
// range that happens to be empty, not a descending one. That is what keeps the direction a
// parse-time fact, so a range with variable bounds cannot run the opposite way at run time
// from the way it reads.
func RangeDescends(op string) bool { return op == ">" || op == ">=" }
