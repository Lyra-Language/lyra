package diagnostic

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// **What nothing was checking about codes.go.**
//
// It is 800-odd lines of constants whose doc comments are, for most of these rules, the only
// written record of why the rule exists. Three things about that list can go wrong quietly:
//
//   - **Two constants can share a code.** Nothing would notice, and the effect is that two
//     unrelated rules become one as far as anybody filtering, suppressing or searching by
//     code is concerned.
//   - **The numbering can drift out of order.** It had: E045–E048 sat between E023 and E024,
//     the W014–W020 run came before W001, and E051–E054 were scattered through the fifties.
//     Someone looking for the next free number has to scan the file, and the way that goes
//     wrong is reuse.
//   - **A code can stop being used without being retired.** That is the one with a real
//     policy behind it, recorded on lyra-E051: a retired code is *reserved*, never reassigned,
//     because a diagnostic code is a thing people search for and pointing it at a later
//     feature makes every older hit describe the wrong thing.
//
// There is deliberately **no severity here**. A code names a rule, not how loudly it is
// reported: lyra-E009 is an error for a `bool` or `data` scrutinee and a warning for the open
// types, decided at the reporting site.

var codePattern = regexp.MustCompile(`^lyra-([EW])(\d{3})$`)

// retiredCodes are defined but deliberately unreferenced: the rule is gone and the number is
// reserved so it is never pointed at something else. Each entry is a claim, and the test
// checks it both ways — an unused code missing from here fails, and an entry here that *is*
// used fails as stale.
var retiredCodes = map[string]string{
	"lyra-E030": "the trait-borrow restriction was lifted when ownership learned about method bodies",
	"lyra-E051": "raw pointers landed 08/18; the operations report as lyra-E059/E060/E061 now",
}

// missingCodes are numbers with no constant, each because the rule was promoted or removed.
// Listed so the gap reads as deliberate rather than as the next number free to take.
var missingCodes = map[string]string{
	"lyra-W007": "the `??`-on-non-optional warning became the hard error lyra-E049 on 08/13",
}

type codeConst struct {
	name, code, letter string
	num                int
}

func TestCodes_AreUniqueWellFormedAndOrdered(t *testing.T) {
	consts := parseCodes(t)
	if len(consts) < 50 {
		t.Fatalf("only found %d codes — the parser is not seeing codes.go", len(consts))
	}

	byCode := map[string]string{}
	byName := map[string]bool{}
	for _, c := range consts {
		if !codePattern.MatchString(c.code) {
			t.Errorf("%s = %q is not of the form lyra-E### or lyra-W###", c.name, c.code)
		}
		if prev, dup := byCode[c.code]; dup {
			t.Errorf("%s and %s share the code %q — two rules become one to anyone "+
				"filtering, suppressing or searching by code", prev, c.name, c.code)
		}
		byCode[c.code] = c.name
		if byName[c.name] {
			t.Errorf("%s is declared twice", c.name)
		}
		byName[c.name] = true
	}

	// Numeric order as written, errors before warnings.
	for i := 1; i < len(consts); i++ {
		prev, cur := consts[i-1], consts[i]
		if prev.letter == cur.letter && prev.num >= cur.num {
			t.Errorf("%s (%s) follows %s (%s): codes are kept in numeric order so the next "+
				"free number is the last one, not the result of scanning the file",
				cur.name, cur.code, prev.name, prev.code)
		}
		if prev.letter == "W" && cur.letter == "E" {
			t.Errorf("%s (%s) follows a warning: every lyra-E… comes before every lyra-W…",
				cur.name, cur.code)
		}
	}

	// A gap must be a declared one.
	for _, letter := range []string{"E", "W"} {
		var nums []int
		for _, c := range consts {
			if c.letter == letter {
				nums = append(nums, c.num)
			}
		}
		sort.Ints(nums)
		have := map[int]bool{}
		for _, n := range nums {
			have[n] = true
		}
		for n := 1; n <= nums[len(nums)-1]; n++ {
			code := fmt.Sprintf("lyra-%s%03d", letter, n)
			if !have[n] && missingCodes[code] == "" {
				t.Errorf("%s has no constant and is not listed in missingCodes — a gap has to "+
					"say why, or it reads as the next number free to take", code)
			}
		}
	}
	for code := range missingCodes {
		if _, defined := byCode[code]; defined {
			t.Errorf("%s is listed in missingCodes but is defined — the entry is stale", code)
		}
	}
}

// TestCodes_UnusedAreDeclaredRetired holds the reserved-code policy that lyra-E051's own
// comment states: a code whose rule is gone keeps its number rather than being reassigned,
// because people search by code. A code that quietly stops being reported is the shape that
// policy exists to catch, so it has to be declared here to stop being used.
func TestCodes_UnusedAreDeclaredRetired(t *testing.T) {
	consts := parseCodes(t)
	used := referencedCodeConsts(t)

	for _, c := range consts {
		_, retired := retiredCodes[c.code]
		switch {
		case used[c.name] && retired:
			t.Errorf("%s (%s) is listed in retiredCodes but is still reported — remove the "+
				"entry, or stop reporting it", c.name, c.code)
		case !used[c.name] && !retired:
			t.Errorf("%s (%s) is defined but never reported. If its rule is gone, add it to "+
				"retiredCodes with the reason and leave the number reserved; if it is new, "+
				"wire it up before landing it", c.name, c.code)
		}
	}
}

// parseCodes reads the constant block out of codes.go in source order.
func parseCodes(t *testing.T) []codeConst {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "codes.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing codes.go: %v", err)
	}
	var out []codeConst
	ast.Inspect(parsed, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		code, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		m := codePattern.FindStringSubmatch(code)
		if m == nil {
			return true
		}
		num, _ := strconv.Atoi(m[2])
		out = append(out, codeConst{name: vs.Names[0].Name, code: code, letter: m[1], num: num})
		return true
	})
	return out
}

// referencedCodeConsts is every Code* constant named anywhere outside this package — which
// is where a code is actually attached to a diagnostic.
func referencedCodeConsts(t *testing.T) map[string]bool {
	t.Helper()
	ref := regexp.MustCompile(`\b(?:diag|diagnostic)\.(Code[A-Za-z0-9]+)\b`)
	used := map[string]bool{}
	for _, root := range []string{"../../pkg", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range ref.FindAllStringSubmatch(string(src), -1) {
				used[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	if len(used) == 0 {
		t.Fatal("found no code references at all — the walk is not reaching the source")
	}
	return used
}
