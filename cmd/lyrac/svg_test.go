package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// std.svg reads a document into its tree: each element it knows with its attributes
// parsed, and one it does not know kept by name, so the rest still reads. Path data is
// kept up to its first error, as SVG draws it (moveto, lineto, H, V and Z so far). resvg's
// shapes/rect/simple-case.svg (from tools/resvg-tests.sh) is the first real file; the others
// are written here, one rule each.
func TestRun_SvgBuildsTheTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()

	var docs []string
	var want []string
	add := func(name, doc string, tree ...string) {
		path := filepath.Join(dir, name+".svg")
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		docs = append(docs, path)
		want = append(want, "== "+name)
		want = append(want, tree...)
	}

	simple := filepath.Join(root, "cmd/lyrac/testdata/svg/resvg/tests/shapes/rect/simple-case.svg")
	if _, err := os.Stat(simple); err == nil {
		docs = append(docs, simple)
		want = append(want,
			"== simple-case",
			"svg id=svg1 x=- y=- width=- height=- viewBox=0 0 200 200 par=-",
			`  title "Simple case"`,
			"  path id=path1 fill=red stroke=- d=M 20 20 L 20 180 L 180 180 L 180 20",
			"  rect id=rect1 x=20 y=20 width=160 height=160 rx=- ry=- fill=green stroke=-",
			"  rect id=frame x=1 y=1 width=198 height=198 rx=- ry=- fill=none stroke=black",
		)
	} else {
		t.Log("resvg's tests are not fetched (tools/resvg-tests.sh); simple-case.svg skipped")
	}

	add("unknown", `<svg><desc>d</desc><g><rect width="1"/></g><circle id="c" r="5"/></svg>`,
		"svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  other desc",
		"  other g",
		"  circle id=c cx=- cy=- r=5 fill=- stroke=-",
	)
	add("lengths", `<svg width="50%" height="2.5cm" x="3px"><rect x="1e1" y="+2" width="10q" height="1em"/></svg>`,
		"svg id=- x=3px y=- width=50% height=2.5cm viewBox=- par=-",
		"  rect id=- x=10 y=2 width=- height=1em rx=- ry=- fill=- stroke=-",
	)
	add("paths", `<svg>
<path d="M10 20L30 40"/>
<path d="M 10,20 30,40"/>
<path d="M1-2-3-4"/>
<path d="M 0 0 H 10 Z"/>
<path d="M 0 0 H 10 V 10 h -5 v -5 z"/>
<path d="M 0 0 H 1 2,3 L 4 4"/>
<path d="M 0 0 Z 5"/>
<path d="M 0 0 Zm 1 1 z"/>
<path d="M 0 0 H"/>
<path d="M 0 0 V 1,"/>
<path d="M 0 0 L 10"/>
<path d="M 0 0 L 1 2 3"/>
<path d="M 0 0 1,,2"/>
<path d="M 0 0, L 1 1"/>
<path d="L 5 5"/>
<path d="m 1 1 l -1 -1"/>
<path/>
</svg>`,
		"svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  path id=- fill=- stroke=- d=M 10 20 L 30 40",
		"  path id=- fill=- stroke=- d=M 10 20 30 40",
		"  path id=- fill=- stroke=- d=M 1 -2 -3 -4",
		"  path id=- fill=- stroke=- d=M 0 0 H 10 Z",
		"  path id=- fill=- stroke=- d=M 0 0 H 10 V 10 h -5 v -5 Z",
		"  path id=- fill=- stroke=- d=M 0 0 H 1 H 2 H 3 L 4 4",
		"  path id=- fill=- stroke=- d=M 0 0 Z",
		"  path id=- fill=- stroke=- d=M 0 0 Z m 1 1 Z",
		"  path id=- fill=- stroke=- d=M 0 0",
		"  path id=- fill=- stroke=- d=M 0 0 V 1",
		"  path id=- fill=- stroke=- d=M 0 0",
		"  path id=- fill=- stroke=- d=M 0 0 L 1 2",
		"  path id=- fill=- stroke=- d=M 0 0",
		"  path id=- fill=- stroke=- d=M 0 0",
		"  path id=- fill=- stroke=- d=",
		"  path id=- fill=- stroke=- d=m 1 1 l -1 -1",
		"  path id=- fill=- stroke=- d=-",
	)
	add("aspect", `<svg><svg preserveAspectRatio="  xMinYMax
  slice "/><svg preserveAspectRatio="defer xMidYMid"/><svg preserveAspectRatio="xMidYMid bogus"/><svg preserveAspectRatio="none meet extra"/></svg>`,
		"svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  svg id=- x=- y=- width=- height=- viewBox=- par=xMinYMax slice",
		"  svg id=- x=- y=- width=- height=- viewBox=- par=xMidYMid",
		"  svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  svg id=- x=- y=- width=- height=- viewBox=- par=-",
	)
	add("viewbox", `<svg viewBox="0,0 , 10 -5"><svg viewBox="1 2 3"/><svg viewBox=".5.5 1e1 2"/></svg>`,
		"svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  svg id=- x=- y=- width=- height=- viewBox=- par=-",
		"  svg id=- x=- y=- width=- height=- viewBox=0.5 0.5 10 2 par=-",
	)
	add("not-svg", `<html/>`, "error: The root element is <html>, not <svg>.")
	add("not-xml", `<svg>`, "error: line 1: <svg> is never closed")

	var calls []string
	for _, f := range docs {
		name := strings.TrimSuffix(filepath.Base(f), ".svg")
		calls = append(calls, `  show("`+name+`", "`+f+`")`)
	}
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(`import std.svg.{ parse_svg_file, SvgElement, Svg, Title, Rect, Circle, Path, Other }
import std.svg.{ CssLengthPercentage, CssLPLength, CssLPPercentage, CssLength }
import std.svg.{ Num, Px, Cm, Mm, In, Pt, Em, Rem, Ex, Pc, CssColor, ViewBox }
import std.svg.{ PreserveAspectRatio, AspectRatioAlign, MeetOrSlice, Meet, Slice }
import std.svg.{ AlignNone, XMinYMin, XMidYMin, XMaxYMin, XMinYMid, XMidYMid, XMaxYMid }
import std.svg.{ XMinYMax, XMidYMax, XMaxYMax }
import std.svg.{ PathData, PathSegment, MoveToAbs, MoveToRel, LineToAbs, LineToRel, CoordinatePair }
import std.svg.{ LineToHorizAbs, LineToHorizRel, LineToVertAbs, LineToVertRel, ClosePath }

let main = () -> void => {
`+strings.Join(calls, "\n")+`
}

let show = (name: string, path: string) -> void => {
  println("== ${name}")
  match parse_svg_file(path) {
    Err(why) => println("error: ${why}"),
    Ok(e) => print_tree(e, ""),
  }
}

let print_tree = (e: SvgElement, indent: string) -> void => match e {
  Svg { id: id, x: x, y: y, width: w, height: h, viewBox: vb, preserveAspectRatio: par, children: cs } => {
    println("${indent}svg id=${text(id)} x=${length(x)} y=${length(y)} width=${length(w)} height=${length(h)} viewBox=${view_box(vb)} par=${aspect(par)}")
    for c in cs { print_tree(c, indent ++ "  ") }
  },
  Title { text: t } => println("${indent}title \"${text(t)}\""),
  Rect { id: id, x: x, y: y, width: w, height: h, rx: rx, ry: ry, fill: f, stroke: s } =>
    println("${indent}rect id=${text(id)} x=${length(x)} y=${length(y)} width=${length(w)} height=${length(h)} rx=${length(rx)} ry=${length(ry)} fill=${color(f)} stroke=${color(s)}"),
  Circle { id: id, cx: cx, cy: cy, r: r, fill: f, stroke: s } =>
    println("${indent}circle id=${text(id)} cx=${length(cx)} cy=${length(cy)} r=${length(r)} fill=${color(f)} stroke=${color(s)}"),
  Path { id: id, d: d, fill: f, stroke: s } =>
    println("${indent}path id=${text(id)} fill=${color(f)} stroke=${color(s)} d=${path_data(d)}"),
  Other(name) => println("${indent}other ${name}"),
}

let text = pure (m: Maybe<string>) -> string => m.unwrap_or("-")

let color = pure (m: Maybe<CssColor>) -> string => match m {
  Some(c) => base(c),
  None => "-",
}

let length = pure (m: Maybe<CssLengthPercentage>) -> string => match m {
  Some(CssLPPercentage(p)) => "${base(p)}%",
  Some(CssLPLength(l)) => match l {
    Num(n) => "${base(n)}",
    Px(n) => "${n}px",
    Cm(n) => "${n}cm",
    Mm(n) => "${n}mm",
    In(n) => "${n}in",
    Pt(n) => "${n}pt",
    Em(n) => "${n}em",
    Rem(n) => "${n}rem",
    Ex(n) => "${n}ex",
    Pc(n) => "${n}pc",
  },
  None => "-",
}

let view_box = pure (m: Maybe<ViewBox>) -> string => match m {
  Some(v) => "${v.min_x} ${v.min_y} ${v.width} ${v.height}",
  None => "-",
}

let aspect = pure (p: PreserveAspectRatio) -> string => {
  let (align, meet_or_slice) = p
  let a = match align {
    Some(AlignNone) => "none",
    Some(XMinYMin) => "xMinYMin",
    Some(XMidYMin) => "xMidYMin",
    Some(XMaxYMin) => "xMaxYMin",
    Some(XMinYMid) => "xMinYMid",
    Some(XMidYMid) => "xMidYMid",
    Some(XMaxYMid) => "xMaxYMid",
    Some(XMinYMax) => "xMinYMax",
    Some(XMidYMax) => "xMidYMax",
    Some(XMaxYMax) => "xMaxYMax",
    None => "-",
  }
  match meet_or_slice {
    Some(Meet) => "${a} meet",
    Some(Slice) => "${a} slice",
    None => a,
  }
}

let path_data = pure (m: Maybe<PathData>) -> string => {
  let Some(d) = m else { return "-" }
  var words: []string = []
  for s in base(d) {
    match s {
      MoveToAbs(ps) => words.push("M ${pairs(ps)}"),
      MoveToRel(ps) => words.push("m ${pairs(ps)}"),
      LineToAbs(ps) => words.push("L ${pairs(ps)}"),
      LineToRel(ps) => words.push("l ${pairs(ps)}"),
      LineToHorizAbs { x } => words.push("H ${x}"),
      LineToHorizRel { dx } => words.push("h ${dx}"),
      LineToVertAbs { y } => words.push("V ${y}"),
      LineToVertRel { dy } => words.push("v ${dy}"),
      ClosePath => words.push("Z"),
      _ => words.push("?"),
    }
  }
  words.join(" ")
}

let pairs = pure (ps: []CoordinatePair) -> string =>
  [p in ps | "${p.0} ${p.1}"].join(" ")
`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := captureRun(t, "run", src)
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, stderr)
	}
	got := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		var g, w string
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			t.Errorf("line %d:\n got  %q\n want %q", i+1, g, w)
		}
	}
}
