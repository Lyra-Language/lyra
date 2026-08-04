package ast

import "github.com/Lyra-Language/lyra/pkg/types"

// OverloadSet is the several declarations one name has when it is **overloaded on its
// receiver**: two functions may share a name within a module when each takes a `self`
// first parameter and their receivers have different type heads, so the prelude can give
// `Maybe` and `Result` the same `unwrap_or` instead of inventing asymmetric names.
//
// It is a symbol, not syntax — the parser never produces one. A scope holds it in place
// of the single declaration a name usually has, which is the point: **anything that
// type-asserts a looked-up symbol to *VarDeclStmt now fails instead of silently taking
// one member.** Picking a member requires a receiver type, and a pass that has no
// receiver in hand has no business picking; a failed assertion sends it down its
// not-found path, where the worst case is a missing feature rather than a decision made
// against the wrong declaration. That is the same reasoning hazard 8 in the project's
// CLAUDE.md applies to a switch missing a case, run in the other direction: make the
// omission loud, on purpose.
//
// It lives in this package rather than in `symbols` because `Named` embeds `AstNode`,
// whose `node()` is unexported here — only a type declared alongside it can implement
// the interface a scope stores.
type OverloadSet struct {
	AstBase
	Name string
	// Members are in declaration order, which is the order a diagnostic lists the
	// candidates in — a set that reordered per run would produce diffs in golden
	// output for no reason.
	Members []*VarDeclStmt
}

func (o *OverloadSet) GetName() string { return o.Name }

// Add appends a declaration. The caller has already established that it belongs
// (OverloadableWith); this only records it and keeps the set's location covering the
// first member, so a diagnostic about the name as a whole points at where it was
// introduced.
func (o *OverloadSet) Add(decl *VarDeclStmt) {
	o.Members = append(o.Members, decl)
}

// Lambdas returns each member's function, for a caller that wants the signatures rather
// than the bindings.
func (o *OverloadSet) Lambdas() []*LambdaExpr {
	out := make([]*LambdaExpr, 0, len(o.Members))
	for _, m := range o.Members {
		if lam, ok := m.Value.(*LambdaExpr); ok {
			out = append(out, lam)
		}
	}
	return out
}

// ReceiverParam returns the `self` parameter a function opts into method syntax and
// overloading with, and whether it has one.
//
// **The one definition of "this function takes a receiver."** UFCS asks it to decide
// whether `x.f()` may resolve to `f`, and overload registration asks it to decide
// whether two same-named declarations may coexist; those two must agree, or a pair of
// functions could be admitted as an overload set that no call site can then dispatch
// between. The test is on the declared parameter and never on whether the function has
// clauses — a multi-clause function binds plain names in its head, so it can name one
// `self` like any other.
func ReceiverParam(fn *LambdaExpr) (*Parameter, bool) {
	if fn == nil || len(fn.Parameters) == 0 {
		return nil, false
	}
	recv := &fn.Parameters[0]
	if recv.Pattern == nil || recv.Pattern.GetName() != "self" {
		return nil, false
	}
	return recv, true
}

// ReceiverHead returns the type head a declaration would be overloaded under, and why it
// cannot be overloaded when it cannot.
//
// The returned reason is a fragment for the "already defined" diagnostic, phrased to say
// what would make the declaration admissible — a redeclaration is the reader's actual
// mistake in nearly every case, so the message leads with that and this explains why
// overloading did not rescue it.
func ReceiverHead(decl *VarDeclStmt) (head string, reason string) {
	lam, ok := declLambda(decl)
	if !ok {
		return "", "it is not a function"
	}
	recv, ok := ReceiverParam(lam)
	if !ok {
		return "", "its first parameter is not named `self`"
	}
	head, ok = types.HeadName(recv.Type)
	if !ok {
		return "", "its `self` parameter has no concrete type to dispatch on" +
			" (a type variable matches every receiver, so it cannot be one of several)"
	}
	return head, ""
}

// OverloadableWith reports whether decl may join set, and explains the refusal.
//
// Three conditions, each of which exists to keep resolution honest rather than to be
// strict for its own sake:
//
//   - **Both take a `self` receiver.** Without one there is nothing to dispatch on.
//   - **Their heads differ.** Two candidates matching one receiver would need a
//     specificity ordering to rank; refusing the overlap at the declaration is a fixed
//     error in one place instead of an ambiguity reported at every call site.
//   - **They agree on `pub`.** A set is one name to the rest of the program, and the
//     key it is stored under (declKey) is chosen from its visibility — a half-exported
//     set would be findable from another module for some receivers and not others.
func OverloadableWith(set *OverloadSet, decl *VarDeclStmt) (string, bool) {
	head, reason := ReceiverHead(decl)
	if reason != "" {
		return reason, false
	}
	for _, member := range set.Members {
		memberHead, memberReason := ReceiverHead(member)
		if memberReason != "" {
			return memberReason, false
		}
		if memberHead == head {
			return "both take a `" + head + "` receiver — overloads are told apart by" +
				" their receiver's type, so two of one type cannot be", false
		}
		if member.IsPublic != decl.IsPublic {
			return "they disagree on `pub` — an overloaded name is exported or private" +
				" as a whole, since a call resolves against every candidate", false
		}
	}
	return "", true
}

// declLambda returns the function a top-level binding binds, when it binds one.
func declLambda(decl *VarDeclStmt) (*LambdaExpr, bool) {
	if decl == nil {
		return nil, false
	}
	lam, ok := decl.Value.(*LambdaExpr)
	return lam, ok
}

// NewOverloadSet starts a set from the declaration a name already had, at that
// declaration's location.
func NewOverloadSet(first *VarDeclStmt) *OverloadSet {
	set := &OverloadSet{Name: first.Name, Members: []*VarDeclStmt{first}}
	set.Location = first.GetLocation()
	return set
}
