package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
)

// Formatting implements textDocument/formatting by running **lyrafmt**, the formatter
// written in Lyra (`examples/lyrafmt/`), over the buffer's text.
//
// **It shells out rather than reimplementing the rules here**, and that is the whole
// design. lyrafmt is the self-hosting probe: its indentation, spacing and whitespace rules
// are ~250 lines of Lyra whose answers are pinned by a test over every file in the repo. A
// Go copy beside it would be a second implementation of one question — the drift this
// project refuses everywhere else it has two passes deciding the same thing — and the two
// would disagree the first day either changed. One formatter, two callers: the CLI and this.
//
// The cost is that lyrafmt must be *built* (it links the tree-sitter runtime and the
// grammar; see `examples/lyrafmt/libs.sh`), and an editor whose machine has no lyrafmt gets
// no formatting. That is the honest trade and it degrades quietly: no binary, a syntax
// error, or a lyrafmt that fails for any reason returns **no edits**, never an error dialog
// and never a partial rewrite. A formatter that mangles a buffer is far worse than one that
// declines.
//
// The go-lsp library registers the capability automatically when this method is present.
func (h *Handler) Formatting(_ context.Context, params *lsp.DocumentFormattingParams) (result []lsp.TextEdit, retErr error) {
	defer recoverHandler("formatting", &result, &retErr)

	_, source, ok := h.docFor(string(params.TextDocument.URI))
	if !ok || source == "" {
		return nil, nil
	}
	formatter, ok := lyrafmtPath()
	if !ok {
		return nil, nil
	}
	formatted, ok := runLyrafmt(formatter, source)
	if !ok || formatted == source {
		return nil, nil
	}
	// One edit covering the whole document. A minimal diff would be kinder to the
	// client's cursor, but the client computes that itself from a whole-file replace far
	// better than a line differ here would — and a wrong range on a formatting edit
	// corrupts the buffer.
	return []lsp.TextEdit{{Range: wholeDocument(source), NewText: formatted}}, nil
}

// wholeDocument is the range covering every character of source, in the UTF-16 units LSP
// counts positions in.
func wholeDocument(source string) lsp.Range {
	lines := strings.Split(source, "\n")
	last := len(lines) - 1
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   lsp.Position{Line: last, Character: utf16Len16(lines[last])},
	}
}

// runLyrafmt formats source by piping it through `lyrafmt -`, and reports whether that
// produced anything usable. A non-zero exit is lyrafmt declining (a syntax error, most
// often, which is the normal state of a buffer being typed into) and is not an error here.
//
// The timeout is a guard on the *editor*, not on lyrafmt: formatting runs on a keystroke
// path, and a hung child would freeze the buffer with no way for the user to tell why.
func runLyrafmt(formatter, source string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, formatter, "-")
	cmd.Stdin = strings.NewReader(source)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", false
	}
	// lyrafmt reports a refusal on stdout and exits non-zero, so a zero exit with no
	// output would mean an empty buffer — which the caller already ruled out.
	if out.Len() == 0 {
		return "", false
	}
	return out.String(), true
}

// A tool the server runs, found once: `$<env>`, then `<name>` on `PATH`, then beside this
// executable (toolBeside). Cached, since nothing in a running server changes the answer
// and formatting is on a keystroke path.
type toolLookup struct {
	env, name string
	mu        sync.Mutex
	looked    bool
	found     string
}

var (
	lyrafmtTool = &toolLookup{env: "LYRA_FMT", name: "lyrafmt"}
	// lui generates `.lui` markup for its diagnostics (markup.go).
	luiTool = &toolLookup{env: "LYRA_LUI", name: "lui"}
)

// reset clears the cached path so a test can point the variable elsewhere.
func (t *toolLookup) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.looked, t.found = false, ""
}

func (t *toolLookup) path() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.looked {
		return t.found, t.found != ""
	}
	t.looked = true
	if env := os.Getenv(t.env); env != "" {
		if _, err := os.Stat(env); err == nil {
			t.found = env
			return t.found, true
		}
	}
	if found, err := exec.LookPath(t.name); err == nil {
		t.found = found
		return t.found, true
	}
	if self, err := os.Executable(); err == nil {
		if beside, ok := toolBeside(self, t.name); ok {
			t.found = beside
		}
	}
	return t.found, t.found != ""
}

// resetLyrafmtLookup clears the cached path so a test can point LYRA_FMT elsewhere.
func resetLyrafmtLookup() { lyrafmtTool.reset() }

func lyrafmtPath() (string, bool) { return lyrafmtTool.path() }

// lyrafmtBeside is toolBeside for the formatter.
func lyrafmtBeside(exe string) (string, bool) { return toolBeside(exe, "lyrafmt") }

// toolBeside looks for a tool (lyrafmt, lui) in the directory holding exe, **with
// symlinks resolved first**.
//
// `os.Executable` does not resolve them consistently — on Linux it reads /proc/self/exe,
// already resolved, and on macOS it can hand back the symlink's own path — and the language
// server is normally reached through exactly such a symlink: the extensions' own advice is
// to put `ln -s …/build/lyra-lsp ~/.local/bin/lyra-lsp` on PATH. Looking beside the *link*
// finds nothing, so formatting silently did nothing in Zed while working from the build
// directory, which is the platform split `modules.StdRoot` carries the same guard for.
//
// The unresolved directory is tried too, for the layout where someone copies both binaries
// somewhere together: `EvalSymlinks` on a real file answers itself, so the two collapse.
func toolBeside(exe, name string) (string, bool) {
	dirs := []string{filepath.Dir(exe)}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		if dir := filepath.Dir(resolved); dir != dirs[0] {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}
