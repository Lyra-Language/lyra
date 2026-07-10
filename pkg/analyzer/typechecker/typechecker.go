package typechecker

import (
	"fmt"
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/regex"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

type TypeChecker struct {
	symTable      *symbols.SymbolTable
	scopeTable    *symbols.ScopeTable
	typeTable     *typetable.TypeTable
	methodTable   *typetable.MethodTable
	scope         *symbols.Scope
	errors        []TypeError
	paramTypes    map[string]types.Type         // non-nil only while checking a function body
	paramMods     map[string]types.TypeModifier // ref/mut/own modifier per parameter, alongside paramTypes
	resolvedTypes map[string]types.Type         // cache for resolveType to avoid duplicate "unknown type" errors
	enclosingRet  *types.ReturnType             // declared return type of the lambda body currently being checked; nil at top level
	traitImpls    []*ast.TraitImplStmt          // every impl block in the program, collected up front by Check; see resolveTraitMethod
	genericBounds map[string][]string           // type-parameter name -> trait bounds in scope (from an impl's `where` clause) while checking its method bodies; see dispatchViaGenericBound
}

func New(symTable *symbols.SymbolTable, scopeTable *symbols.ScopeTable, typeTable *typetable.TypeTable) *TypeChecker {
	return &TypeChecker{
		symTable:      symTable,
		scopeTable:    scopeTable,
		typeTable:     typeTable,
		methodTable:   typetable.NewMethodTable(),
		scope:         symTable.GlobalScope,
		resolvedTypes: make(map[string]types.Type),
	}
}

// MethodTable exposes the call-site -> resolved-trait-method mapping built
// during Check, for passes that run after typechecking (e.g. the purity
// checker) and need to know which method body a given call dispatches to.
func (tc *TypeChecker) MethodTable() *typetable.MethodTable {
	return tc.methodTable
}

// enterScope temporarily sets tc.scope to the scope recorded for node,
// then restores the previous scope. If node has no recorded scope (e.g. it
// was collected before ScopeTable was introduced) the call is a no-op.
func (tc *TypeChecker) enterScope(node ast.AstNode, fn func()) {
	scope, ok := tc.scopeTable.Get(node)
	if !ok {
		fn()
		return
	}
	old := tc.scope
	tc.scope = scope
	fn()
	tc.scope = old
}

func (tc *TypeChecker) Check(program *ast.Program) []TypeError {
	// Collected up front (not lazily) so a method call site can dispatch
	// against an impl declared later in the same file — Lyra has no
	// declare-before-use requirement for top-level type/trait/impl blocks.
	for _, stmt := range program.Statements {
		if impl, ok := stmt.(*ast.TraitImplStmt); ok {
			tc.traitImpls = append(tc.traitImpls, impl)
		}
	}
	for _, stmt := range program.Statements {
		tc.checkNode(stmt)
	}
	return tc.errors
}

func (tc *TypeChecker) checkNode(node ast.AstNode) {
	// Guard against both untyped nils and typed nils (e.g. (*ast.ExpressionStmt)(nil)
	// stored in an ast.AstNode interface — a common Go gotcha when sub-functions return
	// a concrete nil pointer that gets wrapped in the interface).
	if node == nil {
		return
	}
	if rv := reflect.ValueOf(node); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch n := node.(type) {
	case *ast.TypeDeclStmt:
		tc.checkTypeDecl(n)
	case *ast.VarDeclStmt:
		tc.checkVarDecl(n)
	case *ast.DestructuringDeclStmt:
		tc.checkDestructuringDecl(n)
	case *ast.IfDestructuringStmt:
		tc.checkIfDestructuringStmt(n)
	case *ast.ElseDestructuringStmt:
		tc.checkElseDestructuringStmt(n)
	case *ast.VarReassignmentStmt:
		tc.checkVarReassignment(n)
	case *ast.ExpressionStmt:
		tc.checkExpressionStmt(n)
	case *ast.DerefAssignmentStmt:
		tc.checkDerefAssignment(n)
	case *ast.LValueAssignmentStmt:
		tc.checkLValueAssignment(n)
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(n)
	case *ast.TraitImplStmt:
		tc.checkTraitImpl(n)
	}
}

func (tc *TypeChecker) checkTypeDecl(decl *ast.TypeDeclStmt) {
	switch decl.Type.(type) {
	case types.NamedStructType:
		tc.checkStructDecl(decl)
	case *types.ConstrainedType:
		tc.checkConstrainedTypeDecl(decl)
	}
}

// checkConstrainedTypeDecl validates the constraints on a constrained-type
// declaration. Currently this means compiling every PatternConstraint regex
// at type-declaration time so users see syntax errors immediately.
func (tc *TypeChecker) checkConstrainedTypeDecl(decl *ast.TypeDeclStmt) {
	ct := decl.Type.(*types.ConstrainedType)
	for _, c := range ct.Constraints {
		pc, ok := c.(*types.PatternConstraint)
		if !ok {
			continue
		}
		body := regexPatternBody(pc.Pattern)
		if _, err := regex.Compile(body); err != nil {
			tc.addError(decl.GetLocation(), SeverityError,
				"type %s: invalid pattern constraint %s: %s",
				ct.Name, pc.Pattern, err)
		}
	}
}

// regexPatternBody strips the r/…/ delimiters from a PatternConstraint.Pattern
// value.  The grammar stores the full regex-literal text (e.g. r/[0-9]+/);
// regex.Compile expects just the inner body ([0-9]+).
func regexPatternBody(p string) string {
	if len(p) >= 3 && p[:2] == "r/" && p[len(p)-1] == '/' {
		return p[2 : len(p)-1]
	}
	return p // already stripped or bare pattern string
}

func (tc *TypeChecker) checkStructDecl(decl *ast.TypeDeclStmt) {

}

func (tc *TypeChecker) checkExpressionStmt(n *ast.ExpressionStmt) {
	switch e := n.Expression.(type) {
	case *ast.MathAssignOpExpr:
		tc.checkMathAssignOp(e)
	case *ast.BooleanLiteralExpr:
		tc.checkBooleanLiteralExpr(e)
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(e)
	case *ast.NotBooleanExpr:
		tc.checkNotBooleanExpr(e)
	case *ast.StringConcatExpr:
		tc.inferStringConcatExpr(e)
	case *ast.FunctionCallExpr:
		// inferExprType also handles type-conversion calls (e.g. f32(x)); for an
		// ordinary call it resolves to the callee's return type, which the
		// must-use check then inspects for a silently-dropped Result/Maybe.
		tc.checkMustUseResult(e, tc.inferExprType(e))
	case *ast.TryExpr:
		// `foo()?` propagates the error and yields the success payload; flag only
		// when that payload is itself an unhandled Result/Maybe (a nested one).
		tc.checkMustUseResult(e, tc.inferExprType(e))
	case *ast.IfExpr:
		tc.checkIfExpr(e, false)
	case *ast.MatchExpr:
		tc.checkMatchExpr(e)
	case *ast.ForInLoopExpr:
		tc.checkForInLoopExpr(e)
	case *ast.ForLoopExpr:
		tc.checkForLoopExpr(e)
	case *ast.RangeExpr:
		tc.inferRangeExpr(e)
	}
}

func (tc *TypeChecker) checkVarDecl(decl *ast.VarDeclStmt) {
	if decl.Value == nil {
		// Uninitialized declarations are not allowed: a binding must have a value
		// at its declaration so it can never be read before assignment. (Allowing
		// uninitialized `var` behind a definite-assignment pass may come later.)
		tc.addErrorCode(decl.GetLocation(), SeverityError, diag.CodeUninitializedDeclaration,
			"`%s %s` must be initialized: add `= <value>` (uninitialized declarations are not allowed)",
			decl.BindingKind, decl.Name)
		return
	}

	// A `const` must be evaluable at compile time: reject any initializer that
	// isn't a literal, another constant, or an expression built purely from those.
	if decl.BindingKind == ast.BindingConst {
		tc.checkConstInitializer(decl.Value)
	}

	// Lambda values (function declarations) are handled separately.
	// Full lambda type inference is not yet implemented, so the regular
	// annotation check is skipped for them.
	if lambda, ok := decl.Value.(*ast.LambdaExpr); ok {
		tc.checkLambdaBody(decl.Name, lambda)
		return
	}

	inferredType := tc.inferExprType(decl.Value)
	if inferredType == nil {
		return
	}

	if decl.Type == nil {
		tc.typeTable.Set(decl.Value, promoteToDefault(inferredType))
		return
	}

	// Resolve user-defined type names (e.g. UnresolvedType{"Hex"} → *ConstrainedType)
	// so that assignability and constraint checks operate on the concrete type.
	resolvedDeclType := tc.resolveType(decl.Type, decl.Location)

	if !isAssignable(inferredType, resolvedDeclType) {
		tc.typeTable.Set(decl.Value, inferredType)
		tc.addError(decl.GetLocation(), SeverityError,
			"%s: cannot assign %s to %s", decl.Name, inferredType, decl.Type)
		return
	}

	if !tc.checkAllocationCompat(inferredType, resolvedDeclType, decl.GetLocation(), decl.Name) {
		tc.typeTable.Set(decl.Value, inferredType)
		return
	}

	// Check that the literal value fits within the annotated integer type's range.
	tc.checkIntegerLiteralRange(decl.Name, decl.Value, resolvedDeclType)

	// Validate string literals against any pattern constraints on the declared type.
	tc.checkPatternConstraints(decl.Name, decl.Value, resolvedDeclType)

	// Store the annotation type — this is the effective type the expression is used as.
	// e.g. literal 42 annotated as i32 should be recorded as i32, not the untyped int.
	tc.typeTable.Set(decl.Value, resolvedDeclType)
}

// checkDestructuringDecl type-checks a destructuring declaration. It infers
// the RHS type, checks any whole-expression type annotation, then walks the
// pattern against that type (walkDestructuredPattern) — reporting any
// shape mismatch (tuple arity, missing struct field, pattern/type kind
// mismatch) — binding each name it introduces directly into the current
// (real) scope.
//
// Binding has to happen here, at typecheck time, rather than at collection
// time: unlike a plain `let x = ...`, a destructured name has no single
// initializer expression of its own to resolve a type from later (`x`'s type
// comes from caching `tc.typeTable.Get(x's Value)`; "a" and "b" in `let (a, b)
// = ...` share one Value, the whole tuple). The type of each name is only
// known once decl.Value has been inferred, so binding happens immediately
// after that, into the block scope already pushed by the collector — which is
// exactly why this is safe: it mutates the real `*symbols.Scope` for this
// block, so nested/sibling blocks stay correctly isolated, unlike a flat
// ad-hoc map would. `var`/`let mut` are threaded through so interior-mutation
// and purity checks treat a destructured name the same as any other binding.
func (tc *TypeChecker) checkDestructuringDecl(decl *ast.DestructuringDeclStmt) {
	if decl.Value == nil {
		return
	}
	inferredType := tc.inferExprType(decl.Value)
	if inferredType == nil {
		return
	}

	// If there's a whole-expression type annotation, verify assignability.
	if decl.Type != nil {
		resolvedDeclType := tc.resolveType(decl.Type, decl.Location)
		if !isAssignable(inferredType, resolvedDeclType) {
			tc.addError(decl.GetLocation(), SeverityError,
				"cannot assign %s to %s", inferredType, decl.Type)
			return
		}
		if !tc.checkAllocationCompat(inferredType, resolvedDeclType, decl.GetLocation(), "") {
			return
		}
		inferredType = resolvedDeclType
	}

	bindingKind := ast.BindingLet
	if decl.Keyword == "var" {
		bindingKind = ast.BindingVar
	}
	tc.walkDestructuredPattern(decl.Pattern, inferredType, func(name string, typ types.Type) {
		// Overwrite (rather than Define) so this typed entry replaces the
		// untyped DestructuringDeclStmt placeholder the collector registered for
		// this name; later identifier references then resolve to the leaf's
		// type. Genuine name conflicts are already reported by the collector's
		// collectPatternDeclaration, so no duplicate error is raised here.
		tc.scope.Symbols[name] = &ast.VarDeclStmt{
			AstBase:     ast.AstBase{Location: decl.GetLocation()},
			BindingKind: bindingKind,
			IsMut:       decl.IsMut,
			Name:        name,
			Type:        typ,
		}
	})
}

// checkIfDestructuringStmt type-checks `if let pat = v { Then } else { Else }`.
// The collector pushed a scope around Then that holds pat's bound names (see
// CollectDestructuringIfStatement) and recorded it against stmt itself, so
// entering stmt's scope and binding there — via the same checkDestructuringDecl
// used for a plain `let` — makes the names resolvable from Then (a nested
// child scope of stmt's, so Lookup finds them through the parent chain) without
// leaking them into Else or the enclosing scope. Else is checked in whatever
// scope was already current (the enclosing one), matching reaching Else
// meaning the pattern failed to match.
func (tc *TypeChecker) checkIfDestructuringStmt(stmt *ast.IfDestructuringStmt) {
	tc.enterScope(stmt, func() {
		tc.checkDestructuringDecl(&stmt.DestructuringStatement)
	})
	tc.inferBlockType(stmt.Then)
	if stmt.Else != nil {
		tc.inferBlockType(stmt.Else)
	}
}

// checkElseDestructuringStmt type-checks `let pat = v else { Else }`. Unlike
// if-let, pat's names belong to the *enclosing* scope (persisting after the
// statement, like a plain `let`) — so binding goes through checkDestructuringDecl
// directly, no enterScope wrapper, writing into whatever scope is already
// current. Else is checked first so it never resolves the names being bound
// (matching the collector's CollectDestructuringElseStatement, which registers
// them only after collecting Else); the order has no effect on correctness
// here since Else type-checks in its own nested scope regardless, but mirrors
// the collector for readability.
func (tc *TypeChecker) checkElseDestructuringStmt(stmt *ast.ElseDestructuringStmt) {
	tc.inferBlockType(stmt.Else)
	tc.checkDestructuringDecl(&stmt.DestructuringStatement)
}

// walkDestructuredPattern recursively type-checks pat against t (the type it
// is being destructured against) and calls bind(name, type) for every
// identifier pat introduces, with the type its position/field corresponds to
// within t:
//   - tuple positions pair element-by-element; arity must match exactly
//     unless a RestPattern is present.
//   - struct fields pair by name against t's fields. A field's nested Pattern
//     — including the `{x: y}` rename form, which the collector represents
//     identically to a with-pattern field `{x: somePattern}` — is walked
//     recursively against that field's type; a rest field (`...rest`)
//     matches any remaining fields with no error (struct destructuring is
//     inherently partial).
//   - array elements all pair against the array's uniform element type
//     (no arity check: array length is often only known at runtime,
//     mirroring the existing leniency in checkArrayMatchArm); a *named* rest
//     element (`...rest`) binds the whole array type.
//   - a data pattern (`Some(x)`, `Ok(x)`, bare-payload `Some x`) resolves t to
//     a DataType, matches the named constructor, and pairs its nested Pattern
//     against that constructor's positional payload types
//     (bindDataPatternPayload) — same idea as a tuple, just sourced from the
//     constructor's declared params instead of a TupleType.
//
// Any mismatch (wrong tuple/constructor arity, missing struct field, unknown
// constructor, a pattern that doesn't match the *kind* of t) is reported as
// an error, and that sub-pattern's names are left unbound rather than
// guessed. An IdentifierPattern of "_" is the conventional discard and never
// binds.
func (tc *TypeChecker) walkDestructuredPattern(pat ast.Pattern, t types.Type, bind func(name string, typ types.Type)) {
	if pat == nil || t == nil {
		return
	}
	t = tc.resolveType(t, pat.GetLocation())

	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name != "_" {
			bind(p.Name, t)
		}

	case *ast.TuplePattern:
		tt, ok := t.(types.TupleType)
		if !ok {
			tc.addError(p.GetLocation(), SeverityError,
				"cannot destructure %s with a tuple pattern", t)
			return
		}
		hasRest := false
		for _, el := range p.Elements {
			if _, ok := el.(*ast.RestPattern); ok {
				hasRest = true
				break
			}
		}
		if !hasRest && len(p.Elements) != len(tt.Elements) {
			tc.addError(p.GetLocation(), SeverityError,
				"tuple pattern has %d element(s) but tuple has %d",
				len(p.Elements), len(tt.Elements))
			return // mismatched arity: elements below can't be paired up reliably
		}
		for i, el := range p.Elements {
			if i < len(tt.Elements) {
				tc.walkDestructuredPattern(el, tt.Elements[i], bind)
			}
		}

	case *ast.ArrayPattern:
		elemType := arrayElementType(t)
		if elemType == nil {
			tc.addError(p.GetLocation(), SeverityError,
				"cannot destructure %s with an array pattern", t)
			return
		}
		for _, el := range p.Elements {
			if rp, isRest := el.(*ast.RestPattern); isRest {
				if rp.Identifier != "" {
					bind(rp.Identifier, t)
				}
				continue
			}
			tc.walkDestructuredPattern(el, elemType, bind)
		}

	case *ast.StructPattern:
		fields := structFieldTypes(t)
		if fields == nil {
			tc.addError(p.GetLocation(), SeverityError,
				"cannot destructure %s with a struct pattern", t)
			return
		}
		for _, f := range p.Fields {
			if _, isRest := f.Pattern.(*ast.RestPattern); isRest {
				continue
			}
			fieldType, ok := fields[f.Name]
			if !ok {
				tc.addError(f.GetLocation(), SeverityError,
					"%s has no field %q", t, f.Name)
				continue
			}
			if f.Pattern != nil {
				tc.walkDestructuredPattern(f.Pattern, fieldType, bind)
			} else {
				bind(f.Name, fieldType)
			}
		}

	case *ast.DataPattern:
		dt, ok := tc.resolveToDataType(t)
		if !ok {
			tc.addError(p.GetLocation(), SeverityError,
				"cannot destructure %s with a data pattern", t)
			return
		}
		var ctor *types.DataTypeConstructor
		for i := range dt.Constructors {
			if dt.Constructors[i].Name == p.Name {
				ctor = &dt.Constructors[i]
				break
			}
		}
		if ctor == nil {
			tc.addError(p.GetLocation(), SeverityError,
				"%s is not a constructor of %s", p.Name, dt.Name)
			return
		}
		tc.bindDataPatternPayload(p, ctor.Params, bind)
	}
}

// bindDataPatternPayload pairs a DataPattern's nested Pattern (if any)
// against the matched constructor's positional payload types. `Ctor(...)`
// syntax always wraps the payload in a TuplePattern — even for a single
// argument (`Some(x)` is `Pattern: TuplePattern{[x]}`) — so a one-element
// TuplePattern against a one-param constructor is paired directly against
// that param's type, never treated as if the payload itself were a real
// tuple type. Bare `Ctor x` syntax (no parens) is only valid for a
// single-param constructor and binds directly. A zero-param constructor with
// no pattern (plain `None`) binds nothing; any arity mismatch is reported.
func (tc *TypeChecker) bindDataPatternPayload(p *ast.DataPattern, params []types.Type, bind func(name string, typ types.Type)) {
	if p.Pattern == nil {
		if len(params) != 0 {
			tc.addError(p.GetLocation(), SeverityError,
				"%s takes %d argument(s) but the pattern has none", p.Name, len(params))
		}
		return
	}
	if tp, ok := p.Pattern.(*ast.TuplePattern); ok {
		if len(tp.Elements) != len(params) {
			tc.addError(p.GetLocation(), SeverityError,
				"%s takes %d argument(s) but the pattern has %d", p.Name, len(params), len(tp.Elements))
			return
		}
		for i, el := range tp.Elements {
			tc.walkDestructuredPattern(el, params[i], bind)
		}
		return
	}
	if len(params) != 1 {
		tc.addError(p.GetLocation(), SeverityError,
			"%s takes %d argument(s) but the pattern has 1", p.Name, len(params))
		return
	}
	tc.walkDestructuredPattern(p.Pattern, params[0], bind)
}

// arrayElementType extracts the element type of a dynamic or static array
// type, or nil if t is neither.
func arrayElementType(t types.Type) types.Type {
	switch at := t.(type) {
	case types.DynamicArrayType:
		return at.ElementType
	case types.StaticArrayType:
		return at.ElementType
	}
	return nil
}

// structFields returns the fields of a named or anonymous struct type,
// with ok=false if t is neither.
func structFields(t types.Type) ([]types.StructField, bool) {
	switch st := t.(type) {
	case types.NamedStructType:
		return st.Fields, true
	case types.AnonymousStructType:
		return st.Fields, true
	}
	return nil, false
}

// resolveGenericStruct turns a ParameterizedType naming a generic struct into
// that struct with its type arguments substituted into the field types — so
// `Box<i64>` becomes a struct whose `value` field is `i64`, and `Box<t>` (the
// Self type inside a generic impl body) keeps `value` as `t`. This is what lets
// field and method access work on a generic struct instance. Any other type
// (including a ParameterizedType whose head isn't a struct) is returned
// unchanged, so callers can keep the original for trait dispatch, which needs
// the type arguments the substituted struct no longer carries.
func (tc *TypeChecker) resolveGenericStruct(t types.Type) types.Type {
	p, ok := t.(types.ParameterizedType)
	if !ok {
		return t
	}
	decl, ok := tc.symTable.Types[p.Name]
	if !ok {
		return t
	}
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		return t
	}
	// The generic parameter names live on the declaration (the NamedStructType's
	// own GenericParams field is not populated by the collector today); pair them
	// positionally with the usage-site type arguments.
	subst := make(map[string]types.Type, len(decl.GenericParams))
	for i, gp := range decl.GenericParams {
		if i < len(p.TypeArguments) {
			subst[gp.Name] = p.TypeArguments[i]
		}
	}
	fields := make([]types.StructField, len(st.Fields))
	copy(fields, st.Fields)
	for i := range fields {
		fields[i].Type = substituteGenerics(fields[i].Type, subst)
	}
	return types.NamedStructType{
		Name:          st.Name,
		Fields:        fields,
		GenericParams: st.GenericParams,
		Allocation:    st.Allocation,
	}
}

// structFieldTypes maps each field name of a named or anonymous struct type
// to its type, or nil if t is neither.
func structFieldTypes(t types.Type) map[string]types.Type {
	fields, ok := structFields(t)
	if !ok {
		return nil
	}
	m := make(map[string]types.Type, len(fields))
	for _, f := range fields {
		m[f.Name] = f.Type
	}
	return m
}

// checkPatternConstraints tests a string-literal value against every
// PatternConstraint on the declared type.  Non-string values and non-pattern
// constraints are silently skipped — this is purely an extra check layered on
// top of the ordinary type-assignability check.
func (tc *TypeChecker) checkPatternConstraints(name string, value ast.Expression, declType types.Type) {
	ct, ok := declType.(*types.ConstrainedType)
	if !ok {
		return
	}
	strLit, ok := value.(*ast.StringLiteralExpr)
	if !ok {
		return // only checkable at compile time for string literals
	}
	for _, c := range ct.Constraints {
		pc, ok := c.(*types.PatternConstraint)
		if !ok {
			continue
		}
		re, err := regex.Compile(regexPatternBody(pc.Pattern))
		if err != nil {
			// The broken regex is already reported at the type declaration site;
			// don't double-report here.
			continue
		}
		matched, err := re.MatchString(strLit.Value)
		if err != nil {
			continue // DFA capacity exceeded — don't block the user
		}
		if !matched {
			tc.addError(value.GetLocation(), SeverityError,
				"%s: value %q does not satisfy pattern constraint r/%s/",
				name, strLit.Value, pc.Pattern)
		}
	}
}

// checkAssignToBinding verifies that value can be assigned to the existing
// binding named name. It enforces that the binding exists, is a reassignable
// variable, and that value's inferred type is assignable to the binding's
// effective type. Any failure emits the appropriate diagnostic and returns nil;
// on success it returns the binding's effective type so callers can run
// follow-up checks (e.g. an integer-literal range check). loc anchors the
// mutability and assignability errors.
func (tc *TypeChecker) checkAssignToBinding(name string, value ast.Expression, loc ast.Location) types.Type {
	sym, ok := tc.scope.Lookup(name)
	if !ok {
		return nil
	}
	decl, ok := sym.(*ast.VarDeclStmt)
	if !ok {
		return nil
	}
	if !decl.IsMutable() {
		tc.addImmutableBindingError(loc, name, decl.BindingKind)
		return nil
	}
	effective := tc.effectiveType(decl)
	if effective == nil {
		return nil
	}
	rhsType := tc.inferExprType(value)
	if rhsType == nil {
		return nil
	}
	if !isAssignable(rhsType, effective) {
		tc.addError(loc, SeverityError,
			"%s: cannot assign %s to %s", name, rhsType, effective)
		return nil
	}
	if !tc.checkAllocationCompat(rhsType, effective, loc, name) {
		return nil
	}
	return effective
}

func (tc *TypeChecker) checkVarReassignment(stmt *ast.VarReassignmentStmt) {
	effective := tc.checkAssignToBinding(stmt.Name, stmt.Value, stmt.GetLocation())
	if effective == nil {
		return
	}
	// Check that the literal value fits within the variable's integer type's range.
	tc.checkIntegerLiteralRange(stmt.Name, stmt.Value, effective)
}

// checkDerefAssignment handles the grammar's representation of const reassignment.
// When the parser sees `X = val` where X is a const identifier, it emits a
// DerefAssignmentStmt with a DerefExpr wrapping the const IdentifierExpr.
func (tc *TypeChecker) checkDerefAssignment(stmt *ast.DerefAssignmentStmt) {
	ident, ok := stmt.Target.Operand.(*ast.IdentifierExpr)
	if !ok || !ident.IsConst {
		return
	}
	tc.addImmutableBindingError(stmt.Target.Operand.GetLocation(), ident.Name, ast.BindingConst)
}

// checkLValueAssignment type-checks an interior-mutation statement
// (`p.x = v`, `arr[i] = v`, `grid[i].y = v`) and enforces the mutability rule:
// the path must be rooted at a binding that permits interior mutation, i.e. a
// `var` or a `let mut`. A plain `let` is deeply immutable — interior mutation is
// rejected even several hops down the path (`a.b.c = v` walks back to `a`).
func (tc *TypeChecker) checkLValueAssignment(stmt *ast.LValueAssignmentStmt) {
	// Enforce mutability of the root binding first; this is the point of the
	// statement form and should be reported even if the value doesn't type-check.
	if root := rootIdentifier(stmt.Target); root != nil {
		if root.IsConst {
			tc.addImmutableBindingError(root.GetLocation(), root.Name, ast.BindingConst)
		} else if mod, ok := tc.paramMods[root.Name]; ok {
			// The path is rooted at a function parameter. The `ref`/`mut`/`own`
			// modifier governs whether its interior may be mutated: a bare or
			// `ref` parameter is an immutable borrow, while `mut` (mutable borrow)
			// and `own` (owned local) both permit interior mutation. Checked
			// before the scope lookup because a parameter shadows any outer
			// binding of the same name (mirroring IdentifierExpr resolution).
			if !paramAllowsInteriorMutation(mod) {
				tc.addParamImmutableError(root.GetLocation(), root.Name, mod)
			}
		} else if sym, ok := tc.scope.Lookup(root.Name); ok {
			if decl, ok := sym.(*ast.VarDeclStmt); ok && !decl.CanMutateInterior() {
				tc.addInteriorImmutableError(root.GetLocation(), root.Name, decl.BindingKind)
			}
		}
	}

	// A field declared `readonly` is frozen: it cannot be mutated even through a
	// mutable binding, and (like a deeply-immutable `let` binding) nothing
	// reached *through* it can be mutated either. Walk every member hop in the
	// path and reject the write if any traverses a frozen field.
	tc.checkFrozenFieldPath(stmt.Target)

	targetType := tc.inferExprType(stmt.Target)
	valueType := tc.inferExprType(stmt.Value)
	if targetType == nil || valueType == nil {
		return
	}
	if !isAssignable(valueType, targetType) {
		tc.addError(stmt.GetLocation(), SeverityError,
			"cannot assign %s to %s", valueType, targetType)
		return
	}
	if !tc.checkAllocationCompat(valueType, targetType, stmt.GetLocation(), "") {
		return
	}
	tc.checkIntegerLiteralRange(stmt.Target.GetName(), stmt.Value, targetType)
}

// rootIdentifier walks a member/index path back to the identifier it is rooted
// at (`grid[i].y` → `grid`). Returns nil when the path is not rooted at a plain
// identifier (e.g. a function-call result or a parenthesized expression), in
// which case interior-mutability cannot be attributed to a local binding.
func rootIdentifier(expr ast.Expression) *ast.IdentifierExpr {
	for {
		switch e := expr.(type) {
		case *ast.IdentifierExpr:
			return e
		case *ast.MemberExpr:
			expr = e.Object
		case *ast.IndexExpr:
			expr = e.Object
		default:
			return nil
		}
	}
}

// checkFrozenFieldPath walks the member hops of an assignment target from the
// written field inward and reports a write that traverses a `readonly` field.
// The outermost hop (the field actually being written) is checked first so it is
// reported in preference to a frozen field deeper in the path. Index hops carry
// no field-mutability information and are skipped over.
func (tc *TypeChecker) checkFrozenFieldPath(target ast.Expression) {
	for {
		switch e := target.(type) {
		case *ast.MemberExpr:
			objType := tc.resolveType(tc.inferExprType(e.Object), e.Object.GetLocation())
			if f, ok := structFieldByName(objType, e.Property.Name); ok && f.Frozen {
				tc.addError(e.GetLocation(), SeverityError,
					"cannot mutate readonly field %q: it is immutable after construction", e.Property.Name)
				return
			}
			target = e.Object
		case *ast.IndexExpr:
			target = e.Object
		default:
			return
		}
	}
}

// structFieldByName returns the named field of a (named or anonymous) struct type.
func structFieldByName(t types.Type, name string) (types.StructField, bool) {
	fields, _ := structFields(t)
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return types.StructField{}, false
}

// addInteriorImmutableError reports an attempt to mutate the interior of a value
// reached through an immutable binding.
func (tc *TypeChecker) addInteriorImmutableError(loc ast.Location, name string, kind ast.BindingKind) {
	if kind == ast.BindingConst {
		tc.addImmutableBindingError(loc, name, kind)
		return
	}
	tc.addError(loc, SeverityError,
		"%s: `let` binding is deeply immutable; its interior cannot be mutated (use `let mut` to allow interior mutation, or `var` to also allow reassignment)", name)
}

// paramAllowsInteriorMutation reports whether a parameter with the given
// `ref`/`mut`/`own` modifier may have its interior mutated. A bare parameter
// (no modifier) and a `ref` parameter are immutable borrows; `mut` (mutable
// borrow) and `own` (owned local) both permit interior mutation.
func paramAllowsInteriorMutation(mod types.TypeModifier) bool {
	return mod == types.Mut || mod == types.Own
}

// addParamImmutableError reports an attempt to mutate the interior of a value
// reached through an immutable-borrow parameter (bare or `ref`).
func (tc *TypeChecker) addParamImmutableError(loc ast.Location, name string, mod types.TypeModifier) {
	kind := "an immutable borrow by default"
	if mod == types.Ref {
		kind = "a `ref` (immutable borrow)"
	}
	tc.addError(loc, SeverityError,
		"%s: parameter is %s; its interior cannot be mutated (declare it `mut <type>` to mutate the caller's value, or `own <type>` for an owned local copy)",
		name, kind)
}

func (tc *TypeChecker) checkMathAssignOp(expr *ast.MathAssignOpExpr) {
	tc.checkAssignToBinding(expr.Left.Name, expr.Right, expr.GetLocation())
}

func (tc *TypeChecker) checkBooleanLiteralExpr(expr *ast.BooleanLiteralExpr) {
	exprType := tc.inferExprType(expr)
	if exprType == nil {
		return
	}
	if !types.IsBoolean(exprType) {
		tc.addExpectedTypeError(expr, types.PrimitiveType{Name: types.Boolean}, exprType)
	}
}

func (tc *TypeChecker) checkNotBooleanExpr(expr *ast.NotBooleanExpr) {
	exprType := tc.inferExprType(expr.Expression)
	if exprType == nil {
		return
	}
	if !types.IsBoolean(exprType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"'!' operator: operand must be boolean, got %s", exprType)
	}
}

func (tc *TypeChecker) checkBooleanBinaryOpExpr(expr *ast.BooleanBinaryOpExpr) {
	leftType := tc.inferExprType(expr.Left)
	rightType := tc.inferExprType(expr.Right)

	if leftType == nil || rightType == nil {
		return
	}

	switch expr.Operator {
	case ast.BooleanBinaryOpAnd, ast.BooleanBinaryOpOr:
		if !types.IsBoolean(leftType) || !types.IsBoolean(rightType) {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: operands must both be boolean, got %s and %s", expr.Operator, leftType, rightType)
		}
	case ast.BooleanBinaryOpEq, ast.BooleanBinaryOpNEq:
		if !areEqualityCompatible(leftType, rightType) {
			tc.addIncompatibleTypesError(expr, string(expr.Operator), leftType, rightType)
		} else if isFloatType(leftType) || isFloatType(rightType) {
			tc.addError(expr.GetLocation(), SeverityWarning,
				"operator %s: comparing float values with == or != may give unexpected results due to floating-point precision", expr.Operator)
		}
	case ast.BooleanBinaryOpLT, ast.BooleanBinaryOpLTE, ast.BooleanBinaryOpGT, ast.BooleanBinaryOpGTE:
		if !types.IsNumeric(leftType) || !types.IsNumeric(rightType) {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: operands must be numeric, got %s and %s", expr.Operator, leftType, rightType)
			return
		}
		if numericResultType(leftType, rightType) == nil {
			tc.addIncompatibleTypesError(expr, string(expr.Operator), leftType, rightType)
		}
	}
}

func (tc *TypeChecker) addImmutableBindingError(loc ast.Location, name string, kind ast.BindingKind) {
	switch kind {
	case ast.BindingConst:
		tc.addError(loc, SeverityError,
			"%s: 'const' binding is immutable and cannot be reassigned", name)
	default: // BindingLet
		tc.addError(loc, SeverityError,
			"%s: 'let' binding is immutable; use 'var' to allow reassignment", name)
	}
}

func (tc *TypeChecker) addExpectedTypeError(expr ast.Expression, expected, actual types.Type) {
	tc.addError(expr.GetLocation(), SeverityError,
		"%s: expected %s, got %s instead", expr.GetName(), expected, actual)
}

func (tc *TypeChecker) addIncompatibleTypesError(expr ast.Expression, operator string, leftType, rightType types.Type) {
	tc.addError(expr.GetLocation(), SeverityError,
		"operator %s: incompatible types: %s and %s", operator, leftType, rightType)
}

// effectiveType returns the concrete type of a declaration: the annotation if
// present (resolved through the symbol table), or the TypeTable entry recorded
// when the initializer was checked.
func (tc *TypeChecker) effectiveType(decl *ast.VarDeclStmt) types.Type {
	if decl.Type != nil {
		return tc.resolveType(decl.Type, decl.Location)
	}
	if decl.Value != nil {
		if t, ok := tc.typeTable.Get(decl.Value); ok {
			return t
		}
	}
	return nil
}

// resolveType looks up an UnresolvedType name in the symbol table and returns
// the concrete declared type (e.g. *ConstrainedType, NamedStructType, DataType).
// All other type values are returned unchanged.
//
// If the UnresolvedType carries a usage-site allocation modifier (e.g. from
// `let n: shared Node`), it is applied on top of the declaration's default via
// types.WithAllocation after the name is resolved. The cache stores the base
// type (declaration-level allocation) so that allocation-annotated and
// unannotated references to the same name both benefit from it.
//
// Results are cached so that repeated resolutions of the same name only emit
// "unknown type" once per Check run.
func (tc *TypeChecker) resolveType(t types.Type, loc ast.Location) types.Type {
	ut, ok := t.(types.UnresolvedType)
	if !ok {
		return t
	}
	if cached, ok := tc.resolvedTypes[ut.Name]; ok {
		return types.WithAllocation(cached, ut.Allocation)
	}
	decl, ok := tc.symTable.Types[ut.Name]
	if !ok {
		tc.addError(loc, SeverityError, "unknown type %q", t)
		tc.resolvedTypes[ut.Name] = t // cache unresolved itself so the error fires only once
		return t
	}
	tc.resolvedTypes[ut.Name] = decl.Type
	return types.WithAllocation(decl.Type, ut.Allocation)
}

// resolveTypeIfKnown resolves an UnresolvedType only when the name is actually
// in the symbol table, returning t unchanged and emitting no diagnostic when it
// is not found. Use this instead of resolveType when the caller must not produce
// a duplicate "unknown type" diagnostic (e.g. the return-type annotation in
// checkLambdaBody, where the parameter-annotation pass may have already emitted
// the error or where the caller intends to report a different error).
func (tc *TypeChecker) resolveTypeIfKnown(t types.Type) types.Type {
	ut, ok := t.(types.UnresolvedType)
	if !ok {
		return t
	}
	if cached, ok := tc.resolvedTypes[ut.Name]; ok {
		return types.WithAllocation(cached, ut.Allocation)
	}
	if decl, ok := tc.symTable.Types[ut.Name]; ok {
		return types.WithAllocation(decl.Type, ut.Allocation)
	}
	return t
}

// inferExprType returns the type of expr, or nil if it cannot be determined yet.
func (tc *TypeChecker) inferExprType(expr ast.Expression) types.Type {
	if expr == nil {
		return nil
	}
	// Check the side table first — a prior check may have already resolved this.
	if t, ok := tc.typeTable.Get(expr); ok {
		return t
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return e.GetType()
	case *ast.FloatLiteralExpr:
		return e.GetType()
	case *ast.StringLiteralExpr:
		return e.GetType()
	case *ast.BooleanLiteralExpr:
		return e.GetType()
	case *ast.CharacterLiteralExpr:
		return e.GetType()
	case *ast.ArrayLiteralExpr:
		return tc.inferArrayLiteralType(e)
	case *ast.FunctionCallExpr:
		if t := tc.inferTypeConversion(e); t != nil {
			return t
		}
		return tc.inferFunctionCallExpr(e)
	case *ast.LambdaExpr:
		return tc.inferLambdaExprType(e)
	case *ast.MemberExpr:
		return tc.inferMemberExprType(e)
	case *ast.TryExpr:
		return tc.inferTryExpr(e)
	case *ast.NegationExpr:
		return tc.inferNegationExpr(e)
	case *ast.StructInstanceExpr:
		return tc.inferStructInstanceExpr(e)
	case *ast.AnonymousStructInstanceExpr:
		return tc.inferAnonymousStructInstanceExpr(e)
	case *ast.NotBooleanExpr:
		tc.checkNotBooleanExpr(e)
		return types.PrimitiveType{Name: types.Boolean}
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(e)
		return types.PrimitiveType{Name: types.Boolean}
	case *ast.BlockExpr:
		return tc.inferBlockType(e)
	case *ast.IfExpr:
		return tc.checkIfExpr(e, true)
	case *ast.MatchExpr:
		return tc.checkMatchExpr(e)
	case *ast.MathBinaryOpExpr:
		return tc.inferMathBinaryExpr(e)
	case *ast.StringConcatExpr:
		return tc.inferStringConcatExpr(e)
	case *ast.RegexLiteralExpr:
		// Validate regex syntax at compile time; the type of a regex literal
		// is the built-in `regex` type.
		if _, err := regex.Compile(e.Pattern); err != nil {
			tc.addError(e.GetLocation(), SeverityError,
				"invalid regex literal r/%s/: %s", e.Pattern, err)
		}
		return types.PrimitiveType{Name: types.Regex}
	case *ast.InterpolatedStringExpr:
		return types.PrimitiveType{Name: types.String}
	case *ast.DataConstructorExpr:
		// Resolve the data type that owns this constructor so that the type of
		// a data-constructor expression (e.g. `Some 42`) is the enclosing
		// DataType (e.g. `Maybe`), not nil.
		if dt, ok := tc.findDataTypeByConstructor(e.Constructor); ok {
			return dt
		}
		return nil
	case *ast.TupleLiteralExpr:
		return tc.inferTupleLiteralExpr(e)
	case *ast.IndexExpr:
		return tc.inferIndexExpr(e)
	case *ast.RangeExpr:
		return tc.inferRangeExpr(e)
	case *ast.ForInLoopExpr:
		return tc.checkForInLoopExpr(e)
	case *ast.ForLoopExpr:
		tc.checkForLoopExpr(e)
		return nil
	case *ast.NullCoalescingExpr:
		return tc.inferNullCoalescingExpr(e)
	case *ast.SizeofExpr:
		tc.resolveType(e.Type, e.GetLocation())
		return types.PrimitiveType{Name: types.UInt64}
	case *ast.IdentifierExpr:
		// Consult the parameter scope installed by withParamScope while
		// type-checking a function body.
		if tc.paramTypes != nil {
			if t, ok := tc.paramTypes[e.Name]; ok {
				tc.typeTable.Set(e, t)
				return t
			}
		}
		sym, ok := tc.scope.Lookup(e.Name)
		if !ok {
			tc.addError(e.GetLocation(), SeverityError, "undefined identifier %q", e.Name)
			return nil
		}
		if v, ok := sym.(*ast.VarDeclStmt); ok {
			var t types.Type
			if v.Value != nil {
				if cached, ok := tc.typeTable.Get(v.Value); ok {
					t = cached
				}
			}
			if t == nil {
				t = v.Type
			}
			if t != nil {
				tc.typeTable.Set(e, t)
			}
			return t
		}
		if _, ok := sym.(*ast.DestructuringDeclStmt); ok {
			// The collector registers each destructured name as a placeholder
			// pointing at its declaration; a successful destructuring overwrites it
			// with a typed VarDeclStmt (checkDestructuringDecl). A remaining
			// placeholder means the destructuring never bound this name with a type
			// (e.g. arity mismatch / non-destructurable scrutinee), so the name is
			// effectively undefined.
			tc.addError(e.GetLocation(), SeverityError, "undefined identifier %q", e.Name)
			return nil
		}
		tc.addError(e.GetLocation(), SeverityError, "undefined symbol %q", e.Name)
		return nil
	}
	tc.addError(expr.GetLocation(), SeverityError, "unknown expression type %q", expr.GetName())
	return nil
}

// promoteToDefault converts an untyped literal type to its default concrete type:
//   - UntypedInt / UntypedSignedInt → i64
//   - UntypedFloat                 → f64
//   - StaticArrayType              → promote element type recursively
//
// All other types are returned unchanged.
func promoteToDefault(t types.Type) types.Type {
	switch v := t.(type) {
	case types.PrimitiveType:
		switch v.Name {
		case types.UntypedInt, types.UntypedSignedInt:
			return types.PrimitiveType{Name: types.Int64}
		case types.UntypedFloat:
			return types.PrimitiveType{Name: types.Float64}
		}
	case types.StaticArrayType:
		// Promote the element type so that e.g. [1, 2, 3] (UntypedInt elements)
		// becomes StaticArrayType{int, 3} when there is no annotation.
		v.ElementType = promoteToDefault(v.ElementType)
		return v
	}
	return t
}

// inferTypeConversion handles calls of the form `TypeName(expr)` where TypeName
// is a concrete numeric primitive. Returns nil for ordinary function calls.
func (tc *TypeChecker) inferTypeConversion(call *ast.FunctionCallExpr) types.Type {
	ident, ok := call.Function.(*ast.IdentifierExpr)
	if !ok {
		return nil
	}
	targetType, ok := numericPrimitiveByName(ident.Name)
	if !ok {
		return nil
	}
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: type conversion requires exactly 1 argument, got %d", ident.Name, len(call.Arguments))
		return targetType
	}
	argType := tc.inferExprType(call.Arguments[0])
	if argType == nil {
		return targetType
	}
	if !types.IsNumeric(argType) {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s", argType, ident.Name)
		// Return targetType so the caller knows this is a type-conversion expression
		// and doesn't fall through to inferFunctionCallExpr (which would emit a
		// spurious "undefined function" error for the type name).
		return targetType
	}
	if isFloatType(argType) && isIntType(targetType) {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: use floor(), ceil(), or round() to convert explicitly", argType, ident.Name)
		return targetType
	}
	if srcPrec, dstPrec := floatPrecision(argType), floatPrecision(targetType); srcPrec > dstPrec && dstPrec > 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: narrowing conversion may lose precision", argType, ident.Name)
		return targetType
	}
	// Integer→integer conversion of a compile-time constant that does not fit the
	// target (e.g. u8(256), i8(300), u8(-1)). This makes lossy int conversions
	// loud for the constant case, matching the float-narrowing error above.
	// Non-constant int narrowing is deferred to a future value-range pass, the
	// same scope limit checkIntegerLiteralRange already has.
	if toP, ok := targetType.(types.PrimitiveType); ok && isAnyConcreteInt(toP.Name) && isIntType(argType) {
		if value, isConst := extractIntLiteralValue(call.Arguments[0]); isConst && !integerFitsInType(value, toP.Name) {
			tc.addError(call.GetLocation(), SeverityError,
				"cannot convert %d to %s: literal value is out of range", value, ident.Name)
			return targetType
		}
	}
	return targetType
}

func (tc *TypeChecker) inferMathBinaryExpr(expr *ast.MathBinaryOpExpr) types.Type {
	left := tc.inferExprType(expr.Left)
	right := tc.inferExprType(expr.Right)

	if left == nil || right == nil {
		return nil
	}

	if !types.IsNumeric(left) || !types.IsNumeric(right) {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: operands must be numeric, got %s and %s", expr.Operator, left, right)
		return nil
	}

	if expr.Operator == ast.MathBinaryOpDiv || expr.Operator == ast.MathBinaryOpMod || expr.Operator == ast.MathBinaryOpRemainder {
		if isLiteralZero(expr.Right) {
			tc.addError(expr.Right.GetLocation(), SeverityError,
				"operator %s: division by zero", expr.Operator)
			return nil
		}
	}

	result := numericResultType(left, right)
	if result == nil {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: incompatible types: %s and %s", expr.Operator, left, right)
		return nil
	}

	return result
}

func isLiteralZero(expr ast.Expression) bool {
	if e, ok := expr.(*ast.FloatLiteralExpr); ok {
		return e.Value == 0
	}
	// Fold compile-time integer constants so a constant zero divisor is caught
	// even when it isn't written as a bare 0 literal (e.g. 5 - 5, 2 * 0). A bare
	// 0 still folds through extractIntLiteralValue's IntegerLiteralExpr case.
	if value, ok := extractIntLiteralValue(expr); ok {
		return value == 0
	}
	return false
}

// inferArrayLiteralType infers the type of an array literal expression.
// An array literal always produces a StaticArrayType — its length is known at
// compile time from the number of elements. The element type is the common type
// of all elements (via branchCommonType). When the elements are empty the
// element type is nil, which signals an unresolved/empty array.
//
// Whether the containing variable is static or dynamic is determined by the
// annotation type on the VarDeclStmt, not by the literal itself. isAssignable
// allows a StaticArrayType to widen into a DynamicArrayType so that:
//
//	let xs: []int = [1, 2, 3]   // OK — StaticArrayType{int,3} → DynamicArrayType{int}
//	let xs: [3]int = [1, 2, 3]  // OK — exact match
func (tc *TypeChecker) inferArrayLiteralType(expr *ast.ArrayLiteralExpr) types.Type {
	var elemType types.Type
	for _, el := range expr.Elements {
		t := tc.inferExprType(el) // keep untyped (UntypedInt, etc.) so the annotation can widen
		if t == nil {
			continue
		}
		if elemType == nil {
			elemType = t
			continue
		}
		common, ok := branchCommonType(elemType, t)
		if !ok {
			tc.addError(el.GetLocation(), SeverityError,
				"array literal: element type %s is not compatible with preceding element type %s",
				t, elemType)
			return nil
		}
		elemType = common
	}
	return types.StaticArrayType{ElementType: elemType, Size: len(expr.Elements)}
}

func (tc *TypeChecker) inferStringConcatExpr(expr *ast.StringConcatExpr) types.Type {
	left := tc.inferExprType(expr.Left)
	right := tc.inferExprType(expr.Right)

	if left == nil || right == nil {
		return nil
	}

	if !types.IsString(left) || !types.IsString(right) {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator ++: operands must be strings, got %s and %s", left, right)
		return nil
	}

	return types.PrimitiveType{Name: types.String}
}

func (tc *TypeChecker) inferNegationExpr(expr *ast.NegationExpr) types.Type {
	operandType := tc.inferExprType(expr.Operand)
	if operandType == nil {
		return nil
	}
	if !types.IsNumeric(operandType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot negate non-numeric type %s", operandType)
		return nil
	}
	p, ok := operandType.(types.PrimitiveType)
	if ok && isAnyConcreteUnsignedInt(p.Name) {
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot negate unsigned type %s", operandType)
		return nil
	}
	if ok && (p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt) {
		return types.PrimitiveType{Name: types.UntypedSignedInt}
	}
	return operandType
}

func (tc *TypeChecker) inferTupleLiteralExpr(expr *ast.TupleLiteralExpr) types.Type {
	name := expr.Name
	if name == "" {
		name = "?"
	}

	// A capitalized application like `Some(42)` parses as a named tuple literal,
	// but if its name is a data-type constructor it denotes that data type, not a
	// tuple. (Juxtaposition application `Some 42` was removed; call syntax is the
	// only applied-constructor form.) Resolve it the same way a nullary
	// constructor or the old `data_constructor_expr` did — by constructor name —
	// so `?`, `??`, and must-use see the owning data type (e.g. `Maybe`).
	dt, isCtor := types.DataType{}, false
	if name != "?" {
		dt, isCtor = tc.findDataTypeByConstructor(name)
	}

	elements := make([]types.Type, len(expr.Elements))
	for i, elem := range expr.Elements {
		t := tc.inferExprType(elem)
		if t == nil {
			// A data constructor resolves by name regardless of whether its
			// payload type-checks (matching the previous data_constructor_expr
			// behavior); a plain tuple needs every element's type.
			if isCtor {
				continue
			}
			return nil
		}
		elements[i] = promoteToDefault(t)
		tc.typeTable.Set(elem, elements[i])
	}

	if isCtor {
		return dt
	}
	return types.TupleType{Name: name, Elements: elements}
}

// resolveConstantInt returns the compile-time integer value of expr, if
// determinable. It looks through let-bound identifiers whose initializer is
// itself a constant integer expression.
func (tc *TypeChecker) resolveConstantInt(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return e.Value, true
	case *ast.IdentifierExpr:
		sym, ok := tc.scope.Lookup(e.Name)
		if !ok {
			return 0, false
		}
		v, ok := sym.(*ast.VarDeclStmt)
		if !ok || v.Value == nil {
			return 0, false
		}
		return tc.resolveConstantInt(v.Value)
	}
	return 0, false
}

func (tc *TypeChecker) inferIndexExpr(expr *ast.IndexExpr) types.Type {
	objectType := tc.inferExprType(expr.Object)
	indexType := tc.inferExprType(expr.Index)

	if objectType == nil {
		return nil
	}

	if indexType != nil && !isIntType(indexType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"index must be an integer, got %s", indexType)
		return nil
	}

	switch t := objectType.(type) {
	case types.StaticArrayType:
		if idx, ok := tc.resolveConstantInt(expr.Index); ok {
			if idx < 0 || int(idx) >= t.Size {
				tc.addError(expr.GetLocation(), SeverityError,
					"index %d out of range for array of size %d", idx, t.Size)
				return nil
			}
		}
		return t.ElementType
	case types.DynamicArrayType:
		return t.ElementType
	case types.PrimitiveType:
		if t.Name == types.String {
			return types.PrimitiveType{Name: types.Char}
		}
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot index into type %s", objectType)
		return nil
	case types.TupleType:
		idxVal, ok := tc.resolveConstantInt(expr.Index)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"tuple index must be an integer literal")
			return nil
		}
		idx := int(idxVal)
		if idx < 0 || idx >= len(t.Elements) {
			tc.addError(expr.GetLocation(), SeverityError,
				"tuple index %d out of range for tuple with %d elements", idx, len(t.Elements))
			return nil
		}
		return t.Elements[idx]
	default:
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot index into type %s", objectType)
		return nil
	}
}

func (tc *TypeChecker) inferStructInstanceExpr(expr *ast.StructInstanceExpr) types.Type {
	// The name is either a struct type (`Point { … }`) or a data constructor with
	// an inline-record payload (`data Tree = … | Node { … }`, built as `Node { … }`).
	var decl *ast.TypeDeclStmt
	var structType types.NamedStructType
	// resultType is what the literal evaluates to: the struct itself for a real
	// struct, or the owning data type for an inline-record constructor.
	var resultType types.Type

	if d, ok := tc.symTable.Types[expr.Name]; ok {
		st, ok := d.Type.(types.NamedStructType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError, "%s: not a struct type", expr.Name)
			return nil
		}
		if len(st.Fields) == 0 {
			tc.addError(expr.GetLocation(), SeverityError, "%s: no fields declared", expr.Name)
			return nil
		}
		decl, structType, resultType = d, st, st
	} else if dDecl, fields, dt, ok := tc.findInlineRecordConstructor(expr.Name); ok {
		// `Node { … }` where Node is a data constructor whose payload is an inline
		// record (anonymous struct). Check the supplied fields against the record's
		// fields; the literal evaluates to the owning data type. decl is the data
		// type's declaration so its generic parameters drive the same inference as
		// a generic struct (`data Box<t> = Wrap { value: t }`).
		decl = dDecl
		structType = types.NamedStructType{Name: expr.Name, Fields: fields}
		resultType = dt
	} else {
		tc.addError(expr.GetLocation(), SeverityError, "undefined struct type %q", expr.Name)
		return nil
	}

	// Resolve the generic type parameters for this instantiation.
	typeSubst := make(map[string]types.Type, len(decl.GenericParams))
	genericParamNames := make(map[string]bool, len(decl.GenericParams))
	for _, p := range decl.GenericParams {
		genericParamNames[p.Name] = true
	}
	switch {
	case len(expr.GenericArgs) == len(decl.GenericParams):
		// Explicit turbofish arguments (`Point2::<i32> { … }`). This branch also
		// covers a non-generic struct, where both lengths are 0 and the loop is
		// a no-op.
		for i, param := range decl.GenericParams {
			typeSubst[param.Name] = expr.GenericArgs[i]
		}
	case len(expr.GenericArgs) == 0:
		// No turbofish: infer each type parameter from the value supplied for a
		// field declared with that parameter (`Point2 { x: 1, y: 2 }` infers
		// t = i64). A parameter with no inferable field stays unbound, and its
		// fields are checked leniently below.
		tc.inferStructGenericArgs(expr, structType, genericParamNames, typeSubst)
	default:
		tc.addError(expr.GetLocation(), SeverityError, "%s: expected %d generic arguments, got %d", expr.Name, len(decl.GenericParams), len(expr.GenericArgs))
		return nil
	}

	// Build a quick name->type lookup for the declared fields.
	fieldTypes := make(map[string]types.Type, len(structType.Fields))
	for _, f := range structType.Fields {
		// Substitute generic type parameters if available, otherwise use the field's declared type.
		if typeSub, ok := typeSubst[f.Type.GetName()]; ok {
			fieldTypes[f.Name] = typeSub
		} else {
			fieldTypes[f.Name] = f.Type
		}
	}

	// Check each field in the instance against the declared type and build a set of field names.
	fieldNames := make(map[string]struct{}, len(expr.Fields))
	for idx, f := range expr.Fields {
		name := f.Name
		if name == "" {
			name = structType.Fields[idx].Name
		}
		fieldNames[name] = struct{}{}
		expected, ok := fieldTypes[name]
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError, "%s: unknown field %q", expr.Name, name)
			continue
		}
		// A field still typed as a generic parameter (one we could not infer)
		// accepts any value: this checker does not fully reason about generics,
		// so an un-inferred parameter must not produce a spurious mismatch.
		if genericParamNames[expected.GetName()] {
			tc.inferExprType(f.Value) // still record the value's type
			continue
		}
		// Resolve both sides: a field declared with a named-struct type is stored
		// as an UnresolvedType, which would otherwise never compare equal to the
		// inferred NamedStructType of a nested struct literal (`Point{...}`).
		expected = tc.resolveType(expected, f.Value.GetLocation())
		actual := tc.resolveType(tc.inferExprType(f.Value), f.Value.GetLocation())
		if actual != nil && !isAssignable(actual, expected) {
			tc.addError(f.Value.GetLocation(), SeverityError, "%s.%s: cannot assign %s to %s", expr.Name, name, actual, expected)
		}
	}

	if expr.BaseStruct != nil {
		// Record update syntax: the base struct supplies any fields not listed in
		// the update, so missing-field errors are suppressed for those.
		// We do verify that the base expression has the same struct type.
		baseType := tc.inferExprType(expr.BaseStruct)
		if baseType != nil && !types.TypesEqual(baseType, structType) {
			tc.addError(expr.BaseStruct.GetLocation(), SeverityError,
				"%s: base struct has type %s, expected %s", expr.Name, baseType, structType)
		}
	} else {
		// Full struct literal: every field without a default must be supplied.
		for _, f := range structType.Fields {
			if _, ok := fieldNames[f.Name]; !ok {
				if f.DefaultValue != nil {
					continue
				}
				tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeMissingStructField, "%s: missing field %q", expr.Name, f.Name)
			}
		}
	}

	return resultType
}

// findInlineRecordConstructor looks up a data constructor by name and, when its
// payload is a single inline record (anonymous struct), returns the data type's
// declaration, the record's fields, and the owning data type. Used so that
// `Node { … }` for a `data Tree = … | Node { … }` constructor type-checks like a
// struct literal but evaluates to the data type. Returns ok=false when no such
// constructor exists or its payload is not an inline record.
func (tc *TypeChecker) findInlineRecordConstructor(ctorName string) (*ast.TypeDeclStmt, []types.StructField, types.DataType, bool) {
	for _, decl := range tc.symTable.Types {
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, ctor := range dt.Constructors {
			if ctor.Name != ctorName {
				continue
			}
			if len(ctor.Params) == 1 {
				if anon, ok := ctor.Params[0].(types.AnonymousStructType); ok {
					return decl, anon.Fields, dt, true
				}
			}
			return nil, nil, types.DataType{}, false
		}
	}
	return nil, nil, types.DataType{}, false
}

// inferStructGenericArgs infers a generic struct's type arguments from the
// values supplied to fields declared with a bare type parameter, writing the
// bindings into typeSubst. Used when a generic struct literal is written without
// a turbofish (`Point2 { x: 1, y: 2 }`): the value for the first field declared
// as `t` fixes `t`. Fields whose declared type is not a bare parameter, and
// parameters never matched by a field, are left for the caller to handle.
func (tc *TypeChecker) inferStructGenericArgs(expr *ast.StructInstanceExpr, structType types.NamedStructType, genericParamNames map[string]bool, typeSubst map[string]types.Type) {
	declaredByName := make(map[string]types.Type, len(structType.Fields))
	for _, f := range structType.Fields {
		declaredByName[f.Name] = f.Type
	}
	for idx, f := range expr.Fields {
		fieldName := f.Name
		if fieldName == "" && idx < len(structType.Fields) {
			fieldName = structType.Fields[idx].Name
		}
		declared, ok := declaredByName[fieldName]
		if !ok {
			continue
		}
		paramName := declared.GetName()
		if !genericParamNames[paramName] {
			continue // field is not declared with a bare type parameter
		}
		if _, done := typeSubst[paramName]; done {
			continue // already inferred from an earlier field
		}
		if vt := tc.inferExprType(f.Value); vt != nil {
			typeSubst[paramName] = promoteToDefault(vt)
		}
	}
}

func (tc *TypeChecker) inferAnonymousStructInstanceExpr(expr *ast.AnonymousStructInstanceExpr) types.Type {
	structTypeFields := tc.convertAnonymousStructFieldsToTypeFields(expr.Fields)
	structType := types.AnonymousStructType{
		Fields: structTypeFields,
	}

	return structType
}

func (tc *TypeChecker) convertAnonymousStructFieldsToTypeFields(fields []ast.StructField) []types.StructField {
	structTypeFields := make([]types.StructField, len(fields))
	for i, f := range fields {
		structTypeFields[i] = types.StructField{
			Name: f.Name,
			Type: tc.inferExprType(f.Value),
		}
	}
	return structTypeFields
}

// inferLambdaExprType returns a LambdaType for a bare lambda expression,
// recording it in the type table so subsequent uses of the same AST node
// are handled via the cache (first line of inferExprType).
func (tc *TypeChecker) inferLambdaExprType(lambda *ast.LambdaExpr) types.Type {
	t := &types.LambdaType{
		ReturnType: types.ReturnType{Type: lambda.ReturnType.Type},
	}
	for _, p := range lambda.Parameters {
		t.Parameters = append(t.Parameters, types.ParameterType{
			Type:         tc.resolveType(p.Type, p.GetLocation()),
			DefaultValue: p.DefaultValue,
		})
	}
	tc.typeTable.Set(lambda, t)
	return t
}

// inferMemberExprType resolves member access (e.g. obj.field, obj.method())
// on struct types. It checks that the object is a struct, the field exists,
// and returns the field's type.
func (tc *TypeChecker) inferMemberExprType(m *ast.MemberExpr) types.Type {
	objType := tc.inferExprType(m.Object)
	// A field whose declared type is itself a named struct is stored as an
	// UnresolvedType (just the name), so member access on it (`line.start.x`)
	// would otherwise fall through to the non-struct error. Resolve the object
	// type through the symbol table first so nested-struct paths work.
	objType = tc.resolveType(objType, m.Object.GetLocation())
	// A generic struct instance (`Box<i64>`, or `Box<t>` for Self in an impl
	// body) arrives as a ParameterizedType; resolve it to the struct with its
	// type arguments substituted so its fields are visible. A field read needs no
	// trait dispatch, so resolving fully here (also improving the error message)
	// is safe.
	objType = tc.resolveGenericStruct(objType)
	fieldName := m.Property.Name

	if f, ok := structFieldByName(objType, fieldName); ok {
		tc.typeTable.Set(m, f.Type)
		return f.Type
	}

	switch t := objType.(type) {
	case types.NamedStructType:
		tc.addError(m.GetLocation(), SeverityError,
			"%s has no field %q", t.Name, fieldName)
	case types.AnonymousStructType:
		tc.addError(m.GetLocation(), SeverityError,
			"anonymous struct has no field %q", fieldName)
	default:
		// When the object type is nil (e.g. undefined identifier), don't
		// report a second error here — the undefined-identifier diagnostic
		// already explains the problem. inferMemberCall handles call-specific
		// errors for call sites where the object resolves but the field isn't callable.
		if objType != nil {
			tc.addError(m.GetLocation(), SeverityError,
				"member access on non-struct type %s", objType)
		}
	}
	return nil
}

func (tc *TypeChecker) addError(loc ast.Location, sev Severity, format string, args ...any) {
	tc.addErrorCode(loc, sev, diag.CodeTypeError, format, args...)
}

// addErrorCode is addError with an explicit diagnostic code instead of the
// generic CodeTypeError, for checks that want a stable, distinguishable code.
func (tc *TypeChecker) addErrorCode(loc ast.Location, sev Severity, code, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location: loc,
		Severity: sev,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	})
}

// checkAllocationCompat verifies that owning a value of type from into a slot of
// type to does not silently cross a storage-flavor boundary (see
// allocationCompatible). On a concrete mismatch it emits lyra-E018 and returns
// false; otherwise returns true. subject names the binding/target for the
// message ("" to omit the prefix). Call only at owning sites, after isAssignable
// has confirmed the types are otherwise compatible.
func (tc *TypeChecker) checkAllocationCompat(from, to types.Type, loc ast.Location, subject string) bool {
	if allocationCompatible(from, to) {
		return true
	}
	prefix := ""
	if subject != "" {
		prefix = subject + ": "
	}
	tc.addErrorCode(loc, SeverityError, diag.CodeAllocationMismatch,
		"%scannot store a '%s' value where a '%s' value is expected; converting allocation is an explicit operation",
		prefix, types.AllocationOf(from), types.AllocationOf(to))
	return false
}

func (tc *TypeChecker) addErrorRelated(loc ast.Location, sev Severity, related []diag.RelatedInformation, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location:           loc,
		Severity:           sev,
		Code:               diag.CodeTypeError,
		Message:            fmt.Sprintf(format, args...),
		RelatedInformation: related,
	})
}
