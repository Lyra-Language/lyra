package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/go-lsp/lsp"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// `.lui` markup (lyra/tools/lui) is diagnosed here, at the markup's own lines.
//
// The markup is not Lyra, but what it says is: lui generates it into a module whose every
// element's line ends with the markup line it came from (`// view.lui:12`). So the buffer is
// put through **lui itself** — `lui --stdin`, the generator's one implementation, never a
// copy here — and the module it answers is analyzed as an overlay at the path its `.lyra`
// would have, beside the markup, so its imports resolve as the real file's do. Each
// diagnostic on that module is then moved to the markup line its nearest marker names.
//
// - **A mistake in the markup itself** (an unknown tag, a missing `on_click`) is lui's, at
//   its line: lui answers `N: what` and exits 1.
// - **lui is found as lyrafmt is**: `$LYRA_LUI`, `lui` on `PATH`, beside this executable.
//   Without it there is nothing to say, and nothing is said — the same rule as formatting.
// - **The marker format has two readers**, `lui map` (in Lyra, over a file) and
//   markupLineOf here (over the module in memory); TestMarkup_MapsAsLuiMapDoes holds them to
//   the same answers.
// - Only diagnostics: hover, completion and the rest answer nothing on a `.lui` document.

// isMarkup reports whether path is a `.lui` document.
func isMarkup(path string) bool { return strings.HasSuffix(path, ".lui") }

// analyzeMarkup answers the diagnostics of the markup document at path, whose text is
// source.
func (h *Handler) analyzeMarkup(uri lsp.DocumentURI, path, source string) []lsp.Diagnostic {
	lui, ok := luiTool.path()
	if !ok {
		return []lsp.Diagnostic{}
	}
	name := filepath.Base(path)
	// The whole path, not the name: lui reads the files an `<import src="…"/>` names
	// beside it, and takes the name for its markers itself.
	code, line, problem, ok := runLui(lui, path, source)
	if !ok {
		return []lsp.Diagnostic{}
	}
	if problem != "" {
		return []lsp.Diagnostic{markupDiagnostic(source, line, lsp.SeverityError, "", problem)}
	}

	module := strings.TrimSuffix(path, ".lui") + ".lyra"
	opts := modules.DefaultOptions()
	overlay := h.overlay(uri, source)
	overlay[module] = []byte(code)
	opts.Overlay = overlay
	opts.ParseCache = h.parseCache
	units, resolved := modules.Resolve(module, modules.DefaultRoots(module), opts)
	var res *driver.Result
	if len(units) == 0 {
		res = driver.Analyze([]byte(code))
	} else {
		res = driver.AnalyzeUnitsCached(units, h.collectCache)
	}
	all := append(resolved, res.Diagnostics...)

	rows := strings.Split(code, "\n")
	out := []lsp.Diagnostic{}
	seen := map[string]bool{}
	for _, d := range diagnosticsFor(all, module) {
		at := 1
		if n, ok := markupLineOf(rows, name, d.Location.StartLine); ok {
			at = n
		}
		sev := lsp.SeverityError
		switch d.Severity {
		case diag.SeverityWarning:
			sev = lsp.SeverityWarning
		case diag.SeverityInfo:
			sev = lsp.SeverityInformation
		}
		// One mistake is often reported more than once on the generated line; on the
		// markup's it is one.
		key := strconv.Itoa(at) + "\x00" + d.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, markupDiagnostic(source, at, sev, d.Code, d.Message))
	}
	return out
}

// markupDiagnostic is a diagnostic over line (1-based) of the markup, its text without
// the indentation: the generated module's column means nothing here.
func markupDiagnostic(source string, line int, sev lsp.DiagnosticSeverity, code, message string) lsp.Diagnostic {
	lines := strings.Split(source, "\n")
	i := line - 1
	if i < 0 || i >= len(lines) {
		i = 0
	}
	text := lines[i]
	start := len(text) - len(strings.TrimLeft(text, " \t"))
	return lsp.Diagnostic{
		Range: lsp.Range{
			Start: lsp.Position{Line: i, Character: utf16Len16(text[:start])},
			End:   lsp.Position{Line: i, Character: utf16Len16(strings.TrimRight(text, " \t\r"))},
		},
		Severity: &sev,
		Code:     codeToLSP(code),
		Source:   "lyra",
		Message:  message,
	}
}

// runLui generates source (the markup file at path) with `lui --stdin`: the module, or the
// markup's own mistake as its line and message. ok is false when lui could not be run or
// answered in no shape this understands — no diagnostics then, as with lyrafmt.
func runLui(lui, path, source string) (code string, line int, problem string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, lui, "--stdin", "--name", path)
	cmd.Stdin = strings.NewReader(source)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0, "", true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return "", 0, "", false
	}
	text := strings.TrimSpace(out.String())
	at, rest, found := strings.Cut(text, ": ")
	n, convErr := strconv.Atoi(at)
	if !found || convErr != nil {
		return "", 0, "", false
	}
	return "", n, rest, true
}

// markupLineOf is the markup line that line row (1-based) of a generated module came from:
// the nearest marker at or above it — `// view.lui:N` ending a line (that line), a
// component's `/// From view.lui, line N.`, or a marker alone on its line (the next line
// is N: a script). lui's `markup_line` reads the same markers; keep them in step.
func markupLineOf(rows []string, name string, row int) (int, bool) {
	k := row - 1
	if k >= len(rows) {
		k = len(rows) - 1
	}
	for ; k > 0; k-- {
		r := rows[k]
		if n, ok := numberAfter(r, "/// From "+name+", line "); ok {
			return n, true
		}
		if n, ok := numberAfter(r, "// "+name+":"); ok {
			if strings.HasPrefix(strings.TrimSpace(r), "// ") {
				return n + (row - 1) - (k + 1), true
			}
			return n, true
		}
	}
	return 0, false
}

// numberAfter is the number right after prefix in s, if s holds one there.
func numberAfter(s, prefix string) (int, bool) {
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0, false
	}
	rest := s[i+len(prefix):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(rest[:end])
	return n, err == nil
}
