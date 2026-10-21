package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `lyrac run --live`: --watch, except that a change is **swapped into the running
// program** when that is safe, so it keeps its state — `main`'s locals, the heap, the
// globals, and what it holds through C (a window, a GPU device). The design is
// pkg/backend/LIVE.md; the backend's half is pkg/backend/llvm/live.go.
//
// The program is started as **generation 0**, an executable whose `main` calls through
// patchable slots and polls at its loops' back-edges. Each good save after that is built
// as **generation N**, the whole program as a bundle linked against generation 0, and
// checked against generation 0 — not against the generation before it, so every one
// applied is complete and skipping one costs nothing. Then lyrac writes the bundle's path
// down a pipe and sends SIGUSR1; the program loads it at its next back-edge in `main` and
// answers `applied` or `failed: <why>`.
//
// **When a change cannot be swapped safely lyrac restarts the program, as --watch does,
// and says why**, so --live is never worse than --watch: `main` itself changed (it is the
// frame that never returns), a type's fields or constructors changed (values already in
// memory have the old shape), a top-level `let` is new or changed type (its storage is
// generation 0's), the program links a new library, or the program did not answer — its
// loop is not in `main`, or it is blocked.
//
// macOS only so far; elsewhere --live is --watch, and says so once.

// liveReplyWait is how long the program has to answer a load. A frame loop answers within
// a frame; one that does not is not polling, and restarting it is the only way to update
// it.
const liveReplyWait = 2 * time.Second

// liveEmit asks lowerAndEmit for a live build's generation 0, and carries back the
// LiveInfo the backend produced.
type liveEmit struct {
	info *llvm.LiveInfo
}

// liveSession is a running generation 0's live half: what the generations after it are
// checked against, and the pipe they are sent down.
type liveSession struct {
	base      liveBase
	exe       string   // generation 0, which every bundle is linked against
	toProgram *os.File // the pipe each bundle's path goes down
	replies   chan string
	ready     atomic.Bool // the program has installed its handler and said so
	gen       int         // the last generation applied
}

// liveBase is what a generation must share with generation 0 to run on its state.
type liveBase struct {
	info   *llvm.LiveInfo
	shapes map[string]string // each type, by declaration key, to its Lyra shape
	inits  map[string]string // each top-level `let`/`var`'s symbol, to its initializer's text
	links  []string
}

// liveAvailable reports whether this run can swap code, and why not. Only a program with
// an OS, run here on macOS, can: the Genesis has no loader (`--live` there is `--watch`),
// and Linux is not done yet (todo.md).
func liveAvailable(o buildOptions, res *driver.Result) (bool, string) {
	if !res.Target.Host {
		return false, "a " + res.Target.Display + " program has no loader"
	}
	p, err := platformOf(o)
	if err != nil {
		return false, err.Error()
	}
	if !p.IsMachine() || p.OS != "darwin" {
		return false, "it swaps code only on macOS so far"
	}
	return true, ""
}

// startGeneration0 builds res as a live program's generation 0 and starts it, or returns
// nil with the build's errors printed.
func startGeneration0(o buildOptions, res *driver.Result, entry *driver.EntryPoint) *child {
	dir, err := os.MkdirTemp("", "lyrac-run-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return nil
	}
	o.ephemeral = true
	o.out = filepath.Join(dir, filepath.Base(replaceExt(o.path, "")))
	o.liveEmit = &liveEmit{}
	exe, code := lowerAndEmit(o, res, entry)
	if code != 0 {
		os.RemoveAll(dir)
		return nil
	}
	p, _ := platformOf(o)
	// The pipe is a pair of descriptors the program inherits as 3 and 4 (the backend's
	// liveReadFD and liveWriteFD), announced by LiveEnv; a program not started so has
	// neither, and its live pieces stay inert.
	toR, toW, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		os.RemoveAll(dir)
		return nil
	}
	fromR, fromW, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		toR.Close()
		toW.Close()
		os.RemoveAll(dir)
		return nil
	}
	cmd := exec.Command(exe, o.programArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{toR, fromW}
	cmd.Env = append(os.Environ(), llvm.LiveEnv+"=1")
	err = cmd.Start()
	toR.Close()
	fromW.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: cannot run %s: %v\n", exe, err)
		toW.Close()
		fromR.Close()
		os.RemoveAll(dir)
		return nil
	}
	s := &liveSession{
		base: liveBase{
			info:   o.liveEmit.info,
			shapes: typeShapes(res),
			inits:  initializers(res),
			links:  linkFlags(res, p),
		},
		exe:       exe,
		toProgram: toW,
		replies:   make(chan string, 4),
	}
	go s.listen(fromR)
	c := &child{cmd: cmd, dir: dir, done: make(chan error, 1), live: s}
	go func() { c.done <- cmd.Wait() }()
	return c
}

// listen reads the program's replies, one a line, until it exits. `ready` is noted
// rather than queued: it comes once, whenever the program starts, and a reload asks
// whether it has come rather than waiting for it.
func (s *liveSession) listen(from *os.File) {
	defer from.Close()
	defer close(s.replies)
	lines := bufio.NewScanner(from)
	for lines.Scan() {
		if lines.Text() == "ready" {
			s.ready.Store(true)
			continue
		}
		s.replies <- lines.Text()
	}
}

// reload is a save while generation 0 runs: build the program again and apply it to c,
// or start a new generation 0 in its place. It returns the files the build read; the
// child to replace c with (nil when c keeps running); and whether the change was
// applied. A nil child that was not applied is a build that failed, and c carries on.
func (c *child) reload(o buildOptions, changed string) (*child, []string, bool) {
	res, entry, files, _ := typedProgram(o.path)
	if entry == nil {
		return nil, files, false
	}
	s := c.live
	reason, lib := s.prepare(o, res, entry, c.dir)
	if reason == "" {
		reason = s.load(c, lib)
	}
	if reason == "" {
		s.gen++
		fmt.Fprintf(os.Stderr, "lyrac: %s changed; applied\n", filepath.Base(changed))
		for _, note := range s.base.notes(res) {
			fmt.Fprintf(os.Stderr, "lyrac: %s\n", note)
		}
		return nil, files, true
	}
	fmt.Fprintf(os.Stderr, "lyrac: %s changed; restarting: %s\n", filepath.Base(changed), reason)
	return startGeneration0(o, res, entry), files, false
}

// prepare builds res as the next generation and checks it against generation 0. It
// returns why it cannot be applied, or "" and the bundle to load.
func (s *liveSession) prepare(o buildOptions, res *driver.Result, entry *driver.EntryPoint, dir string) (string, string) {
	p, err := platformOf(o)
	if err != nil {
		return err.Error(), ""
	}
	gen := s.gen + 1
	be := llvm.NewForPlatform(p).Live(gen)
	ir, err := be.Emit(res, entry)
	if err != nil {
		return fmt.Sprintf("the new code did not compile: %v", err), ""
	}
	// Checked before linking, which is the slow half of a build, so a change that must
	// restart starts the new program as soon as it can.
	if why := s.base.incompatible(be.LiveInfo(), typeShapes(res), linkFlags(res, p)); why != "" {
		return why, ""
	}
	cc, err := findCC(o.cc)
	if err != nil {
		return err.Error(), ""
	}
	if err := llvm.CheckCoroutineSupport(ir, cc); err != nil {
		return err.Error(), ""
	}
	name := filepath.Base(replaceExt(o.path, ""))
	ll := filepath.Join(dir, fmt.Sprintf("%s.%d.ll", name, gen))
	if err := os.WriteFile(ll, ir, 0o644); err != nil {
		return err.Error(), ""
	}
	// **A bundle loaded by generation 0, linking no libraries.** `-bundle_loader` binds
	// what generation 0 defines — its globals, its slots — to generation 0's, and
	// `-undefined dynamic_lookup` leaves every foreign function to the process, which has
	// already loaded it: linking a static archive here would give the bundle a second copy
	// of its state (ImGui's context, the menu bar's delegate), and the program would split
	// in two. A function generation 0 never linked fails the load, which restarts.
	lib := filepath.Join(dir, fmt.Sprintf("%s.%d.bundle", name, gen))
	link := exec.Command(cc, o.opt, "-bundle", "-bundle_loader", s.exe, "-Wl,-undefined,dynamic_lookup", ll, "-o", lib)
	if out, err := link.CombinedOutput(); err != nil {
		return fmt.Sprintf("the new code did not link: %v\n%s", err, out), ""
	}
	return "", lib
}

// load sends lib to the running program and waits for its answer, returning why it was
// not applied, or "".
func (s *liveSession) load(c *child, lib string) string {
	if !s.ready.Load() {
		return "the program had not started"
	}
	if _, err := fmt.Fprintf(s.toProgram, "%s\n", lib); err != nil {
		return "the program is not listening: " + err.Error()
	}
	// The number from the backend's table, rather than syscall.SIGUSR1, which Windows
	// lacks — and liveAvailable has already said this is macOS.
	usr1, _ := llvm.LiveSignal("darwin")
	if err := c.cmd.Process.Signal(syscall.Signal(usr1)); err != nil {
		return "the program is not listening: " + err.Error()
	}
	select {
	case reply, ok := <-s.replies:
		switch {
		case !ok:
			return "the program exited"
		case reply == "applied":
			return ""
		default:
			return "the new code did not load: " + strings.TrimPrefix(reply, "failed: ")
		}
	case <-time.After(liveReplyWait):
		return fmt.Sprintf("the program did not take the change within %v — only a loop in `main` picks one up", liveReplyWait)
	}
}

// incompatible is why a generation with info cannot run on generation 0's state, or "".
func (b liveBase) incompatible(info *llvm.LiveInfo, shapes map[string]string, links []string) string {
	if info.Main != b.info.Main {
		return "`main` changed (only what it calls is swapped)"
	}
	for _, key := range llvm.SortedKeys(b.shapes) {
		if now, ok := shapes[key]; ok && now != b.shapes[key] {
			return fmt.Sprintf("type `%s` changed its fields or constructors", displayKey(key))
		}
	}
	// The layout in memory, which a type's Lyra shape does not show through an alias.
	for _, name := range llvm.SortedKeys(b.info.Types) {
		if now, ok := info.Types[name]; ok && now != b.info.Types[name] {
			return fmt.Sprintf("the layout of `%s` changed", strings.TrimPrefix(name, "%"))
		}
	}
	for _, sym := range llvm.SortedKeys(info.Globals) {
		was, ok := b.info.Globals[sym]
		switch {
		case !ok && strings.HasPrefix(sym, "lyra_global_"):
			return fmt.Sprintf("a top-level `%s` is new", globalName(sym))
		case !ok:
			return fmt.Sprintf("the runtime needs state it did not have (`%s`)", sym)
		case was != info.Globals[sym]:
			return fmt.Sprintf("top-level `%s` changed type", globalName(sym))
		}
	}
	if !slices.Equal(links, b.links) {
		return "the libraries it links changed"
	}
	return ""
}

// notes are what an applied generation leaves stale by design: a top-level value whose
// initializer changed keeps the value it has, since it is state.
func (b liveBase) notes(res *driver.Result) []string {
	var out []string
	now := initializers(res)
	for _, sym := range llvm.SortedKeys(b.inits) {
		if text, ok := now[sym]; ok && text != b.inits[sym] {
			out = append(out, fmt.Sprintf("`%s`'s initializer changed; it keeps its value until a restart", globalName(sym)))
		}
	}
	return out
}

// typeShapes is each type the program declares, by declaration key, with its Lyra shape:
// a struct's fields and a data type's constructors in order, each with its types. **The
// shape, not the LLVM layout**, because inserting a constructor in the middle renumbers
// the tags without changing the union's size, and a payload changing from i32 to f32 does
// not change it either.
func typeShapes(res *driver.Result) map[string]string {
	// Every declaration is read, not one resolved by name, so ranging over the table is
	// the right access here (rule 4 is about resolution).
	out := make(map[string]string, len(res.SymbolTable.Types))
	for key, decl := range res.SymbolTable.Types {
		out[key] = lyraShape(decl)
	}
	return out
}

func lyraShape(decl *ast.TypeDeclStmt) string {
	var b strings.Builder
	if decl.IsAlias {
		b.WriteString("alias ")
	}
	for _, g := range decl.GenericParams {
		fmt.Fprintf(&b, "<%s %v>", g.Name, g.Constraints)
	}
	fields := func(fs []types.StructField) {
		for _, f := range fs {
			fmt.Fprintf(&b, "%s: %s; ", f.Name, f.Type)
		}
	}
	switch t := decl.Type.(type) {
	case types.NamedStructType:
		fields(t.Fields)
	case *types.NamedStructType:
		fields(t.Fields)
	case types.UnionType:
		fields(t.Members)
	case *types.UnionType:
		fields(t.Members)
	case types.DataType:
		dataShape(&b, t.Constructors)
	case *types.DataType:
		dataShape(&b, t.Constructors)
	default:
		fmt.Fprintf(&b, "%s", decl.Type)
	}
	return b.String()
}

func dataShape(b *strings.Builder, ctors []types.DataTypeConstructor) {
	for _, c := range ctors {
		fmt.Fprintf(b, "%s(", c.Name)
		for _, p := range c.Params {
			fmt.Fprintf(b, "%s, ", p)
		}
		b.WriteString("); ")
	}
}

// initializers is the source text of each top-level `let`/`var` holding data, by the
// symbol of its global (the backend's `lyra_global_<module>.<name>`).
func initializers(res *driver.Result) map[string]string {
	out := map[string]string{}
	files := map[string][]string{}
	for _, stmt := range res.Program.Statements {
		vd, ok := stmt.(*ast.VarDeclStmt)
		if !ok || vd.Value == nil || vd.BindingKind == ast.BindingConst {
			continue
		}
		if _, isFunc := vd.Value.(*ast.LambdaExpr); isFunc {
			continue
		}
		loc := vd.Value.GetLocation()
		lines, ok := files[loc.File]
		if !ok {
			src, _ := os.ReadFile(loc.File)
			lines = strings.Split(string(src), "\n")
			files[loc.File] = lines
		}
		symbol := "lyra_global_" + vd.Name
		if module := res.SymbolTable.ModuleOfFile[vd.GetLocation().File]; module != "" {
			symbol = "lyra_global_" + module + "." + vd.Name
		}
		out[symbol] = spanText(lines, loc)
	}
	return out
}

// spanText is the text loc covers, or "" when the lines do not reach it.
func spanText(lines []string, loc ast.Location) string {
	if loc.StartLine < 1 || loc.EndLine > len(lines) || loc.StartLine > loc.EndLine {
		return ""
	}
	span := slices.Clone(lines[loc.StartLine-1 : loc.EndLine])
	last := len(span) - 1
	if loc.EndCol-1 <= len(span[last]) && loc.EndCol > 0 {
		span[last] = span[last][:loc.EndCol-1]
	}
	if loc.StartCol-1 <= len(span[0]) && loc.StartCol > 0 {
		span[0] = span[0][loc.StartCol-1:]
	}
	return strings.Join(span, "\n")
}

// displayKey is a type's declaration key (`<module>::<name>`) as a person reads it.
func displayKey(key string) string {
	if i := strings.LastIndex(key, "::"); i >= 0 {
		return key[i+2:]
	}
	return key
}

// globalName is a top-level value's global symbol as a person reads it.
func globalName(sym string) string {
	name := strings.TrimPrefix(sym, "lyra_global_")
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
