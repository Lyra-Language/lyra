package symbols

import (
	"maps"
	"slices"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// NoteFixedFormat records a `fixed<I, F>` the program wrote.
func (st *SymbolTable) NoteFixedFormat(format types.FixedFormat) {
	if st.fixedFormats == nil {
		st.fixedFormats = map[types.FixedFormat]bool{}
	}
	st.fixedFormats[format] = true
}

// FixedFormats answers the formats written so far, in a stable order. There are 116 possible
// formats and a program writes two or three, so what a prelude impl of `Ord` or `Show` would
// be for all of them is generated for the ones in the program (driver/fixed_impls.go).
func (st *SymbolTable) FixedFormats() []types.FixedFormat {
	return slices.SortedFunc(maps.Keys(st.fixedFormats), func(a, b types.FixedFormat) int {
		if a.Integer != b.Integer {
			return a.Integer - b.Integer
		}
		return a.Fraction - b.Fraction
	})
}
