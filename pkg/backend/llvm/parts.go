package llvm

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/value"
)

// Part is one piece of a program split for separate compilation: a whole LLVM module of
// its own, which compiles alone and links with the others (EmitParts).
type Part struct {
	// Name says what the part holds — `m.ui.layout`, `m.ui.layout.2` (a large module's
	// second piece), `p.3` (a program part), `globals` — for messages and file names.
	Name string
	IR   []byte
}

// ErrNotSplittable is EmitParts refusing a module whose shape it does not handle: the
// caller builds it whole, as before.
var ErrNotSplittable = errors.New("llvm: the module cannot be split")

// EmitParts is Emit split into Parts that compile separately — so `lyrac` can compile
// them in parallel and keep each object, keyed by its text, for the next build to reuse
// (cmd/lyrac/objects.go). Linked together, they are the program Emit's one module is.
//
// **A part's text depends only on what it holds**, so an edit recompiles the parts it
// touched and no others:
//
//   - A function written in a module goes to that module's part (`m.<module>`, split in
//     pieces by name when the module is large), so `std` and `ui` parts are the same text
//     in every program that does not change them.
//   - Everything the program makes for itself — a generic's specializations, closures,
//     drop/retain/equality glue — goes to program parts (`p.<k>`, by name), and the
//     module's mutable globals to `globals`.
//   - A name numbered program-wide is given one that stays put (stableNames): a closure by
//     its lambda's place, glue by its type, a string constant by its content.
//   - A part declares what it uses from the others; private constants are copied into
//     every part using them (they are all `constant`, so a copy is the same value).
//
// The module is renamed in place, so it is good for this one call; Emit and EmitParts each
// emit afresh. A shape the split does not handle — a function that is not external, an
// alias, metadata — is ErrNotSplittable rather than a guess.
func (b *Backend) EmitParts(res *driver.Result, entry *driver.EntryPoint) ([]Part, error) {
	m, err := b.emitModule(res, entry)
	if err != nil {
		return nil, err
	}
	if err := splittable(m); err != nil {
		return nil, err
	}
	coro := map[*ir.Func]bool{}
	byName := map[string]*ir.Func{}
	for _, f := range m.Funcs {
		byName[f.Name()] = f
	}
	for _, sym := range b.coroSymbols {
		if f, ok := byName[sym]; ok {
			coro[f] = true
		}
	}
	renameStably(m, b.stableNames)
	nameConstants(m)
	return assembleParts(m, b.funcModule, coro)
}

// splittable refuses what the split does not handle, so nothing is guessed at.
func splittable(m *ir.Module) error {
	if len(m.Aliases) > 0 || len(m.IFuncs) > 0 || len(m.ComdatDefs) > 0 ||
		len(m.NamedMetadataDefs) > 0 || len(m.MetadataDefs) > 0 || len(m.ModuleAsms) > 0 {
		return fmt.Errorf("%w: it has aliases, comdats, metadata or module asm", ErrNotSplittable)
	}
	for _, f := range m.Funcs {
		if len(f.Blocks) > 0 && f.Linkage != enum.LinkageNone && f.Linkage != enum.LinkageExternal {
			return fmt.Errorf("%w: @%s is %s", ErrNotSplittable, f.Name(), f.Linkage)
		}
	}
	for _, g := range m.Globals {
		private := g.Linkage == enum.LinkagePrivate || g.Linkage == enum.LinkageInternal
		if private && !g.Immutable {
			return fmt.Errorf("%w: @%s is a private variable", ErrNotSplittable, g.Name())
		}
		if !private && g.Linkage != enum.LinkageNone && g.Linkage != enum.LinkageExternal {
			return fmt.Errorf("%w: @%s is %s", ErrNotSplittable, g.Name(), g.Linkage)
		}
		if strings.HasPrefix(g.Name(), "llvm.") {
			return fmt.Errorf("%w: @%s is LLVM's own", ErrNotSplittable, g.Name())
		}
	}
	return nil
}

// numberedGlue is a function named by a program-wide count before what it is for: drop,
// retain and equality glue by its type, a coroutine by its function.
var numberedGlue = regexp.MustCompile(`^(lyra_drop|lyra_retain|lyra_eq|lyra\.seq)[_.]\d+[_.](.+)$`)

// renameStably gives each function numbered program-wide its stable name: the one the
// lowering recorded (a closure, an environment's drop), or its numbered name less the
// number (glue, coroutines). Two wanting one name keep module order, the later numbered.
func renameStably(m *ir.Module, recorded map[*ir.Func]string) {
	taken := map[string]bool{}
	for _, f := range m.Funcs {
		taken[f.Name()] = true
	}
	for _, g := range m.Globals {
		taken[g.Name()] = true
	}
	for _, f := range m.Funcs {
		want, ok := recorded[f]
		if !ok {
			match := numberedGlue.FindStringSubmatch(f.Name())
			if match == nil {
				continue
			}
			want = match[1] + "." + match[2]
		}
		name := want
		for i := 2; taken[name]; i++ {
			name = fmt.Sprintf("%s.%d", want, i)
		}
		delete(taken, f.Name())
		taken[name] = true
		f.SetName(name)
	}
}

// nameConstants names every private constant by its content (`.k.<hash>`), so one used by
// a part reads the same whatever else the program holds. A constant naming another is
// named after it; two of one content share a name, and a part emits it once.
func nameConstants(m *ir.Module) {
	named := map[*ir.Global]bool{}
	var name func(g *ir.Global, depth int)
	name = func(g *ir.Global, depth int) {
		if named[g] || depth > 64 {
			return
		}
		named[g] = true
		refs := map[any]bool{}
		walkRefs(reflect.ValueOf(g.Init), refs, map[uintptr]bool{})
		for r := range refs {
			if dep, ok := r.(*ir.Global); ok && isPrivate(dep) {
				name(dep, depth+1)
			}
		}
		text := g.LLString()
		text = text[strings.Index(text, " = ")+3:]
		g.SetName(".k." + shortHash(text))
	}
	for _, g := range m.Globals {
		if isPrivate(g) {
			name(g, 0)
		}
	}
}

func isPrivate(g *ir.Global) bool {
	return g.Linkage == enum.LinkagePrivate || g.Linkage == enum.LinkageInternal
}

// moduleOfFile is the module a file belongs to ("" for the entry's, or with no table).
func (l *lowerer) moduleOfFile(file string) string {
	if l.res == nil || l.res.SymbolTable == nil {
		return ""
	}
	return l.res.SymbolTable.ModuleOfFile[file]
}

// shortHash is a stable name for a piece of text: 16 hex digits of its SHA-256.
func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

const (
	// partFuncs is about how many functions a part holds before a module's are split by
	// name. Small enough that a large module compiles on several cores, large enough that
	// a part is not mostly the declarations of its neighbours.
	partFuncs = 400
	// maxPieces caps how many parts one module, or the program's own code, becomes.
	maxPieces = 16
)

// pieces is how many parts n functions make: a power of two, so a module growing past a
// boundary reshuffles its parts once rather than at every function.
func pieces(n int) int {
	k := 1
	for k < maxPieces && n > k*partFuncs {
		k *= 2
	}
	return k
}

func bucket(name string, k int) int {
	if k == 1 {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	return int(h.Sum32() % uint32(k))
}

// assembleParts places every defined function and mutable global, then writes each part:
// the type definitions it names, the constants and globals it uses (globals declared where
// another part defines them), the functions it calls declared, and its own defined.
func assembleParts(m *ir.Module, funcModule map[*ir.Func]string, coro map[*ir.Func]bool) ([]Part, error) {
	// Each defined function's group: its module, or the program.
	groupOf := map[*ir.Func]string{}
	count := map[string]int{}
	for _, f := range m.Funcs {
		if len(f.Blocks) == 0 {
			continue
		}
		group := "p"
		if mod, ok := funcModule[f]; ok {
			if mod == "" {
				mod = "main"
			}
			group = "m." + mod
		}
		groupOf[f] = group
		count[group]++
	}
	partOf := map[*ir.Func]string{}
	for f, group := range groupOf {
		k := pieces(count[group])
		i := bucket(f.Name(), k)
		switch {
		case group == "p":
			partOf[f] = fmt.Sprintf("p.%d", i)
		case i == 0:
			partOf[f] = group
		default:
			partOf[f] = fmt.Sprintf("%s.%d", group, i+1)
		}
	}
	var names []string
	members := map[string][]*ir.Func{}
	for _, f := range m.Funcs { // module order, so a part's text is deterministic
		if p, ok := partOf[f]; ok {
			if _, seen := members[p]; !seen {
				names = append(names, p)
			}
			members[p] = append(members[p], f)
		}
	}
	sort.Strings(names)
	var mutable []*ir.Global
	for _, g := range m.Globals {
		if !isPrivate(g) {
			mutable = append(mutable, g)
		}
	}
	if len(mutable) > 0 {
		names = append(names, "globals")
	}

	types := typeDefLines(m)
	stubs := map[*ir.Func]string{}
	var parts []Part
	for _, name := range names {
		var defs []*ir.Func
		var ownGlobals []*ir.Global
		if name == "globals" {
			ownGlobals = mutable
		} else {
			defs = members[name]
		}
		text, err := writePart(m, defs, ownGlobals, types, stubs, coro)
		if err != nil {
			return nil, fmt.Errorf("llvm: part %s: %w", name, err)
		}
		parts = append(parts, Part{Name: name, IR: []byte(text)})
	}
	return parts, nil
}

// writePart is one part's text.
func writePart(m *ir.Module, defs []*ir.Func, ownGlobals []*ir.Global, typeLines map[string]string, stubs map[*ir.Func]string, coro map[*ir.Func]bool) (string, error) {
	refs := map[any]bool{}
	seen := map[uintptr]bool{}
	defined := map[*ir.Func]bool{}
	for _, f := range defs {
		defined[f] = true
		for _, block := range f.Blocks {
			for _, inst := range block.Insts {
				ops, ok := inst.(interface{ Operands() []*value.Value })
				if !ok {
					return "", fmt.Errorf("%w: %T lists no operands", ErrNotSplittable, inst)
				}
				for _, op := range ops.Operands() {
					walkRefs(reflect.ValueOf(*op), refs, seen)
				}
			}
			if ops, ok := block.Term.(interface{ Operands() []*value.Value }); ok {
				for _, op := range ops.Operands() {
					walkRefs(reflect.ValueOf(*op), refs, seen)
				}
			} else if block.Term != nil {
				return "", fmt.Errorf("%w: %T lists no operands", ErrNotSplittable, block.Term)
			}
		}
	}
	for _, g := range ownGlobals {
		walkRefs(reflect.ValueOf(g.Init), refs, seen)
	}
	// What the constants used refer to, transitively.
	for changed := true; changed; {
		changed = false
		for r := range refs {
			g, ok := r.(*ir.Global)
			if !ok || !isPrivate(g) {
				continue
			}
			before := len(refs)
			walkRefs(reflect.ValueOf(g.Init), refs, seen)
			changed = changed || len(refs) != before
		}
	}

	var body strings.Builder
	owned := map[*ir.Global]bool{}
	for _, g := range ownGlobals {
		owned[g] = true
	}
	written := map[string]bool{}
	for _, g := range m.Globals { // module order
		switch {
		case owned[g]:
			body.WriteString(g.LLString())
		case !refs[g]:
			continue
		case isPrivate(g):
			if written[g.Name()] {
				continue // a twin by content (nameConstants)
			}
			written[g.Name()] = true
			body.WriteString(g.LLString())
		default:
			decl := ir.NewGlobal(g.Name(), g.ContentType)
			decl.Immutable = g.Immutable
			decl.Linkage = enum.LinkageExternal
			body.WriteString(decl.LLString())
		}
		body.WriteByte('\n')
	}
	for _, f := range m.Funcs {
		if defined[f] || !refs[f] {
			continue
		}
		if len(f.Blocks) == 0 {
			body.WriteString(f.LLString()) // already a declaration
		} else {
			body.WriteString(declarationOf(f, stubs))
		}
		body.WriteByte('\n')
	}
	for _, f := range defs {
		text := f.LLString()
		if coro[f] {
			text = addCoroutineAttributes(text, []string{f.Name()})
		}
		body.WriteString("\n")
		body.WriteString(text)
		body.WriteString("\n")
	}

	// The type definitions it names, and those they name.
	used := map[string]bool{}
	var pending []string
	note := func(text string) {
		for _, ident := range typeRef.FindAllString(text, -1) {
			if _, isType := typeLines[ident]; isType && !used[ident] {
				used[ident] = true
				pending = append(pending, ident)
			}
		}
	}
	note(body.String())
	for len(pending) > 0 {
		ident := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		note(typeLines[ident])
	}
	var out strings.Builder
	for _, t := range m.TypeDefs { // module order
		ident := t.String()
		if used[ident] {
			out.WriteString(typeLines[ident])
			out.WriteByte('\n')
		}
	}
	out.WriteString(body.String())
	return out.String(), nil
}

// typeRef is how a named type appears in IR text: `%Name` or `%"quoted name"`. A local
// value has the same shape; one that happens to share a type's name only brings that
// type's definition along.
var typeRef = regexp.MustCompile(`%("(?:[^"\\]|\\.)*"|[-a-zA-Z$._][-a-zA-Z$._0-9]*)`)

func typeDefLines(m *ir.Module) map[string]string {
	lines := map[string]string{}
	for _, t := range m.TypeDefs {
		lines[t.String()] = fmt.Sprintf("%s = type %s", t, t.LLString())
	}
	return lines
}

// declarationOf is a defined function's declaration, for the parts that call it: its
// signature, parameter and return attributes and calling convention — what a call must
// agree on — and no body.
func declarationOf(f *ir.Func, stubs map[*ir.Func]string) string {
	if text, ok := stubs[f]; ok {
		return text
	}
	params := make([]*ir.Param, len(f.Params))
	for i, p := range f.Params {
		params[i] = ir.NewParam("", p.Typ)
		params[i].Attrs = p.Attrs
	}
	decl := ir.NewFunc(f.Name(), f.Sig.RetType, params...)
	decl.Sig.Variadic = f.Sig.Variadic
	decl.CallingConv = f.CallingConv
	decl.ReturnAttrs = f.ReturnAttrs
	text := decl.LLString()
	stubs[f] = text
	return text
}

var (
	funcPtrType   = reflect.TypeOf(&ir.Func{})
	globalPtrType = reflect.TypeOf(&ir.Global{})
	constantPkg   = reflect.TypeOf(constant.Int{}).PkgPath()
)

// walkRefs adds to refs every function and global a value names, looking through
// constant expressions, aggregates and vectors — by reflection, so a kind of constant
// llir has and this file does not mention is still searched. Only the `constant` package's
// values are entered: a function or global ends the walk (what it holds is its own part's
// business), and a local value — an instruction, a parameter, a block — names nothing a
// part must declare.
func walkRefs(v reflect.Value, refs map[any]bool, seen map[uintptr]bool) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkRefs(v.Elem(), refs, seen)
		}
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		switch {
		case v.Type() == funcPtrType, v.Type() == globalPtrType:
			refs[v.Interface()] = true
		case v.Type().Elem().PkgPath() == constantPkg && !seen[v.Pointer()]:
			seen[v.Pointer()] = true
			walkRefs(v.Elem(), refs, seen)
		}
	case reflect.Struct:
		if v.Type().PkgPath() != constantPkg {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkRefs(v.Field(i), refs, seen)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkRefs(v.Index(i), refs, seen)
		}
	}
}
