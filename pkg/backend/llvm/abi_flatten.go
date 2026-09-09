package llvm

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/abi"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Flattening a Lyra aggregate into the shape `pkg/abi` classifies: its size, its
// alignment, and every scalar leaf inside it at the offset the layout puts it.
//
// **The offsets must be the ones `SizeAndAlign` produces**, not ones computed a second
// way, because that is what the emitted `getelementptr`s and the fixture's `offsetof`
// agree on. So this walks with the same alignment arithmetic `aggregateSizeAndAlign`
// does — a field starts at `alignUp(runningSize, fieldAlign)` — and any divergence
// between the two is a wrong classification, which links cleanly and computes garbage.

// isCAggregate reports whether a type crosses the boundary as an aggregate, i.e. needs
// classification at all. A scalar and a raw pointer pass as themselves.
//
// **It resolves first**, because a named type in an `extern`'s signature arrives as an
// `UnresolvedType` — the signature is collected, not inferred — and asking the bare name
// answers "not an aggregate" for every struct a foreign function takes. That silently
// skipped the whole classifier and emitted `%Vec2` where clang expects `[2 x float]`,
// which is rule 8's shape at the boundary: it links and computes garbage.
func (l *lowerer) isCAggregate(t types.Type) bool {
	if t == nil {
		return false
	}
	switch types.StripNewtype(l.resolveForLayout(t)).(type) {
	case types.NamedStructType, types.AnonymousStructType, types.UnionType,
		types.StaticArrayType, types.TupleType:
		return true
	}
	return false
}

// flattenAggregate builds the abi.Aggregate for t.
func (l *lowerer) flattenAggregate(t types.Type) (abi.Aggregate, error) {
	resolved := l.resolveForLayout(t)
	size, align, ok := SizeAndAlign(resolved)
	if !ok {
		return abi.Aggregate{}, fmt.Errorf("llvm: cannot size %s for the C boundary", t)
	}
	var leaves []abi.Leaf
	if err := l.appendLeaves(resolved, 0, &leaves); err != nil {
		return abi.Aggregate{}, err
	}
	return abi.Aggregate{Size: size, Align: align, Leaves: leaves}, nil
}

// appendLeaves walks t, adding one Leaf per scalar at its absolute offset.
func (l *lowerer) appendLeaves(t types.Type, base int, out *[]abi.Leaf) error {
	switch v := types.StripNewtype(l.resolveForLayout(t)).(type) {
	case types.PrimitiveType:
		size, _, ok := SizeAndAlign(v)
		if !ok {
			return fmt.Errorf("llvm: cannot size %s for the C boundary", v)
		}
		*out = append(*out, abi.Leaf{Offset: base, Kind: primitiveLeafKind(v.Name), Bytes: size})
		return nil
	case types.RawPointerType:
		// A pointer is an INTEGER leaf in both psABIs.
		*out = append(*out, abi.Leaf{Offset: base, Kind: abi.Integer, Bytes: pointerSize})
		return nil
	case types.NamedStructType:
		return l.appendFieldLeaves(fieldTypes(v.Fields), base, out)
	case types.AnonymousStructType:
		return l.appendFieldLeaves(fieldTypes(v.Fields), base, out)
	case types.TupleType:
		return l.appendFieldLeaves(v.Elements, base, out)
	case types.StaticArrayType:
		es, ea, ok := SizeAndAlign(v.ElementType)
		if !ok {
			return fmt.Errorf("llvm: cannot size the element of %s", v)
		}
		stride := alignUp(es, ea)
		for i := 0; i < v.Size; i++ {
			if err := l.appendLeaves(v.ElementType, base+i*stride, out); err != nil {
				return err
			}
		}
		return nil
	case types.UnionType:
		// **A union contributes the member that reaches furthest.** Neither psABI has a
		// rule for unions as such — both classify by what the bytes *are* — and for a
		// union that is its widest member's shape. Taking every member would double-count
		// the same eightbyte and, worse, mix an SSE member with an INTEGER one that
		// aliases it, which classifies as INTEGER and puts a float in a general register.
		var widest types.Type
		widestSize := -1
		for _, m := range v.Members {
			ms, _, ok := SizeAndAlign(m.Type)
			if !ok {
				return fmt.Errorf("llvm: cannot size member %q of union %q", m.Name, v.Name)
			}
			if ms > widestSize {
				widest, widestSize = m.Type, ms
			}
		}
		if widest == nil {
			return fmt.Errorf("llvm: union %q has no members", v.Name)
		}
		return l.appendLeaves(widest, base, out)
	}
	return fmt.Errorf("llvm: %s has no C layout", t)
}

// appendFieldLeaves walks a field list, placing each at the offset the layout gives it.
func (l *lowerer) appendFieldLeaves(fields []types.Type, base int, out *[]abi.Leaf) error {
	off := 0
	for _, f := range fields {
		fs, fa, ok := SizeAndAlign(l.resolveForLayout(f))
		if !ok {
			return fmt.Errorf("llvm: cannot size field %s for the C boundary", f)
		}
		off = alignUp(off, fa)
		if err := l.appendLeaves(f, base+off, out); err != nil {
			return err
		}
		off += fs
	}
	return nil
}

// primitiveLeafKind is the register class a scalar belongs to. Only the float widths are
// SSE; everything else — including `bool` and `rune` — is INTEGER.
//
// **f16 is deliberately INTEGER.** Neither psABI's classification rules as implemented
// here cover half floats, and calling one SSE would be a guess; the differential test has
// no f16 shape to hold it honest, so it takes the conservative class. A C library passing
// `_Float16` by value in an aggregate is rare enough to wait for a shape in that test.
func primitiveLeafKind(name types.PrimitiveTypeName) abi.LeafKind {
	switch name {
	case types.Float32:
		return abi.Float
	case types.Float64:
		return abi.Double
	}
	return abi.Integer
}
