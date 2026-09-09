package abi_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/abi"
)

// **The test that lets struct-by-value exist.** todo.md refused the feature until there
// was "a real classifier for both ABIs, validated case-by-case against clang", and this is
// that validation: for every shape and every target, compile a C declaration with clang,
// read the signature it lowered to, and demand this package say the same thing.
//
// It is a differential test rather than a table of expected strings on purpose. A table
// records what clang did on the day someone looked; this asks clang, so it keeps holding
// when a clang release changes its mind — which for an ABI it may legitimately do, and
// which a table would report as this package being correct.
//
// The shapes are chosen to cover both classifiers' branches and, deliberately, every
// aggregate raylib passes by value: Vector2/3/4, Color, Rectangle, Camera2D, Texture2D.

type shape struct {
	name   string
	cBody  string
	agg    abi.Aggregate
	reason string // what branch this shape is here to exercise
}

func i(off, bytes int) abi.Leaf { return abi.Leaf{Offset: off, Kind: abi.Integer, Bytes: bytes} }
func f(off int) abi.Leaf        { return abi.Leaf{Offset: off, Kind: abi.Float, Bytes: 4} }
func d(off int) abi.Leaf        { return abi.Leaf{Offset: off, Kind: abi.Double, Bytes: 8} }

func shapes() []shape {
	return []shape{
		{"i32_i32", "int32_t a; int32_t b;",
			abi.Aggregate{Size: 8, Align: 4, Leaves: []abi.Leaf{i(0, 4), i(4, 4)}},
			"one INTEGER eightbyte; aarch64 coalesces to i64"},
		{"i32x4", "int32_t a,b,c,d;",
			abi.Aggregate{Size: 16, Align: 4, Leaves: []abi.Leaf{i(0, 4), i(4, 4), i(8, 4), i(12, 4)}},
			"two INTEGER eightbytes — SysV splits the arity, aarch64 does not"},
		{"i32_double", "int32_t a; double b;",
			abi.Aggregate{Size: 16, Align: 8, Leaves: []abi.Leaf{i(0, 4), d(8)}},
			"mixed INTEGER+SSE, and the i32 partial eightbyte"},
		{"f32_f32", "float a,b;",
			abi.Aggregate{Size: 8, Align: 4, Leaves: []abi.Leaf{f(0), f(4)}},
			"raylib Vector2 — HFA-2 on aarch64, <2 x float> on SysV"},
		{"f32x3", "float a,b,c;",
			abi.Aggregate{Size: 12, Align: 4, Leaves: []abi.Leaf{f(0), f(4), f(8)}},
			"raylib Vector3 — HFA-3; SysV splits <2 x float> + float"},
		{"f32x4", "float a,b,c,d;",
			abi.Aggregate{Size: 16, Align: 4, Leaves: []abi.Leaf{f(0), f(4), f(8), f(12)}},
			"raylib Vector4/Rectangle — HFA-4, the largest one"},
		{"f32x5", "float a,b,c,d,e;",
			abi.Aggregate{Size: 20, Align: 4, Leaves: []abi.Leaf{f(0), f(4), f(8), f(12), f(16)}},
			"one past the HFA limit — memory on both"},
		{"f32x6", "float a,b,c,d,e,g;",
			abi.Aggregate{Size: 24, Align: 4, Leaves: []abi.Leaf{f(0), f(4), f(8), f(12), f(16), f(20)}},
			"raylib Camera2D — six floats, so memory despite being all-float"},
		{"u8x4", "uint8_t a,b,c,d;",
			abi.Aggregate{Size: 4, Align: 1, Leaves: []abi.Leaf{i(0, 1), i(1, 1), i(2, 1), i(3, 1)}},
			"raylib Color — the return/parameter width asymmetry on aarch64"},
		{"u8_u8", "uint8_t a,b;",
			abi.Aggregate{Size: 2, Align: 1, Leaves: []abi.Leaf{i(0, 1), i(1, 1)}},
			"the narrowest aggregate — i16 return, i64 parameter"},
		{"i32x5", "int32_t a,b,c,d,e;",
			abi.Aggregate{Size: 20, Align: 4, Leaves: []abi.Leaf{i(0, 4), i(4, 4), i(8, 4), i(12, 4), i(16, 4)}},
			"raylib Texture2D — over 16 bytes, so memory"},
		{"ptr_i32x4", "void *p; int32_t a,b,c,d;",
			abi.Aggregate{Size: 24, Align: 8, Leaves: []abi.Leaf{i(0, 8), i(8, 4), i(12, 4), i(16, 4), i(20, 4)}},
			"raylib Image — a pointer is an INTEGER leaf"},
		{"f64_f64", "double a,b;",
			abi.Aggregate{Size: 16, Align: 8, Leaves: []abi.Leaf{d(0), d(8)}},
			"HFA of doubles"},
		{"f64x3", "double a,b,c;",
			abi.Aggregate{Size: 24, Align: 8, Leaves: []abi.Leaf{d(0), d(8), d(16)}},
			"24 bytes and still an HFA on aarch64 — memory on SysV. The single sharpest disagreement between the two."},
		{"i64_i64", "int64_t a,b;",
			abi.Aggregate{Size: 16, Align: 8, Leaves: []abi.Leaf{i(0, 8), i(8, 8)}},
			"two full INTEGER eightbytes"},
		{"single_f32", "float a;",
			abi.Aggregate{Size: 4, Align: 4, Leaves: []abi.Leaf{f(0)}},
			"a one-member HFA, which surprises but is what clang emits"},
		{"single_i32", "int32_t a;",
			abi.Aggregate{Size: 4, Align: 4, Leaves: []abi.Leaf{i(0, 4)}},
			"the smallest INTEGER case"},
		{"f32_i32", "float a; int32_t b;",
			abi.Aggregate{Size: 8, Align: 4, Leaves: []abi.Leaf{f(0), i(4, 4)}},
			"one float and one int in one eightbyte — INTEGER, not SSE"},
		{"nested_v2x2", "struct { float x, y; } a; struct { float x, y; } b;",
			abi.Aggregate{Size: 16, Align: 4, Leaves: []abi.Leaf{f(0), f(4), f(8), f(12)}},
			"an HFA is homogeneous through nesting — two Vector2s are one HFA-4"},
	}
}

var targets = []struct {
	target abi.Target
	triple string
}{
	{abi.AArch64, "arm64-apple-macosx14.0.0"},
	{abi.AArch64, "aarch64-unknown-linux-gnu"},
	{abi.X86_64SysV, "x86_64-unknown-linux-gnu"},
}

func TestClassifyAgreesWithClang(t *testing.T) {
	clang := lookClang(t)
	for _, tc := range targets {
		t.Run(tc.triple, func(t *testing.T) {
			got := clangLowering(t, clang, tc.triple)
			for _, s := range shapes() {
				t.Run(s.name, func(t *testing.T) {
					wantParam, ok := got["take_"+s.name]
					if !ok {
						t.Fatalf("clang emitted no declaration for take_%s", s.name)
					}
					wantRet, ok := got["give_"+s.name]
					if !ok {
						t.Fatalf("clang emitted no declaration for give_%s", s.name)
					}
					if p := renderParam(abi.Classify(tc.target, s.agg, false)); p != wantParam {
						t.Errorf("parameter: clang says %q, abi says %q\n  (%s)", wantParam, p, s.reason)
					}
					if r := renderReturn(s.name, abi.Classify(tc.target, s.agg, true)); r != wantRet {
						t.Errorf("return: clang says %q, abi says %q\n  (%s)", wantRet, r, s.reason)
					}
				})
			}
		})
	}
}

// renderParam renders a Class the way the parameter list of a clang-emitted `declare`
// reads, so the two can be compared as text.
func renderParam(c abi.Class) string {
	if c.Indirect {
		return "ptr"
	}
	out := make([]string, len(c.Parts))
	for i, p := range c.Parts {
		out[i] = renderPart(p)
	}
	return strings.Join(out, ", ")
}

// renderReturn renders the *return* half. An indirect return is `sret`, which clang emits
// as a void function with a leading pointer; a multi-part one is a literal struct; and an
// AAPCS64 HFA return is the named struct type itself.
func renderReturn(name string, c abi.Class) string {
	if c.Indirect {
		return "sret"
	}
	if c.ReturnAsStruct {
		if len(c.Parts) == 1 {
			return "%struct.S_" + name
		}
		out := make([]string, len(c.Parts))
		for i, p := range c.Parts {
			out[i] = renderPart(p)
		}
		return "{ " + strings.Join(out, ", ") + " }"
	}
	return renderPart(c.Parts[0])
}

func renderPart(p abi.Part) string {
	switch p.Kind {
	case abi.PartInt:
		return fmt.Sprintf("i%d", p.Bits)
	case abi.PartFloat:
		return "float"
	case abi.PartDouble:
		return "double"
	case abi.PartFloatArray:
		return fmt.Sprintf("[%d x float]", p.Count)
	case abi.PartDoubleArray:
		return fmt.Sprintf("[%d x double]", p.Count)
	case abi.PartIntArray:
		return fmt.Sprintf("[%d x i64]", p.Count)
	case abi.PartFloatVec2:
		return "<2 x float>"
	}
	return "?"
}

// declHeadRe finds `declare <ret> @<name>(` — the parameter list is then scanned with a
// paren counter rather than a regex, because `byval(%struct.X)` and `sret(%struct.X)`
// contain the very character a `[^)]*` class would stop at. That mis-parse is what made
// the first run of this test blame the classifier for the test's own regex.
var declHeadRe = regexp.MustCompile(`(?m)^declare\s+(.+?)\s+@(\w+)\(`)

// attrRe strips the parameter attributes that are not part of the ABI *type*.
// `alignstack(8)` is one of them, and it is the difference between macOS and Linux on
// aarch64: an HFA parameter carries it on Linux and not on macOS, while the type — which
// is what this package decides — is `[N x float]` on both.
var attrRe = regexp.MustCompile(`\s*\b(noundef|dead_on_unwind|writable|signext|zeroext|nonnull|readonly|align\s+\d+|alignstack\(\d+\)|byval\([^)]*\)|sret\([^)]*\))`)

// paramList returns the text between the parens starting at `open`, honouring nesting.
func paramList(ir string, open int) (string, bool) {
	depth := 0
	for i := open; i < len(ir); i++ {
		switch ir[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return ir[open+1 : i], true
			}
		case '\n':
			return "", false
		}
	}
	return "", false
}

// clangLowering compiles the shapes for one triple and returns, per function, the text of
// the piece being compared: the parameter list for `take_*`, the return type for `give_*`.
func clangLowering(t *testing.T, clang, triple string) map[string]string {
	t.Helper()
	var src strings.Builder
	src.WriteString("#include <stdint.h>\n")
	for _, s := range shapes() {
		fmt.Fprintf(&src, "typedef struct { %s } S_%s;\n", s.cBody, s.name)
		fmt.Fprintf(&src, "void take_%s(S_%s v);\nS_%s give_%s(void);\n", s.name, s.name, s.name, s.name)
		fmt.Fprintf(&src, "void use_%s(void) { take_%s(give_%s()); }\n", s.name, s.name, s.name)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "shapes.c")
	if err := os.WriteFile(path, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(clang, "-target", triple, "-S", "-emit-llvm", "-O0", "-o", "-", path).CombinedOutput()
	if err != nil {
		t.Skipf("clang cannot target %s here: %v", triple, err)
	}
	ir := string(out)
	res := map[string]string{}
	for _, m := range declHeadRe.FindAllStringSubmatchIndex(ir, -1) {
		ret := ir[m[2]:m[3]]
		name := ir[m[4]:m[5]]
		raw, ok := paramList(ir, m[1]-1)
		if !ok {
			continue
		}
		params := strings.TrimSpace(attrRe.ReplaceAllString(raw, ""))
		switch {
		case strings.HasPrefix(name, "take_"):
			res[name] = params
		case strings.HasPrefix(name, "give_"):
			// An `sret` return is a void function whose first parameter is the buffer
			// the caller supplies, so the *return type* in the IR is `void`.
			if strings.Contains(raw, "sret(") {
				res[name] = "sret"
			} else {
				res[name] = ret
			}
		}
	}
	return res
}

func lookClang(t *testing.T) string {
	t.Helper()
	for _, c := range []string{"clang", "clang-15"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	t.Skip("no clang on PATH; the ABI classifier cannot be validated here")
	return ""
}
