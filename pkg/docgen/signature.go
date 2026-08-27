package docgen

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A signature is re-rendered from the AST rather than sliced out of the source text.
//
// Slicing would be simpler and is wrong in both directions: a declaration's source span
// runs to the end of its *body*, so a signature would have to be cut at a `=>` or a `{`
// that the body may itself contain, and a multi-file module's declarations come from
// several files this package has not read. Re-rendering also normalizes — the three
// spellings of a function definition all print as one — which is what a reference page
// wants.
//
// The types come from the AST, so what prints is what was *written*, with one
// deliberate exception: an omitted return type prints the inferred one, since a reader
// asking what a function returns wants the answer and not the omission.

// bindingSignature renders a `let`/`var`/`const`, function or not.
func bindingSignature(s *ast.VarDeclStmt) string {
	var b strings.Builder
	if s.IsPublic {
		b.WriteString("pub ")
	}
	b.WriteString(s.BindingKind.String())
	if s.IsMut {
		b.WriteString(" mut")
	}
	b.WriteString(" ")
	b.WriteString(s.Name)
	b.WriteString(genericList(s.GenericParams))

	lambda, isFn := s.Value.(*ast.LambdaExpr)
	if !isFn {
		if s.Type != nil {
			b.WriteString(": ")
			b.WriteString(typeName(s.Type))
		}
		return b.String()
	}

	b.WriteString(whereClause(s.GenericParams, lambda))
	b.WriteString(" = ")
	b.WriteString(lambdaSignature(lambda))
	return b.String()
}

// lambdaSignature renders the modifiers, parameters and return type of a function
// value — everything up to but not including the body.
func lambdaSignature(l *ast.LambdaExpr) string {
	var b strings.Builder
	for _, mod := range []struct {
		on   bool
		word string
	}{
		{l.IsUnsafe, "unsafe"},
		{l.IsPure, "pure"},
		{l.IsDet, "det"},
		{l.IsNoAlloc, "noalloc"},
		{l.IsAsync, "async"},
		{l.IsGenerator, "gen"},
	} {
		if mod.on {
			b.WriteString(mod.word)
			b.WriteString(" ")
		}
	}

	b.WriteString("(")
	b.WriteString(strings.Join(parameterList(l), ", "))
	b.WriteString(")")

	if rt := l.ReturnType.Type; rt != nil {
		b.WriteString(" -> ")
		if mod := string(l.ReturnType.TypeModifier); mod != "" {
			b.WriteString(mod)
			b.WriteString(" ")
		}
		b.WriteString(typeName(rt))
	}
	return b.String()
}

// parameterList renders each parameter as `name: Type`, with its modifier and default.
//
// A multi-clause function has no named parameters of its own — the clauses bind
// patterns — so it falls back to the declared types alone, which is the whole of what
// the signature knows.
func parameterList(l *ast.LambdaExpr) []string {
	out := make([]string, 0, len(l.Parameters))
	for i := range l.Parameters {
		p := &l.Parameters[i]
		var b strings.Builder
		if p.Pattern != nil {
			if name := p.Pattern.GetName(); name != "" {
				b.WriteString(name)
				if p.Type != nil {
					b.WriteString(": ")
				}
			}
		}
		if p.Type != nil {
			// The borrow modifier binds to the **type**, after the colon:
			// `(self: mut Rng)`, not `(mut self: Rng)`. The other order is a
			// syntax error, and rendering it produced a page whose signatures
			// looked plausible and did not parse.
			if mod := string(p.TypeModifier); mod != "" {
				b.WriteString(mod)
				b.WriteString(" ")
			}
			b.WriteString(typeName(p.Type))
		}
		if p.DefaultValue != nil {
			b.WriteString(" = ")
			b.WriteString(renderConstant(p.DefaultValue))
		}
		out = append(out, b.String())
	}
	return out
}

// renderConstant prints a parameter's default value. Only the literal forms a default
// can actually take are handled; anything else prints as `…` rather than as a Go struct
// dump, which is what `%v` on an AST node produces.
func renderConstant(e ast.Expression) string {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		return fmt.Sprintf("%d", v.Value)
	case *ast.FloatLiteralExpr:
		return fmt.Sprintf("%g", v.Value)
	case *ast.StringLiteralExpr:
		return fmt.Sprintf("%q", v.Value)
	case *ast.BooleanLiteralExpr:
		return fmt.Sprintf("%t", v.Value)
	case *ast.IdentifierExpr:
		return v.Name
	}
	return "…"
}

// genericList renders `<t, u>`; the bounds go in a `where` clause instead, because that
// is where a multi-bound parameter has to live and one spelling is better than two.
func genericList(params []ast.GenericParam) string {
	if len(params) == 0 {
		return ""
	}
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return "<" + strings.Join(names, ", ") + ">"
}

// whereClause renders the bounds on a declaration's type parameters.
//
// It reads them from both places they can be recorded: GenericParam.Constraints, filled
// in from an inline `<t: Show>`, and LambdaExpr.GenericBounds, which the collector lifts
// off a trailing `where` clause. A signature that showed only one of the two would drop
// half the bounds in the language depending on which spelling the author chose.
func whereClause(params []ast.GenericParam, l *ast.LambdaExpr) string {
	seen := map[string]bool{}
	var parts []string
	add := func(name string, bounds []string) {
		if len(bounds) == 0 || seen[name] {
			return
		}
		seen[name] = true
		parts = append(parts, name+": "+strings.Join(bounds, " + "))
	}
	// Declaration order, so the clause reads in the order the parameters do.
	for _, p := range params {
		if l != nil && len(l.GenericBounds[p.Name]) > 0 {
			add(p.Name, l.GenericBounds[p.Name])
			continue
		}
		add(p.Name, p.Constraints)
	}
	if len(parts) == 0 {
		return ""
	}
	return " where " + strings.Join(parts, ", ")
}

// typeSignature renders a `type` declaration's head — the line a reader scans for, not
// the whole body. Fields and constructors are listed as members instead, where each can
// carry its own documentation.
func typeSignature(s *ast.TypeDeclStmt) string {
	var b strings.Builder
	if s.IsPublic {
		b.WriteString("pub ")
	}
	name := s.Name + genericList(s.GenericParams)

	switch t := s.Type.(type) {
	case types.NamedStructType:
		b.WriteString("struct " + name)
	case types.DataType:
		b.WriteString("data " + name + " = " + constructorList(t))
	case *types.ConstrainedType:
		b.WriteString("newtype " + name + " = " + typeName(t.Type))
	case types.TupleType:
		b.WriteString("tuple " + name + "(" + typeNames(t.Elements) + ")")
	default:
		if s.IsAlias {
			b.WriteString("type " + name + " = " + typeName(s.Type))
		} else {
			b.WriteString("type " + name)
		}
	}
	return b.String()
}

// constructorList renders `None | Some t`, which is short enough to belong on the
// signature line and is the thing a reader of a sum type most wants to see at once.
func constructorList(t types.DataType) string {
	parts := make([]string, len(t.Constructors))
	for i, c := range t.Constructors {
		parts[i] = constructorSignature(c)
	}
	return strings.Join(parts, " | ")
}

func constructorSignature(c types.DataTypeConstructor) string {
	fields := c.FieldTypes()
	if len(fields) == 0 {
		return c.Name
	}
	// A packed positional payload was written parenthesized (`Rect(i64, i64)`); a
	// single unpacked one was written juxtaposed (`Some t`). Rendering them alike
	// would print a spelling the parser rejects for one of the two.
	if c.Packed {
		return c.Name + "(" + typeNames(fields) + ")"
	}
	return c.Name + " " + typeNames(fields)
}

func typeMembers(s *ast.TypeDeclStmt) []Member {
	var out []Member
	switch t := s.Type.(type) {
	case types.NamedStructType:
		for _, f := range t.Fields {
			sig := f.Name + ": " + typeName(f.Type)
			if f.Frozen {
				sig = "readonly " + sig
			}
			out = append(out, Member{Name: f.Name, Signature: sig, Doc: s.MemberDoc(f.Name)})
		}
	case types.DataType:
		for _, c := range t.Constructors {
			out = append(out, Member{
				Name:      c.Name,
				Signature: constructorSignature(c),
				Doc:       s.MemberDoc(c.Name),
			})
		}
	}
	return out
}

func traitSignature(s *ast.TraitDeclStmt) string {
	var b strings.Builder
	if s.IsPublic {
		b.WriteString("pub ")
	}
	b.WriteString("trait " + s.Name + genericList(s.GenericParams))
	if len(s.Bounds) > 0 {
		b.WriteString(": " + strings.Join(s.Bounds, " + "))
	}
	return b.String()
}

// A method's name on a page is `MethodName.Key()`, never `GetName()` — the same
// source-syntax rule typeName follows, one level down. `GetName()` is the bare `Value`, so
// an operator-named method renders as `/` where an author writes `(_/_)`: a line that does
// not compile, on a page read as the code to write. It also erases *kind*, which is part of
// a method's identity — prefix `-` and binary `-` share a spelling and are different
// methods, so both would render identically as `-`.
//
// Invisible until the prelude shipped operator-named methods (`Add`/`Sub`/`Mul`/`Div`,
// 08/14), because every trait method before them was an ordinary identifier, where the two
// agree.
func traitMembers(s *ast.TraitDeclStmt) []Member {
	out := make([]Member, 0, len(s.Methods))
	for i := range s.Methods {
		m := &s.Methods[i]
		var b strings.Builder
		for _, mod := range []struct {
			on   bool
			word string
		}{{m.IsPure, "pure"}, {m.IsDet, "det"}, {m.IsNoAlloc, "noalloc"}} {
			if mod.on {
				b.WriteString(mod.word + " ")
			}
		}
		b.WriteString(m.Name.Key())
		if m.Signature != nil {
			b.WriteString(": " + typeName(m.Signature))
		}
		out = append(out, Member{Name: m.Name.Key(), Signature: b.String(), Doc: m.Doc})
	}
	return out
}

// implName is what an impl is listed and sorted under. `Ord for string` rather than
// `Ord`, because a trait is implemented for many types and the page would otherwise
// show several entries with one name.
func implName(s *ast.TraitImplStmt) string {
	return s.TraitName + " for " + typeName(s.Type)
}

func implSignature(s *ast.TraitImplStmt) string {
	var b strings.Builder
	b.WriteString("impl " + s.TraitName)
	if len(s.TraitArgs) > 0 {
		b.WriteString("<" + typeNames(s.TraitArgs) + ">")
	}
	b.WriteString(" for " + typeName(s.Type))
	if len(s.Constraints) > 0 {
		parts := make([]string, len(s.Constraints))
		for i, c := range s.Constraints {
			parts[i] = c.GenericType + ": " + strings.Join(c.TraitBounds, " + ")
		}
		b.WriteString(" where " + strings.Join(parts, ", "))
	}
	return b.String()
}

func implMembers(s *ast.TraitImplStmt) []Member {
	out := make([]Member, 0, len(s.Methods))
	for i := range s.Methods {
		m := &s.Methods[i]
		var b strings.Builder
		for _, mod := range []struct {
			on   bool
			word string
		}{{m.IsPure, "pure"}, {m.IsDet, "det"}, {m.IsNoAlloc, "noalloc"}} {
			if mod.on {
				b.WriteString(mod.word + " ")
			}
		}
		// Key(), not GetName() — see traitMembers.
		b.WriteString(m.Name.Key())
		out = append(out, Member{Name: m.Name.Key(), Signature: b.String(), Doc: m.Doc})
	}
	return out
}

// typeName renders a type in **source syntax**, nil-safe.
//
// It is not `t.GetName()` or `t.String()`, and it cannot be. Those exist for diagnostics,
// where a type is *described*; a documentation page is read as the code to write, so
// every name on it has to be a spelling the parser accepts. The two disagree in ways
// that are individually small and collectively fatal to that promise:
//
//	GetName()                 source            why it matters
//	DynamicArray<string>      []string          `DynamicArray` is not a word in Lyra
//	StaticArray<i64, 3>       [3]i64            same
//	AnonymousTuple(i64, u8)   (i64, u8)         same
//	Maybe                     Maybe<t>          GetName drops the arguments entirely
//
// The last is the worst of them: a page rendering `self: Maybe` states a type that
// exists and is the wrong one. Anything this function does not special-case falls
// through to String(), which is right for a primitive, a named type and a type
// variable — the cases where the diagnostic spelling *is* the source spelling.
//
// A nil type is an un-annotated binding inference did not reach; `?` matches what
// types.ReturnType already prints rather than crashing a run on an incomplete program.
func typeName(t types.Type) string {
	switch v := t.(type) {
	case nil:
		return "?"
	case types.PrimitiveType:
		return primitiveName(v.Name)
	case types.DynamicArrayType:
		return allocationPrefix(v.Allocation) + "[]" + typeName(v.ElementType)
	case types.StaticArrayType:
		return allocationPrefix(v.Allocation) + fmt.Sprintf("[%d]", v.Size) + typeName(v.ElementType)
	case types.TupleType:
		// An anonymous tuple is written as a bare parenthesized list; a named one
		// is its name applied to that list.
		if types.IsAnonymousTupleName(v.Name) {
			return "(" + typeNames(v.Elements) + ")"
		}
		return v.Name + "(" + typeNames(v.Elements) + ")"
	case types.ParameterizedType:
		if len(v.TypeArguments) == 0 {
			return v.Name
		}
		return v.Name + "<" + typeNames(v.TypeArguments) + ">"
	case types.WeakType:
		return "weak " + typeName(v.Inner)
	case types.RawPointerType:
		if v.IsMut {
			return "^mut " + typeName(v.Pointee)
		}
		return "^" + typeName(v.Pointee)
	case types.NamedStructType:
		return allocationPrefix(v.Allocation) + v.Name
	case types.AnonymousStructType:
		fields := make([]string, len(v.Fields))
		for i, f := range v.Fields {
			fields[i] = f.Name + ": " + typeName(f.Type)
		}
		return "struct { " + strings.Join(fields, ", ") + " }"
	case *types.LambdaType:
		params := make([]string, len(v.Parameters))
		for i, p := range v.Parameters {
			// **A named parameter renders its name**, which only an `extern` has: E067
			// requires one there and refuses it in a plain function type, so rendering it
			// whenever present is right everywhere without asking which kind this is.
			// It also has to be rendered — a page's signature is read as the code to
			// write, and an extern printed without its names no longer parses.
			params[i] = typeName(p.Type)
			if p.Name != "" {
				params[i] = p.Name + ": " + params[i]
			}
		}
		if v.IsVariadic {
			params = append(params, "...")
		}
		return v.EffectPrefix() + "(" + strings.Join(params, ", ") + ") -> " + typeName(v.ReturnType.Type)
	}
	return t.String()
}

// primitiveName is the *keyword* for a primitive, which is not always what the type
// calls itself.
//
// Three of them differ, and each differs because the internal name is written for a
// diagnostic rather than for a reader copying it:
//
//   - `boolean` is spelled `bool` in the language. There is no `boolean` keyword, so a
//     signature printing one does not compile.
//   - the untyped literal types render as "integer literal" / "float literal", which are
//     phrases for a sentence ("cannot assign integer literal to string"), not types. A
//     binding still untyped at the end of inference takes the literal's default, so the
//     type a caller sees is `i64` or `f64`.
func primitiveName(n types.PrimitiveTypeName) string {
	switch n {
	case types.Boolean:
		return "bool"
	case types.UntypedInt, types.UntypedSignedInt:
		return "i64"
	case types.UntypedFloat:
		return "f64"
	}
	return string(n)
}

// allocationPrefix renders `shared `/`stack `/`fixed ` before a type that carries one.
// Unspecified renders nothing, which is the common case and the one with no keyword.
func allocationPrefix(a types.AllocationModifier) string {
	if a == types.AllocationModifier("") || a == types.Unspecified {
		return ""
	}
	return string(a) + " "
}

func typeNames(ts []types.Type) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = typeName(t)
	}
	return strings.Join(parts, ", ")
}

// externSignature renders an `extern` declaration in source syntax:
//
//	@link("z")
//	unsafe extern pure crc32: (crc: u64, buf: ^u8, len: u32) -> u64
//
// **The `@link` lines are part of it**, which is the one way this differs from every
// other signature on a page. Elsewhere an attribute is metadata a reader can ignore;
// here it is a build requirement that rides the declaration, so a page omitting it
// documents a function the reader cannot successfully call.
//
// `unsafe` goes before `extern` and the bound after it, because that is where the
// language puts them: the keyword marks the *claim*, and the claim follows it.
func externSignature(s *ast.ExternDeclStmt) string {
	var b strings.Builder
	for _, lib := range s.Links {
		b.WriteString("@link(\"")
		b.WriteString(lib)
		b.WriteString("\")\n")
	}
	if s.IsUnsafe {
		b.WriteString("unsafe ")
	}
	b.WriteString("extern")
	for _, mod := range []struct {
		on   bool
		word string
	}{{s.IsPure, "pure"}, {s.IsDet, "det"}, {s.IsNoAlloc, "noalloc"}} {
		if mod.on {
			b.WriteString(" ")
			b.WriteString(mod.word)
		}
	}
	b.WriteString(" ")
	b.WriteString(s.Name)
	b.WriteString(": ")
	b.WriteString(typeName(s.Signature))
	return b.String()
}
