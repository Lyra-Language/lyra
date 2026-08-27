package ast

import "github.com/Lyra-Language/lyra/pkg/types"

// ExternDeclStmt is a foreign function: a signature with no body, and the effect bound its
// caller is asked to trust.
//
//	extern getpid: () -> i32
//	unsafe extern pure sqrt: (f64) -> f64
//
// **`unsafe` is about the bound, not the declaration.** An extern with none carries every
// effect and is safe to write; narrowing it asserts something no compiler can check — a
// wrong `pure` does not fail here, it silently corrupts the effect analysis of every
// caller. So `IsUnsafe` records the keyword and the checker requires it exactly when a
// bound is present.
type ExternDeclStmt struct {
	AstBase
	Name string
	// NameLocation spans just the name, for a diagnostic about the declaration rather
	// than about its signature. Tagged out of the printer like every auxiliary position.
	NameLocation Location `print:"-"`
	Signature    *types.LambdaType
	IsUnsafe     bool
	IsPure       bool
	IsDet        bool
	IsNoAlloc    bool
	// Links are the libraries `@link("m")` names, in source order. Collected here and
	// unioned across the program by the driver; see todo.md, Foreign functions.
	Links []string
	Doc   *Doc
	// fn caches Func()'s result. Unexported and reached only through it — see there for
	// why one instance per declaration is the invariant.
	fn *LambdaExpr
}

func (e *ExternDeclStmt) statementNode() {}

func (e *ExternDeclStmt) GetName() string { return e.Name }

// Func is the extern presented as the body-less function it is, so a call to one resolves,
// type-checks and is charged effects by the machinery every other call already goes
// through — SymbolTable.Functions, inferIdentifierCall, the purity fixpoint.
//
// This is TraitMethod.DefaultImpl()'s arrangement and it is here for the same reason: an
// extern is a signature standing in for a body someone else supplies, which is a shape the
// compiler already has. Building a second one would mean teaching every call path what an
// extern is, and each of those is a place the two could disagree about a call.
//
// **`Body == nil` is what marks it**, and every consumer that walks a body already tests
// for that — inferLambdaReturnType returns early, the purity walk finds nothing to charge
// (which is why an extern's effects come from its bound instead), and the backend has no
// statements to lower. A body-less lambda was already reachable before this existed, from
// a `let f: (i64) -> i64` with no value.
func (e *ExternDeclStmt) Func() *LambdaExpr {
	if e.fn != nil {
		return e.fn
	}
	lam := &LambdaExpr{
		ExprBase:  ExprBase{AstBase: AstBase{Location: e.GetLocation()}},
		IsExtern:  true,
		IsUnsafe:  true, // calling one always needs an `unsafe` context (lyra-E011)
		IsPure:    e.IsPure,
		IsDet:     e.IsDet,
		IsNoAlloc: e.IsNoAlloc,
	}
	if e.Signature != nil {
		lam.ReturnType = e.Signature.ReturnType
		lam.IsVariadic = e.Signature.IsVariadic
		for i, p := range e.Signature.Parameters {
			lam.Parameters = append(lam.Parameters, Parameter{
				AstBase: AstBase{Location: e.GetLocation()},
				// A foreign signature names no parameters — `(f64) -> f64` is all there
				// is — so they are numbered. Nothing reads these names: there is no body
				// to resolve them in, and a call site matches positionally.
				Pattern:      &IdentifierPattern{Name: externParamName(i)},
				Type:         p.Type,
				TypeModifier: p.Borrow,
			})
		}
	}
	e.fn = lam
	return lam
}

func externParamName(i int) string {
	return "arg" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
