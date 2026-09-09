// Package abi classifies an aggregate for a C calling convention: how a struct or union
// is passed to, and returned from, a C function.
//
// **This is the piece struct-by-value was refused for**, and the reason it is its own
// package with its own differential test. Layout — where a field sits — has been right
// since the FFI landed and is proved against C by the fixture. *Passing* is a different
// question, LLVM does not answer it, and the three targets this project can reach
// disagree in ways that change the emitted signature and even its arity. The failure mode
// is the worst available: a wrong classification links cleanly and computes garbage.
//
// So nothing here is inferred from first principles. `abi_diff_test.go` asks **clang**
// what it lowers each shape to, for every target, and fails if this package disagrees —
// which is what "validated case-by-case against clang" in todo.md asks for.
package abi

// Target is a calling convention this package can classify for.
//
// It is deliberately *not* a full target triple: the ABI question is decided by the
// architecture and the platform's variant of it, and two triples that agree on those
// classify identically. `FromTriple` maps the triples lyrac can meet onto these.
type Target int

const (
	// Unknown is a target with no classifier. It is not an error here — the front end
	// turns it into lyra-E063, refusing struct-by-value rather than guessing, which is
	// what keeps a new platform a *refusal* instead of silent garbage.
	Unknown Target = iota
	// AArch64 is AAPCS64, which macOS and Linux agree on for everything this package
	// decides. (They differ on `alignstack` for an HFA parameter, which is an attribute
	// on the call rather than a type, and on nothing else measured.)
	AArch64
	// X86_64SysV is the System V AMD64 psABI — Linux, the BSDs and macOS on Intel.
	X86_64SysV
)

func (t Target) String() string {
	switch t {
	case AArch64:
		return "aarch64"
	case X86_64SysV:
		return "x86-64 sysv"
	}
	return "unknown"
}

// Leaf is one scalar inside a flattened aggregate: its offset from the aggregate's start,
// and what it is. Padding is absent — a hole is simply a gap between offsets, which is
// what both classifiers actually need to know.
type Leaf struct {
	Offset int
	Kind   LeafKind
	Bytes  int
}

// LeafKind is the only distinction either ABI draws between scalars: whether a value goes
// in an integer register or a floating-point one. A pointer is an integer here, which is
// what both psABIs say.
type LeafKind int

const (
	Integer LeafKind = iota
	Float            // f32
	Double           // f64
)

// Aggregate is a struct or union flattened for classification.
type Aggregate struct {
	Size  int
	Align int
	// Leaves are every scalar the aggregate contains, in offset order, with nested
	// structs and arrays flattened. A **union** contributes only its largest member's
	// leaves — see Classify.
	Leaves []Leaf
}

// PartKind is the shape of one piece of a Direct classification.
type PartKind int

const (
	// PartInt is an integer of Bits bits.
	PartInt PartKind = iota
	// PartFloat is `float`, PartDouble `double`.
	PartFloat
	PartDouble
	// PartFloatArray is `[N x float]`, PartDoubleArray `[N x double]` — AAPCS64's
	// homogeneous float aggregate.
	PartFloatArray
	PartDoubleArray
	// PartIntArray is `[N x i64]` — AAPCS64's coalesced non-HFA aggregate.
	PartIntArray
	// PartFloatVec2 is `<2 x float>` — SysV's packing of two floats in one SSE
	// eightbyte. A *vector* rather than an array, and the distinction is real: they
	// have different calling-convention meanings on this target.
	PartFloatVec2
)

// Part is one piece of a Direct classification — one LLVM parameter, or one member of the
// literal struct a multi-part value is returned in.
type Part struct {
	Kind  PartKind
	Bits  int // PartInt only
	Count int // the array kinds only
}

// Class is how one aggregate crosses.
type Class struct {
	// Indirect means memory: the caller allocates and passes a pointer (`byval` on
	// SysV), and a return is written through a caller-supplied `sret` pointer. Parts is
	// empty.
	Indirect bool
	// Parts are the values passed in registers. **More than one Part means more than
	// one LLVM parameter**, which is the thing that makes this reach call lowering
	// rather than declarations alone — on SysV a 16-byte struct is two arguments.
	Parts []Part
	// ReturnAsStruct means a multi-part *return* is an LLVM literal struct of Parts
	// rather than several values, since a function returns once. AAPCS64 returns an HFA
	// as the aggregate's own type, which this also covers.
	ReturnAsStruct bool
}

// Classify decides how agg crosses on target, in the given position.
//
// A **union** is classified from the same Aggregate a struct is: its size and alignment
// are its own, and its leaves are those of the member that reaches furthest. Neither psABI
// has a rule for unions as such — both classify by what the bytes *are*, and for a union
// that is the widest member's shape.
func Classify(t Target, agg Aggregate, isReturn bool) Class {
	switch t {
	case AArch64:
		return classifyAArch64(agg, isReturn)
	case X86_64SysV:
		return classifySysV(agg, isReturn)
	}
	// Unknown: the caller refuses rather than guesses.
	return Class{Indirect: true}
}
