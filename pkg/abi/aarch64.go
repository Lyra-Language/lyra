package abi

// AAPCS64 — the ARM 64-bit procedure call standard, as macOS and Linux both implement it
// for everything decided here.
//
// Three rules, in order, and the first is the one that makes floats special:
//
//  1. A **homogeneous floating aggregate** (HFA) — every leaf the same float type, at most
//     four of them — travels in the float registers as `[N x float]`/`[N x double]`. It is
//     an HFA however deeply it nests, so `struct { Vector2 a; Vector2 b; }` of four floats
//     is one, which is exactly why raylib's `Rectangle` and `Camera2D` behave as they do.
//     **Size does not disqualify it**: `{double×3}` is 24 bytes and still travels in
//     registers, where a 24-byte integer struct would not.
//  2. Otherwise, an aggregate of **16 bytes or less** is coalesced into integer registers.
//  3. Otherwise it goes in memory: a pointer parameter, or an `sret` return.
//
// The one asymmetry, and it is measured rather than reasoned: a **parameter** rounds up to
// whole 64-bit units (`{u8,u8}` is passed as `i64`), while a **return** uses the smallest
// integer covering the size (`{u8,u8}` comes back as `i16`). Both were read off clang.
func classifyAArch64(agg Aggregate, isReturn bool) Class {
	if hfa, kind, n := homogeneousFloatAggregate(agg); hfa {
		part := Part{Kind: PartFloatArray, Count: n}
		if kind == Double {
			part.Kind = PartDoubleArray
		}
		// A returned HFA is the aggregate's own type in clang's output, which is the
		// same registers by another spelling; ReturnAsStruct is how the backend says it.
		return Class{Parts: []Part{part}, ReturnAsStruct: isReturn}
	}
	if agg.Size > 16 {
		return Class{Indirect: true}
	}
	if isReturn {
		if agg.Size > 8 {
			return Class{Parts: []Part{{Kind: PartIntArray, Count: 2}}}
		}
		return Class{Parts: []Part{{Kind: PartInt, Bits: returnIntBits(agg.Size)}}}
	}
	if agg.Size > 8 {
		return Class{Parts: []Part{{Kind: PartIntArray, Count: 2}}}
	}
	return Class{Parts: []Part{{Kind: PartInt, Bits: 64}}}
}

// returnIntBits is the width clang returns a small non-HFA aggregate in: the smallest
// power-of-two integer that covers the size. Measured — 2 bytes come back as `i16` and
// 4 as `i32`, where the *parameter* form of the same struct is an `i64`.
func returnIntBits(size int) int {
	switch {
	case size <= 1:
		return 8
	case size <= 2:
		return 16
	case size <= 4:
		return 32
	default:
		return 64
	}
}

// homogeneousFloatAggregate reports whether agg is an HFA, and of what.
//
// The rule is every leaf the same float kind, **at most four** of them, and no padding
// that would make the aggregate bigger than those leaves — the last is what stops a
// `struct { float a; double pad_forcing_hole; }` shape from qualifying by accident.
// A single float counts (`{float}` is passed as `[1 x float]`), which surprises but is
// what clang emits.
func homogeneousFloatAggregate(agg Aggregate) (bool, LeafKind, int) {
	if len(agg.Leaves) == 0 || len(agg.Leaves) > 4 {
		return false, Integer, 0
	}
	kind := agg.Leaves[0].Kind
	if kind != Float && kind != Double {
		return false, Integer, 0
	}
	for _, l := range agg.Leaves {
		if l.Kind != kind {
			return false, Integer, 0
		}
	}
	// No holes: an HFA's members are contiguous, so the leaves must exactly fill the
	// aggregate. A trailing pad would make the size larger than the members occupy.
	each := agg.Leaves[0].Bytes
	if agg.Size != each*len(agg.Leaves) {
		return false, Integer, 0
	}
	for i, l := range agg.Leaves {
		if l.Offset != i*each {
			return false, Integer, 0
		}
	}
	return true, kind, len(agg.Leaves)
}
