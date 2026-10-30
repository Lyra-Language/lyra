package driver

import (
	"fmt"
	"sort"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// syntaxReport is one unit's syntax errors and the top-level items they sit in, whose
// later diagnostics describe tree-sitter's recovery rather than the program.
type syntaxReport struct {
	diags  []diag.Diagnostic
	broken []ast.Location
}

// collectParseErrors walks the tree-sitter CST for ERROR and MISSING nodes and
// returns them as diagnostics. Tree-sitter embeds parse errors as named nodes
// rather than failing the parse, so this is the only way to surface them.
// Positions are converted from tree-sitter's 0-based rows/columns to Lyra's
// 1-based ast.Location convention so downstream mapping treats them like any
// other diagnostic.
//
// **An unbalanced delimiter is reported as itself.** Tree-sitter's ERROR and MISSING
// nodes say where recovery happened, not what went wrong: an unclosed `add(1,` came out
// as `unexpected "= add(1, \n  let y"`, and an unclosed `(1 + 2` as `unexpected "2"`
// plus a `missing )` a line later. So the delimiters are matched over the tree's own
// tokens (strings and comments are already single tokens there), and each unclosed
// opener is named at its own position. The ERROR and MISSING nodes reaching past the
// first such opener are that opener's echoes and are dropped; earlier ones stay.
func collectParseErrors(root *sitter.Node, source []byte) syntaxReport {
	var r syntaxReport
	if !root.HasError() {
		return r
	}
	delims := unbalancedDelimiters(root)
	cutoff := ^uint(0)
	if len(delims) > 0 {
		cutoff = delims[0].at.StartByte()
	}
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node.IsMissing() || node.IsError() {
			r.broken = append(r.broken, topLevelSpan(root, node))
			if node.EndByte() > cutoff {
				return
			}
		}
		if node.IsMissing() {
			r.diags = append(r.diags, diag.Diagnostic{
				Severity: diag.SeverityError,
				Location: nodeLocation(node),
				Message:  fmt.Sprintf("missing %s", node.Kind()),
			})
			return
		}
		if node.IsError() {
			text := node.Utf8Text(source)
			msg := "syntax error"
			if len(text) > 0 && len(text) <= 40 {
				msg = fmt.Sprintf("syntax error: unexpected %q", text)
			}
			if hint, ok := uppercaseTypeParamMessage(source, node.StartByte(), node.EndByte()); ok {
				msg = "syntax error: " + hint
			}
			r.diags = append(r.diags, diag.Diagnostic{
				Severity: diag.SeverityError,
				Location: nodeLocation(node),
				Message:  msg,
			})
			return
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			if child := node.Child(i); child != nil && child.HasError() {
				walk(child)
			}
		}
	}
	walk(root)
	for _, d := range delims {
		r.diags = append(r.diags, d.diagnostic())
		r.broken = append(r.broken, topLevelSpan(root, d.at))
	}
	return r
}

// closerOf maps every opening delimiter token the grammar has to the token closing it.
// `${` and its `}` are the scanner's interpolation markers, aliased into the tree.
var closerOf = map[string]string{
	"(": ")", "[": "]", "#[": "]", "?[": "]", "{": "}", ".{": "}", "${": "}",
}

// delimiterProblem is an opener nothing closes (closedBy is the closer that ended its
// region instead, or nil at the end of the file), a closer of the wrong kind for the
// opener it meets (mismatches), or a closer nothing opened.
type delimiterProblem struct {
	at         *sitter.Node
	closedBy   *sitter.Node
	mismatches *sitter.Node
	unclosed   bool
}

func (p delimiterProblem) diagnostic() diag.Diagnostic {
	kind := p.at.Kind()
	d := diag.Diagnostic{Severity: diag.SeverityError, Location: nodeLocation(p.at)}
	switch {
	case p.mismatches != nil:
		open := p.mismatches.Kind()
		at := nodeLocation(p.mismatches)
		d.Message = fmt.Sprintf("syntax error: `%s` cannot close the `%s` at %d:%d; expected `%s`",
			kind, open, at.StartLine, at.StartCol, closerOf[open])
		d.RelatedInformation = []diag.RelatedInformation{{Location: at, Message: fmt.Sprintf("`%s` opened here", open)}}
	case !p.unclosed:
		d.Message = fmt.Sprintf("syntax error: unmatched `%s`: nothing open for it to close", kind)
	case p.closedBy == nil:
		d.Message = fmt.Sprintf("syntax error: unclosed `%s`: expected `%s` before the end of the file",
			kind, closerOf[kind])
	default:
		at := nodeLocation(p.closedBy)
		d.Message = fmt.Sprintf("syntax error: unclosed `%s`: expected `%s` before the `%s` at %d:%d",
			kind, closerOf[kind], p.closedBy.Kind(), at.StartLine, at.StartCol)
		d.RelatedInformation = []diag.RelatedInformation{{
			Location: at,
			Message:  fmt.Sprintf("this `%s` closes an enclosing delimiter first", p.closedBy.Kind()),
		}}
	}
	return d
}

// unbalancedDelimiters matches every delimiter token in the tree, in source order. A
// closer pops back to the nearest opener of its own kind, so the openers above that one
// are the unclosed ones. A closer with no such opener is a typo for the innermost one's
// closer when both are on one line (`[1, 2)`, one mistake reported once), and otherwise
// unmatched (`print(1))`), popping nothing. Of the openers still open at the end of the
// file only the innermost is reported: the ones around it are usually waiting on it (an
// unterminated `${` holds its string, and the string the rest of the file).
// Tokens recovery invented (MISSING) are skipped: they are tree-sitter's guess at the
// fix, and counting them would balance exactly the delimiters this is looking for.
func unbalancedDelimiters(root *sitter.Node) []delimiterProblem {
	var open []*sitter.Node
	var problems []delimiterProblem
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node.ChildCount() > 0 {
			for i := uint(0); i < node.ChildCount(); i++ {
				if child := node.Child(i); child != nil {
					walk(child)
				}
			}
			return
		}
		if node.IsMissing() || node.IsNamed() {
			return
		}
		kind := node.Kind()
		if _, ok := closerOf[kind]; ok {
			open = append(open, node)
			return
		}
		if kind != ")" && kind != "]" && kind != "}" {
			return
		}
		for i := len(open) - 1; i >= 0; i-- {
			if closerOf[open[i].Kind()] == kind {
				for _, o := range open[i+1:] {
					problems = append(problems, delimiterProblem{at: o, closedBy: node, unclosed: true})
				}
				open = open[:i]
				return
			}
		}
		if n := len(open); n > 0 && open[n-1].StartPosition().Row == node.StartPosition().Row {
			problems = append(problems, delimiterProblem{at: node, mismatches: open[n-1]})
			open = open[:n-1]
			return
		}
		problems = append(problems, delimiterProblem{at: node})
	}
	walk(root)
	if n := len(open); n > 0 {
		problems = append(problems, delimiterProblem{at: open[n-1], unclosed: true})
	}
	sort.SliceStable(problems, func(i, j int) bool {
		return problems[i].at.StartByte() < problems[j].at.StartByte()
	})
	return problems
}

// topLevelSpan is the span of the top-level item holding node — the unit a syntax error
// makes unreliable. A file that failed to parse as a whole is one span.
func topLevelSpan(root, node *sitter.Node) ast.Location {
	if root.IsError() {
		return nodeLocation(root)
	}
	at := node.StartByte()
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child != nil && child.StartByte() <= at && at <= child.EndByte() {
			return nodeLocation(child)
		}
	}
	return nodeLocation(node)
}

// holdBackAfterSyntaxErrors drops the diagnostics from diags[from:] (every pass after
// parsing) that a syntax error makes unreliable. In a file that does not parse, every
// warning goes — the file is mid-edit, and an "unused" warning there is usually a use
// that landed in an ERROR node — and so does every error inside a top-level item holding
// a syntax error, which reports the shape recovery guessed (`(1 + 2` then `print(x)` was
// "operands must be numeric, got integer literal and void"). Errors in the file's other
// items, and in other files, stay: they are real.
func holdBackAfterSyntaxErrors(diags []diag.Diagnostic, from int, broken map[string][]ast.Location) []diag.Diagnostic {
	if len(broken) == 0 {
		return diags
	}
	kept := diags[:from]
	for _, d := range diags[from:] {
		file := d.File
		if file == "" {
			file = d.Location.File
		}
		spans, ok := broken[file]
		if ok && (d.Severity == diag.SeverityWarning || startsWithin(d.Location, spans)) {
			continue
		}
		kept = append(kept, d)
	}
	return kept
}

func startsWithin(loc ast.Location, spans []ast.Location) bool {
	if loc.StartLine == 0 {
		return false
	}
	pos := [2]int{loc.StartLine, loc.StartCol}
	for _, s := range spans {
		if !lessPos(pos, [2]int{s.StartLine, s.StartCol}) && !lessPos([2]int{s.EndLine, s.EndCol}, pos) {
			return true
		}
	}
	return false
}

func lessPos(a, b [2]int) bool {
	return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1])
}
