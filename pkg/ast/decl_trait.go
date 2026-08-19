package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type TraitDeclStmt struct {
	AstBase
	Name          string
	NameLocation  Location `print:"-"` // span of just the trait name (Location covers the whole decl)
	GenericParams []GenericParam
	Bounds        []string
	Methods       []TraitMethod
	IsPublic      bool
	// Builtin is the kind named by a `@builtin(X)` attribute, before validation —
	// the *request*. Empty when unmarked. Resolved into CanonicalKind by the
	// collector's canonical pass, which is where the shape gate and the
	// duplicate-claim rule live.
	Builtin string
	// CanonicalKind is the compiler-known trait this declaration *is*, once
	// resolved: "Ord" (the trait `<`/`<=`/`>`/`>=`/`<=>` derive from) or "Eq"
	// (the override for `==`). Empty for an ordinary trait.
	//
	// It exists for the same reason TypeDeclStmt's does: the compiler has to know
	// these two by identity rather than by spelling, or a user's own `trait Ord`
	// is silently taken for the prelude's.
	CanonicalKind string
	// ShadowedCanonical records that this trait has a canonical kind's *name* while
	// a marker elsewhere claims that kind — so a diagnostic can say which of the two
	// mistakes was made. Mirrors TypeDeclStmt.ShadowedCanonical.
	ShadowedCanonical string
	// Doc is the `///` comment block above the trait declaration, nil if undocumented.
	Doc *Doc
}

func (t *TraitDeclStmt) statementNode() {}

func (t *TraitDeclStmt) GetName() string { return t.Name }

type TraitMethod struct {
	Name          MethodName
	Signature     *types.LambdaType
	DefaultMethod *LambdaClause
	// IsPure/IsDet/IsNoAlloc are effect bounds declared on the method in the
	// trait (`pure show: (Self) -> string`). They are a *contract*: every impl of
	// the method must satisfy the bound, and a call through a `where` bound to
	// this trait can rely on it.
	IsPure    bool
	IsDet     bool
	IsNoAlloc bool
	// Doc is the `///` comment block above the method's signature in the trait.
	// This is the *contract's* documentation — what any implementation must do —
	// which is why an impl's own method carries a separate one.
	Doc *Doc
	// defaultImpl caches DefaultMethod presented as the TraitMethodImpl it stands in
	// for. Unexported and reached only through DefaultImpl(), because **one instance
	// per trait method** is the invariant the rest of the compiler rests on.
	defaultImpl *TraitMethodImpl
}

func (t *TraitMethod) GetName() string {
	return t.Name.GetName()
}

// DefaultImpl is this method's default body presented as the impl method it stands in
// for — nil when the method declares no default.
//
// Every consumer of an impl method already handles a *TraitMethodImpl: dispatch, the
// MethodTable, the purity fixpoint, the ownership pass and the backend's emitted-method
// cache. A default is the same thing without an impl to sit in, so presenting it as one
// is what lets the feature reuse all of them instead of teaching each about a second
// shape.
//
// **The identity is the point, which is why this lives on the AST rather than in a
// per-pass cache.** Those consumers key on the pointer — the emitted-method cache, the
// per-specialization ownership table, the purity pass's effect map — so two passes
// building their own would disagree about whether they are looking at the same method,
// and the body would be emitted once per call site. One instance per trait method, *not*
// one per impl: the impl is what a specialization varies over (`Cat$Named$shout` beside
// `Dog$Named$shout`), so sharing this is exactly what makes the body shared and the
// specializations distinct — the arrangement a generic function already has.
//
// Must be called on an addressable trait method (`&trait.Methods[i]`), or the cache is
// written to a copy and every call hands back a fresh pointer.
func (t *TraitMethod) DefaultImpl() *TraitMethodImpl {
	if t.DefaultMethod == nil {
		return nil
	}
	if t.defaultImpl == nil {
		t.defaultImpl = &TraitMethodImpl{
			Name:      t.Name,
			IsPure:    t.IsPure,
			IsDet:     t.IsDet,
			IsNoAlloc: t.IsNoAlloc,
			Clause:    *t.DefaultMethod,
			Doc:       t.Doc,
		}
	}
	return t.defaultImpl
}

// MethodNameKind classifies a trait method name. Prefix vs binary matters for
// operators that share a spelling (e.g. "-" unary vs binary).
type MethodNameKind byte

const (
	MethodNameKindIdentifier MethodNameKind = iota
	MethodNameKindPrefix
	MethodNameKindSuffix
	MethodNameKindBinary
)

type MethodName struct {
	Kind  MethodNameKind
	Value string // identifier text or operator spelling
}

func NewMethodNameIdentifier(name string) MethodName {
	return MethodName{Kind: MethodNameKindIdentifier, Value: name}
}

func NewMethodNamePrefix(op PrefixOperator) MethodName {
	return MethodName{Kind: MethodNameKindPrefix, Value: string(op)}
}

func NewMethodNameSuffix(op SuffixOperator) MethodName {
	return MethodName{Kind: MethodNameKindSuffix, Value: string(op)}
}

func NewMethodNameBinary(op BinaryOperator) MethodName {
	return MethodName{Kind: MethodNameKindBinary, Value: string(op)}
}

func (m MethodName) GetName() string {
	return m.Value
}

// Key is the string that identifies this method within its trait, for a map keyed by
// name alone (`typetable.BoundMethodRef.Method`, the purity pass's impl groups).
//
// An identifier is its own key. An **operator** is spelled the way it is declared —
// `(_-_)` for binary, `(-_)` for prefix, `(_--)` for suffix — because kind is part of a
// method's identity: prefix `-` and binary `-` share a spelling and are different
// methods, so keying on `Value` alone would merge two groups and let one operator's
// effects be charged to the other.
func (m MethodName) Key() string {
	switch m.Kind {
	case MethodNameKindBinary:
		return "(_" + m.Value + "_)"
	case MethodNameKindPrefix:
		return "(" + m.Value + "_)"
	case MethodNameKindSuffix:
		return "(_" + m.Value + ")"
	}
	return m.Value
}

type PrefixOperator string

const (
	Negate     PrefixOperator = "-"
	Not        PrefixOperator = "!"
	BitwiseNot PrefixOperator = "~"
)

type SuffixOperator string

const (
	Increment SuffixOperator = "++"
	Decrement SuffixOperator = "--"
)

type BinaryOperator string

const (
	Equal              BinaryOperator = "=="
	NotEqual           BinaryOperator = "!="
	GreaterThan        BinaryOperator = ">"
	LessThan           BinaryOperator = "<"
	GreaterThanOrEqual BinaryOperator = ">="
	LessThanOrEqual    BinaryOperator = "<="
	Spaceship          BinaryOperator = "<=>"
	And                BinaryOperator = "&&"
	Or                 BinaryOperator = "||"
	Add                BinaryOperator = "+"
	Subtract           BinaryOperator = "-"
	Multiply           BinaryOperator = "*"
	Divide             BinaryOperator = "/"
	Modulus            BinaryOperator = "%"
	Power              BinaryOperator = "**"
	LeftShift          BinaryOperator = "<<"
	RightShift         BinaryOperator = ">>"
	BitwiseAnd         BinaryOperator = "&"
	BitwiseOr          BinaryOperator = "|"
	BitwiseXor         BinaryOperator = "^"
)
