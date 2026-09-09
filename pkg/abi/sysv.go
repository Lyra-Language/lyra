package abi

// System V AMD64 — classification per **eightbyte**, which is the rule that makes this
// ABI reach call lowering rather than declarations alone: a 16-byte struct becomes *two*
// arguments, so the arity of the emitted call depends on the type.
//
// The psABI's algorithm, reduced to what an aggregate of 16 bytes or less needs:
//
//  1. Anything larger than 16 bytes is MEMORY — `byval` in, `sret` out.
//  2. Otherwise each eightbyte is SSE if every leaf overlapping it is a float or double,
//     and INTEGER otherwise. (The psABI's full rule merges classes pairwise; for the
//     shapes an FFI meets, "all float ⇒ SSE, else INTEGER" is the same answer, and the
//     differential test is what holds that claim honest.)
//  3. An INTEGER eightbyte is an integer wide enough for the bytes actually used in it —
//     `{i32,double}` passes its first eightbyte as `i32`, not `i64`.
//  4. An SSE eightbyte is `<2 x float>` for two floats, `float` for one, `double` for a
//     double.
func classifySysV(agg Aggregate, isReturn bool) Class {
	if agg.Size > 16 {
		return Class{Indirect: true}
	}
	if len(agg.Leaves) == 0 {
		return Class{Indirect: true}
	}
	var parts []Part
	for start := 0; start < agg.Size; start += 8 {
		end := min(start+8, agg.Size)
		parts = append(parts, classifyEightbyte(agg, start, end))
	}
	return Class{Parts: parts, ReturnAsStruct: isReturn && len(parts) > 1}
}

// classifyEightbyte decides one 8-byte chunk [start, end).
func classifyEightbyte(agg Aggregate, start, end int) Part {
	var floats, doubles, others int
	used := 0
	for _, l := range agg.Leaves {
		if l.Offset >= end || l.Offset+l.Bytes <= start {
			continue
		}
		switch l.Kind {
		case Float:
			floats++
		case Double:
			doubles++
		default:
			others++
		}
		if reach := l.Offset + l.Bytes - start; reach > used {
			used = reach
		}
	}
	if others == 0 && (floats > 0 || doubles > 0) {
		switch {
		case doubles > 0:
			return Part{Kind: PartDouble}
		case floats >= 2:
			return Part{Kind: PartFloatVec2}
		default:
			return Part{Kind: PartFloat}
		}
	}
	return Part{Kind: PartInt, Bits: eightbyteIntBits(used)}
}

// eightbyteIntBits is the integer width for an INTEGER eightbyte holding `used` bytes.
// Rounded up to a power of two, which is what clang emits: four used bytes are an `i32`,
// and anything past four is an `i64`.
func eightbyteIntBits(used int) int {
	switch {
	case used <= 1:
		return 8
	case used <= 2:
		return 16
	case used <= 4:
		return 32
	default:
		return 64
	}
}
