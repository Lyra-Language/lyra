package types

// UnionType is a C union: one block of storage its members read several ways.
//
// **It is not a `data` type, and the difference is one machine word.** A `data` type is
// a *tagged* union — it carries a discriminant, which is what makes `match` on it safe.
// A C union is untagged: nothing records which member was last written, so reading any
// other one reinterprets whatever bytes are there. That is why a member read is
// `unsafe`, and why the two can never be the same declaration.
//
// It exists for the FFI. `SDL_Event` is a union, and so is every "tag plus payload" C
// API — the tag is a member, and the caller reads it first to learn which of the others
// is live. Lyra cannot check that discipline, which is precisely the claim `unsafe`
// stands for everywhere else in this language.
//
// **Every member sits at offset 0**; the size is the largest member's rounded up to the
// alignment, and the alignment is the largest member's. `SizeAndAlign` is where that is
// computed, and it is the one place that has to know a union is not a struct — get it
// wrong and the layout links cleanly and computes garbage, which is the FFI's worst
// failure mode.
//
// Members reuse StructField because a union member *is* a name and a type. The two
// fields that a struct field carries and a union member cannot are refused by the
// collector rather than modelled away: `readonly` (there is nothing to freeze when every
// member aliases the same bytes) and a default value (there is no one member to default).
type UnionType struct {
	Name          string
	Members       []StructField
	GenericParams []GenericType
}

func (UnionType) typeNode() {}

func (u UnionType) GetName() string { return u.Name }
func (u UnionType) String() string  { return u.GetName() }

// MemberByName finds a member by name. Written here rather than at each call site
// because four passes ask the same question — the typechecker's field access, the
// FFI-safety check, layout and the backend's member read.
func (u UnionType) MemberByName(name string) (StructField, bool) {
	for _, m := range u.Members {
		if m.Name == name {
			return m, true
		}
	}
	return StructField{}, false
}
