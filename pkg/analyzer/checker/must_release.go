package checker

import (
	"fmt"
	"sort"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckMustRelease flags a binding of a `@must_release(f)` type that goes out of scope
// with `f` never called on it (`lyra-W022`).
//
// # What the attribute means
//
// `@must_release(unload_sound) struct Sound { … }` says a `Sound` names a resource
// **something other than Lyra owns**, and that `unload_sound` is the call that gives it
// back. Lyra has no destructors and is not getting any — the reasoning is in
// COMPLETED.md (09/09), and it comes down to a destructor being a call at a point with
// no syntax, which a language whose effect bounds are *written* cannot afford. So the
// release call stays the program's to write, and this pass is what notices when it was
// not written.
//
// # The three states, and why a borrow is not a discharge
//
// Per binding: **held** from its declaration, **released** once the named function is
// called on it, **escaped** once its fate leaves this function. Only the second and
// third suppress the report, and the distinction between them and an ordinary call is
// the whole design:
//
//	let s = load_sound("blip.wav").unwrap_or_panic()
//	play_sound(s)      // a *borrow* — s is still ours, still held
//	unload_sound(s)    // the release — discharged
//
// Were any call to count, the commonest form of the bug — load it, play it, forget to
// unload it — would be the one shape that never reported. What tells the two apart is
// `own`, which the language already has and already means exactly this: an `own`
// parameter takes ownership, so passing to one is an escape, and every other mode is a
// borrow that leaves the obligation here. A caller wanting to hand a resource to a
// helper writes `own` on that helper's parameter, which is a claim the reader can see.
//
// # The analysis, and which way it errs
//
// Flow-sensitive per function body, mirroring CheckUseAfterMove's shape (it answers the
// mirror-image question and the two should be read together). Where they differ is the
// **join**, and it is not a detail: a use-after-move takes the union of its branches
// because moved-in-either is the conservative direction *there*. Here the report fires
// on the state that is left over, so the conservative direction is the intersection —
// a resource released down any one branch is treated as released. That deliberately
// misses `if ok { unload_sound(s) }` with no else.
//
// Under-reporting is the house rule for a new diagnostic (CheckUseAfterMove states it
// too), and it is the right bet twice over here: the alternative fires on early-return
// and match-arm code that is perfectly correct, and this is a *warning*, so the ceiling
// on a false positive is noise nobody can suppress — there is no `#[allow]` in this
// language.
//
// # Escape is the default, and that is deliberate
//
// A mention of a held binding **anywhere but a borrowing call argument escapes it** —
// returned, stored in a struct, put in an array, wrapped in a `Some`, captured, aliased
// by another `let`. There is no list of construction kinds here, and there must not be:
// rule 8's most expensive instance in this compiler was one omission across eight
// switches over the array-literal family, and a *whitelist* of the two positions that
// keep an obligation cannot suffer it. A node kind added tomorrow escapes by default,
// which is the direction that stays quiet rather than the one that invents a warning.
//
// It costs some precision — `s == other` and `"${s}"` escape a value they only read —
// and that is the same trade in the same direction.
//
// # What it does not cover
//
//   - **Only a `struct` carries the attribute.** That is where a C handle arrives, and
//     it is the shape raylib's `Sound`/`Wave` and every SDL handle take. A `data` type
//     is Lyra's own tagged union — marking one would be claiming a Lyra-managed value is
//     foreign — and a `newtype` (the plausible `newtype Fd = i32`) cannot carry an
//     attribute at all until the grammar's constrained_type rule takes an attribute_list.
//   - **Only a local binding.** A resource stored in a field, an array or a global has a
//     lifetime this pass cannot see the end of, so storing one is an escape.
func CheckMustRelease(
	program *ast.Program,
	symTable *symbols.SymbolTable,
	tt *typetable.TypeTable,
	mt *typetable.MethodTable,
) []diag.Diagnostic {
	if tt == nil || symTable == nil {
		return nil
	}
	c := &mustRelease{symTable: symTable, tt: tt, mt: mt}
	for _, stmt := range program.Statements {
		if vds, ok := stmt.(*ast.VarDeclStmt); ok {
			if lam, ok := vds.Value.(*ast.LambdaExpr); ok {
				c.lambda(lam)
				continue
			}
		}
		if s, ok := stmt.(ast.Statement); ok {
			// A top-level statement is not inside any function, so nothing here can
			// go out of scope before the program ends. Walk it for nested lambdas
			// and nothing else.
			c.stmt(heldState{}, s)
		}
	}
	sort.SliceStable(c.diagnostics, func(i, j int) bool {
		a, b := c.diagnostics[i].Location, c.diagnostics[j].Location
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartCol < b.StartCol
	})
	return c.diagnostics
}

type mustRelease struct {
	symTable    *symbols.SymbolTable
	tt          *typetable.TypeTable
	mt          *typetable.MethodTable
	diagnostics []diag.Diagnostic
}

// resource is one still-held binding: where it was acquired, the type that carries the
// obligation, the name of the function that discharges it, and — where it resolved — the
// declaration that name means.
//
// **The declaration is what a discharge is tested against, not the name** (rule 9): a
// name does not identify a function, and comparing text would let another module's
// same-named `unload_sound` discharge this one. The name is kept for the message, and
// as the fallback for a callee too dynamic to resolve.
type resource struct {
	loc       ast.Location
	typName   string
	release   string
	releaseFn *ast.LambdaExpr
	// aliasOf names the binding this one is a *view* of — a `match` arm's payload
	// unwrapped from a `Maybe` that a longer-lived binding still holds. Releasing
	// through the view discharges the binding it came from, because they are one
	// resource; the view itself carries no separate obligation. "" for an ordinary
	// binding that owns what it holds.
	aliasOf string
	// wrapped marks an obligation reached through a canonical wrapper — the `Sound`
	// inside a `Maybe<Sound>`. It changes the message and nothing else, because the
	// fix is a different one: `unload_sound(chime)` does not compile when `chime` is
	// a `Maybe`, and a diagnostic naming a call the reader cannot write is worse than
	// one naming none. This codebase has made that exact mistake before and wrote it
	// down (CLAUDE.md, trait default methods: the message advised `where Self: A`,
	// which is not syntax this language has).
	wrapped bool
}

// heldState maps a binding name to the resource it still holds. Absent means it holds
// none — released, escaped, or never a resource at all — so every operation that
// discharges an obligation is a delete and the report is what is left.
type heldState map[string]resource

func (s heldState) clone() heldState {
	out := make(heldState, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// mergeHeld is the join of two branch outcomes: **held in both** to still be held, which
// is the intersection. See the pass header — this is the opposite of mergeMoves' union,
// for the opposite reason, and flipping it turns every early-return into a false report.
func mergeHeld(a, b heldState) heldState {
	out := heldState{}
	for k, v := range a {
		if _, ok := b[k]; ok {
			out[k] = v
		}
	}
	return out
}

// lambda analyzes one function body from a fresh state, and reports what its body still
// holds at the end. A nested lambda is analyzed on its own, as CheckUseAfterMove does:
// whether a closure runs once, later or never is the question captures raise.
func (c *mustRelease) lambda(lam *ast.LambdaExpr) {
	run := func(body ast.Expression) {
		if body == nil {
			return
		}
		// A parameter arrives already owned by the caller, so it is not acquired here
		// and its release is the caller's business — the state starts empty.
		c.report(c.expr(heldState{}, body))
	}
	run(lam.Body)
	for i := range lam.LambdaClauses {
		run(lam.LambdaClauses[i].Body)
	}
}

// report emits one warning per binding left holding a resource.
func (c *mustRelease) report(st heldState) {
	names := make([]string, 0, len(st))
	for name := range st {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := st[name]
		fix := fmt.Sprintf("call `%s(%s)`", r.release, name)
		if r.wrapped {
			fix = fmt.Sprintf(
				"unwrap it and release the %s — `match %s { Some(v) => %s(v), None => {} }`",
				r.typName, name, r.release)
		}
		c.diagnostics = append(c.diagnostics, diag.Diagnostic{
			Location: r.loc,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeUnreleasedResource,
			Message: fmt.Sprintf(
				"%q holds "+article(r.typName)+" %s and goes out of scope without being released; %s. "+
					"%s is marked `@must_release(%s)` because it names a resource Lyra does not own — "+
					"there are no destructors, so nothing will release it for you. "+
					"If the value outlives this function, return it or pass it to an `own` parameter, "+
					"which is what says the obligation went with it",
				name, r.typName, fix, r.typName, r.release),
		})
	}
}

func (c *mustRelease) stmt(st heldState, s ast.Statement) heldState {
	switch v := s.(type) {
	case nil:
		return st

	case *ast.VarDeclStmt:
		if lam, ok := v.Value.(*ast.LambdaExpr); ok {
			// The same two acts the LambdaExpr case in `expr` performs, and they have
			// to be spelled again because this branch short-circuits past it: a
			// `let f = () => …` is by far the commonest way to write a closure, so
			// dropping the capture half here would exempt the usual spelling and
			// keep the rare one — a hole shaped exactly like the code people write.
			c.lambda(lam)
			c.capturedEscape(st, lam)
			delete(st, v.Name)
			return st
		}
		st = c.expr(st, v.Value)
		// A fresh binding of the name replaces whatever it held. If the old value was
		// a still-held resource this loses it — deliberately: rebinding over a live
		// resource is a leak this pass cannot attribute to a scope end, and reporting
		// it belongs to a separate diagnostic rather than to this one's message.
		delete(st, v.Name)
		if r, ok := c.resourceOf(v.Value); ok {
			r.loc = v.GetLocation()
			st[v.Name] = r
		}
		return st

	case *ast.VarReassignmentStmt:
		st = c.expr(st, v.Value)
		delete(st, v.Name)
		if r, ok := c.resourceOf(v.Value); ok {
			r.loc = v.GetLocation()
			st[v.Name] = r
		}
		return st

	case *ast.ExpressionStmt:
		return c.expr(st, v.Expression)

	case *ast.ReturnStmt:
		// `return s` hands the obligation to the caller, which the escape default in
		// `expr` already does — the bare name is walked like any other mention. No
		// special case here, and deliberately none: a second spelling of the same
		// rule is a second thing to keep in step.
		return c.expr(st, v.Value)

	case *ast.IfDestructuringStmt:
		return c.ifLet(st, &v.DestructuringStatement, v.Then, v.Else)
	case *ast.ElseDestructuringStmt:
		return c.letElse(st, &v.DestructuringStatement, v.Else)
	case *ast.DestructuringDeclStmt:
		// A plain `let (a, b) = …`. The same rule as `let … else` with no else: the
		// payload outlives the statement, so the obligation moves onto it where the
		// pattern binds exactly one name, and the enclosing block reports it.
		var u unwrapper
		st, u = c.beginUnwrap(st, v.Value)
		return c.bindPayload(st, u, v.Pattern, v.GetLocation())

	case *ast.TraitImplStmt:
		for i := range v.Methods {
			c.report(c.expr(heldState{}, v.Methods[i].Clause.Body))
		}
		return st

	case *ast.TraitDeclStmt:
		for i := range v.Methods {
			if d := v.Methods[i].DefaultMethod; d != nil {
				c.report(c.expr(heldState{}, d.Body))
			}
		}
		return st
	}

	ast.WalkStmtChildren(s, func(child ast.Statement) bool {
		st = c.stmt(st, child)
		return false
	}, func(e ast.Expression) bool {
		st = c.expr(st, e)
		return false
	})
	return st
}

func (c *mustRelease) expr(st heldState, e ast.Expression) heldState {
	switch v := e.(type) {
	case nil:
		return st

	case *ast.IdentifierExpr:
		// **The escape default.** A held binding reached here is being mentioned
		// somewhere that is not a borrowing call argument — `call` handles those
		// without routing them through this — so its fate leaves this pass's sight
		// and the obligation goes with it. See the header: a whitelist of the
		// positions that *keep* an obligation is what makes a new expression kind
		// safe by default instead of one more switch to keep in step.
		c.clear(st, v.Name)
		return st

	case *ast.MemberExpr:
		// **Reading part of a value is a borrow, not a handover.** `s.id` looks at the
		// resource; the resource itself is going nowhere, so the obligation stays.
		// Falling to the escape default here guts the feature for any resource with a
		// readable field — one `println("${w.frame_count}")` and a leaked `Wave` goes
		// unreported — which is how this was found, on a program written in the
		// if-let style against the real raylib bindings.
		//
		// A *partial move* (`let inner = s.handle`, where the field is itself a
		// resource) is a different question this pass does not model, and it
		// under-reports rather than guessing — `bareName` admits only a plain
		// identifier for the same reason.
		if _, isName := bareName(v.Object); isName {
			return st
		}
		return c.expr(st, v.Object)

	case *ast.IndexExpr:
		// The same rule for `xs[i]`: reading an element borrows the container.
		if _, isName := bareName(v.Object); !isName {
			st = c.expr(st, v.Object)
		}
		return c.expr(st, v.Index)

	case *ast.TupleIndexExpr:
		if _, isName := bareName(v.Object); isName {
			return st
		}
		return c.expr(st, v.Object)

	case *ast.LambdaExpr:
		// Analyzed on its own. Anything it captures escapes, which capturedEscape
		// records against the *enclosing* state below.
		c.lambda(v)
		c.capturedEscape(st, v)
		return st

	case *ast.FunctionCallExpr:
		return c.call(st, v)

	case *ast.BlockExpr:
		return c.block(st, v)

	case *ast.IfExpr:
		st = c.expr(st, v.Condition)
		return mergeHeld(c.expr(st.clone(), v.Then), c.expr(st.clone(), v.Else))

	case *ast.MatchExpr:
		return c.matchArms(st, v)

	case *ast.ForLoopExpr:
		if v.Init != nil {
			st = c.stmt(st, v.Init)
		}
		if v.Condition != nil {
			st = c.expr(st, *v.Condition)
		}
		st = c.loopBody(st, v.Body)
		if v.Post != nil {
			st = c.expr(st, *v.Post)
		}
		return st

	case *ast.ForInLoopExpr:
		st = c.expr(st, v.Iterable)
		return c.loopBody(st, v.Body)
	}

	ast.WalkExprChildren(e, func(s ast.Statement) bool {
		st = c.stmt(st, s)
		return false
	}, func(child ast.Expression) bool {
		st = c.expr(st, child)
		return false
	})
	return st
}

// block runs a block's statements and reports anything **declared inside it** that is
// still held when it ends.
//
// Scoping matters for a resource in a way it does not for a move: a binding declared in
// an `if` branch dies at that branch's brace, and the join above would drop it from the
// merged state — silently, since the intersection is what suppresses a false report.
// Reporting per block is what keeps that from swallowing a real one.
func (c *mustRelease) block(st heldState, b *ast.BlockExpr) heldState {
	before := make(map[string]bool, len(st))
	for name := range st {
		before[name] = true
	}
	for _, s := range b.Statements {
		st = c.stmt(st, s)
	}
	// A block's final statement may *be* the value it answers with — `{ let s = …; s }`
	// hands the resource out — and that needs nothing here: the tail is walked like
	// any other expression and the escape default in `expr` releases the claim.
	inner := heldState{}
	for name, r := range st {
		if !before[name] {
			inner[name] = r
			delete(st, name)
		}
	}
	c.report(inner)
	return st
}

// loopBody analyzes a loop body. A resource acquired in the body must be released in the
// body — the next iteration rebinds the name — so the body is a scope like any other and
// `block` does the reporting.
func (c *mustRelease) loopBody(st heldState, body *ast.BlockExpr) heldState {
	// The body may not run at all, so a release inside it cannot discharge something
	// acquired outside it: analyze it against a copy and keep the outer state.
	c.block(st.clone(), body)
	return st
}

// call walks a call, discharging a binding passed to its own release function and
// escaping one passed to an `own` parameter.
func (c *mustRelease) call(st heldState, e *ast.FunctionCallExpr) heldState {
	st = c.expr(st, e.Function)
	params, recv := c.calleeParams(e)
	calleeFn, calleeName := c.callee(e)

	// A `.`-call's receiver is parameter 0 and the arguments start at 1 (hazard 10).
	// UFCS never reaches here with a separate receiver — it desugars into Arguments —
	// so this is the `Trait::method` and dispatched-method shape.
	offset := 0
	if recv != nil {
		offset = 1
		if name, ok := bareName(recv); ok {
			c.discharge(st, name, calleeFn, calleeName, params, 0)
		} else {
			st = c.expr(st, recv)
		}
	}
	for i, arg := range e.Arguments {
		// A bare name goes to `discharge` and **not** through c.expr: this is the
		// one position that can leave an obligation in place, so routing it through
		// the walker would hit the escape default and the release could never be
		// seen. Anything else is an expression to walk, and a held binding inside
		// it escapes, as it should — `f(Some(s))` puts s somewhere this cannot see.
		if name, ok := bareName(arg); ok {
			c.discharge(st, name, calleeFn, calleeName, params, i+offset)
			continue
		}
		st = c.expr(st, arg)
	}
	return st
}

// discharge removes a binding from the held set when this argument position either
// releases it (the callee *is* its release function) or takes ownership of it (`own`).
func (c *mustRelease) discharge(
	st heldState,
	name string,
	calleeFn *ast.LambdaExpr,
	calleeName string,
	params []ast.Parameter,
	pos int,
) {
	r, held := st[name]
	if !held {
		return
	}
	released := false
	switch {
	case r.releaseFn != nil && calleeFn != nil:
		// Both resolved: compare declarations, so another module's same-named
		// function cannot discharge this obligation.
		released = calleeFn == r.releaseFn
	case calleeName != "" && calleeName == r.release:
		// One side did not resolve — a call through a local, a dispatched method.
		// Fall back to the spelling rather than reporting: an unrecognized call
		// shape must not manufacture a warning.
		released = true
	}
	if released {
		c.clear(st, name)
		return
	}
	if pos < len(params) && params[pos].TypeModifier == types.Own {
		delete(st, name)
	}
}

// clear discharges a binding **and whatever it is a view of**. One resource can be held
// under two names — a `match` arm's payload beside the binding it was unwrapped from — and
// the view's fate is the resource's fate whichever way it goes: released through the view,
// or escaped through it.
//
// The escape half is not a refinement but the commonest shape there is:
//
//	match sound_from_wave(w) { Some(snd) => Some(snd), None => None }
//
// hands the payload to the caller inside a fresh `Maybe`, so the binding it came from left
// too. Clearing only on release reported `examples/raylib/breakout.lyra` as leaking a
// sound it hands straight back.
func (c *mustRelease) clear(st heldState, name string) {
	r, held := st[name]
	delete(st, name)
	if held && r.aliasOf != "" {
		delete(st, r.aliasOf)
	}
}

// capturedEscape drops any held binding a nested lambda mentions. A closure may run
// later, elsewhere, or never, so whatever it captures has a fate this pass cannot see —
// the same reason CheckUseAfterMove analyzes a lambda body from a fresh state.
func (c *mustRelease) capturedEscape(st heldState, lam *ast.LambdaExpr) {
	if len(st) == 0 {
		return
	}
	onExpr := func(e ast.Expression) bool {
		if id, ok := e.(*ast.IdentifierExpr); ok {
			delete(st, id.Name)
		}
		return true
	}
	if lam.Body != nil {
		ast.WalkExpr(lam.Body, nil, onExpr)
	}
	for i := range lam.LambdaClauses {
		if b := lam.LambdaClauses[i].Body; b != nil {
			ast.WalkExpr(b, nil, onExpr)
		}
	}
}

// resourceOf reports whether an expression produces a value carrying an obligation, and
// what discharges it. The type is read from the TypeTable — the settled type, so a value
// reached through a generic or a `match` arm is recognized the same as a direct call.
func (c *mustRelease) resourceOf(e ast.Expression) (resource, bool) {
	if e == nil {
		return resource{}, false
	}
	t, ok := c.tt.Get(e)
	if !ok {
		return resource{}, false
	}
	name, hasHead := types.HeadName(t)
	if !hasHead || name == "" {
		return resource{}, false
	}
	decl, ok := c.symTable.LookupTypeFrom(name, e.GetLocation())
	if !ok || decl == nil {
		return resource{}, false
	}
	// **A `Maybe<Sound>` carries a `Sound`'s obligation**, and without this the pass
	// would be decorative: acquiring a foreign resource can fail, so every acquisition
	// in a binding module answers `Maybe<T>` — `load_sound`, `wave_from_memory`,
	// `sound_from_wave`. Tracking only a bare `Sound` meant the check stayed silent on
	// the exact shape it exists for, which is how it was caught: deleting an
	// `unload_sound` from `examples/raylib/breakout.lyra` produced no warning at all.
	//
	// Recognised by CanonicalKind rather than by the name `Maybe`, so a program's own
	// unrelated `Maybe` is not treated as a wrapper and the prelude's is found however
	// it was reached.
	if decl.MustRelease == "" && decl.CanonicalKind != "" {
		if inner, found := c.wrappedResource(t, e); found {
			return inner, true
		}
	}
	if decl.MustRelease == "" {
		return resource{}, false
	}
	// Resolved from the *type declaration's* location, not the use site: the release
	// function is named by the module that declared the type, and in a binding module
	// it is normally not exported. Looking it up from here would answer for whatever
	// the using module can see.
	releaseFn, _ := c.symTable.LookupFunctionFrom(decl.MustRelease, decl.GetLocation())
	return resource{typName: decl.Name, release: decl.MustRelease, releaseFn: releaseFn}, true
}

// callee resolves a call to the function declaration it reaches, and to the name it was
// written with. Either may be empty: a call through a local or a dispatched method may
// resolve to neither, which `discharge` treats as "not a release" rather than guessing.
func (c *mustRelease) callee(e *ast.FunctionCallExpr) (*ast.LambdaExpr, string) {
	var written string
	switch fn := e.Function.(type) {
	case *ast.IdentifierExpr:
		written = fn.Name
	case *ast.MemberExpr:
		written = fn.Property.Name
	}
	// The overload the typechecker picked comes first, for rule 9's reason: a
	// receiver-overloaded name maps to several declarations and only this says which.
	if fn, ok := c.tt.Callee(e); ok && fn != nil {
		return fn, written
	}
	if res, ok := c.mt.GetResolution(e); ok {
		if lam, err := res.Lambda(); err == nil && lam != nil {
			return lam, written
		}
	}
	if id, ok := e.Function.(*ast.IdentifierExpr); ok {
		// From the calling file: a private or prelude-shadowing declaration is keyed
		// by module, so an unqualified lookup would answer for another module's.
		fn, _ := c.symTable.LookupFunctionFrom(id.Name, e.GetLocation())
		return fn, written
	}
	return nil, written
}

// wrappedResource answers the obligation carried by a canonical wrapper's payload —
// the `Sound` inside a `Maybe<Sound>`. Every type argument is tried, so a
// `Result<Sound, string>` is found as readily as a `Maybe<Sound>`.
func (c *mustRelease) wrappedResource(t types.Type, e ast.Expression) (resource, bool) {
	pt, ok := t.(types.ParameterizedType)
	if !ok {
		return resource{}, false
	}
	for _, arg := range pt.TypeArguments {
		name, hasHead := types.HeadName(arg)
		if !hasHead || name == "" {
			continue
		}
		decl, ok := c.symTable.LookupTypeFrom(name, e.GetLocation())
		if !ok || decl == nil || decl.MustRelease == "" {
			continue
		}
		releaseFn, _ := c.symTable.LookupFunctionFrom(decl.MustRelease, decl.GetLocation())
		return resource{
			typName:   decl.Name,
			release:   decl.MustRelease,
			releaseFn: releaseFn,
			wrapped:   true,
		}, true
	}
	return resource{}, false
}

// unwrapping is the shared half of `match`, `if let` and `else let`: each is a construct
// whose pattern may pull a resource out of the wrapper a binding is holding.
//
// The shape they exist for is how the language gets at one:
//
//	match chime { Some(s) => unload_sound(s), None => {} }
//	if let Some(s) = chime { unload_sound(s) }
//
// The obligation is on `chime`, and the call that discharges it names `s` — a *different*
// binding, introduced by the pattern. So an alternative that destructures a held scrutinee
// has the payload name seeded with the same obligation, and the scrutinee's own claim moves
// into that alternative rather than staying behind. One still holding its payload at the
// end is reported there, which is the rule `block` applies one level out: a scope that
// acquires is a scope that must discharge.
//
// **The payload is seeded only when the pattern binds exactly one name**, which is the
// whole of how it decides *which* name receives the obligation. A pattern binding is an
// `IdentifierPattern` and not an expression, so the TypeTable has no per-name type to
// consult, and matching a pattern's shape against the scrutinee's type positionally would
// be a new structural walk over both — rule 8's family, with a missing case for every
// pattern kind added later. One name is unambiguous and covers every wrapper unwrap
// (`Some(v)`, `Ok(v)`, `Err(e)`), which is what these constructs are for. A multi-binding
// pattern falls through to the escape default and stays silent, which is the direction
// this pass errs in everywhere else.
type unwrapper struct {
	scrutinee string   // the binding being destructured; "" when it is not a bare name
	held      resource // what it holds
	isHeld    bool
}

// beginUnwrap reads the scrutinee without letting the escape default consume it.
//
// A bare-name scrutinee is deliberately **not** walked, for the reason `call` gives about
// its arguments: `expr` would delete the binding before any of this could read it, and the
// seeding below would be dead code. It was, until a test unwrapped a resource and forgot
// to release it and nothing fired.
func (c *mustRelease) beginUnwrap(st heldState, scrutinee ast.Expression) (heldState, unwrapper) {
	if name, isName := bareName(scrutinee); isName {
		held, isHeld := st[name]
		return st, unwrapper{scrutinee: name, held: held, isHeld: isHeld}
	}
	// Not a binding — and this is the shape the idiom actually takes, so it is not a
	// fallback: `let Some(v) = load_sound(p) else { return }` acquires and unwraps in
	// one statement, and the resource never sits in a binding of its own. Walk the
	// expression, then ask whether it produced an obligation. The empty `scrutinee`
	// name is what says there is no prior binding to take the claim away from.
	st = c.expr(st, scrutinee)
	if r, ok := c.resourceOf(scrutinee); ok {
		return st, unwrapper{held: r, isHeld: true}
	}
	return st, unwrapper{}
}

// arm runs one alternative — a `match` arm, or one branch of an `if let` — seeding the
// payload where `binds` says this alternative's pattern destructures the scrutinee, and
// reporting what the alternative failed to discharge.
func (c *mustRelease) arm(
	st heldState,
	u unwrapper,
	pat ast.Pattern,
	binds bool,
	run func(heldState) heldState,
) heldState {
	out := st.clone()
	var payload []string
	owning := u.scrutinee == ""
	if binds && u.isHeld && pat != nil {
		if bound := patternBoundNames(pat); len(bound) == 1 {
			payload = bound
			r := u.held
			r.loc = pat.GetLocation()
			// Inside the alternative the payload stands for the resource, so the
			// message names the direct call rather than the unwrap that just happened.
			r.wrapped = false
			if owning {
				// The scrutinee was a temporary — `if let Some(v) = load(…)` — so this
				// alternative is the only place the value is ever visible, and failing
				// to release it here is a leak with nowhere else to be reported.
				out[bound[0]] = r
			} else {
				// **A named binding keeps its own claim and the payload is a view of
				// it.** Releasing through the view discharges the binding; merely
				// *using* the view does not, because the binding outlives this
				// alternative and may be released later — which is what
				// `match held { Some(t) => draw(t), … }` does every frame.
				r.aliasOf = u.scrutinee
				out[bound[0]] = r
			}
		}
	}
	out = run(out)
	// A payload never reaches the join: an owning one is reported here, since nothing
	// outside can release it, and a view is simply dropped — its binding carries the
	// claim and answers for it at its own scope end.
	leaked := heldState{}
	for _, name := range payload {
		if r, still := out[name]; still {
			if owning {
				leaked[name] = r
			}
			delete(out, name)
		}
	}
	c.report(leaked)
	return out
}

func (c *mustRelease) matchArms(st heldState, v *ast.MatchExpr) heldState {
	st, u := c.beginUnwrap(st, v.Scrutinee)
	var merged *heldState
	for i := range v.MatchArms {
		arm := v.MatchArms[i]
		out := c.arm(st, u, arm.Pattern, true, func(s heldState) heldState {
			if g := arm.Guard; g != nil {
				s = c.expr(s, g.Condition)
			}
			return c.expr(s, arm.Body)
		})
		if merged == nil {
			merged = &out
			continue
		}
		joined := mergeHeld(*merged, out)
		merged = &joined
	}
	if merged == nil {
		return st
	}
	return *merged
}

// ifLet walks an `if let`: the scrutinee, then the two branches as alternatives, joined
// by intersection. The payload is in scope in `then` only — the branch that ran because
// the match succeeded — which is the branch that can release it and therefore the one
// that must.
//
// It exists rather than being covered by a DestructuringDeclStmt case for the reason
// CheckUseAfterMove's twin gives: these embed the declaration **by value**, so the walker
// never sees a `*ast.DestructuringDeclStmt` and that case never fires.
func (c *mustRelease) ifLet(
	st heldState,
	d *ast.DestructuringDeclStmt,
	then, els *ast.BlockExpr,
) heldState {
	st, u := c.beginUnwrap(st, d.Value)
	branch := func(b *ast.BlockExpr, binds bool) heldState {
		return c.arm(st, u, d.Pattern, binds, func(s heldState) heldState {
			if b == nil {
				return s
			}
			return c.expr(s, b)
		})
	}
	return mergeHeld(branch(then, true), branch(els, false))
}

// letElse walks `let Some(v) = m else { … }`, which is **not** a branch and must not be
// analyzed as one.
//
// It is Rust's `let … else`: the payload binds in the **enclosing** scope and lives on
// after the statement, while the else block is the diverging path that never sees it.
// Verified rather than assumed — `let Some(v) = opt() else { println("${v}") }` is
// `undefined identifier "v"`, and a use after the statement checks clean — because the
// obvious reading is the opposite one, and `CheckUseAfterMove`'s twin takes it: that pass
// binds the pattern's names in the *else* branch, which is where they are not.
//
// So the obligation moves to the payload and this scope's own end is what reports it,
// exactly as for an ordinary `let`. The else block runs against a copy: it diverges, so
// what it does is on a path where the payload was never bound, and it cannot discharge
// what the main path is still holding.
func (c *mustRelease) letElse(
	st heldState,
	d *ast.DestructuringDeclStmt,
	els *ast.BlockExpr,
) heldState {
	st, u := c.beginUnwrap(st, d.Value)
	if els != nil {
		c.block(st.clone(), els)
	}
	return c.bindPayload(st, u, d.Pattern, d.GetLocation())
}

// bindPayload moves a held scrutinee's obligation onto the single name its pattern binds,
// for the two constructs whose payload outlives the statement — `let … else` and a plain
// destructuring `let`. The one-name rule is the `arm` header's, for the same reason.
func (c *mustRelease) bindPayload(
	st heldState,
	u unwrapper,
	pat ast.Pattern,
	loc ast.Location,
) heldState {
	if !u.isHeld || pat == nil {
		return st
	}
	bound := patternBoundNames(pat)
	if len(bound) != 1 {
		return st
	}
	delete(st, u.scrutinee)
	r := u.held
	r.loc = loc
	r.wrapped = false
	st[bound[0]] = r
	return st
}

// calleeParams returns the callee's declared parameters and, for a `.`-call, the
// receiver filling parameter 0. Mirrors CheckUseAfterMove's helper of the same name,
// for the same reason: the borrow modes live on the resolved signature, and a trait
// method resolves through the MethodTable rather than by name.
func (c *mustRelease) calleeParams(e *ast.FunctionCallExpr) ([]ast.Parameter, ast.Expression) {
	if fn, ok := c.tt.Callee(e); ok && fn != nil {
		return fn.Parameters, nil
	}
	if id, ok := e.Function.(*ast.IdentifierExpr); ok {
		if fn, _ := c.symTable.LookupFunctionFrom(id.Name, e.GetLocation()); fn != nil {
			return fn.Parameters, nil
		}
	}
	res, ok := c.mt.GetResolution(e)
	if !ok {
		return nil, nil
	}
	lam, err := res.Lambda()
	if err != nil || lam == nil {
		return nil, nil
	}
	if member, isDot := e.Function.(*ast.MemberExpr); isDot {
		return lam.Parameters, member.Object
	}
	return lam.Parameters, nil
}

// bareName is the binding an expression names, and whether it names one at all. Only a
// plain identifier does: a field or an element is a *part* of something, and releasing
// through one is its own question rather than this one.
func bareName(e ast.Expression) (string, bool) {
	id, ok := e.(*ast.IdentifierExpr)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// article picks "a" or "an" for a type name. Crude on purpose — it reads the first letter
// and nothing else, which is right for every type name a program is likely to write and
// wrong for the handful that begin with a vowel letter but a consonant sound. "a Image"
// is the kind of thing a reader notices in a message they were already unhappy to see.
func article(typeName string) string {
	if typeName == "" {
		return "a"
	}
	switch typeName[0] {
	case 'A', 'E', 'I', 'O', 'U', 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}
