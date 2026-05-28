package regex

// byteSet is a compact 256-bit set over byte values. Used for character
// classes, where each bit indicates membership of the corresponding byte.
//
// All operations are O(1) (four word ops) and value-typed for cheap copy.
type byteSet [4]uint64

func (s *byteSet) add(b byte)     { s[b>>6] |= 1 << (b & 63) }
func (s byteSet) has(b byte) bool { return s[b>>6]>>(b&63)&1 == 1 }
func (s byteSet) isEmpty() bool   { return s[0]|s[1]|s[2]|s[3] == 0 }
func (s byteSet) isFull() bool {
	const full = ^uint64(0)
	return s[0] == full && s[1] == full && s[2] == full && s[3] == full
}

func (s byteSet) complement() byteSet {
	return byteSet{^s[0], ^s[1], ^s[2], ^s[3]}
}

func (s byteSet) union(o byteSet) byteSet {
	return byteSet{s[0] | o[0], s[1] | o[1], s[2] | o[2], s[3] | o[3]}
}

func (s byteSet) intersect(o byteSet) byteSet {
	return byteSet{s[0] & o[0], s[1] & o[1], s[2] & o[2], s[3] & o[3]}
}

// addRange inserts every byte b with lo <= b <= hi into the set.
func (s *byteSet) addRange(lo, hi byte) {
	for b := int(lo); b <= int(hi); b++ {
		s.add(byte(b))
	}
}

// fullByteSet returns a set containing every byte 0x00..0xFF.
func fullByteSet() byteSet {
	full := ^uint64(0)
	return byteSet{full, full, full, full}
}

// singletonByteSet returns a set containing only the byte b.
func singletonByteSet(b byte) byteSet {
	var s byteSet
	s.add(b)
	return s
}
